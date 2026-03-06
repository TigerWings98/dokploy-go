package handler

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v4"
)

func (h *Handler) registerDockerRoutes(g *echo.Group) {
	g.GET("/containers", h.ListContainers)
	g.GET("/services", h.ListDockerServices)
	g.POST("/prune", h.DockerPrune)
}

func (h *Handler) ListContainers(c echo.Context) error {
	if h.Docker == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "Docker client not available")
	}

	containers, err := h.Docker.ListContainers(context.Background())
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, containers)
}

func (h *Handler) ListDockerServices(c echo.Context) error {
	if h.Docker == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "Docker client not available")
	}

	services, err := h.Docker.ListServices(context.Background())
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, services)
}

func (h *Handler) DockerPrune(c echo.Context) error {
	if h.Docker == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "Docker client not available")
	}

	if err := h.Docker.PruneSystem(context.Background()); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, map[string]string{"message": "Prune completed"})
}
