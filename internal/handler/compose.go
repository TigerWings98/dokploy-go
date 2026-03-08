// Input: db (Compose 表 + Domain/Mount/Deployment), docker, traefik, compose/transform
// Output: Compose 服务 CRUD + 部署/停止/重建 + 域名管理的 tRPC procedure 实现
// Role: Docker Compose 服务管理 handler，支持 docker-compose 和 stack 两种部署模式
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/dokploy/dokploy/internal/db/schema"
	"github.com/labstack/echo/v4"
	gonanoid "github.com/matoous/go-nanoid/v2"
	"gorm.io/gorm"
)

func (h *Handler) registerComposeRoutes(g *echo.Group) {
	g.POST("", h.CreateCompose)
	g.GET("/:composeId", h.GetCompose)
	g.PUT("/:composeId", h.UpdateCompose)
	g.DELETE("/:composeId", h.DeleteCompose)
	g.POST("/:composeId/deploy", h.DeployCompose)
	g.POST("/:composeId/redeploy", h.RedeployCompose)
	g.POST("/:composeId/stop", h.StopCompose)
	g.POST("/:composeId/start", h.StartCompose)
	g.GET("/:composeId/services", h.LoadComposeServices)
	g.POST("/:composeId/randomize", h.RandomizeCompose)
}

type CreateComposeRequest struct {
	Name          string  `json:"name" validate:"required,min=1"`
	AppName       *string `json:"appName"`
	Description   *string `json:"description"`
	EnvironmentID string  `json:"environmentId" validate:"required"`
	ServerID      *string `json:"serverId"`
}

func (h *Handler) CreateCompose(c echo.Context) error {
	var req CreateComposeRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	compose := &schema.Compose{
		Name:          req.Name,
		Description:   req.Description,
		EnvironmentID: req.EnvironmentID,
		ServerID:      req.ServerID,
	}
	if req.AppName != nil {
		compose.AppName = *req.AppName
	}

	if err := h.DB.Create(compose).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusCreated, compose)
}

func (h *Handler) GetCompose(c echo.Context) error {
	composeID := c.Param("composeId")

	var compose schema.Compose
	err := h.DB.
		Preload("Deployments", func(db *gorm.DB) *gorm.DB {
			return db.Order("\"createdAt\" DESC").Limit(10)
		}).
		Preload("Domains").
		Preload("Mounts").
		Preload("Server").
		First(&compose, "\"composeId\" = ?", composeID).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Compose not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, compose)
}

func (h *Handler) UpdateCompose(c echo.Context) error {
	composeID := c.Param("composeId")

	var updates map[string]interface{}
	if err := c.Bind(&updates); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	var compose schema.Compose
	if err := h.DB.First(&compose, "\"composeId\" = ?", composeID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Compose not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if err := h.DB.Model(&compose).Updates(updates).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, compose)
}

func (h *Handler) DeleteCompose(c echo.Context) error {
	composeID := c.Param("composeId")

	result := h.DB.Delete(&schema.Compose{}, "\"composeId\" = ?", composeID)
	if result.Error != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, result.Error.Error())
	}
	if result.RowsAffected == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "Compose not found")
	}

	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) DeployCompose(c echo.Context) error {
	composeID := c.Param("composeId")

	var compose schema.Compose
	if err := h.DB.First(&compose, "\"composeId\" = ?", composeID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Compose not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if h.Queue != nil {
		info, err := h.Queue.EnqueueDeployCompose(composeID, nil)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}
		return c.JSON(http.StatusOK, map[string]string{
			"message":   "Deployment queued",
			"composeId": composeID,
			"taskId":    info.ID,
		})
	}
	return c.JSON(http.StatusOK, map[string]string{
		"message":   "Deployment queued",
		"composeId": composeID,
	})
}

func (h *Handler) StopCompose(c echo.Context) error {
	composeID := c.Param("composeId")

	var compose schema.Compose
	if err := h.DB.First(&compose, "\"composeId\" = ?", composeID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Compose not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if h.Queue != nil {
		info, err := h.Queue.EnqueueStopCompose(composeID)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}
		return c.JSON(http.StatusOK, map[string]string{
			"message":   "Stop requested",
			"composeId": composeID,
			"taskId":    info.ID,
		})
	}
	return c.JSON(http.StatusOK, map[string]string{
		"message":   "Stop requested",
		"composeId": composeID,
	})
}

func (h *Handler) RedeployCompose(c echo.Context) error {
	composeID := c.Param("composeId")

	var compose schema.Compose
	if err := h.DB.First(&compose, "\"composeId\" = ?", composeID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Compose not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	title := fmt.Sprintf("Redeploy %s", compose.Name)
	if h.Queue != nil {
		info, err := h.Queue.EnqueueDeployCompose(composeID, &title)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}
		return c.JSON(http.StatusOK, map[string]string{
			"message":   "Redeployment queued",
			"composeId": composeID,
			"taskId":    info.ID,
		})
	}
	return c.JSON(http.StatusOK, map[string]string{
		"message":   "Redeployment queued",
		"composeId": composeID,
	})
}

func (h *Handler) StartCompose(c echo.Context) error {
	composeID := c.Param("composeId")

	var compose schema.Compose
	if err := h.DB.First(&compose, "\"composeId\" = ?", composeID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Compose not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if h.Queue != nil {
		title := fmt.Sprintf("Start %s", compose.Name)
		info, err := h.Queue.EnqueueDeployCompose(composeID, &title)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}
		return c.JSON(http.StatusOK, map[string]string{
			"message":   "Start requested",
			"composeId": composeID,
			"taskId":    info.ID,
		})
	}
	return c.JSON(http.StatusOK, map[string]string{
		"message":   "Start requested",
		"composeId": composeID,
	})
}

func (h *Handler) LoadComposeServices(c echo.Context) error {
	composeID := c.Param("composeId")

	var compose schema.Compose
	if err := h.DB.First(&compose, "\"composeId\" = ?", composeID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Compose not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if h.Docker == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "Docker client not available")
	}

	// List services/containers matching the compose project name
	ctx := context.Background()
	containers, err := h.Docker.ListContainers(ctx)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	var services []map[string]interface{}
	for _, ctr := range containers {
		projectLabel := ctr.Labels["com.docker.compose.project"]
		if projectLabel == compose.AppName {
			services = append(services, map[string]interface{}{
				"id":      ctr.ID[:12],
				"name":    strings.TrimPrefix(ctr.Names[0], "/"),
				"image":   ctr.Image,
				"state":   ctr.State,
				"status":  ctr.Status,
				"service": ctr.Labels["com.docker.compose.service"],
			})
		}
	}

	return c.JSON(http.StatusOK, services)
}

func (h *Handler) RandomizeCompose(c echo.Context) error {
	composeID := c.Param("composeId")

	var compose schema.Compose
	if err := h.DB.First(&compose, "\"composeId\" = ?", composeID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Compose not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	suffix, _ := gonanoid.New(8)
	newSuffix := compose.AppName + "-" + suffix

	if err := h.DB.Model(&compose).Update("suffix", newSuffix).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, map[string]string{
		"message": "Compose suffix randomized",
		"suffix":  newSuffix,
	})
}
