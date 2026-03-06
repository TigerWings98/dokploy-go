package handler

import (
	"errors"
	"net/http"

	"github.com/dokploy/dokploy/internal/db/schema"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

func (h *Handler) registerRedisRoutes(g *echo.Group) {
	g.POST("", h.CreateRedis)
	g.GET("/:redisId", h.GetRedis)
	g.PUT("/:redisId", h.UpdateRedis)
	g.DELETE("/:redisId", h.DeleteRedis)
	g.POST("/:redisId/deploy", h.DeployRedis)
	g.POST("/:redisId/stop", h.StopRedis)
}

type CreateRedisRequest struct {
	Name             string  `json:"name" validate:"required"`
	Description      *string `json:"description"`
	DatabasePassword string  `json:"databasePassword" validate:"required"`
	DockerImage      string  `json:"dockerImage" validate:"required"`
	EnvironmentID    string  `json:"environmentId" validate:"required"`
	ServerID         *string `json:"serverId"`
}

func (h *Handler) CreateRedis(c echo.Context) error {
	var req CreateRedisRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	r := &schema.Redis{
		Name:             req.Name,
		Description:      req.Description,
		DatabasePassword: req.DatabasePassword,
		DockerImage:      req.DockerImage,
		EnvironmentID:    req.EnvironmentID,
		ServerID:         req.ServerID,
	}

	if err := h.DB.Create(r).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusCreated, r)
}

func (h *Handler) GetRedis(c echo.Context) error {
	id := c.Param("redisId")

	var r schema.Redis
	err := h.DB.Preload("Mounts").First(&r, "\"redisId\" = ?", id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Redis not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, r)
}

func (h *Handler) UpdateRedis(c echo.Context) error {
	id := c.Param("redisId")

	var updates map[string]interface{}
	if err := c.Bind(&updates); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	var r schema.Redis
	if err := h.DB.First(&r, "\"redisId\" = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Redis not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if err := h.DB.Model(&r).Updates(updates).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, r)
}

func (h *Handler) DeleteRedis(c echo.Context) error {
	id := c.Param("redisId")

	result := h.DB.Delete(&schema.Redis{}, "\"redisId\" = ?", id)
	if result.Error != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, result.Error.Error())
	}
	if result.RowsAffected == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "Redis not found")
	}

	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) DeployRedis(c echo.Context) error {
	id := c.Param("redisId")

	var r schema.Redis
	if err := h.DB.First(&r, "\"redisId\" = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Redis not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, map[string]string{"message": "Deployment queued"})
}

func (h *Handler) StopRedis(c echo.Context) error {
	id := c.Param("redisId")

	var r schema.Redis
	if err := h.DB.First(&r, "\"redisId\" = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Redis not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, map[string]string{"message": "Stop queued"})
}
