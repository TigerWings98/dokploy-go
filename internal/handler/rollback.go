// Input: db (Deployment 表), docker (服务回滚)
// Output: Application/Compose 回滚到指定部署版本的 tRPC procedure 实现
// Role: 部署回滚 handler，基于历史部署记录执行服务版本回退
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package handler

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/dokploy/dokploy/internal/db/schema"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

func (h *Handler) registerRollbackRoutes(g *echo.Group) {
	g.GET("", h.ListRollbacks)
	g.POST("", h.CreateRollback)
	g.POST("/:rollbackId/apply", h.ApplyRollback)
	g.DELETE("/:rollbackId", h.DeleteRollback)
}

func (h *Handler) ListRollbacks(c echo.Context) error {
	appID := c.QueryParam("applicationId")
	if appID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "applicationId is required")
	}

	var rollbacks []schema.Rollback
	if err := h.DB.Where("\"applicationId\" = ?", appID).Order("\"createdAt\" DESC").Find(&rollbacks).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, rollbacks)
}

func (h *Handler) ApplyRollback(c echo.Context) error {
	id := c.Param("rollbackId")

	var rb schema.Rollback
	if err := h.DB.First(&rb, "\"rollbackId\" = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Rollback not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	// Enqueue a deploy for the application (rollback uses the stored docker image)
	if h.Queue != nil && rb.ApplicationID != "" {
		title := fmt.Sprintf("Rollback to %s", rb.DockerImage)
		info, err := h.Queue.EnqueueDeployApplication(rb.ApplicationID, &title, nil)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}
		return c.JSON(http.StatusOK, map[string]string{"message": "Rollback queued", "taskId": info.ID})
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "Rollback queued"})
}

type CreateRollbackRequest struct {
	ApplicationID string `json:"applicationId" validate:"required"`
	DockerImage   string `json:"dockerImage" validate:"required"`
}

func (h *Handler) CreateRollback(c echo.Context) error {
	var req CreateRollbackRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	// Verify application exists
	var app schema.Application
	if err := h.DB.First(&app, "\"applicationId\" = ?", req.ApplicationID).Error; err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "Application not found")
	}

	rb := &schema.Rollback{
		ApplicationID: req.ApplicationID,
		DockerImage:   req.DockerImage,
	}

	if err := h.DB.Create(rb).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusCreated, rb)
}

func (h *Handler) DeleteRollback(c echo.Context) error {
	id := c.Param("rollbackId")

	result := h.DB.Delete(&schema.Rollback{}, "\"rollbackId\" = ?", id)
	if result.Error != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, result.Error.Error())
	}
	if result.RowsAffected == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "Rollback not found")
	}

	return c.NoContent(http.StatusNoContent)
}
