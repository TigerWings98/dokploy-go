package setup

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/dokploy/dokploy/internal/config"
	"github.com/dokploy/dokploy/internal/docker"
)

// Setup handles server initialization.
type Setup struct {
	cfg    *config.Config
	docker *docker.Client
}

// New creates a new Setup instance.
func New(cfg *config.Config, dockerClient *docker.Client) *Setup {
	return &Setup{cfg: cfg, docker: dockerClient}
}

// Initialize performs the initial server setup.
func (s *Setup) Initialize() error {
	log.Println("Initializing Dokploy server...")

	// 1. Create directory structure
	if err := s.setupDirectories(); err != nil {
		return fmt.Errorf("failed to create directories: %w", err)
	}

	// 2. Initialize Docker Swarm
	if err := s.initializeSwarm(); err != nil {
		log.Printf("Warning: failed to initialize Swarm: %v", err)
	}

	// 3. Initialize Docker network
	if err := s.initializeNetwork(); err != nil {
		log.Printf("Warning: failed to initialize network: %v", err)
	}

	// 4. Create default Traefik config
	if err := s.createTraefikConfig(); err != nil {
		log.Printf("Warning: failed to create Traefik config: %v", err)
	}

	// 5. Create default middlewares
	if err := s.createDefaultMiddlewares(); err != nil {
		log.Printf("Warning: failed to create default middlewares: %v", err)
	}

	log.Println("Server initialization complete")
	return nil
}

// setupDirectories creates all required directories.
func (s *Setup) setupDirectories() error {
	paths := s.cfg.Paths
	dirs := []string{
		paths.BasePath,
		paths.MainTraefikPath,
		paths.DynamicTraefikPath,
		paths.LogsPath,
		paths.ApplicationsPath,
		paths.ComposePath,
		paths.SSHPath,
		paths.CertificatesPath,
		paths.MonitoringPath,
		paths.SchedulesPath,
		paths.VolumeBackupsPath,
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create %s: %w", dir, err)
		}
	}

	// SSH directory needs restrictive permissions
	if err := os.Chmod(paths.SSHPath, 0700); err != nil {
		log.Printf("Warning: failed to set SSH directory permissions: %v", err)
	}

	return nil
}

// initializeSwarm initializes Docker Swarm if not already initialized.
func (s *Setup) initializeSwarm() error {
	if s.docker == nil {
		return fmt.Errorf("docker client not available")
	}

	ctx := context.Background()
	info, err := s.docker.DockerClient().Info(ctx)
	if err != nil {
		return fmt.Errorf("failed to get docker info: %w", err)
	}

	if info.Swarm.LocalNodeState == "active" {
		log.Println("Docker Swarm already initialized")
		return nil
	}

	log.Println("Initializing Docker Swarm...")
	_, err = s.docker.DockerClient().SwarmInit(ctx, swarmInitRequest())
	if err != nil {
		return fmt.Errorf("failed to initialize swarm: %w", err)
	}

	log.Println("Docker Swarm initialized successfully")
	return nil
}

// initializeNetwork creates the dokploy-network if it doesn't exist.
func (s *Setup) initializeNetwork() error {
	if s.docker == nil {
		return fmt.Errorf("docker client not available")
	}

	networkName := "dokploy-network"
	exists, err := s.docker.NetworkExists(context.Background(), networkName)
	if err != nil {
		return err
	}

	if exists {
		log.Printf("Network %s already exists", networkName)
		return nil
	}

	log.Printf("Creating network %s...", networkName)
	return s.docker.CreateNetwork(context.Background(), networkName, "overlay")
}

// createTraefikConfig creates the default Traefik static configuration.
func (s *Setup) createTraefikConfig() error {
	configPath := filepath.Join(s.cfg.Paths.MainTraefikPath, "traefik.yml")

	if _, err := os.Stat(configPath); err == nil {
		log.Println("Traefik config already exists")
		return nil
	}

	traefikPort := getEnvDefault("TRAEFIK_PORT", "80")
	traefikSSLPort := getEnvDefault("TRAEFIK_SSL_PORT", "443")

	config := fmt.Sprintf(`api:
  insecure: true
entryPoints:
  web:
    address: ":%s"
  websecure:
    address: ":%s"
    http:
      tls: {}
providers:
  docker:
    exposedByDefault: false
    network: dokploy-network
  file:
    directory: "%s"
    watch: true
log:
  level: ERROR
`, traefikPort, traefikSSLPort, s.cfg.Paths.DynamicTraefikPath)

	return os.WriteFile(configPath, []byte(config), 0644)
}

// createDefaultMiddlewares creates the default middleware config for HTTPS redirect.
func (s *Setup) createDefaultMiddlewares() error {
	middlewarePath := filepath.Join(s.cfg.Paths.DynamicTraefikPath, "middlewares.yml")

	if _, err := os.Stat(middlewarePath); err == nil {
		return nil
	}

	config := `http:
  middlewares:
    redirect-to-https:
      redirectScheme:
        scheme: https
        permanent: true
`
	return os.WriteFile(middlewarePath, []byte(config), 0644)
}

func getEnvDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
