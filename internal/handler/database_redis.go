// Input: db (Redis 表)
// Output: Redis 数据库服务 CRUD 的 tRPC procedure 实现
// Role: Redis 数据库服务专属 handler
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
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
	g.POST("/:redisId/start", h.StartRedis)
	g.POST("/:redisId/reload", h.ReloadRedis)
	g.POST("/:redisId/rebuild", h.RebuildRedis)
	g.POST("/:redisId/change-status", h.ChangeRedisStatus)
	g.POST("/:redisId/save-external-port", h.SaveRedisExternalPort)
	g.POST("/:redisId/save-environment", h.SaveRedisEnvironment)
	g.POST("/:redisId/move", h.MoveRedis)
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

	if h.DBSvc != nil {
		go h.DBSvc.DeployRedis(id, nil)
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "Deployment started"})
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

	if h.DBSvc != nil {
		go h.DBSvc.StopDatabase(id, schema.DatabaseTypeRedis)
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "Stop started"})
}

func (h *Handler) StartRedis(c echo.Context) error {
	return h.dbStart(c, "redis", "redisId", "redisId")
}
func (h *Handler) ReloadRedis(c echo.Context) error {
	return h.dbReload(c, "redis", "redisId", "redisId")
}
func (h *Handler) RebuildRedis(c echo.Context) error {
	return h.dbRebuild(c, "redis", "redisId")
}
func (h *Handler) ChangeRedisStatus(c echo.Context) error {
	return h.dbChangeStatus(c, "redis", "redisId", "redisId")
}
func (h *Handler) SaveRedisExternalPort(c echo.Context) error {
	return h.dbSaveExternalPort(c, "redis", "redisId", "redisId", "redis")
}
func (h *Handler) SaveRedisEnvironment(c echo.Context) error {
	return h.dbSaveEnvironment(c, "redis", "redisId", "redisId", "redis")
}
func (h *Handler) MoveRedis(c echo.Context) error {
	return h.dbMove(c, "redis", "redisId", "redisId", "redis")
}
