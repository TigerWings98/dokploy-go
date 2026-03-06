package handler

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

func (h *Handler) registerDockerRoutes(g *echo.Group) {
	g.GET("/containers", h.ListContainers)
	g.GET("/services", h.ListDockerServices)
	g.POST("/prune", h.DockerPrune)
}

func (h *Handler) ListContainers(c echo.Context) error {
	// TODO: List Docker containers using Docker SDK
	return c.JSON(http.StatusOK, []interface{}{})
}

func (h *Handler) ListDockerServices(c echo.Context) error {
	// TODO: List Docker services
	return c.JSON(http.StatusOK, []interface{}{})
}

func (h *Handler) DockerPrune(c echo.Context) error {
	// TODO: Docker system prune
	return c.JSON(http.StatusOK, map[string]string{
		"message": "Prune requested",
	})
}
