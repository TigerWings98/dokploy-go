// Input: db (PreviewDeployment 表), service/preview
// Output: PreviewDeployment CRUD + PR 关联管理的 tRPC procedure 实现
// Role: PR 预览部署管理 handler，处理预览环境的创建/更新/删除
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

func (h *Handler) registerPreviewDeploymentRoutes(g *echo.Group) {
	g.GET("", h.ListPreviewDeployments)
	g.GET("/:previewDeploymentId", h.GetPreviewDeployment)
	g.DELETE("/:previewDeploymentId", h.DeletePreviewDeployment)
	g.POST("/:previewDeploymentId/redeploy", h.RedeployPreviewDeployment)
}

func (h *Handler) ListPreviewDeployments(c echo.Context) error {
	appID := c.QueryParam("applicationId")
	if appID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "applicationId is required")
	}

	var previews []schema.PreviewDeployment
	err := h.DB.
		Preload("Deployments", func(db *gorm.DB) *gorm.DB {
			return db.Order("\"createdAt\" DESC").Limit(10)
		}).
		Preload("Domains").
		Where("\"applicationId\" = ?", appID).
		Order("\"createdAt\" DESC").
		Find(&previews).Error
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, previews)
}

func (h *Handler) GetPreviewDeployment(c echo.Context) error {
	id := c.Param("previewDeploymentId")

	var preview schema.PreviewDeployment
	err := h.DB.
		Preload("Application").
		Preload("Application.Server").
		Preload("Deployments", func(db *gorm.DB) *gorm.DB {
			return db.Order("\"createdAt\" DESC").Limit(10)
		}).
		Preload("Domains").
		First(&preview, "\"previewDeploymentId\" = ?", id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Preview deployment not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, preview)
}

func (h *Handler) DeletePreviewDeployment(c echo.Context) error {
	id := c.Param("previewDeploymentId")

	if h.PreviewSvc == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "Preview service not available")
	}

	if err := h.PreviewSvc.RemovePreviewDeployment(id); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, true)
}

type RedeployPreviewRequest struct {
	Title       *string `json:"title"`
	Description *string `json:"description"`
}

func (h *Handler) RedeployPreviewDeployment(c echo.Context) error {
	id := c.Param("previewDeploymentId")

	var req RedeployPreviewRequest
	_ = c.Bind(&req)

	var preview schema.PreviewDeployment
	if err := h.DB.
		Preload("Application").
		First(&preview, "\"previewDeploymentId\" = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Preview deployment not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	title := "Rebuild Preview Deployment"
	if req.Title != nil {
		title = *req.Title
	}

	if h.Queue != nil && preview.ApplicationID != "" {
		info, err := h.Queue.EnqueueDeployApplication(preview.ApplicationID, &title, nil)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}
		return c.JSON(http.StatusOK, map[string]string{
			"message": fmt.Sprintf("Preview redeploy queued"),
			"taskId":  info.ID,
		})
	}

	return c.JSON(http.StatusOK, true)
}
