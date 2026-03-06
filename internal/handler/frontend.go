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
// In production: serves the Next.js static export from dist/
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

	// Production: serve static files with SSR-like redirect logic
	distDir := findDistDir()
	if distDir == "" {
		// No frontend build found - serve a simple fallback
		e.GET("/*", func(c echo.Context) error {
			if strings.HasPrefix(c.Request().URL.Path, "/api/") {
				return echo.NewHTTPError(http.StatusNotFound)
			}
			return c.HTML(http.StatusOK, `<!DOCTYPE html>
<html><head><title>Dokploy</title></head>
<body><h1>Dokploy Go</h1>
<p>Frontend not found. Set NEXT_DEV_URL to proxy to Next.js dev server, or place the build output in the dist/ directory.</p>
</body></html>`)
		})
		return
	}

	// Serve static assets
	staticFS := http.FileServer(http.Dir(distDir))

	e.GET("/*", func(c echo.Context) error {
		path := c.Request().URL.Path

		// Don't intercept API routes
		if strings.HasPrefix(path, "/api/") {
			return echo.NewHTTPError(http.StatusNotFound)
		}

		// Handle SSR-equivalent redirect logic for page routes
		if redirect := h.handlePageRedirects(c, path); redirect != "" {
			return c.Redirect(http.StatusFound, redirect)
		}

		// Try to serve the static file
		filePath := filepath.Join(distDir, path)
		if _, err := os.Stat(filePath); err == nil {
			staticFS.ServeHTTP(c.Response(), c.Request())
			return nil
		}

		// For page routes, try .html extension
		htmlPath := filePath + ".html"
		if _, err := os.Stat(htmlPath); err == nil {
			return c.File(htmlPath)
		}

		// Try index.html in subdirectory
		indexPath := filepath.Join(filePath, "index.html")
		if _, err := os.Stat(indexPath); err == nil {
			return c.File(indexPath)
		}

		// SPA fallback: serve root index.html for non-file routes
		rootIndex := filepath.Join(distDir, "index.html")
		if _, err := os.Stat(rootIndex); err == nil {
			return c.File(rootIndex)
		}

		return echo.NewHTTPError(http.StatusNotFound)
	})
}

// handlePageRedirects replicates the getServerSideProps redirect logic.
func (h *Handler) handlePageRedirects(c echo.Context, path string) string {
	// Login page: redirect to /register if no admin, redirect to /dashboard if logged in
	if path == "/" || path == "" {
		if h.isAuthenticated(c) {
			return "/dashboard/projects"
		}
		if !h.DB.IsAdminPresent() {
			return "/register"
		}
		return ""
	}

	// Register page: redirect to login if admin already exists (non-cloud)
	if path == "/register" {
		if h.DB.IsAdminPresent() {
			return "/"
		}
		return ""
	}

	// Dashboard pages: redirect to login if not authenticated
	if strings.HasPrefix(path, "/dashboard") {
		if !h.isAuthenticated(c) {
			return "/"
		}
		return ""
	}

	return ""
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
	// Check common locations
	candidates := []string{
		"dist",                            // relative to CWD
		"frontend/out",                    // Next.js static export
		"frontend/.next/static",           // Next.js build
		"/app/dist",                       // Docker container
		"/app/apps/dokploy/.next/static",  // Original Next.js in container
	}

	for _, dir := range candidates {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir
		}
	}

	return ""
}
