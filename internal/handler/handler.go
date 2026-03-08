// Input: db, auth, config, queue, docker, traefik, notify, scheduler, backup, service 等全部核心模块
// Output: Handler struct + Options 模式构造 + SetupRoutes 路由注册
// Role: 路由中枢，通过 Options 模式注入所有依赖，注册 tRPC/WS/静态文件/Auth 等全部 HTTP 路由
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package handler

import (
	"github.com/dokploy/dokploy/internal/auth"
	"github.com/dokploy/dokploy/internal/backup"
	"github.com/dokploy/dokploy/internal/config"
	"github.com/dokploy/dokploy/internal/db"
	"github.com/dokploy/dokploy/internal/docker"
	mw "github.com/dokploy/dokploy/internal/middleware"
	"github.com/dokploy/dokploy/internal/notify"
	"github.com/dokploy/dokploy/internal/queue"
	"github.com/dokploy/dokploy/internal/scheduler"
	"github.com/dokploy/dokploy/internal/service"
	"github.com/dokploy/dokploy/internal/traefik"
	"github.com/labstack/echo/v4"
)

// Handler holds shared dependencies for all route handlers.
type Handler struct {
	DB        *db.DB
	Auth      *auth.Auth
	Config    *config.Config
	Queue     *queue.Queue
	Docker    *docker.Client
	Traefik   *traefik.Manager
	Notifier  *notify.Notifier
	Scheduler *scheduler.Scheduler
	BackupSvc  *backup.Service
	PreviewSvc *service.PreviewService
	CertsPath  string
}

// New creates a new Handler.
func New(database *db.DB, a *auth.Auth, opts ...HandlerOption) *Handler {
	h := &Handler{DB: database, Auth: a}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// HandlerOption configures the Handler.
type HandlerOption func(*Handler)

// WithQueue sets the task queue.
func WithQueue(q *queue.Queue) HandlerOption {
	return func(h *Handler) { h.Queue = q }
}

// WithDocker sets the Docker client.
func WithDocker(d *docker.Client) HandlerOption {
	return func(h *Handler) { h.Docker = d }
}

// WithTraefik sets the Traefik config manager.
func WithTraefik(t *traefik.Manager) HandlerOption {
	return func(h *Handler) { h.Traefik = t }
}

// WithNotifier sets the notification sender.
func WithNotifier(n *notify.Notifier) HandlerOption {
	return func(h *Handler) { h.Notifier = n }
}

// WithScheduler sets the scheduler.
func WithScheduler(s *scheduler.Scheduler) HandlerOption {
	return func(h *Handler) { h.Scheduler = s }
}

// WithBackupService sets the backup service.
func WithBackupService(b *backup.Service) HandlerOption {
	return func(h *Handler) { h.BackupSvc = b }
}

// WithConfig sets the application configuration.
func WithConfig(c *config.Config) HandlerOption {
	return func(h *Handler) { h.Config = c }
}

// WithCertsPath sets the certificates directory path.
func WithCertsPath(p string) HandlerOption {
	return func(h *Handler) { h.CertsPath = p }
}

// WithPreviewService sets the preview deployment service.
func WithPreviewService(s *service.PreviewService) HandlerOption {
	return func(h *Handler) { h.PreviewSvc = s }
}

// RegisterRoutes registers all API routes.
func (h *Handler) RegisterRoutes(e *echo.Echo) {
	// Public routes
	e.GET("/health", h.HealthCheck)

	// Auth routes (public, no auth middleware - better-auth compatible)
	h.registerAuthRoutes(e.Group("/api/auth"))

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

	// Individual git provider routes (frontend-compatible)
	h.registerGithubRoutes(api.Group("/github"))
	h.registerGitlabRoutes(api.Group("/gitlab"))
	h.registerGiteaRoutes(api.Group("/gitea"))
	h.registerBitbucketRoutes(api.Group("/bitbucket"))

	// Organization routes
	h.registerOrganizationRoutes(api.Group("/organization"))

	// Preview Deployment routes
	h.registerPreviewDeploymentRoutes(api.Group("/preview-deployment"))

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

	// tRPC compatibility layer (matches frontend's tRPC client calls)
	trpc := e.Group("/api/trpc")
	trpc.Use(mw.TRPCAuthMiddleware(h.Auth, "settings.health", "sso.showSignInWithSSO", "compose.templates"))
	trpc.Any("/:procedures", h.HandleTRPC)

	// OpenAPI REST 兼容层（与 TS 版 @dokploy/trpc-openapi 行为一致）
	// 暴露 /api/{router}.{procedure} 格式的 REST 端点，供 Swagger/外部脚本/GitHub Actions 调用
	// 认证方式：x-api-key header 或 session cookie
	openapi := e.Group("/api")
	openapi.Use(mw.AuthMiddleware(h.Auth))
	openapi.Any("/*", h.HandleOpenAPI)

	// Webhook routes (public, no auth)
	h.registerWebhookRoutes(e)
}

func (h *Handler) HealthCheck(c echo.Context) error {
	return c.JSON(200, map[string]string{"status": "ok"})
}
