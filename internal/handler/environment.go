// Input: db (Application/Compose/Database 表的 env 字段)
// Output: 环境变量更新的 tRPC procedure 实现
// Role: 环境变量管理 handler，更新 Application/Compose/Database 的 env 文本字段
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package handler

import (
	"errors"
	"net/http"

	"github.com/dokploy/dokploy/internal/db/schema"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

func (h *Handler) registerEnvironmentRoutes(g *echo.Group) {
	g.POST("", h.CreateEnvironment)
	g.GET("/:environmentId", h.GetEnvironment)
	g.PUT("/:environmentId", h.UpdateEnvironment)
	g.DELETE("/:environmentId", h.DeleteEnvironment)
}

type CreateEnvironmentRequest struct {
	Name        string  `json:"name" validate:"required"`
	Description *string `json:"description"`
	ProjectID   string  `json:"projectId" validate:"required"`
}

func (h *Handler) CreateEnvironment(c echo.Context) error {
	var req CreateEnvironmentRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	env := &schema.Environment{
		Name:        req.Name,
		Description: req.Description,
		ProjectID:   req.ProjectID,
	}

	if err := h.DB.Create(env).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusCreated, env)
}

func (h *Handler) GetEnvironment(c echo.Context) error {
	id := c.Param("environmentId")

	var env schema.Environment
	err := h.DB.
		Preload("Applications").
		Preload("Postgres").
		Preload("MySQL").
		Preload("MariaDB").
		Preload("Mongo").
		Preload("Redis").
		Preload("Compose").
		First(&env, "\"environmentId\" = ?", id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Environment not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, env)
}

func (h *Handler) UpdateEnvironment(c echo.Context) error {
	id := c.Param("environmentId")

	var updates map[string]interface{}
	if err := c.Bind(&updates); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	var env schema.Environment
	if err := h.DB.First(&env, "\"environmentId\" = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Environment not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if err := h.DB.Model(&env).Updates(updates).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, env)
}

func (h *Handler) DeleteEnvironment(c echo.Context) error {
	id := c.Param("environmentId")

	result := h.DB.Delete(&schema.Environment{}, "\"environmentId\" = ?", id)
	if result.Error != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, result.Error.Error())
	}
	if result.RowsAffected == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "Environment not found")
	}

	return c.NoContent(http.StatusNoContent)
}
