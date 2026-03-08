// Input: db (Mount 表)
// Output: Mount CRUD 的 tRPC procedure 实现
// Role: 挂载管理 handler，配置 bind/volume/file 类型的挂载点
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package handler

import (
	"errors"
	"net/http"

	"github.com/dokploy/dokploy/internal/db/schema"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

func (h *Handler) registerMountRoutes(g *echo.Group) {
	g.POST("", h.CreateMount)
	g.GET("/:mountId", h.GetMount)
	g.PUT("/:mountId", h.UpdateMount)
	g.DELETE("/:mountId", h.DeleteMount)
}

type CreateMountRequest struct {
	Type          string  `json:"type" validate:"required"`
	HostPath      *string `json:"hostPath"`
	VolumeName    *string `json:"volumeName"`
	Content       *string `json:"content"`
	MountPath     string  `json:"mountPath" validate:"required"`
	ServiceName   *string `json:"serviceName"`
	FilePath      *string `json:"filePath"`
	ApplicationID *string `json:"applicationId"`
	PostgresID    *string `json:"postgresId"`
	MariaDBID     *string `json:"mariadbId"`
	MongoID       *string `json:"mongoId"`
	MySQLID       *string `json:"mysqlId"`
	RedisID       *string `json:"redisId"`
	ComposeID     *string `json:"composeId"`
}

func (h *Handler) CreateMount(c echo.Context) error {
	var req CreateMountRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	m := &schema.Mount{
		Type:          schema.MountType(req.Type),
		HostPath:      req.HostPath,
		VolumeName:    req.VolumeName,
		Content:       req.Content,
		MountPath:     req.MountPath,
		ServiceName:   req.ServiceName,
		FilePath:      req.FilePath,
		ApplicationID: req.ApplicationID,
		PostgresID:    req.PostgresID,
		MariaDBID:     req.MariaDBID,
		MongoID:       req.MongoID,
		MySQLID:       req.MySQLID,
		RedisID:       req.RedisID,
		ComposeID:     req.ComposeID,
	}

	if err := h.DB.Create(m).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusCreated, m)
}

func (h *Handler) GetMount(c echo.Context) error {
	id := c.Param("mountId")

	var m schema.Mount
	if err := h.DB.First(&m, "\"mountId\" = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Mount not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, m)
}

func (h *Handler) UpdateMount(c echo.Context) error {
	id := c.Param("mountId")

	var updates map[string]interface{}
	if err := c.Bind(&updates); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	var m schema.Mount
	if err := h.DB.First(&m, "\"mountId\" = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Mount not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if err := h.DB.Model(&m).Updates(updates).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, m)
}

func (h *Handler) DeleteMount(c echo.Context) error {
	id := c.Param("mountId")

	result := h.DB.Delete(&schema.Mount{}, "\"mountId\" = ?", id)
	if result.Error != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, result.Error.Error())
	}
	if result.RowsAffected == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "Mount not found")
	}

	return c.NoContent(http.StatusNoContent)
}
