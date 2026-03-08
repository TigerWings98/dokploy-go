// Input: db, backup (卷备份操作)
// Output: Volume Backup CRUD + 手动触发/恢复的 tRPC procedure 实现
// Role: Docker 卷备份管理 handler，配置卷备份策略和 S3 上传/恢复
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
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
	Name           string  `json:"name" validate:"required"`
	VolumeName     string  `json:"volumeName" validate:"required"`
	AppName        string  `json:"appName" validate:"required"`
	ServiceName    *string `json:"serviceName"`
	ServiceType    string  `json:"serviceType" validate:"required"`
	CronExpression string  `json:"cronExpression" validate:"required"`
	Prefix         string  `json:"prefix" validate:"required"`
	Enabled        *bool   `json:"enabled"`
	DestinationID  string  `json:"destinationId" validate:"required"`
}

func (h *Handler) CreateVolumeBackup(c echo.Context) error {
	var req CreateVolumeBackupRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	vb := &schema.VolumeBackup{
		Name:           req.Name,
		VolumeName:     req.VolumeName,
		AppName:        req.AppName,
		ServiceName:    req.ServiceName,
		ServiceType:    req.ServiceType,
		CronExpression: req.CronExpression,
		Prefix:         req.Prefix,
		Enabled:        req.Enabled,
		DestinationID:  req.DestinationID,
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

	if h.Queue != nil {
		info, err := h.Queue.EnqueueBackupRun(id)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}
		return c.JSON(http.StatusOK, map[string]string{"message": "Volume backup queued", "taskId": info.ID})
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "Volume backup queued"})
}
