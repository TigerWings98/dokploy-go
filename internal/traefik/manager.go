// Input: schema.Domain/Redirect/Security, config.Paths
// Output: Manager (CreateDomainConfig/RemoveDomainConfig/CreateSecurityMiddleware 等)
// Role: Traefik 动态路由管理器，根据 Domain/Redirect/Security 配置生成和管理 YAML 配置文件
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package traefik

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/dokploy/dokploy/internal/db/schema"
)

// Manager handles Traefik dynamic configuration management.
type Manager struct {
	dynamicConfigPath string
}

// NewManager creates a new Traefik config manager.
func NewManager(dynamicConfigPath string) *Manager {
	return &Manager{dynamicConfigPath: dynamicConfigPath}
}

// ManageDomain creates or updates Traefik routing config for a domain.
// This is the main entry point called when domains are created/updated.
func (m *Manager) ManageDomain(appName string, domain schema.Domain, redirects []schema.Redirect, securities []schema.Security) error {
	path := ConfigFilePath(m.dynamicConfigPath, appName)
	config, err := m.loadOrCreate(path)
	if err != nil {
		return err
	}

	if config.HTTP == nil {
		config.HTTP = &HTTPConfig{
			Routers:     make(map[string]*Router),
			Services:    make(map[string]*Service),
			Middlewares: make(map[string]*Middleware),
		}
	}
	ensureHTTPMaps(config)

	key := 0
	if domain.UniqueConfigKey != nil {
		key = *domain.UniqueConfigKey
	}

	routerName := fmt.Sprintf("%s-router-%d", appName, key)
	routerNameSecure := fmt.Sprintf("%s-router-websecure-%d", appName, key)
	serviceName := fmt.Sprintf("%s-service-%d", appName, key)

	port := 80
	if domain.Port != nil {
		port = *domain.Port
	}

	// Service config
	config.HTTP.Services[serviceName] = &Service{
		LoadBalancer: &LoadBalancer{
			Servers:        []LBServer{{URL: fmt.Sprintf("http://%s:%d", appName, port)}},
			PassHostHeader: boolPtr(true),
		},
	}

	// Build host rule (punycode for IDN domains)
	host := toPunycode(domain.Host)
	rule := fmt.Sprintf("Host(`%s`)", host)
	pathStr := "/"
	if domain.Path != nil && *domain.Path != "" {
		pathStr = *domain.Path
	}
	if pathStr != "/" {
		rule += fmt.Sprintf(" && PathPrefix(`%s`)", pathStr)
	}

	// Collect middlewares for this domain
	var middlewareNames []string

	// Path middlewares
	middlewareNames = append(middlewareNames, m.addPathMiddlewares(config, appName, key, domain)...)

	// Security (basic auth) middleware
	if len(securities) > 0 {
		authName := fmt.Sprintf("auth-%s", appName)
		var users []string
		for _, sec := range securities {
			users = append(users, fmt.Sprintf("%s:%s", sec.Username, sec.Password))
		}
		config.HTTP.Middlewares[authName] = &Middleware{
			BasicAuth: &BasicAuthMiddleware{Users: users},
		}
		middlewareNames = append(middlewareNames, authName)
	}

	// Redirect middlewares (skip for preview domains)
	if domain.DomainType != schema.DomainTypePreviewDeployment {
		for _, r := range redirects {
			rKey := 0
			if r.UniqueConfigKey != nil {
				rKey = *r.UniqueConfigKey
			}
			mwName := fmt.Sprintf("redirect-%s-%d", appName, rKey)
			config.HTTP.Middlewares[mwName] = &Middleware{
				RedirectRegex: &RedirectRegexMiddleware{
					Regex:       r.Regex,
					Replacement: r.Replacement,
					Permanent:   r.Permanent,
				},
			}
			middlewareNames = append(middlewareNames, mwName)
		}
	}

	// HTTP router (web entrypoint)
	httpMiddlewares := make([]string, len(middlewareNames))
	copy(httpMiddlewares, middlewareNames)
	if domain.HTTPS {
		httpMiddlewares = append(httpMiddlewares, "redirect-to-https")
		// Ensure redirect-to-https middleware exists
		config.HTTP.Middlewares["redirect-to-https"] = &Middleware{
			RedirectScheme: &RedirectSchemeMiddleware{
				Scheme:    "https",
				Permanent: true,
			},
		}
	}

	config.HTTP.Routers[routerName] = &Router{
		Rule:        rule,
		Service:     serviceName,
		EntryPoints: []string{"web"},
		Middlewares: httpMiddlewares,
	}

	// HTTPS router (websecure entrypoint)
	if domain.HTTPS {
		httpsRouter := &Router{
			Rule:        rule,
			Service:     serviceName,
			EntryPoints: []string{"websecure"},
			Middlewares: middlewareNames,
		}

		switch domain.CertificateType {
		case schema.CertificateTypeLetsencrypt:
			httpsRouter.TLS = &TLS{CertResolver: "letsencrypt"}
		case schema.CertificateTypeCustom:
			resolver := ""
			if domain.CustomCertResolver != nil {
				resolver = *domain.CustomCertResolver
			}
			httpsRouter.TLS = &TLS{CertResolver: resolver}
		default:
			httpsRouter.TLS = &TLS{}
		}

		config.HTTP.Routers[routerNameSecure] = httpsRouter
	} else {
		delete(config.HTTP.Routers, routerNameSecure)
	}

	return WriteConfig(path, config)
}

// RemoveDomain removes Traefik config for a specific domain.
func (m *Manager) RemoveDomain(appName string, uniqueConfigKey int) error {
	path := ConfigFilePath(m.dynamicConfigPath, appName)
	config, err := ReadConfig(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	if config.HTTP == nil {
		return nil
	}

	routerName := fmt.Sprintf("%s-router-%d", appName, uniqueConfigKey)
	routerNameSecure := fmt.Sprintf("%s-router-websecure-%d", appName, uniqueConfigKey)
	serviceName := fmt.Sprintf("%s-service-%d", appName, uniqueConfigKey)

	delete(config.HTTP.Routers, routerName)
	delete(config.HTTP.Routers, routerNameSecure)
	delete(config.HTTP.Services, serviceName)

	// Remove path middlewares for this key
	stripName := fmt.Sprintf("stripprefix-%s-%d", appName, uniqueConfigKey)
	addName := fmt.Sprintf("addprefix-%s-%d", appName, uniqueConfigKey)
	delete(config.HTTP.Middlewares, stripName)
	delete(config.HTTP.Middlewares, addName)

	// If no routers remain, delete the config file
	if len(config.HTTP.Routers) == 0 {
		return RemoveConfig(path)
	}

	return WriteConfig(path, config)
}

// RemoveApplicationConfig removes entire Traefik config for an application.
func (m *Manager) RemoveApplicationConfig(appName string) error {
	path := ConfigFilePath(m.dynamicConfigPath, appName)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	}
	return RemoveConfig(path)
}

// CreateApplicationConfig creates Traefik routing config for an application domain.
func (m *Manager) CreateApplicationConfig(appName, host string, port int, https bool, certType string) error {
	config := GenerateApplicationConfig(appName, host, port, https, certType)
	path := ConfigFilePath(m.dynamicConfigPath, appName)
	return WriteConfig(path, config)
}

// UpdateBasicAuth updates Traefik basic auth middleware for an application.
func (m *Manager) UpdateBasicAuth(appName string, credentials []string) error {
	path := ConfigFilePath(m.dynamicConfigPath, appName)
	config, err := ReadConfig(path)
	if err != nil {
		return fmt.Errorf("failed to read config: %w", err)
	}

	ensureHTTPMaps(config)

	middlewareName := fmt.Sprintf("auth-%s", appName)

	if len(credentials) > 0 {
		config.HTTP.Middlewares[middlewareName] = &Middleware{
			BasicAuth: &BasicAuthMiddleware{Users: credentials},
		}
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

	ensureHTTPMaps(config)

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

	ensureHTTPMaps(config)

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

// UpdateWebServerConfig updates the Dokploy dashboard's own Traefik config.
func (m *Manager) UpdateWebServerConfig(host string, https bool, certType schema.CertificateType, port string) error {
	path := ConfigFilePath(m.dynamicConfigPath, "dokploy")
	config := &DynamicConfig{
		HTTP: &HTTPConfig{
			Routers:     make(map[string]*Router),
			Services:    make(map[string]*Service),
			Middlewares: make(map[string]*Middleware),
		},
	}

	if port == "" {
		port = "3000"
	}

	serviceName := "dokploy-service-app"
	config.HTTP.Services[serviceName] = &Service{
		LoadBalancer: &LoadBalancer{
			Servers:        []LBServer{{URL: fmt.Sprintf("http://dokploy:%s", port)}},
			PassHostHeader: boolPtr(true),
		},
	}

	punyHost := toPunycode(host)
	rule := fmt.Sprintf("Host(`%s`)", punyHost)

	// HTTP router
	httpRouter := &Router{
		Rule:        rule,
		Service:     serviceName,
		EntryPoints: []string{"web"},
	}
	if https {
		httpRouter.Middlewares = []string{"redirect-to-https"}
		config.HTTP.Middlewares["redirect-to-https"] = &Middleware{
			RedirectScheme: &RedirectSchemeMiddleware{
				Scheme:    "https",
				Permanent: true,
			},
		}
	}
	config.HTTP.Routers["dokploy-router-app"] = httpRouter

	// HTTPS router
	if https {
		httpsRouter := &Router{
			Rule:        rule,
			Service:     serviceName,
			EntryPoints: []string{"websecure"},
		}
		if certType == schema.CertificateTypeLetsencrypt {
			httpsRouter.TLS = &TLS{CertResolver: "letsencrypt"}
		} else {
			httpsRouter.TLS = &TLS{}
		}
		config.HTTP.Routers["dokploy-router-app-secure"] = httpsRouter
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

// ReadAppConfig reads the current Traefik config for an app.
func (m *Manager) ReadAppConfig(appName string) (*DynamicConfig, error) {
	path := ConfigFilePath(m.dynamicConfigPath, appName)
	return ReadConfig(path)
}

// WriteAppConfig writes a Traefik config for an app.
func (m *Manager) WriteAppConfig(appName string, config *DynamicConfig) error {
	path := ConfigFilePath(m.dynamicConfigPath, appName)
	return WriteConfig(path, config)
}

// ReadMainConfig reads the main/global Traefik configuration file as raw YAML string.
func (m *Manager) ReadMainConfig() (string, error) {
	path := filepath.Join(filepath.Dir(m.dynamicConfigPath), "traefik.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return string(data), nil
}

// WriteMainConfig writes the main Traefik configuration file.
func (m *Manager) WriteMainConfig(content string) error {
	path := filepath.Join(filepath.Dir(m.dynamicConfigPath), "traefik.yaml")
	return os.WriteFile(path, []byte(content), 0644)
}

// ReadServiceConfig reads a service-specific Traefik dynamic config as raw YAML string.
func (m *Manager) ReadServiceConfig(serviceName string) (string, error) {
	path := ConfigFilePath(m.dynamicConfigPath, serviceName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return string(data), nil
}

// WriteServiceConfig writes a service-specific Traefik dynamic config from raw YAML string.
func (m *Manager) WriteServiceConfig(serviceName string, content string) error {
	path := ConfigFilePath(m.dynamicConfigPath, serviceName)
	return os.WriteFile(path, []byte(content), 0644)
}

// ReadMiddlewareConfig reads the middleware configuration file.
func (m *Manager) ReadMiddlewareConfig() (string, error) {
	path := filepath.Join(m.dynamicConfigPath, "middlewares.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return string(data), nil
}

// WriteMiddlewareConfig writes the middleware configuration file.
func (m *Manager) WriteMiddlewareConfig(content string) error {
	path := filepath.Join(m.dynamicConfigPath, "middlewares.yaml")
	return os.WriteFile(path, []byte(content), 0644)
}

// --- internal helpers ---

func (m *Manager) loadOrCreate(path string) (*DynamicConfig, error) {
	config, err := ReadConfig(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &DynamicConfig{
				HTTP: &HTTPConfig{
					Routers:     make(map[string]*Router),
					Services:    make(map[string]*Service),
					Middlewares: make(map[string]*Middleware),
				},
			}, nil
		}
		return nil, err
	}
	return config, nil
}

func (m *Manager) addPathMiddlewares(config *DynamicConfig, appName string, key int, domain schema.Domain) []string {
	var names []string

	pathStr := "/"
	if domain.Path != nil {
		pathStr = *domain.Path
	}
	internalPath := "/"
	if domain.InternalPath != nil {
		internalPath = *domain.InternalPath
	}

	// StripPrefix middleware
	if domain.StripPath && pathStr != "/" {
		name := fmt.Sprintf("stripprefix-%s-%d", appName, key)
		config.HTTP.Middlewares[name] = &Middleware{
			StripPrefix: &StripPrefixMiddleware{
				Prefixes: []string{pathStr},
			},
		}
		names = append(names, name)
	}

	// AddPrefix middleware (when internal path differs from external path)
	if internalPath != "/" && internalPath != pathStr {
		name := fmt.Sprintf("addprefix-%s-%d", appName, key)
		config.HTTP.Middlewares[name] = &Middleware{
			AddPrefix: &AddPrefixMiddleware{
				Prefix: internalPath,
			},
		}
		names = append(names, name)
	}

	return names
}

// RedirectEntry represents a redirect rule.
type RedirectEntry struct {
	Regex       string
	Replacement string
	Permanent   bool
}

func ensureHTTPMaps(config *DynamicConfig) {
	if config.HTTP == nil {
		config.HTTP = &HTTPConfig{}
	}
	if config.HTTP.Routers == nil {
		config.HTTP.Routers = make(map[string]*Router)
	}
	if config.HTTP.Services == nil {
		config.HTTP.Services = make(map[string]*Service)
	}
	if config.HTTP.Middlewares == nil {
		config.HTTP.Middlewares = make(map[string]*Middleware)
	}
}

func toPunycode(host string) string {
	u, err := url.Parse("http://" + host)
	if err != nil {
		return host
	}
	return u.Hostname()
}

func boolPtr(b bool) *bool {
	return &b
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
