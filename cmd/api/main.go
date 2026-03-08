// Input: 环境变量配置, PostgreSQL, Redis, API Key
// Output: 外部 REST API 服务
// Role: 外部部署队列 API 入口，接收部署请求并入队 asynq
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package main

import (
	"log"
	"net/http"
	"os"

	"github.com/dokploy/dokploy/internal/config"
	"github.com/dokploy/dokploy/internal/db"
	"github.com/dokploy/dokploy/internal/queue"
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

	redisAddr := os.Getenv("REDIS_URL")
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}

	q := queue.NewQueue(redisAddr)
	defer q.Close()

	apiKey := os.Getenv("API_KEY")

	e := echo.New()
	e.HideBanner = true
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())

	// Health check
	e.GET("/health", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})

	// API key auth middleware
	api := e.Group("")
	api.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if apiKey == "" {
				return next(c)
			}
			key := c.Request().Header.Get("X-API-Key")
			if key != apiKey {
				return echo.NewHTTPError(http.StatusForbidden, "Invalid API Key")
			}
			return next(c)
		}
	})

	// Deploy application
	api.POST("/deploy/application", func(c echo.Context) error {
		var req struct {
			ApplicationID string  `json:"applicationId"`
			Title         *string `json:"title"`
			Description   *string `json:"description"`
		}
		if err := c.Bind(&req); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}

		info, err := q.EnqueueDeployApplication(req.ApplicationID, req.Title, req.Description)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}

		return c.JSON(http.StatusOK, map[string]string{
			"taskId":  info.ID,
			"message": "Deployment queued",
		})
	})

	// Deploy compose
	api.POST("/deploy/compose", func(c echo.Context) error {
		var req struct {
			ComposeID string  `json:"composeId"`
			Title     *string `json:"title"`
		}
		if err := c.Bind(&req); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}

		info, err := q.EnqueueDeployCompose(req.ComposeID, req.Title)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}

		return c.JSON(http.StatusOK, map[string]string{
			"taskId":  info.ID,
			"message": "Deployment queued",
		})
	})

	// Deploy database
	api.POST("/deploy/database", func(c echo.Context) error {
		var req struct {
			DatabaseID string `json:"databaseId"`
			Type       string `json:"type"`
		}
		if err := c.Bind(&req); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}

		info, err := q.EnqueueDeployDatabase(req.DatabaseID, req.Type)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}

		return c.JSON(http.StatusOK, map[string]string{
			"taskId":  info.ID,
			"message": "Deployment queued",
		})
	})

	// Stop application
	api.POST("/stop/application", func(c echo.Context) error {
		var req struct {
			ApplicationID string `json:"applicationId"`
		}
		if err := c.Bind(&req); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}

		info, err := q.EnqueueStopApplication(req.ApplicationID)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}

		return c.JSON(http.StatusOK, map[string]string{
			"taskId":  info.ID,
			"message": "Stop queued",
		})
	})

	// Run backup
	api.POST("/backup/run", func(c echo.Context) error {
		var req struct {
			BackupID string `json:"backupId"`
		}
		if err := c.Bind(&req); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}

		info, err := q.EnqueueBackupRun(req.BackupID)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}

		return c.JSON(http.StatusOK, map[string]string{
			"taskId":  info.ID,
			"message": "Backup queued",
		})
	})

	// Docker cleanup
	api.POST("/docker/cleanup", func(c echo.Context) error {
		info, err := q.EnqueueDockerCleanup()
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}

		return c.JSON(http.StatusOK, map[string]string{
			"taskId":  info.ID,
			"message": "Cleanup queued",
		})
	})

	port := os.Getenv("API_PORT")
	if port == "" {
		port = "3001"
	}

	log.Printf("Dokploy API service starting on :%s", port)
	if err := e.Start(":" + port); err != nil {
		log.Fatalf("failed to start API server: %v", err)
	}
}
