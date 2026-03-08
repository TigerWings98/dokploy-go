// Input: db (Domain 表), traefik (路由生成)
// Output: Domain CRUD + Traefik 配置自动生成/清理的 tRPC procedure 实现
// Role: 域名路由管理 handler，创建/更新/删除域名时自动同步 Traefik 动态配置文件
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package handler

import (
	"errors"
	"log"
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
	Host            string  `json:"host" validate:"required"`
	HTTPS           bool    `json:"https"`
	Port            *int    `json:"port"`
	Path            *string `json:"path"`
	CertificateType string  `json:"certificateType"`
	ApplicationID   *string `json:"applicationId"`
	ComposeID       *string `json:"composeId"`
	ServiceName     *string `json:"serviceName"`
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

	h.generateTraefikForDomain(domain)

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

	// Reload and regenerate traefik config
	h.DB.First(&domain, "\"domainId\" = ?", domainID)
	h.generateTraefikForDomain(&domain)

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

	appName := h.resolveAppName(&domain)

	if err := h.DB.Delete(&domain).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	// Remove Traefik config if no other domains exist for this app
	if h.Traefik != nil && appName != "" {
		var count int64
		if domain.ApplicationID != nil {
			h.DB.Model(&schema.Domain{}).Where("\"applicationId\" = ?", *domain.ApplicationID).Count(&count)
		} else if domain.ComposeID != nil {
			h.DB.Model(&schema.Domain{}).Where("\"composeId\" = ?", *domain.ComposeID).Count(&count)
		}
		if count == 0 {
			if err := h.Traefik.RemoveApplicationConfig(appName); err != nil {
				log.Printf("Failed to remove traefik config for %s: %v", appName, err)
			}
		}
	}

	return c.NoContent(http.StatusNoContent)
}

// generateTraefikForDomain creates/updates Traefik config for a domain.
func (h *Handler) generateTraefikForDomain(domain *schema.Domain) {
	if h.Traefik == nil {
		return
	}

	appName := h.resolveAppName(domain)
	if appName == "" {
		return
	}

	port := 3000
	if domain.Port != nil {
		port = *domain.Port
	}

	certType := string(domain.CertificateType)
	if err := h.Traefik.CreateApplicationConfig(appName, domain.Host, port, domain.HTTPS, certType); err != nil {
		log.Printf("Failed to create traefik config for %s: %v", appName, err)
	}

	if domain.HTTPS {
		if err := h.Traefik.AddHTTPSRedirect(appName, domain.Host); err != nil {
			log.Printf("Failed to add HTTPS redirect for %s: %v", appName, err)
		}
	}
}

// resolveAppName finds the appName from the domain's associated application or compose.
func (h *Handler) resolveAppName(domain *schema.Domain) string {
	if domain.ApplicationID != nil {
		var app schema.Application
		if err := h.DB.First(&app, "\"applicationId\" = ?", *domain.ApplicationID).Error; err == nil {
			return app.AppName
		}
	}
	if domain.ComposeID != nil {
		var compose schema.Compose
		if err := h.DB.First(&compose, "\"composeId\" = ?", *domain.ComposeID).Error; err == nil {
			return compose.AppName
		}
	}
	return ""
}
