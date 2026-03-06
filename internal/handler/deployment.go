package handler

import (
	"net/http"

	"github.com/dokploy/dokploy/internal/db/schema"
	"github.com/labstack/echo/v4"
)

func (h *Handler) registerDeploymentRoutes(g *echo.Group) {
	g.GET("", h.ListDeployments)
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
