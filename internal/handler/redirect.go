// Input: db (Redirect 表), traefik
// Output: Redirect CRUD 的 tRPC procedure 实现
// Role: URL 重定向规则管理 handler，同步更新 Traefik 配置
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package handler

import (
	"errors"
	"log"
	"net/http"

	"github.com/dokploy/dokploy/internal/db/schema"
	"github.com/dokploy/dokploy/internal/traefik"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

func (h *Handler) registerRedirectRoutes(g *echo.Group) {
	g.POST("", h.CreateRedirect)
	g.GET("/:redirectId", h.GetRedirect)
	g.PUT("/:redirectId", h.UpdateRedirect)
	g.DELETE("/:redirectId", h.DeleteRedirect)
}

type CreateRedirectRequest struct {
	Regex         string  `json:"regex" validate:"required"`
	Replacement   string  `json:"replacement" validate:"required"`
	Permanent     bool    `json:"permanent"`
	ApplicationID *string `json:"applicationId"`
	ComposeID     *string `json:"composeId"`
}

func (h *Handler) CreateRedirect(c echo.Context) error {
	var req CreateRedirectRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	r := &schema.Redirect{
		Regex:         req.Regex,
		Replacement:   req.Replacement,
		Permanent:     req.Permanent,
		ApplicationID: req.ApplicationID,
		ComposeID:     req.ComposeID,
	}

	if err := h.DB.Create(r).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	h.syncRedirectsTraefik(r.ApplicationID, r.ComposeID)

	return c.JSON(http.StatusCreated, r)
}

func (h *Handler) GetRedirect(c echo.Context) error {
	id := c.Param("redirectId")

	var r schema.Redirect
	if err := h.DB.First(&r, "\"redirectId\" = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Redirect not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, r)
}

func (h *Handler) UpdateRedirect(c echo.Context) error {
	id := c.Param("redirectId")

	var updates map[string]interface{}
	if err := c.Bind(&updates); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	var r schema.Redirect
	if err := h.DB.First(&r, "\"redirectId\" = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Redirect not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if err := h.DB.Model(&r).Updates(updates).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	h.syncRedirectsTraefik(r.ApplicationID, r.ComposeID)

	return c.JSON(http.StatusOK, r)
}

func (h *Handler) DeleteRedirect(c echo.Context) error {
	id := c.Param("redirectId")

	var r schema.Redirect
	if err := h.DB.First(&r, "\"redirectId\" = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Redirect not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	appID := r.ApplicationID
	composeID := r.ComposeID

	if err := h.DB.Delete(&r).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	h.syncRedirectsTraefik(appID, composeID)

	return c.NoContent(http.StatusNoContent)
}

// syncRedirectsTraefik loads all redirects for an app/compose and updates Traefik.
func (h *Handler) syncRedirectsTraefik(applicationID, composeID *string) {
	if h.Traefik == nil {
		return
	}

	var appName string
	var redirects []schema.Redirect

	if applicationID != nil {
		var app schema.Application
		if err := h.DB.First(&app, "\"applicationId\" = ?", *applicationID).Error; err == nil {
			appName = app.AppName
		}
		h.DB.Where("\"applicationId\" = ?", *applicationID).Find(&redirects)
	} else if composeID != nil {
		var compose schema.Compose
		if err := h.DB.First(&compose, "\"composeId\" = ?", *composeID).Error; err == nil {
			appName = compose.AppName
		}
		h.DB.Where("\"composeId\" = ?", *composeID).Find(&redirects)
	}

	if appName == "" {
		return
	}

	entries := make([]traefik.RedirectEntry, len(redirects))
	for i, r := range redirects {
		entries[i] = traefik.RedirectEntry{
			Regex:       r.Regex,
			Replacement: r.Replacement,
			Permanent:   r.Permanent,
		}
	}

	if err := h.Traefik.UpdateRedirects(appName, entries); err != nil {
		log.Printf("Failed to update traefik redirects for %s: %v", appName, err)
	}
}
