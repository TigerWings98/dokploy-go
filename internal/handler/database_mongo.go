package handler

import (
	"errors"
	"net/http"

	"github.com/dokploy/dokploy/internal/db/schema"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

func (h *Handler) registerMongoRoutes(g *echo.Group) {
	g.POST("", h.CreateMongo)
	g.GET("/:mongoId", h.GetMongo)
	g.PUT("/:mongoId", h.UpdateMongo)
	g.DELETE("/:mongoId", h.DeleteMongo)
	g.POST("/:mongoId/deploy", h.DeployMongo)
	g.POST("/:mongoId/stop", h.StopMongo)
}

type CreateMongoRequest struct {
	Name             string  `json:"name" validate:"required"`
	Description      *string `json:"description"`
	DatabaseUser     string  `json:"databaseUser" validate:"required"`
	DatabasePassword string  `json:"databasePassword" validate:"required"`
	DockerImage      string  `json:"dockerImage" validate:"required"`
	EnvironmentID    string  `json:"environmentId" validate:"required"`
	ServerID         *string `json:"serverId"`
}

func (h *Handler) CreateMongo(c echo.Context) error {
	var req CreateMongoRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	m := &schema.Mongo{
		Name:             req.Name,
		Description:      req.Description,
		DatabaseUser:     req.DatabaseUser,
		DatabasePassword: req.DatabasePassword,
		DockerImage:      req.DockerImage,
		EnvironmentID:    req.EnvironmentID,
		ServerID:         req.ServerID,
	}

	if err := h.DB.Create(m).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusCreated, m)
}

func (h *Handler) GetMongo(c echo.Context) error {
	id := c.Param("mongoId")

	var m schema.Mongo
	err := h.DB.Preload("Mounts").Preload("Backups").First(&m, "\"mongoId\" = ?", id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Mongo not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, m)
}

func (h *Handler) UpdateMongo(c echo.Context) error {
	id := c.Param("mongoId")

	var updates map[string]interface{}
	if err := c.Bind(&updates); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	var m schema.Mongo
	if err := h.DB.First(&m, "\"mongoId\" = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Mongo not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if err := h.DB.Model(&m).Updates(updates).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, m)
}

func (h *Handler) DeleteMongo(c echo.Context) error {
	id := c.Param("mongoId")

	result := h.DB.Delete(&schema.Mongo{}, "\"mongoId\" = ?", id)
	if result.Error != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, result.Error.Error())
	}
	if result.RowsAffected == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "Mongo not found")
	}

	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) DeployMongo(c echo.Context) error {
	id := c.Param("mongoId")

	var m schema.Mongo
	if err := h.DB.First(&m, "\"mongoId\" = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Mongo not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if h.Queue != nil {
		info, err := h.Queue.EnqueueDeployDatabase(id, "mongo")
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}
		return c.JSON(http.StatusOK, map[string]string{"message": "Deployment queued", "taskId": info.ID})
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "Deployment queued"})
}

func (h *Handler) StopMongo(c echo.Context) error {
	id := c.Param("mongoId")

	var m schema.Mongo
	if err := h.DB.First(&m, "\"mongoId\" = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Mongo not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if h.Queue != nil {
		info, err := h.Queue.EnqueueStopDatabase(id, "mongo")
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}
		return c.JSON(http.StatusOK, map[string]string{"message": "Stop queued", "taskId": info.ID})
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "Stop queued"})
}
