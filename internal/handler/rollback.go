package handler

import (
	"errors"
	"net/http"

	"github.com/dokploy/dokploy/internal/db/schema"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

func (h *Handler) registerRollbackRoutes(g *echo.Group) {
	g.GET("", h.ListRollbacks)
	g.POST("/:rollbackId/apply", h.ApplyRollback)
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

	// TODO: Apply rollback by updating docker service to the specified image
	return c.JSON(http.StatusOK, map[string]string{"message": "Rollback queued"})
}
