package handler

import (
	"errors"
	"net/http"

	"github.com/dokploy/dokploy/internal/db/schema"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

func (h *Handler) registerSecurityRoutes(g *echo.Group) {
	g.POST("", h.CreateSecurity)
	g.GET("/:securityId", h.GetSecurity)
	g.PUT("/:securityId", h.UpdateSecurity)
	g.DELETE("/:securityId", h.DeleteSecurity)
}

type CreateSecurityRequest struct {
	Username      string  `json:"username" validate:"required"`
	Password      string  `json:"password" validate:"required"`
	ApplicationID *string `json:"applicationId"`
	ComposeID     *string `json:"composeId"`
}

func (h *Handler) CreateSecurity(c echo.Context) error {
	var req CreateSecurityRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	s := &schema.Security{
		Username:      req.Username,
		Password:      req.Password,
		ApplicationID: req.ApplicationID,
		ComposeID:     req.ComposeID,
	}

	if err := h.DB.Create(s).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	// TODO: Update Traefik basic auth middleware

	return c.JSON(http.StatusCreated, s)
}

func (h *Handler) GetSecurity(c echo.Context) error {
	id := c.Param("securityId")

	var s schema.Security
	if err := h.DB.First(&s, "\"securityId\" = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Security not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, s)
}

func (h *Handler) UpdateSecurity(c echo.Context) error {
	id := c.Param("securityId")

	var updates map[string]interface{}
	if err := c.Bind(&updates); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	var s schema.Security
	if err := h.DB.First(&s, "\"securityId\" = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Security not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if err := h.DB.Model(&s).Updates(updates).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	// TODO: Update Traefik basic auth middleware

	return c.JSON(http.StatusOK, s)
}

func (h *Handler) DeleteSecurity(c echo.Context) error {
	id := c.Param("securityId")

	result := h.DB.Delete(&schema.Security{}, "\"securityId\" = ?", id)
	if result.Error != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, result.Error.Error())
	}
	if result.RowsAffected == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "Security not found")
	}

	// TODO: Update Traefik basic auth middleware

	return c.NoContent(http.StatusNoContent)
}
