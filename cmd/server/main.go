package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/dokploy/dokploy/internal/auth"
	"github.com/dokploy/dokploy/internal/config"
	"github.com/dokploy/dokploy/internal/db"
	"github.com/dokploy/dokploy/internal/docker"
	"github.com/dokploy/dokploy/internal/handler"
	"github.com/dokploy/dokploy/internal/queue"
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

	// Initialize auth
	a := auth.New(database)

	// Initialize Docker client
	dockerClient, err := docker.NewClient()
	if err != nil {
		log.Printf("Warning: failed to initialize Docker client: %v", err)
	}

	// Initialize task queue (optional - requires Redis)
	redisAddr := os.Getenv("REDIS_URL")
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}
	q := queue.NewQueue(redisAddr)
	defer q.Close()

	// Start task worker
	worker := queue.NewWorker(redisAddr, 10, queue.TaskHandlers{
		// TODO: Wire actual service handlers
		HandleDeployApplication: nil,
		HandleDeployCompose:     nil,
		HandleDeployDatabase:    nil,
		HandleRebuildDatabase:   nil,
		HandleStopApplication:   nil,
		HandleStartApplication:  nil,
		HandleBackupRun:         nil,
		HandleDockerCleanup:     nil,
	})
	go func() {
		if err := worker.Start(); err != nil {
			log.Printf("Warning: task worker failed to start: %v (Redis may not be available)", err)
		}
	}()

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
	h := handler.New(database, a)
	h.RegisterRoutes(e)

	// Register WebSocket routes
	wsHandler := ws.NewHandler(database, dockerClient, a)
	wsHandler.RegisterRoutes(e)

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
	worker.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := e.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Server stopped")

	// Suppress unused variable warning
	_ = q
}
