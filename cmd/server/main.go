// Input: 环境变量配置, PostgreSQL, Redis, Docker Engine
// Output: HTTP 服务 (Echo), WebSocket 端点, asynq Worker
// Role: 主服务入口，初始化所有依赖并启动 HTTP 服务器、WebSocket 和部署队列 Worker
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/dokploy/dokploy/internal/auth"
	"github.com/dokploy/dokploy/internal/backup"
	"github.com/dokploy/dokploy/internal/config"
	"github.com/dokploy/dokploy/internal/db"
	"github.com/dokploy/dokploy/internal/db/schema"
	"github.com/dokploy/dokploy/internal/docker"
	"github.com/dokploy/dokploy/internal/handler"
	"github.com/dokploy/dokploy/internal/notify"
	"github.com/dokploy/dokploy/internal/queue"
	"github.com/dokploy/dokploy/internal/scheduler"
	"github.com/dokploy/dokploy/internal/service"
	"github.com/dokploy/dokploy/internal/setup"
	"github.com/dokploy/dokploy/internal/traefik"
	"github.com/dokploy/dokploy/internal/ws"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

func main() {
	cfg := config.Load()

	database, err := db.Connect(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	defer database.Close()

	// Auto-create/update all database tables
	if err := database.AutoMigrate(); err != nil {
		log.Fatalf("failed to migrate database: %v", err)
	}

	// Initialize auth
	a := auth.New(database)

	// Initialize Docker client (single connection, reused everywhere)
	dockerClient, err := docker.NewClient()
	if err != nil {
		log.Printf("Warning: failed to initialize Docker client: %v", err)
	}

	// Run server setup/initialization
	s := setup.New(cfg, dockerClient)
	if err := s.Initialize(); err != nil {
		log.Printf("Warning: server setup incomplete: %v", err)
	}

	// Initialize Traefik config manager
	traefikMgr := traefik.NewManager(cfg.Paths.DynamicTraefikPath)

	// Initialize notifier
	notifier := notify.NewNotifier(database)

	// Initialize service layer
	appSvc := service.NewApplicationService(database, dockerClient, cfg)
	composeSvc := service.NewComposeService(database, dockerClient, cfg)
	dbSvc := service.NewDatabaseService(database, dockerClient, cfg)
	previewSvc := service.NewPreviewService(database, dockerClient, cfg, traefikMgr)

	// Initialize backup service with cron scheduler
	backupSvc := backup.NewService(database, cfg, notifier)
	backupSvc.InitCronJobs()
	defer backupSvc.Stop()

	// Initialize schedule service
	sched := scheduler.New(database, cfg)
	sched.InitSchedules()
	defer sched.Stop()

	// Initialize task queue (optional - requires Redis)
	redisAddr := os.Getenv("REDIS_URL")
	if redisAddr == "" {
		// In production Docker, Redis runs as "dokploy-redis" service
		redisHost := os.Getenv("REDIS_HOST")
		if redisHost == "" {
			redisHost = "dokploy-redis"
		}
		redisAddr = redisHost + ":6379"
	}

	var q *queue.Queue
	var worker *queue.Worker
	if queue.IsRedisAvailable(redisAddr) {
		q = queue.NewQueue(redisAddr)
		defer q.Close()

		worker = queue.NewWorker(redisAddr, 10, queue.TaskHandlers{
			HandleDeployApplication: func(ctx context.Context, payload queue.DeployApplicationPayload) error {
				log.Printf("Deploy application: %s", payload.ApplicationID)
				return appSvc.Deploy(payload.ApplicationID, payload.Title, payload.Description)
			},
			HandleDeployCompose: func(ctx context.Context, payload queue.DeployComposePayload) error {
				log.Printf("Deploy compose: %s", payload.ComposeID)
				return composeSvc.Deploy(payload.ComposeID, payload.Title)
			},
			HandleDeployDatabase: func(ctx context.Context, payload queue.DeployDatabasePayload) error {
				log.Printf("Deploy database: %s (%s)", payload.DatabaseID, payload.Type)
				return deployDatabaseByType(dbSvc, payload.DatabaseID, payload.Type)
			},
			HandleRebuildDatabase: func(ctx context.Context, payload queue.DeployDatabasePayload) error {
				log.Printf("Rebuild database: %s (%s)", payload.DatabaseID, payload.Type)
				return dbSvc.RebuildDatabase(payload.DatabaseID, schema.DatabaseType(payload.Type))
			},
			HandleStopCompose: func(ctx context.Context, payload queue.SimpleIDPayload) error {
				log.Printf("Stop compose: %s", payload.ID)
				return composeSvc.Stop(payload.ID)
			},
			HandleStopApplication: func(ctx context.Context, payload queue.SimpleIDPayload) error {
				log.Printf("Stop application: %s", payload.ID)
				return appSvc.Stop(payload.ID)
			},
			HandleStartApplication: func(ctx context.Context, payload queue.SimpleIDPayload) error {
				log.Printf("Start application: %s", payload.ID)
				return appSvc.Start(payload.ID)
			},
			HandleBackupRun: func(ctx context.Context, payload queue.SimpleIDPayload) error {
				log.Printf("Backup run: %s", payload.ID)
				return backupSvc.RunBackup(payload.ID)
			},
			HandleDockerCleanup: func(ctx context.Context) error {
				log.Println("Docker cleanup")
				if dockerClient != nil {
					return dockerClient.PruneSystem(ctx)
				}
				return nil
			},
		})
		go func() {
			if err := worker.Start(); err != nil {
				log.Printf("Warning: task worker failed to start: %v", err)
			}
		}()
	} else {
		log.Println("Redis not available, task queue disabled")
	}

	// Initialize Echo
	e := echo.New()
	e.HideBanner = true

	// Global middleware
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS"},
		AllowHeaders:     []string{"*"},
		AllowCredentials: true,
	}))

	// Register REST API routes
	h := handler.New(database, a,
		handler.WithConfig(cfg),
		handler.WithQueue(q),
		handler.WithDocker(dockerClient),
		handler.WithTraefik(traefikMgr),
		handler.WithNotifier(notifier),
		handler.WithCertsPath(cfg.Paths.CertificatesPath),
		handler.WithScheduler(sched),
		handler.WithBackupService(backupSvc),
		handler.WithPreviewService(previewSvc),
		handler.WithComposeService(composeSvc),
	)
	h.RegisterRoutes(e)

	// Register WebSocket routes
	wsHandler := ws.NewHandler(database, dockerClient, a, cfg.Paths.MonitoringPath)
	wsHandler.RegisterRoutes(e)

	// Register frontend routes (must be last - catches all unmatched routes)
	h.RegisterFrontendRoutes(e)

	// Graceful shutdown
	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}

	go func() {
		log.Printf("Dokploy Go server starting on :%s", port)
		log.Printf("Base path: %s", cfg.Paths.BasePath)
		if err := e.Start(":" + port); err != nil {
			log.Printf("Server stopped: %v", err)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")
	if worker != nil {
		worker.Stop()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := e.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Server stopped")
}

func deployDatabaseByType(dbSvc *service.DatabaseService, id, dbType string) error {
	switch dbType {
	case "postgres":
		return dbSvc.DeployPostgres(id)
	case "mysql":
		return dbSvc.DeployMySQL(id)
	case "mariadb":
		return dbSvc.DeployMariaDB(id)
	case "mongo":
		return dbSvc.DeployMongo(id)
	case "redis":
		return dbSvc.DeployRedis(id)
	default:
		return nil
	}
}
