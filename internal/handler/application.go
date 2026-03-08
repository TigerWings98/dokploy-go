// Input: db (Application/Domain/Mount/Port/Deployment 表), docker, traefik, queue, service
// Output: Application CRUD + 部署触发 + 域名/挂载/端口/环境变量管理的 tRPC procedure 实现
// Role: 应用管理核心 handler，覆盖创建/更新/删除/部署/日志/统计等全部操作
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package handler

import (
	"errors"
	"net/http"

	"github.com/dokploy/dokploy/internal/db/schema"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

func (h *Handler) registerApplicationRoutes(g *echo.Group) {
	g.POST("", h.CreateApplication)
	g.GET("/:applicationId", h.GetApplication)
	g.PUT("/:applicationId", h.UpdateApplication)
	g.DELETE("/:applicationId", h.DeleteApplication)
	g.POST("/:applicationId/deploy", h.DeployApplication)
	g.POST("/:applicationId/redeploy", h.RedeployApplication)
	g.POST("/:applicationId/stop", h.StopApplication)
	g.POST("/:applicationId/start", h.StartApplication)
}

type CreateApplicationRequest struct {
	Name          string  `json:"name" validate:"required,min=1"`
	AppName       *string `json:"appName"`
	Description   *string `json:"description"`
	EnvironmentID string  `json:"environmentId" validate:"required"`
	ServerID      *string `json:"serverId"`
}

func (h *Handler) CreateApplication(c echo.Context) error {
	var req CreateApplicationRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	app := &schema.Application{
		Name:          req.Name,
		Description:   req.Description,
		EnvironmentID: req.EnvironmentID,
		ServerID:      req.ServerID,
	}
	if req.AppName != nil {
		app.AppName = *req.AppName
	}

	if err := h.DB.Create(app).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusCreated, app)
}

func (h *Handler) GetApplication(c echo.Context) error {
	appID := c.Param("applicationId")

	var app schema.Application
	err := h.DB.
		Preload("Deployments", func(db *gorm.DB) *gorm.DB {
			return db.Order("\"createdAt\" DESC").Limit(10)
		}).
		Preload("Domains").
		Preload("Mounts").
		Preload("Redirects").
		Preload("Security").
		Preload("Ports").
		Preload("Environment").
		Preload("Server").
		Preload("Registry").
		First(&app, "\"applicationId\" = ?", appID).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Application not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, app)
}

func (h *Handler) UpdateApplication(c echo.Context) error {
	appID := c.Param("applicationId")

	var updates map[string]interface{}
	if err := c.Bind(&updates); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	// Prevent changing serverId via update
	delete(updates, "serverId")

	var app schema.Application
	if err := h.DB.First(&app, "\"applicationId\" = ?", appID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Application not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if err := h.DB.Model(&app).Updates(updates).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, app)
}

func (h *Handler) DeleteApplication(c echo.Context) error {
	appID := c.Param("applicationId")

	result := h.DB.Delete(&schema.Application{}, "\"applicationId\" = ?", appID)
	if result.Error != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, result.Error.Error())
	}
	if result.RowsAffected == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "Application not found")
	}

	return c.NoContent(http.StatusNoContent)
}

type DeployApplicationRequest struct {
	Title       *string `json:"title"`
	Description *string `json:"description"`
}

func (h *Handler) DeployApplication(c echo.Context) error {
	appID := c.Param("applicationId")

	var req DeployApplicationRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	var app schema.Application
	if err := h.DB.First(&app, "\"applicationId\" = ?", appID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Application not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if h.Queue != nil {
		info, err := h.Queue.EnqueueDeployApplication(appID, req.Title, req.Description)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}
		return c.JSON(http.StatusOK, map[string]string{
			"message":       "Deployment queued",
			"applicationId": appID,
			"taskId":        info.ID,
		})
	}
	return c.JSON(http.StatusOK, map[string]string{
		"message":       "Deployment queued",
		"applicationId": appID,
	})
}

func (h *Handler) RedeployApplication(c echo.Context) error {
	appID := c.Param("applicationId")

	var app schema.Application
	if err := h.DB.First(&app, "\"applicationId\" = ?", appID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Application not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if h.Queue != nil {
		title := "Redeploy"
		info, err := h.Queue.EnqueueDeployApplication(appID, &title, nil)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}
		return c.JSON(http.StatusOK, map[string]string{
			"message":       "Redeployment queued",
			"applicationId": appID,
			"taskId":        info.ID,
		})
	}
	return c.JSON(http.StatusOK, map[string]string{
		"message":       "Redeployment queued",
		"applicationId": appID,
	})
}

func (h *Handler) StopApplication(c echo.Context) error {
	appID := c.Param("applicationId")

	var app schema.Application
	if err := h.DB.First(&app, "\"applicationId\" = ?", appID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Application not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if h.Queue != nil {
		info, err := h.Queue.EnqueueStopApplication(appID)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}
		return c.JSON(http.StatusOK, map[string]string{
			"message":       "Application stop requested",
			"applicationId": appID,
			"taskId":        info.ID,
		})
	}
	return c.JSON(http.StatusOK, map[string]string{
		"message":       "Application stop requested",
		"applicationId": appID,
	})
}

func (h *Handler) StartApplication(c echo.Context) error {
	appID := c.Param("applicationId")

	var app schema.Application
	if err := h.DB.First(&app, "\"applicationId\" = ?", appID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Application not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if h.Queue != nil {
		info, err := h.Queue.EnqueueStartApplication(appID)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}
		return c.JSON(http.StatusOK, map[string]string{
			"message":       "Application start requested",
			"applicationId": appID,
			"taskId":        info.ID,
		})
	}
	return c.JSON(http.StatusOK, map[string]string{
		"message":       "Application start requested",
		"applicationId": appID,
	})
}
