// Input: 文件系统 (Next.js 静态文件) 或开发代理 (localhost:3000)
// Output: 前端静态文件服务 + SPA 路由回退 + 开发模式反向代理
// Role: 前端服务 handler，生产环境服务 Next.js 导出的静态文件，开发环境代理到 Next.js dev server
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package handler

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dokploy/dokploy/internal/db/schema"
	"github.com/labstack/echo/v4"
)

// RegisterFrontendRoutes sets up frontend serving.
// In production: serves the Next.js static export from out/
// In development: proxies to the Next.js dev server.
func (h *Handler) RegisterFrontendRoutes(e *echo.Echo) {
	nextDevURL := os.Getenv("NEXT_DEV_URL")

	if nextDevURL != "" {
		// Development: proxy to Next.js dev server
		target, _ := url.Parse(nextDevURL)
		proxy := httputil.NewSingleHostReverseProxy(target)
		e.Any("/*", echo.WrapHandler(proxy))
		return
	}

	// Production: serve static files from Next.js export output
	distDir := findDistDir()
	if distDir == "" {
		e.GET("/*", func(c echo.Context) error {
			if strings.HasPrefix(c.Request().URL.Path, "/api/") {
				return echo.NewHTTPError(http.StatusNotFound)
			}
			return c.HTML(http.StatusOK, `<!DOCTYPE html>
<html><head><title>Dokploy</title></head>
<body><h1>Dokploy Go</h1>
<p>Frontend not found. Set NEXT_DEV_URL to proxy to Next.js dev server, or place the static export in the expected directory.</p>
</body></html>`)
		})
		return
	}

	// Security headers middleware for frontend routes
	securityHeaders := func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.Response().Header().Set("X-Frame-Options", "DENY")
			c.Response().Header().Set("Content-Security-Policy", "frame-ancestors 'none'")
			c.Response().Header().Set("X-Content-Type-Options", "nosniff")
			c.Response().Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
			return next(c)
		}
	}

	e.Use(securityHeaders)

	// Serve static assets (_next/static/*, images/*, etc.)
	staticFS := http.FileServer(http.Dir(distDir))

	e.GET("/*", func(c echo.Context) error {
		path := c.Request().URL.Path

		// Don't intercept API routes
		if strings.HasPrefix(path, "/api/") {
			return echo.NewHTTPError(http.StatusNotFound)
		}

		// Redirect unauthenticated users from protected pages to login
		if strings.HasPrefix(path, "/dashboard/") && !h.isAuthenticated(c) {
			return c.Redirect(http.StatusFound, "/")
		}

		// Redirect authenticated users from login/register pages to dashboard
		if (path == "/" || path == "/register" || path == "/send-reset-password") && h.isAuthenticated(c) {
			return c.Redirect(http.StatusFound, "/dashboard/projects")
		}

		// 1. Try exact file match (JS, CSS, images, etc.)
		filePath := filepath.Join(distDir, path)
		if info, err := os.Stat(filePath); err == nil && !info.IsDir() {
			// _next/static/* 文件名含 hash，可长期缓存
			if strings.HasPrefix(path, "/_next/static/") {
				c.Response().Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			} else if strings.HasPrefix(path, "/_next/") || strings.HasSuffix(path, ".js") || strings.HasSuffix(path, ".css") {
				c.Response().Header().Set("Cache-Control", "public, max-age=86400")
			} else if strings.HasSuffix(path, ".png") || strings.HasSuffix(path, ".jpg") || strings.HasSuffix(path, ".svg") || strings.HasSuffix(path, ".ico") || strings.HasSuffix(path, ".woff2") {
				c.Response().Header().Set("Cache-Control", "public, max-age=604800")
			}
			staticFS.ServeHTTP(c.Response(), c.Request())
			return nil
		}

		// 2. Try .html extension (e.g., /dashboard/projects -> projects.html)
		htmlPath := filePath + ".html"
		if _, err := os.Stat(htmlPath); err == nil {
			return c.File(htmlPath)
		}

		// 3. Try index.html in subdirectory
		indexPath := filepath.Join(filePath, "index.html")
		if _, err := os.Stat(indexPath); err == nil {
			return c.File(indexPath)
		}

		// 4. SPA fallback: serve 404.html which does client-side routing
		//    This handles dynamic routes like /dashboard/project/[id]/...
		//    The 404.tsx page detects the URL and navigates client-side to the correct page.
		fallbackPath := filepath.Join(distDir, "404.html")
		if _, err := os.Stat(fallbackPath); err == nil {
			return c.File(fallbackPath)
		}

		// 5. Last resort: serve index.html
		rootIndex := filepath.Join(distDir, "index.html")
		if _, err := os.Stat(rootIndex); err == nil {
			return c.File(rootIndex)
		}

		return echo.NewHTTPError(http.StatusNotFound)
	})
}

// isAuthenticated checks if the request has a valid session.
func (h *Handler) isAuthenticated(c echo.Context) bool {
	token := getSessionToken(c)
	if token == "" {
		return false
	}
	var session schema.Session
	err := h.DB.Where("token = ? AND expires_at > ?", token, time.Now().UTC()).First(&session).Error
	return err == nil
}

// findDistDir looks for the frontend build output directory.
func findDistDir() string {
	candidates := []string{
		"out",                                    // relative to CWD
		"frontend/out",                           // alternative layout
		"dokploy/apps/dokploy/out",               // development: Next.js export
		"/app/out",                               // Docker container
		"/app/apps/dokploy/out",                  // Docker container alternative
	}

	for _, dir := range candidates {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir
		}
	}

	return ""
}
