package handler

import (
	"errors"
	"net/http"

	"github.com/dokploy/dokploy/internal/db/schema"
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

	// TODO: Update Traefik redirect middleware

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

	// TODO: Update Traefik redirect middleware

	return c.JSON(http.StatusOK, r)
}

func (h *Handler) DeleteRedirect(c echo.Context) error {
	id := c.Param("redirectId")

	result := h.DB.Delete(&schema.Redirect{}, "\"redirectId\" = ?", id)
	if result.Error != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, result.Error.Error())
	}
	if result.RowsAffected == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "Redirect not found")
	}

	// TODO: Update Traefik redirect middleware

	return c.NoContent(http.StatusNoContent)
}
