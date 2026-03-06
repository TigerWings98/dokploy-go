package handler

import (
	"errors"
	"net/http"

	"github.com/dokploy/dokploy/internal/db/schema"
	mw "github.com/dokploy/dokploy/internal/middleware"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

func (h *Handler) registerCertificateRoutes(g *echo.Group) {
	g.POST("", h.CreateCertificate)
	g.GET("/:certificateId", h.GetCertificate)
	g.GET("", h.ListCertificates)
	g.DELETE("/:certificateId", h.DeleteCertificate)
}

type CreateCertificateRequest struct {
	Name            string  `json:"name" validate:"required"`
	CertificateData string  `json:"certificateData" validate:"required"`
	PrivateKey      string  `json:"privateKey" validate:"required"`
	AutoRenew       *bool   `json:"autoRenew"`
	ServerID        *string `json:"serverId"`
}

func (h *Handler) CreateCertificate(c echo.Context) error {
	var req CreateCertificateRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	user := mw.GetUser(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized)
	}

	var member schema.Member
	if err := h.DB.Where("user_id = ? AND is_default = ?", user.ID, true).First(&member).Error; err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "No default organization found")
	}

	cert := &schema.Certificate{
		Name:            req.Name,
		CertificateData: req.CertificateData,
		PrivateKey:      req.PrivateKey,
		AutoRenew:       req.AutoRenew,
		ServerID:        req.ServerID,
		OrganizationID:  member.OrganizationID,
	}

	if err := h.DB.Create(cert).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	// TODO: Write cert files to disk for Traefik

	return c.JSON(http.StatusCreated, cert)
}

func (h *Handler) GetCertificate(c echo.Context) error {
	id := c.Param("certificateId")

	var cert schema.Certificate
	if err := h.DB.First(&cert, "\"certificateId\" = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Certificate not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, cert)
}

func (h *Handler) ListCertificates(c echo.Context) error {
	user := mw.GetUser(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized)
	}

	var member schema.Member
	if err := h.DB.Where("user_id = ? AND is_default = ?", user.ID, true).First(&member).Error; err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "No default organization found")
	}

	var certs []schema.Certificate
	if err := h.DB.Where("\"organizationId\" = ?", member.OrganizationID).Find(&certs).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, certs)
}

func (h *Handler) DeleteCertificate(c echo.Context) error {
	id := c.Param("certificateId")

	result := h.DB.Delete(&schema.Certificate{}, "\"certificateId\" = ?", id)
	if result.Error != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, result.Error.Error())
	}
	if result.RowsAffected == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "Certificate not found")
	}

	// TODO: Remove cert files from disk

	return c.NoContent(http.StatusNoContent)
}
