// Input: db (Postgres 表)
// Output: PostgreSQL 数据库服务 CRUD 的 tRPC procedure 实现
// Role: PostgreSQL 数据库服务专属 handler，处理创建/更新/部署等操作
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
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
	g.POST("/:postgresId/start", h.StartPostgres)
	g.POST("/:postgresId/reload", h.ReloadPostgres)
	g.POST("/:postgresId/rebuild", h.RebuildPostgres)
	g.POST("/:postgresId/change-status", h.ChangePostgresStatus)
	g.POST("/:postgresId/save-external-port", h.SavePostgresExternalPort)
	g.POST("/:postgresId/save-environment", h.SavePostgresEnvironment)
	g.POST("/:postgresId/move", h.MovePostgres)
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

	// 与 TS 版对齐：内联执行，不走队列
	if h.DBSvc != nil {
		go h.DBSvc.DeployPostgres(id, nil)
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "Deployment started"})
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

	// 与 TS 版对齐：内联执行，不走队列
	if h.DBSvc != nil {
		go h.DBSvc.StopDatabase(id, schema.DatabaseTypePostgres)
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "Stop started"})
}

func (h *Handler) StartPostgres(c echo.Context) error {
	return h.dbStart(c, "postgres", "postgresId", "postgresId")
}
func (h *Handler) ReloadPostgres(c echo.Context) error {
	return h.dbReload(c, "postgres", "postgresId", "postgresId")
}
func (h *Handler) RebuildPostgres(c echo.Context) error {
	return h.dbRebuild(c, "postgres", "postgresId")
}
func (h *Handler) ChangePostgresStatus(c echo.Context) error {
	return h.dbChangeStatus(c, "postgres", "postgresId", "postgresId")
}
func (h *Handler) SavePostgresExternalPort(c echo.Context) error {
	return h.dbSaveExternalPort(c, "postgres", "postgresId", "postgresId", "postgres")
}
func (h *Handler) SavePostgresEnvironment(c echo.Context) error {
	return h.dbSaveEnvironment(c, "postgres", "postgresId", "postgresId", "postgres")
}
func (h *Handler) MovePostgres(c echo.Context) error {
	return h.dbMove(c, "postgres", "postgresId", "postgresId", "postgres")
}
