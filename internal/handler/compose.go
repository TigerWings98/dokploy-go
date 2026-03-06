package handler

import (
	"errors"
	"net/http"

	"github.com/dokploy/dokploy/internal/db/schema"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

func (h *Handler) registerComposeRoutes(g *echo.Group) {
	g.POST("", h.CreateCompose)
	g.GET("/:composeId", h.GetCompose)
	g.PUT("/:composeId", h.UpdateCompose)
	g.DELETE("/:composeId", h.DeleteCompose)
	g.POST("/:composeId/deploy", h.DeployCompose)
	g.POST("/:composeId/stop", h.StopCompose)
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
	// TODO: Enqueue compose deployment
	return c.JSON(http.StatusOK, map[string]string{
		"message":   "Deployment queued",
		"composeId": composeID,
	})
}

func (h *Handler) StopCompose(c echo.Context) error {
	composeID := c.Param("composeId")
	// TODO: Stop compose services
	return c.JSON(http.StatusOK, map[string]string{
		"message":   "Stop requested",
		"composeId": composeID,
	})
}
