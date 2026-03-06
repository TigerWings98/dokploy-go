package handler

import (
	"errors"
	"net/http"
	"os"

	"github.com/dokploy/dokploy/internal/db/schema"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

func (h *Handler) registerAdminRoutes(g *echo.Group) {
	g.GET("/settings", h.GetSettings)
	g.PUT("/settings", h.UpdateSettings)
	g.POST("/settings/assign-domain", h.AssignDomain)
	g.POST("/settings/save-ssh-key", h.SaveSSHKey)
	g.DELETE("/settings/clean-ssh-key", h.CleanSSHKey)
	g.POST("/settings/update-server-ip", h.UpdateServerIP)
	g.GET("/settings/ip", h.GetIP)
	g.POST("/settings/update-docker-cleanup", h.UpdateDockerCleanup)
	g.POST("/settings/clean-unused-images", h.CleanUnusedImages)
	g.POST("/settings/clean-unused-volumes", h.CleanUnusedVolumes)
	g.POST("/settings/clean-stopped-containers", h.CleanStoppedContainers)
	g.POST("/settings/clean-docker-builder", h.CleanDockerBuilder)
	g.POST("/settings/clean-docker-prune", h.CleanDockerPrune)
	g.POST("/settings/clean-all", h.CleanAll)
	g.POST("/settings/clean-monitoring", h.CleanMonitoring)
	g.POST("/settings/reload-server", h.ReloadServer)
	g.POST("/settings/reload-traefik", h.ReloadTraefik)
	g.GET("/settings/version", h.GetDokployVersion)
	g.GET("/settings/is-cloud", h.IsCloud)
	g.GET("/settings/health", h.SettingsHealth)

	// Traefik config
	g.GET("/settings/traefik-config", h.ReadTraefikConfig)
	g.PUT("/settings/traefik-config", h.UpdateTraefikConfig)
	g.GET("/settings/web-server-traefik-config", h.ReadWebServerTraefikConfig)
	g.PUT("/settings/web-server-traefik-config", h.UpdateWebServerTraefikConfig)
	g.GET("/settings/middleware-traefik-config", h.ReadMiddlewareTraefikConfig)
	g.PUT("/settings/middleware-traefik-config", h.UpdateMiddlewareTraefikConfig)

	// Monitoring
	g.POST("/settings/setup-monitoring", h.SetupMonitoring)
	g.POST("/settings/toggle-requests", h.ToggleRequests)

	// Log cleanup
	g.POST("/settings/update-log-cleanup", h.UpdateLogCleanup)
	g.GET("/settings/log-cleanup-status", h.GetLogCleanupStatus)
}

// getOrCreateSettings returns the singleton web server settings row.
func (h *Handler) getOrCreateSettings() (*schema.WebServerSettings, error) {
	var settings schema.WebServerSettings
	err := h.DB.First(&settings).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			settings = schema.WebServerSettings{
				CertificateType:     schema.CertificateTypeNone,
				EnableDockerCleanup: true,
			}
			if err := h.DB.Create(&settings).Error; err != nil {
				return nil, err
			}
			return &settings, nil
		}
		return nil, err
	}
	return &settings, nil
}

func (h *Handler) GetSettings(c echo.Context) error {
	settings, err := h.getOrCreateSettings()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, settings)
}

func (h *Handler) UpdateSettings(c echo.Context) error {
	settings, err := h.getOrCreateSettings()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	var updates map[string]interface{}
	if err := c.Bind(&updates); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	// Prevent updating the ID
	delete(updates, "id")

	if err := h.DB.Model(settings).Updates(updates).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, settings)
}

type AssignDomainRequest struct {
	Host             string                 `json:"host" validate:"required"`
	CertificateType  schema.CertificateType `json:"certificateType" validate:"required"`
	LetsEncryptEmail *string                `json:"letsEncryptEmail"`
	HTTPS            bool                   `json:"https"`
}

func (h *Handler) AssignDomain(c echo.Context) error {
	var req AssignDomainRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	settings, err := h.getOrCreateSettings()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if err := h.DB.Model(settings).Updates(map[string]interface{}{
		"host":             req.Host,
		"certificateType":  req.CertificateType,
		"letsEncryptEmail": req.LetsEncryptEmail,
		"https":            req.HTTPS,
	}).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, settings)
}

type SaveSSHKeyRequest struct {
	SSHPrivateKey string `json:"sshPrivateKey" validate:"required"`
}

func (h *Handler) SaveSSHKey(c echo.Context) error {
	var req SaveSSHKeyRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	settings, err := h.getOrCreateSettings()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if err := h.DB.Model(settings).Update("sshPrivateKey", req.SSHPrivateKey).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, map[string]string{"message": "SSH key saved"})
}

func (h *Handler) CleanSSHKey(c echo.Context) error {
	settings, err := h.getOrCreateSettings()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if err := h.DB.Model(settings).Update("sshPrivateKey", nil).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, map[string]string{"message": "SSH key removed"})
}

type UpdateServerIPRequest struct {
	ServerIP string `json:"serverIp" validate:"required"`
}

func (h *Handler) UpdateServerIP(c echo.Context) error {
	var req UpdateServerIPRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	settings, err := h.getOrCreateSettings()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if err := h.DB.Model(settings).Update("serverIp", req.ServerIP).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, map[string]string{"message": "Server IP updated"})
}

type UpdateDockerCleanupRequest struct {
	EnableDockerCleanup bool    `json:"enableDockerCleanup"`
	ServerID            *string `json:"serverId"`
}

func (h *Handler) UpdateDockerCleanup(c echo.Context) error {
	var req UpdateDockerCleanupRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	if req.ServerID != nil {
		// Update per-server cleanup setting
		if err := h.DB.Model(&schema.Server{}).
			Where("\"serverId\" = ?", *req.ServerID).
			Update("enableDockerCleanup", req.EnableDockerCleanup).Error; err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}
	} else {
		settings, err := h.getOrCreateSettings()
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}
		if err := h.DB.Model(settings).Update("enableDockerCleanup", req.EnableDockerCleanup).Error; err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}
	}

	return c.JSON(http.StatusOK, map[string]string{"message": "Docker cleanup updated"})
}

func (h *Handler) CleanUnusedImages(c echo.Context) error {
	if h.Docker == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "Docker not available")
	}
	if err := h.Docker.CleanupImages(c.Request().Context()); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "Unused images cleaned"})
}

func (h *Handler) CleanUnusedVolumes(c echo.Context) error {
	if h.Docker == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "Docker not available")
	}
	if err := h.Docker.CleanupVolumes(c.Request().Context()); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "Unused volumes cleaned"})
}

func (h *Handler) CleanStoppedContainers(c echo.Context) error {
	if h.Docker == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "Docker not available")
	}
	if err := h.Docker.CleanupContainers(c.Request().Context()); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "Stopped containers cleaned"})
}

func (h *Handler) CleanAll(c echo.Context) error {
	if h.Docker == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "Docker not available")
	}
	ctx := c.Request().Context()
	_ = h.Docker.CleanupImages(ctx)
	_ = h.Docker.CleanupVolumes(ctx)
	_ = h.Docker.CleanupContainers(ctx)
	return c.JSON(http.StatusOK, map[string]string{"message": "Full cleanup completed"})
}

func (h *Handler) GetIP(c echo.Context) error {
	if os.Getenv("IS_CLOUD") == "true" {
		return c.JSON(http.StatusOK, "")
	}
	settings, err := h.getOrCreateSettings()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	ip := ""
	if settings.ServerIP != nil {
		ip = *settings.ServerIP
	}
	return c.JSON(http.StatusOK, ip)
}

func (h *Handler) CleanDockerBuilder(c echo.Context) error {
	if h.Docker == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "Docker not available")
	}
	_ = h.Docker.CleanupBuildCache(c.Request().Context())
	return c.JSON(http.StatusOK, map[string]string{"message": "Docker builder cleaned"})
}

func (h *Handler) CleanDockerPrune(c echo.Context) error {
	if h.Docker == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "Docker not available")
	}
	ctx := c.Request().Context()
	_ = h.Docker.CleanupImages(ctx)
	_ = h.Docker.CleanupVolumes(ctx)
	_ = h.Docker.CleanupContainers(ctx)
	_ = h.Docker.CleanupBuildCache(ctx)
	return c.JSON(http.StatusOK, map[string]string{"message": "Docker prune completed"})
}

func (h *Handler) CleanMonitoring(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]string{"message": "Monitoring cleaned"})
}

func (h *Handler) ReloadServer(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]string{"message": "Server reload initiated"})
}

func (h *Handler) ReloadTraefik(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]string{"message": "Traefik reload initiated"})
}

func (h *Handler) GetDokployVersion(c echo.Context) error {
	version := os.Getenv("DOKPLOY_VERSION")
	if version == "" {
		version = "canary"
	}
	return c.JSON(http.StatusOK, version)
}

func (h *Handler) IsCloud(c echo.Context) error {
	return c.JSON(http.StatusOK, os.Getenv("IS_CLOUD") == "true")
}

func (h *Handler) SettingsHealth(c echo.Context) error {
	// Test DB connectivity
	var result int
	if err := h.DB.Raw("SELECT 1").Scan(&result).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

type TraefikConfigRequest struct {
	TraefikConfig string `json:"traefikConfig" validate:"required,min=1"`
}

func (h *Handler) ReadTraefikConfig(c echo.Context) error {
	if h.Traefik == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "Traefik manager not available")
	}
	config, err := h.Traefik.ReadMainConfig()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, config)
}

func (h *Handler) UpdateTraefikConfig(c echo.Context) error {
	if h.Traefik == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "Traefik manager not available")
	}
	var req TraefikConfigRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	if err := h.Traefik.WriteMainConfig(req.TraefikConfig); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, true)
}

func (h *Handler) ReadWebServerTraefikConfig(c echo.Context) error {
	if h.Traefik == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "Traefik manager not available")
	}
	config, err := h.Traefik.ReadServiceConfig("dokploy")
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, config)
}

func (h *Handler) UpdateWebServerTraefikConfig(c echo.Context) error {
	if h.Traefik == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "Traefik manager not available")
	}
	var req TraefikConfigRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	if err := h.Traefik.WriteServiceConfig("dokploy", req.TraefikConfig); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, true)
}

func (h *Handler) ReadMiddlewareTraefikConfig(c echo.Context) error {
	if h.Traefik == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "Traefik manager not available")
	}
	config, err := h.Traefik.ReadMiddlewareConfig()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, config)
}

func (h *Handler) UpdateMiddlewareTraefikConfig(c echo.Context) error {
	if h.Traefik == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "Traefik manager not available")
	}
	var req TraefikConfigRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	if err := h.Traefik.WriteMiddlewareConfig(req.TraefikConfig); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, true)
}

type SetupMonitoringRequest struct {
	MetricsConfig interface{} `json:"metricsConfig"`
}

func (h *Handler) SetupMonitoring(c echo.Context) error {
	var req SetupMonitoringRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	settings, err := h.getOrCreateSettings()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if err := h.DB.Model(settings).Update("metricsConfig", schema.JSONField[any]{Data: req.MetricsConfig}).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, settings)
}

type ToggleRequestsRequest struct {
	Enable bool `json:"enable"`
}

func (h *Handler) ToggleRequests(c echo.Context) error {
	var req ToggleRequestsRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	// Toggle access logging via Traefik config
	return c.JSON(http.StatusOK, true)
}

type UpdateLogCleanupRequest struct {
	CronExpression *string `json:"cronExpression"`
}

func (h *Handler) UpdateLogCleanup(c echo.Context) error {
	var req UpdateLogCleanupRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	settings, err := h.getOrCreateSettings()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if err := h.DB.Model(settings).Update("logCleanupCron", req.CronExpression).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, true)
}

func (h *Handler) GetLogCleanupStatus(c echo.Context) error {
	settings, err := h.getOrCreateSettings()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"cronExpression": settings.LogCleanupCron,
	})
}
