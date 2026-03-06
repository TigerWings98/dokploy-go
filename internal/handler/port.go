package handler

import (
	"errors"
	"net/http"

	"github.com/dokploy/dokploy/internal/db/schema"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

func (h *Handler) registerPortRoutes(g *echo.Group) {
	g.POST("", h.CreatePort)
	g.GET("/:portId", h.GetPort)
	g.PUT("/:portId", h.UpdatePort)
	g.DELETE("/:portId", h.DeletePort)
}

type CreatePortRequest struct {
	PublishedPort int     `json:"publishedPort" validate:"required"`
	TargetPort    int     `json:"targetPort" validate:"required"`
	Protocol      string  `json:"protocol"`
	ApplicationID *string `json:"applicationId"`
}

func (h *Handler) CreatePort(c echo.Context) error {
	var req CreatePortRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	protocol := schema.ProtocolType("tcp")
	if req.Protocol != "" {
		protocol = schema.ProtocolType(req.Protocol)
	}

	p := &schema.Port{
		PublishedPort: req.PublishedPort,
		TargetPort:    req.TargetPort,
		Protocol:      protocol,
		ApplicationID: req.ApplicationID,
	}

	if err := h.DB.Create(p).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusCreated, p)
}

func (h *Handler) GetPort(c echo.Context) error {
	id := c.Param("portId")

	var p schema.Port
	if err := h.DB.First(&p, "\"portId\" = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Port not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, p)
}

func (h *Handler) UpdatePort(c echo.Context) error {
	id := c.Param("portId")

	var updates map[string]interface{}
	if err := c.Bind(&updates); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	var p schema.Port
	if err := h.DB.First(&p, "\"portId\" = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Port not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if err := h.DB.Model(&p).Updates(updates).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, p)
}

func (h *Handler) DeletePort(c echo.Context) error {
	id := c.Param("portId")

	result := h.DB.Delete(&schema.Port{}, "\"portId\" = ?", id)
	if result.Error != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, result.Error.Error())
	}
	if result.RowsAffected == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "Port not found")
	}

	return c.NoContent(http.StatusNoContent)
}
