// Input: db (Deployment 表), 文件系统 (部署日志文件)
// Output: Deployment 列表/详情/日志读取/取消的 tRPC procedure 实现
// Role: 部署记录管理 handler，查询部署历史和读取部署日志文件
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package handler

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/dokploy/dokploy/internal/db/schema"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

func (h *Handler) registerDeploymentRoutes(g *echo.Group) {
	g.GET("", h.ListDeployments)
	g.GET("/:deploymentId", h.GetDeployment)
	g.DELETE("/:deploymentId", h.DeleteDeployment)
	g.POST("/:deploymentId/cancel", h.CancelDeployment)
	g.GET("/:deploymentId/logs", h.GetDeploymentLogs)
	g.DELETE("/all", h.DeleteAllDeployments)
}

type ListDeploymentsQuery struct {
	ID   string `query:"id" validate:"required"`
	Type string `query:"type" validate:"required,oneof=application compose server schedule previewDeployment backup volumeBackup"`
}

func (h *Handler) ListDeployments(c echo.Context) error {
	var q ListDeploymentsQuery
	if err := c.Bind(&q); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	var deployments []schema.Deployment
	query := h.DB.Order("\"createdAt\" DESC")

	switch q.Type {
	case "application":
		query = query.Where("\"applicationId\" = ?", q.ID)
	case "compose":
		query = query.Where("\"composeId\" = ?", q.ID)
	case "server":
		query = query.Where("\"serverId\" = ?", q.ID)
	case "schedule":
		query = query.Where("\"scheduleId\" = ?", q.ID)
	case "previewDeployment":
		query = query.Where("\"previewDeploymentId\" = ?", q.ID)
	case "backup":
		query = query.Where("\"backupId\" = ?", q.ID)
	case "volumeBackup":
		query = query.Where("\"volumeBackupId\" = ?", q.ID)
	default:
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid type")
	}

	if err := query.Find(&deployments).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, deployments)
}

func (h *Handler) GetDeployment(c echo.Context) error {
	id := c.Param("deploymentId")
	var deployment schema.Deployment
	if err := h.DB.First(&deployment, "\"deploymentId\" = ?", id).Error; err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "Deployment not found")
	}
	return c.JSON(http.StatusOK, deployment)
}

func (h *Handler) DeleteDeployment(c echo.Context) error {
	id := c.Param("deploymentId")

	var deployment schema.Deployment
	if err := h.DB.First(&deployment, "\"deploymentId\" = ?", id).Error; err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "Deployment not found")
	}

	// Remove log file if it exists
	if deployment.LogPath != "" {
		os.Remove(deployment.LogPath)
	}

	if err := h.DB.Delete(&deployment).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, map[string]string{"message": "Deployment deleted"})
}

type DeleteAllDeploymentsRequest struct {
	ID   string `json:"id" validate:"required"`
	Type string `json:"type" validate:"required"`
}

func (h *Handler) DeleteAllDeployments(c echo.Context) error {
	var req DeleteAllDeploymentsRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	// Find all deployments for the given resource
	var deployments []schema.Deployment
	var query *gorm.DB = h.DB.DB

	switch req.Type {
	case "application":
		query = query.Where("\"applicationId\" = ?", req.ID)
	case "compose":
		query = query.Where("\"composeId\" = ?", req.ID)
	default:
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid type")
	}

	if err := query.Find(&deployments).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	// Remove log files
	for _, d := range deployments {
		if d.LogPath != "" {
			os.Remove(d.LogPath)
		}
	}

	// Delete records
	if err := query.Delete(&schema.Deployment{}).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, map[string]string{"message": "All deployments deleted"})
}

func (h *Handler) CancelDeployment(c echo.Context) error {
	id := c.Param("deploymentId")

	var deployment schema.Deployment
	if err := h.DB.First(&deployment, "\"deploymentId\" = ?", id).Error; err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "Deployment not found")
	}

	if deployment.Status == nil || *deployment.Status != schema.DeploymentStatusRunning {
		return echo.NewHTTPError(http.StatusBadRequest, "Deployment is not running")
	}

	if err := h.DB.Model(&deployment).Update("status", schema.DeploymentStatusCancelled).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, map[string]string{"message": "Deployment cancelled"})
}

func (h *Handler) GetDeploymentLogs(c echo.Context) error {
	id := c.Param("deploymentId")

	var deployment schema.Deployment
	if err := h.DB.First(&deployment, "\"deploymentId\" = ?", id).Error; err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "Deployment not found")
	}

	logPath := deployment.LogPath
	if logPath == "" {
		// Try default log path
		logPath = filepath.Join("/etc/dokploy/logs", deployment.DeploymentID+".log")
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		return c.JSON(http.StatusOK, map[string]string{"logs": ""})
	}

	return c.JSON(http.StatusOK, map[string]string{"logs": string(data)})
}
