package handler

import (
	"errors"
	"net/http"

	"github.com/dokploy/dokploy/internal/db/schema"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

func (h *Handler) registerBackupRoutes(g *echo.Group) {
	g.POST("", h.CreateBackup)
	g.GET("/:backupId", h.GetBackup)
	g.PUT("/:backupId", h.UpdateBackup)
	g.DELETE("/:backupId", h.DeleteBackup)
	g.POST("/:backupId/manual", h.ManualBackup)
}

type CreateBackupRequest struct {
	Schedule      string  `json:"schedule" validate:"required"`
	Prefix        string  `json:"prefix" validate:"required"`
	DatabaseType  string  `json:"database" validate:"required"`
	Enabled       *bool   `json:"enabled"`
	DestinationID string  `json:"destinationId" validate:"required"`
	PostgresID    *string `json:"postgresId"`
	MySQLID       *string `json:"mysqlId"`
	MariaDBID     *string `json:"mariadbId"`
	MongoID       *string `json:"mongoId"`
}

func (h *Handler) CreateBackup(c echo.Context) error {
	var req CreateBackupRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	b := &schema.Backup{
		Schedule:      req.Schedule,
		Prefix:        req.Prefix,
		DatabaseType:  schema.DatabaseType(req.DatabaseType),
		Enabled:       req.Enabled,
		DestinationID: req.DestinationID,
		PostgresID:    req.PostgresID,
		MySQLID:       req.MySQLID,
		MariaDBID:     req.MariaDBID,
		MongoID:       req.MongoID,
	}

	if err := h.DB.Create(b).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusCreated, b)
}

func (h *Handler) GetBackup(c echo.Context) error {
	id := c.Param("backupId")

	var b schema.Backup
	err := h.DB.
		Preload("Destination").
		First(&b, "\"backupId\" = ?", id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Backup not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, b)
}

func (h *Handler) UpdateBackup(c echo.Context) error {
	id := c.Param("backupId")

	var updates map[string]interface{}
	if err := c.Bind(&updates); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	var b schema.Backup
	if err := h.DB.First(&b, "\"backupId\" = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Backup not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if err := h.DB.Model(&b).Updates(updates).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, b)
}

func (h *Handler) DeleteBackup(c echo.Context) error {
	id := c.Param("backupId")

	result := h.DB.Delete(&schema.Backup{}, "\"backupId\" = ?", id)
	if result.Error != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, result.Error.Error())
	}
	if result.RowsAffected == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "Backup not found")
	}

	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) ManualBackup(c echo.Context) error {
	id := c.Param("backupId")

	var b schema.Backup
	if err := h.DB.First(&b, "\"backupId\" = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Backup not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	// TODO: Enqueue manual backup task
	return c.JSON(http.StatusOK, map[string]string{"message": "Backup queued"})
}
