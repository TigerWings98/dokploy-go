package handler

import (
	"github.com/dokploy/dokploy/internal/auth"
	"github.com/dokploy/dokploy/internal/db"
	mw "github.com/dokploy/dokploy/internal/middleware"
	"github.com/labstack/echo/v4"
)

// Handler holds shared dependencies for all route handlers.
type Handler struct {
	DB   *db.DB
	Auth *auth.Auth
}

// New creates a new Handler.
func New(database *db.DB, a *auth.Auth) *Handler {
	return &Handler{DB: database, Auth: a}
}

// RegisterRoutes registers all API routes.
func (h *Handler) RegisterRoutes(e *echo.Echo) {
	// Public routes
	e.GET("/health", h.HealthCheck)

	// API v1 group with authentication
	api := e.Group("/api/v1")
	api.Use(mw.AuthMiddleware(h.Auth))

	// Project routes
	projects := api.Group("/project")
	h.registerProjectRoutes(projects)

	// Application routes
	applications := api.Group("/application")
	h.registerApplicationRoutes(applications)

	// Compose routes
	compose := api.Group("/compose")
	h.registerComposeRoutes(compose)

	// Database routes
	h.registerPostgresRoutes(api.Group("/postgres"))
	h.registerMySQLRoutes(api.Group("/mysql"))
	h.registerMariaDBRoutes(api.Group("/mariadb"))
	h.registerMongoRoutes(api.Group("/mongo"))
	h.registerRedisRoutes(api.Group("/redis"))

	// Deployment routes
	h.registerDeploymentRoutes(api.Group("/deployment"))

	// Domain routes
	h.registerDomainRoutes(api.Group("/domain"))

	// Server routes
	h.registerServerRoutes(api.Group("/server"))

	// Settings / Admin routes
	admin := api.Group("/admin")
	admin.Use(mw.AdminMiddleware())
	h.registerAdminRoutes(admin)

	// Docker routes
	h.registerDockerRoutes(api.Group("/docker"))

	// User routes
	h.registerUserRoutes(api.Group("/user"))

	// Notification routes
	h.registerNotificationRoutes(api.Group("/notification"))

	// Certificate routes
	h.registerCertificateRoutes(api.Group("/certificate"))

	// Registry routes
	h.registerRegistryRoutes(api.Group("/registry"))

	// SSH Key routes
	h.registerSSHKeyRoutes(api.Group("/ssh-key"))

	// Git Provider routes
	h.registerGitProviderRoutes(api.Group("/git-provider"))

	// Backup routes
	h.registerBackupRoutes(api.Group("/backup"))

	// Destination routes
	h.registerDestinationRoutes(api.Group("/destination"))

	// Security (basic auth) routes
	h.registerSecurityRoutes(api.Group("/security"))

	// Redirect routes
	h.registerRedirectRoutes(api.Group("/redirects"))

	// Port routes
	h.registerPortRoutes(api.Group("/port"))

	// Mount routes
	h.registerMountRoutes(api.Group("/mount"))

	// Schedule routes
	h.registerScheduleRoutes(api.Group("/schedule"))

	// Rollback routes
	h.registerRollbackRoutes(api.Group("/rollback"))

	// Volume Backup routes
	h.registerVolumeBackupRoutes(api.Group("/volume-backups"))

	// Environment routes
	h.registerEnvironmentRoutes(api.Group("/environment"))
}

func (h *Handler) HealthCheck(c echo.Context) error {
	return c.JSON(200, map[string]string{"status": "ok"})
}
