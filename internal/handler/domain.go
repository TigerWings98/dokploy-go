package handler

import (
	"errors"
	"net/http"

	"github.com/dokploy/dokploy/internal/db/schema"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

func (h *Handler) registerDomainRoutes(g *echo.Group) {
	g.POST("", h.CreateDomain)
	g.GET("/:domainId", h.GetDomain)
	g.PUT("/:domainId", h.UpdateDomain)
	g.DELETE("/:domainId", h.DeleteDomain)
}

type CreateDomainRequest struct {
	Host              string  `json:"host" validate:"required"`
	HTTPS             bool    `json:"https"`
	Port              *int    `json:"port"`
	Path              *string `json:"path"`
	CertificateType   string  `json:"certificateType"`
	ApplicationID     *string `json:"applicationId"`
	ComposeID         *string `json:"composeId"`
	ServiceName       *string `json:"serviceName"`
}

func (h *Handler) CreateDomain(c echo.Context) error {
	var req CreateDomainRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	domain := &schema.Domain{
		Host:            req.Host,
		HTTPS:           req.HTTPS,
		Port:            req.Port,
		Path:            req.Path,
		CertificateType: schema.CertificateType(req.CertificateType),
		ApplicationID:   req.ApplicationID,
		ComposeID:       req.ComposeID,
		ServiceName:     req.ServiceName,
	}

	if err := h.DB.Create(domain).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	// TODO: Generate Traefik config for domain

	return c.JSON(http.StatusCreated, domain)
}

func (h *Handler) GetDomain(c echo.Context) error {
	domainID := c.Param("domainId")

	var domain schema.Domain
	if err := h.DB.First(&domain, "\"domainId\" = ?", domainID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Domain not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, domain)
}

func (h *Handler) UpdateDomain(c echo.Context) error {
	domainID := c.Param("domainId")

	var updates map[string]interface{}
	if err := c.Bind(&updates); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	var domain schema.Domain
	if err := h.DB.First(&domain, "\"domainId\" = ?", domainID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Domain not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if err := h.DB.Model(&domain).Updates(updates).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	// TODO: Update Traefik config

	return c.JSON(http.StatusOK, domain)
}

func (h *Handler) DeleteDomain(c echo.Context) error {
	domainID := c.Param("domainId")

	var domain schema.Domain
	if err := h.DB.First(&domain, "\"domainId\" = ?", domainID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Domain not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if err := h.DB.Delete(&domain).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	// TODO: Remove Traefik config

	return c.NoContent(http.StatusNoContent)
}
