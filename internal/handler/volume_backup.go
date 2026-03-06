package handler

import (
	"errors"
	"net/http"

	"github.com/dokploy/dokploy/internal/db/schema"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

func (h *Handler) registerVolumeBackupRoutes(g *echo.Group) {
	g.POST("", h.CreateVolumeBackup)
	g.GET("/:volumeBackupId", h.GetVolumeBackup)
	g.PUT("/:volumeBackupId", h.UpdateVolumeBackup)
	g.DELETE("/:volumeBackupId", h.DeleteVolumeBackup)
	g.POST("/:volumeBackupId/manual", h.ManualVolumeBackup)
}

type CreateVolumeBackupRequest struct {
	AppName       string  `json:"appName" validate:"required"`
	ServiceName   string  `json:"serviceName" validate:"required"`
	ServiceType   string  `json:"serviceType" validate:"required"`
	SourcePath    string  `json:"sourcePath" validate:"required"`
	Schedule      string  `json:"schedule" validate:"required"`
	Prefix        string  `json:"prefix" validate:"required"`
	Enabled       *bool   `json:"enabled"`
	DestinationID string  `json:"destinationId" validate:"required"`
	ServerID      *string `json:"serverId"`
}

func (h *Handler) CreateVolumeBackup(c echo.Context) error {
	var req CreateVolumeBackupRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	vb := &schema.VolumeBackup{
		AppName:       req.AppName,
		ServiceName:   req.ServiceName,
		ServiceType:   req.ServiceType,
		SourcePath:    req.SourcePath,
		Schedule:      req.Schedule,
		Prefix:        req.Prefix,
		Enabled:       req.Enabled,
		DestinationID: req.DestinationID,
		ServerID:      req.ServerID,
	}

	if err := h.DB.Create(vb).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusCreated, vb)
}

func (h *Handler) GetVolumeBackup(c echo.Context) error {
	id := c.Param("volumeBackupId")

	var vb schema.VolumeBackup
	err := h.DB.Preload("Destination").First(&vb, "\"volumeBackupId\" = ?", id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Volume backup not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, vb)
}

func (h *Handler) UpdateVolumeBackup(c echo.Context) error {
	id := c.Param("volumeBackupId")

	var updates map[string]interface{}
	if err := c.Bind(&updates); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	var vb schema.VolumeBackup
	if err := h.DB.First(&vb, "\"volumeBackupId\" = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Volume backup not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if err := h.DB.Model(&vb).Updates(updates).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, vb)
}

func (h *Handler) DeleteVolumeBackup(c echo.Context) error {
	id := c.Param("volumeBackupId")

	result := h.DB.Delete(&schema.VolumeBackup{}, "\"volumeBackupId\" = ?", id)
	if result.Error != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, result.Error.Error())
	}
	if result.RowsAffected == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "Volume backup not found")
	}

	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) ManualVolumeBackup(c echo.Context) error {
	id := c.Param("volumeBackupId")

	var vb schema.VolumeBackup
	if err := h.DB.First(&vb, "\"volumeBackupId\" = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Volume backup not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	// TODO: Enqueue manual volume backup task
	return c.JSON(http.StatusOK, map[string]string{"message": "Volume backup queued"})
}
