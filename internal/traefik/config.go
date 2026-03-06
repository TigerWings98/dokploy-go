package traefik

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// DynamicConfig represents a Traefik dynamic configuration file.
type DynamicConfig struct {
	HTTP *HTTPConfig `yaml:"http,omitempty"`
	TCP  *TCPConfig  `yaml:"tcp,omitempty"`
}

// HTTPConfig holds HTTP routers, services, and middlewares.
type HTTPConfig struct {
	Routers     map[string]*Router     `yaml:"routers,omitempty"`
	Services    map[string]*Service    `yaml:"services,omitempty"`
	Middlewares map[string]*Middleware `yaml:"middlewares,omitempty"`
}

// TCPConfig holds TCP routers and services.
type TCPConfig struct {
	Routers  map[string]*TCPRouter  `yaml:"routers,omitempty"`
	Services map[string]*TCPService `yaml:"services,omitempty"`
}

// Router represents a Traefik HTTP router.
type Router struct {
	Rule        string   `yaml:"rule"`
	Service     string   `yaml:"service"`
	EntryPoints []string `yaml:"entryPoints,omitempty"`
	Middlewares []string `yaml:"middlewares,omitempty"`
	TLS         *TLS     `yaml:"tls,omitempty"`
	Priority    int      `yaml:"priority,omitempty"`
}

// Service represents a Traefik HTTP service.
type Service struct {
	LoadBalancer *LoadBalancer `yaml:"loadBalancer,omitempty"`
}

// LoadBalancer represents a Traefik load balancer.
type LoadBalancer struct {
	Servers        []LBServer `yaml:"servers"`
	PassHostHeader *bool      `yaml:"passHostHeader,omitempty"`
}

// LBServer represents a backend server.
type LBServer struct {
	URL string `yaml:"url"`
}

// Middleware represents a Traefik middleware.
type Middleware struct {
	BasicAuth      *BasicAuthMiddleware      `yaml:"basicAuth,omitempty"`
	RedirectRegex  *RedirectRegexMiddleware  `yaml:"redirectRegex,omitempty"`
	RedirectScheme *RedirectSchemeMiddleware `yaml:"redirectScheme,omitempty"`
	Headers        *HeadersMiddleware        `yaml:"headers,omitempty"`
	StripPrefix    *StripPrefixMiddleware    `yaml:"stripPrefix,omitempty"`
	AddPrefix      *AddPrefixMiddleware      `yaml:"addPrefix,omitempty"`
}

// BasicAuthMiddleware for basic auth.
type BasicAuthMiddleware struct {
	Users []string `yaml:"users"`
}

// RedirectRegexMiddleware for regex redirects.
type RedirectRegexMiddleware struct {
	Regex       string `yaml:"regex"`
	Replacement string `yaml:"replacement"`
	Permanent   bool   `yaml:"permanent"`
}

// RedirectSchemeMiddleware for scheme redirects (http -> https).
type RedirectSchemeMiddleware struct {
	Scheme    string `yaml:"scheme"`
	Permanent bool   `yaml:"permanent"`
}

// HeadersMiddleware for custom headers.
type HeadersMiddleware struct {
	CustomRequestHeaders  map[string]string `yaml:"customRequestHeaders,omitempty"`
	CustomResponseHeaders map[string]string `yaml:"customResponseHeaders,omitempty"`
}

// StripPrefixMiddleware for stripping path prefixes.
type StripPrefixMiddleware struct {
	Prefixes []string `yaml:"prefixes"`
}

// AddPrefixMiddleware for adding path prefixes.
type AddPrefixMiddleware struct {
	Prefix string `yaml:"prefix"`
}

// TLS represents TLS configuration.
type TLS struct {
	CertResolver string `yaml:"certResolver,omitempty"`
}

// TCPRouter represents a Traefik TCP router.
type TCPRouter struct {
	Rule        string   `yaml:"rule"`
	Service     string   `yaml:"service"`
	EntryPoints []string `yaml:"entryPoints,omitempty"`
	TLS         *TCPTLS  `yaml:"tls,omitempty"`
}

// TCPTLS represents TCP TLS configuration.
type TCPTLS struct {
	Passthrough bool `yaml:"passthrough,omitempty"`
}

// TCPService represents a Traefik TCP service.
type TCPService struct {
	LoadBalancer *TCPLoadBalancer `yaml:"loadBalancer,omitempty"`
}

// TCPLoadBalancer represents a TCP load balancer.
type TCPLoadBalancer struct {
	Servers []TCPServer `yaml:"servers"`
}

// TCPServer represents a TCP backend server.
type TCPServer struct {
	Address string `yaml:"address"`
}

// WriteConfig writes a Traefik dynamic config to a YAML file.
func WriteConfig(path string, config *DynamicConfig) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	data, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal traefik config: %w", err)
	}

	return os.WriteFile(path, data, 0644)
}

// ReadConfig reads a Traefik dynamic config from a YAML file.
func ReadConfig(path string) (*DynamicConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var config DynamicConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse traefik config: %w", err)
	}

	return &config, nil
}

// RemoveConfig removes a Traefik config file.
func RemoveConfig(path string) error {
	return os.Remove(path)
}

// GenerateApplicationConfig generates Traefik config for an application domain.
func GenerateApplicationConfig(appName string, host string, port int, https bool, certType string) *DynamicConfig {
	routerName := fmt.Sprintf("%s-router", appName)
	serviceName := fmt.Sprintf("%s-service", appName)

	router := &Router{
		Rule:        fmt.Sprintf("Host(`%s`)", host),
		Service:     serviceName,
		EntryPoints: []string{"web"},
	}

	if https {
		router.EntryPoints = []string{"websecure"}
		switch certType {
		case "letsencrypt":
			router.TLS = &TLS{CertResolver: "letsencrypt"}
		case "custom":
			router.TLS = &TLS{}
		}
	}

	service := &Service{
		LoadBalancer: &LoadBalancer{
			Servers: []LBServer{
				{URL: fmt.Sprintf("http://%s:%d", appName, port)},
			},
		},
	}

	return &DynamicConfig{
		HTTP: &HTTPConfig{
			Routers:  map[string]*Router{routerName: router},
			Services: map[string]*Service{serviceName: service},
		},
	}
}

// ConfigFilePath returns the expected config file path for an app name.
func ConfigFilePath(dynamicTraefikPath string, appName string) string {
	return filepath.Join(dynamicTraefikPath, fmt.Sprintf("%s.yaml", appName))
}
