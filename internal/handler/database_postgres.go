package handler

import (
	"errors"
	"net/http"

	"github.com/dokploy/dokploy/internal/db/schema"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

func (h *Handler) registerPostgresRoutes(g *echo.Group) {
	g.POST("", h.CreatePostgres)
	g.GET("/:postgresId", h.GetPostgres)
	g.PUT("/:postgresId", h.UpdatePostgres)
	g.DELETE("/:postgresId", h.DeletePostgres)
	g.POST("/:postgresId/deploy", h.DeployPostgres)
	g.POST("/:postgresId/stop", h.StopPostgres)
}

type CreatePostgresRequest struct {
	Name             string  `json:"name" validate:"required"`
	Description      *string `json:"description"`
	DatabaseName     string  `json:"databaseName" validate:"required"`
	DatabaseUser     string  `json:"databaseUser" validate:"required"`
	DatabasePassword string  `json:"databasePassword" validate:"required"`
	DockerImage      string  `json:"dockerImage" validate:"required"`
	EnvironmentID    string  `json:"environmentId" validate:"required"`
	ServerID         *string `json:"serverId"`
}

func (h *Handler) CreatePostgres(c echo.Context) error {
	var req CreatePostgresRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	pg := &schema.Postgres{
		Name:             req.Name,
		Description:      req.Description,
		DatabaseName:     req.DatabaseName,
		DatabaseUser:     req.DatabaseUser,
		DatabasePassword: req.DatabasePassword,
		DockerImage:      req.DockerImage,
		EnvironmentID:    req.EnvironmentID,
		ServerID:         req.ServerID,
	}

	if err := h.DB.Create(pg).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusCreated, pg)
}

func (h *Handler) GetPostgres(c echo.Context) error {
	id := c.Param("postgresId")

	var pg schema.Postgres
	err := h.DB.Preload("Mounts").Preload("Backups").First(&pg, "\"postgresId\" = ?", id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Postgres not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, pg)
}

func (h *Handler) UpdatePostgres(c echo.Context) error {
	id := c.Param("postgresId")

	var updates map[string]interface{}
	if err := c.Bind(&updates); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	var pg schema.Postgres
	if err := h.DB.First(&pg, "\"postgresId\" = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Postgres not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if err := h.DB.Model(&pg).Updates(updates).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, pg)
}

func (h *Handler) DeletePostgres(c echo.Context) error {
	id := c.Param("postgresId")

	result := h.DB.Delete(&schema.Postgres{}, "\"postgresId\" = ?", id)
	if result.Error != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, result.Error.Error())
	}
	if result.RowsAffected == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "Postgres not found")
	}

	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) DeployPostgres(c echo.Context) error {
	id := c.Param("postgresId")

	var pg schema.Postgres
	if err := h.DB.First(&pg, "\"postgresId\" = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Postgres not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	// TODO: Enqueue deploy task
	return c.JSON(http.StatusOK, map[string]string{"message": "Deployment queued"})
}

func (h *Handler) StopPostgres(c echo.Context) error {
	id := c.Param("postgresId")

	var pg schema.Postgres
	if err := h.DB.First(&pg, "\"postgresId\" = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Postgres not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	// TODO: Stop docker service
	return c.JSON(http.StatusOK, map[string]string{"message": "Stop queued"})
}
