package traefik

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Manager handles Traefik dynamic configuration management.
type Manager struct {
	dynamicConfigPath string
}

// NewManager creates a new Traefik config manager.
func NewManager(dynamicConfigPath string) *Manager {
	return &Manager{dynamicConfigPath: dynamicConfigPath}
}

// CreateApplicationConfig creates Traefik routing config for an application domain.
func (m *Manager) CreateApplicationConfig(appName, host string, port int, https bool, certType string) error {
	config := GenerateApplicationConfig(appName, host, port, https, certType)
	path := ConfigFilePath(m.dynamicConfigPath, appName)
	return WriteConfig(path, config)
}

// RemoveApplicationConfig removes Traefik config for an application.
func (m *Manager) RemoveApplicationConfig(appName string) error {
	path := ConfigFilePath(m.dynamicConfigPath, appName)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	}
	return RemoveConfig(path)
}

// UpdateBasicAuth updates Traefik basic auth middleware for an application.
func (m *Manager) UpdateBasicAuth(appName string, credentials []string) error {
	path := ConfigFilePath(m.dynamicConfigPath, appName)
	config, err := ReadConfig(path)
	if err != nil {
		return fmt.Errorf("failed to read config: %w", err)
	}

	if config.HTTP == nil {
		config.HTTP = &HTTPConfig{}
	}
	if config.HTTP.Middlewares == nil {
		config.HTTP.Middlewares = make(map[string]*Middleware)
	}

	middlewareName := fmt.Sprintf("%s-auth", appName)

	if len(credentials) > 0 {
		config.HTTP.Middlewares[middlewareName] = &Middleware{
			BasicAuth: &BasicAuthMiddleware{Users: credentials},
		}
		// Add middleware to router
		for _, router := range config.HTTP.Routers {
			if !containsString(router.Middlewares, middlewareName) {
				router.Middlewares = append(router.Middlewares, middlewareName)
			}
		}
	} else {
		delete(config.HTTP.Middlewares, middlewareName)
		for _, router := range config.HTTP.Routers {
			router.Middlewares = removeString(router.Middlewares, middlewareName)
		}
	}

	return WriteConfig(path, config)
}

// UpdateRedirects updates Traefik redirect regex middleware for an application.
func (m *Manager) UpdateRedirects(appName string, redirects []RedirectEntry) error {
	path := ConfigFilePath(m.dynamicConfigPath, appName)
	config, err := ReadConfig(path)
	if err != nil {
		return fmt.Errorf("failed to read config: %w", err)
	}

	if config.HTTP == nil {
		config.HTTP = &HTTPConfig{}
	}
	if config.HTTP.Middlewares == nil {
		config.HTTP.Middlewares = make(map[string]*Middleware)
	}

	// Remove old redirect middlewares
	for name := range config.HTTP.Middlewares {
		if strings.HasPrefix(name, appName+"-redirect-") {
			delete(config.HTTP.Middlewares, name)
		}
	}

	// Clean router middlewares
	for _, router := range config.HTTP.Routers {
		filtered := make([]string, 0)
		for _, mw := range router.Middlewares {
			if !strings.HasPrefix(mw, appName+"-redirect-") {
				filtered = append(filtered, mw)
			}
		}
		router.Middlewares = filtered
	}

	// Add new redirect middlewares
	for i, r := range redirects {
		middlewareName := fmt.Sprintf("%s-redirect-%d", appName, i)
		config.HTTP.Middlewares[middlewareName] = &Middleware{
			RedirectRegex: &RedirectRegexMiddleware{
				Regex:       r.Regex,
				Replacement: r.Replacement,
				Permanent:   r.Permanent,
			},
		}
		for _, router := range config.HTTP.Routers {
			router.Middlewares = append(router.Middlewares, middlewareName)
		}
	}

	return WriteConfig(path, config)
}

// AddHTTPSRedirect adds an HTTP to HTTPS redirect for an application.
func (m *Manager) AddHTTPSRedirect(appName, host string) error {
	path := ConfigFilePath(m.dynamicConfigPath, appName)
	config, err := ReadConfig(path)
	if err != nil {
		return fmt.Errorf("failed to read config: %w", err)
	}

	if config.HTTP == nil {
		config.HTTP = &HTTPConfig{}
	}
	if config.HTTP.Routers == nil {
		config.HTTP.Routers = make(map[string]*Router)
	}
	if config.HTTP.Middlewares == nil {
		config.HTTP.Middlewares = make(map[string]*Middleware)
	}

	// Add HTTP router that redirects to HTTPS
	httpRouterName := fmt.Sprintf("%s-http-redirect", appName)
	redirectMiddleware := fmt.Sprintf("%s-https-redirect", appName)

	config.HTTP.Middlewares[redirectMiddleware] = &Middleware{
		RedirectScheme: &RedirectSchemeMiddleware{
			Scheme:    "https",
			Permanent: true,
		},
	}

	config.HTTP.Routers[httpRouterName] = &Router{
		Rule:        fmt.Sprintf("Host(`%s`)", host),
		Service:     fmt.Sprintf("%s-service", appName),
		EntryPoints: []string{"web"},
		Middlewares: []string{redirectMiddleware},
	}

	return WriteConfig(path, config)
}

// ListConfigFiles lists all Traefik dynamic config files.
func (m *Manager) ListConfigFiles() ([]string, error) {
	entries, err := os.ReadDir(m.dynamicConfigPath)
	if err != nil {
		return nil, err
	}

	var files []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".yaml") {
			files = append(files, filepath.Join(m.dynamicConfigPath, entry.Name()))
		}
	}
	return files, nil
}

// RedirectEntry represents a redirect rule.
type RedirectEntry struct {
	Regex       string
	Replacement string
	Permanent   bool
}

func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

func removeString(slice []string, s string) []string {
	result := make([]string, 0, len(slice))
	for _, item := range slice {
		if item != s {
			result = append(result, item)
		}
	}
	return result
}
