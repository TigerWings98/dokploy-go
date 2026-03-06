package handler

import (
	"errors"
	"net/http"

	"github.com/dokploy/dokploy/internal/db/schema"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

func (h *Handler) registerMariaDBRoutes(g *echo.Group) {
	g.POST("", h.CreateMariaDB)
	g.GET("/:mariadbId", h.GetMariaDB)
	g.PUT("/:mariadbId", h.UpdateMariaDB)
	g.DELETE("/:mariadbId", h.DeleteMariaDB)
	g.POST("/:mariadbId/deploy", h.DeployMariaDB)
	g.POST("/:mariadbId/stop", h.StopMariaDB)
	g.POST("/:mariadbId/start", h.StartMariaDB)
	g.POST("/:mariadbId/reload", h.ReloadMariaDB)
	g.POST("/:mariadbId/rebuild", h.RebuildMariaDB)
	g.POST("/:mariadbId/change-status", h.ChangeMariaDBStatus)
	g.POST("/:mariadbId/save-external-port", h.SaveMariaDBExternalPort)
	g.POST("/:mariadbId/save-environment", h.SaveMariaDBEnvironment)
	g.POST("/:mariadbId/move", h.MoveMariaDB)
}

type CreateMariaDBRequest struct {
	Name                 string  `json:"name" validate:"required"`
	Description          *string `json:"description"`
	DatabaseName         string  `json:"databaseName" validate:"required"`
	DatabaseUser         string  `json:"databaseUser" validate:"required"`
	DatabasePassword     string  `json:"databasePassword" validate:"required"`
	DatabaseRootPassword string  `json:"databaseRootPassword" validate:"required"`
	DockerImage          string  `json:"dockerImage" validate:"required"`
	EnvironmentID        string  `json:"environmentId" validate:"required"`
	ServerID             *string `json:"serverId"`
}

func (h *Handler) CreateMariaDB(c echo.Context) error {
	var req CreateMariaDBRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	m := &schema.MariaDB{
		Name:                 req.Name,
		Description:          req.Description,
		DatabaseName:         req.DatabaseName,
		DatabaseUser:         req.DatabaseUser,
		DatabasePassword:     req.DatabasePassword,
		DatabaseRootPassword: req.DatabaseRootPassword,
		DockerImage:          req.DockerImage,
		EnvironmentID:        req.EnvironmentID,
		ServerID:             req.ServerID,
	}

	if err := h.DB.Create(m).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusCreated, m)
}

func (h *Handler) GetMariaDB(c echo.Context) error {
	id := c.Param("mariadbId")

	var m schema.MariaDB
	err := h.DB.Preload("Mounts").Preload("Backups").First(&m, "\"mariadbId\" = ?", id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "MariaDB not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, m)
}

func (h *Handler) UpdateMariaDB(c echo.Context) error {
	id := c.Param("mariadbId")

	var updates map[string]interface{}
	if err := c.Bind(&updates); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	var m schema.MariaDB
	if err := h.DB.First(&m, "\"mariadbId\" = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "MariaDB not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if err := h.DB.Model(&m).Updates(updates).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, m)
}

func (h *Handler) DeleteMariaDB(c echo.Context) error {
	id := c.Param("mariadbId")

	result := h.DB.Delete(&schema.MariaDB{}, "\"mariadbId\" = ?", id)
	if result.Error != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, result.Error.Error())
	}
	if result.RowsAffected == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "MariaDB not found")
	}

	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) DeployMariaDB(c echo.Context) error {
	id := c.Param("mariadbId")

	var m schema.MariaDB
	if err := h.DB.First(&m, "\"mariadbId\" = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "MariaDB not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if h.Queue != nil {
		info, err := h.Queue.EnqueueDeployDatabase(id, "mariadb")
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}
		return c.JSON(http.StatusOK, map[string]string{"message": "Deployment queued", "taskId": info.ID})
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "Deployment queued"})
}

func (h *Handler) StopMariaDB(c echo.Context) error {
	id := c.Param("mariadbId")

	var m schema.MariaDB
	if err := h.DB.First(&m, "\"mariadbId\" = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "MariaDB not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if h.Queue != nil {
		info, err := h.Queue.EnqueueStopDatabase(id, "mariadb")
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}
		return c.JSON(http.StatusOK, map[string]string{"message": "Stop queued", "taskId": info.ID})
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "Stop queued"})
}

func (h *Handler) StartMariaDB(c echo.Context) error {
	return h.dbStart(c, "mariadb", "mariadbId", "mariadbId")
}
func (h *Handler) ReloadMariaDB(c echo.Context) error {
	return h.dbReload(c, "mariadb", "mariadbId", "mariadbId")
}
func (h *Handler) RebuildMariaDB(c echo.Context) error {
	return h.dbRebuild(c, "mariadb", "mariadbId")
}
func (h *Handler) ChangeMariaDBStatus(c echo.Context) error {
	return h.dbChangeStatus(c, "mariadb", "mariadbId", "mariadbId")
}
func (h *Handler) SaveMariaDBExternalPort(c echo.Context) error {
	return h.dbSaveExternalPort(c, "mariadb", "mariadbId", "mariadbId", "mariadb")
}
func (h *Handler) SaveMariaDBEnvironment(c echo.Context) error {
	return h.dbSaveEnvironment(c, "mariadb", "mariadbId", "mariadbId", "mariadb")
}
func (h *Handler) MoveMariaDB(c echo.Context) error {
	return h.dbMove(c, "mariadb", "mariadbId", "mariadbId", "mariadb")
}
