package handler

import (
	"errors"
	"net/http"

	"github.com/dokploy/dokploy/internal/db/schema"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

func (h *Handler) registerMySQLRoutes(g *echo.Group) {
	g.POST("", h.CreateMySQL)
	g.GET("/:mysqlId", h.GetMySQL)
	g.PUT("/:mysqlId", h.UpdateMySQL)
	g.DELETE("/:mysqlId", h.DeleteMySQL)
	g.POST("/:mysqlId/deploy", h.DeployMySQL)
	g.POST("/:mysqlId/stop", h.StopMySQL)
	g.POST("/:mysqlId/start", h.StartMySQL)
	g.POST("/:mysqlId/reload", h.ReloadMySQL)
	g.POST("/:mysqlId/rebuild", h.RebuildMySQL)
	g.POST("/:mysqlId/change-status", h.ChangeMySQLStatus)
	g.POST("/:mysqlId/save-external-port", h.SaveMySQLExternalPort)
	g.POST("/:mysqlId/save-environment", h.SaveMySQLEnvironment)
	g.POST("/:mysqlId/move", h.MoveMySQL)
}

type CreateMySQLRequest struct {
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

func (h *Handler) CreateMySQL(c echo.Context) error {
	var req CreateMySQLRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	m := &schema.MySQL{
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

func (h *Handler) GetMySQL(c echo.Context) error {
	id := c.Param("mysqlId")

	var m schema.MySQL
	err := h.DB.Preload("Mounts").Preload("Backups").First(&m, "\"mysqlId\" = ?", id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "MySQL not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, m)
}

func (h *Handler) UpdateMySQL(c echo.Context) error {
	id := c.Param("mysqlId")

	var updates map[string]interface{}
	if err := c.Bind(&updates); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	var m schema.MySQL
	if err := h.DB.First(&m, "\"mysqlId\" = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "MySQL not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if err := h.DB.Model(&m).Updates(updates).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, m)
}

func (h *Handler) DeleteMySQL(c echo.Context) error {
	id := c.Param("mysqlId")

	result := h.DB.Delete(&schema.MySQL{}, "\"mysqlId\" = ?", id)
	if result.Error != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, result.Error.Error())
	}
	if result.RowsAffected == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "MySQL not found")
	}

	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) DeployMySQL(c echo.Context) error {
	id := c.Param("mysqlId")

	var m schema.MySQL
	if err := h.DB.First(&m, "\"mysqlId\" = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "MySQL not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if h.Queue != nil {
		info, err := h.Queue.EnqueueDeployDatabase(id, "mysql")
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}
		return c.JSON(http.StatusOK, map[string]string{"message": "Deployment queued", "taskId": info.ID})
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "Deployment queued"})
}

func (h *Handler) StopMySQL(c echo.Context) error {
	id := c.Param("mysqlId")

	var m schema.MySQL
	if err := h.DB.First(&m, "\"mysqlId\" = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "MySQL not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if h.Queue != nil {
		info, err := h.Queue.EnqueueStopDatabase(id, "mysql")
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}
		return c.JSON(http.StatusOK, map[string]string{"message": "Stop queued", "taskId": info.ID})
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "Stop queued"})
}

func (h *Handler) StartMySQL(c echo.Context) error {
	return h.dbStart(c, "mysql", "mysqlId", "mysqlId")
}
func (h *Handler) ReloadMySQL(c echo.Context) error {
	return h.dbReload(c, "mysql", "mysqlId", "mysqlId")
}
func (h *Handler) RebuildMySQL(c echo.Context) error {
	return h.dbRebuild(c, "mysql", "mysqlId")
}
func (h *Handler) ChangeMySQLStatus(c echo.Context) error {
	return h.dbChangeStatus(c, "mysql", "mysqlId", "mysqlId")
}
func (h *Handler) SaveMySQLExternalPort(c echo.Context) error {
	return h.dbSaveExternalPort(c, "mysql", "mysqlId", "mysqlId", "mysql")
}
func (h *Handler) SaveMySQLEnvironment(c echo.Context) error {
	return h.dbSaveEnvironment(c, "mysql", "mysqlId", "mysqlId", "mysql")
}
func (h *Handler) MoveMySQL(c echo.Context) error {
	return h.dbMove(c, "mysql", "mysqlId", "mysqlId", "mysql")
}
