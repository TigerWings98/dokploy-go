package handler

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

func (h *Handler) registerAdminRoutes(g *echo.Group) {
	g.GET("/settings", h.GetSettings)
	g.PUT("/settings", h.UpdateSettings)
}

func (h *Handler) GetSettings(c echo.Context) error {
	// TODO: Return admin settings
	return c.JSON(http.StatusOK, map[string]interface{}{
		"message": "admin settings",
	})
}

func (h *Handler) UpdateSettings(c echo.Context) error {
	// TODO: Update admin settings
	return c.JSON(http.StatusOK, map[string]interface{}{
		"message": "settings updated",
	})
}
