package setup

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

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

	// 6. Deploy Traefik service
	if err := s.deployTraefikService(); err != nil {
		log.Printf("Warning: failed to deploy Traefik: %v", err)
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

// deployTraefikService deploys Traefik as a Docker Swarm service if not running.
func (s *Setup) deployTraefikService() error {
	if s.docker == nil {
		return fmt.Errorf("docker client not available")
	}

	ctx := context.Background()
	// Check if traefik service already exists
	container, err := s.docker.GetContainerByName(ctx, "dokploy-traefik")
	if err != nil {
		return err
	}
	if container != nil {
		log.Println("Traefik service already running")
		return nil
	}

	// Deploy Traefik via docker stack or service create
	traefikPort := getEnvDefault("TRAEFIK_PORT", "80")
	traefikSSLPort := getEnvDefault("TRAEFIK_SSL_PORT", "443")

	cmd := fmt.Sprintf(
		"docker service create --name dokploy-traefik "+
			"--constraint 'node.role==manager' "+
			"--network dokploy-network "+
			"--publish %s:%s --publish %s:%s --publish 8080:8080 "+
			"--mount type=bind,source=/var/run/docker.sock,target=/var/run/docker.sock "+
			"--mount type=bind,source=%s/traefik.yml,target=/etc/traefik/traefik.yml "+
			"--mount type=bind,source=%s,target=%s "+
			"traefik:v3.1",
		traefikPort, traefikPort,
		traefikSSLPort, traefikSSLPort,
		s.cfg.Paths.MainTraefikPath,
		s.cfg.Paths.DynamicTraefikPath, s.cfg.Paths.DynamicTraefikPath,
	)

	log.Println("Deploying Traefik service...")
	_, _ = s.docker.DockerClient().Info(ctx) // ensure connection
	// Use process.ExecAsync for the docker service create
	result, execErr := execCommand(cmd)
	if execErr != nil {
		return fmt.Errorf("failed to deploy Traefik: %w (output: %s)", execErr, result)
	}

	log.Println("Traefik service deployed successfully")
	return nil
}

// AddLetsEncryptResolver adds Let's Encrypt certificate resolver to traefik.yml.
func (s *Setup) AddLetsEncryptResolver(email string) error {
	configPath := filepath.Join(s.cfg.Paths.MainTraefikPath, "traefik.yml")

	data, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}

	content := string(data)
	// Check if certResolver already exists
	if strings.Contains(content, "certificatesResolvers") {
		return nil
	}

	resolver := fmt.Sprintf(`
certificatesResolvers:
  letsencrypt:
    acme:
      email: "%s"
      storage: "%s/acme.json"
      httpChallenge:
        entryPoint: web
`, email, s.cfg.Paths.MainTraefikPath)

	content += resolver
	return os.WriteFile(configPath, []byte(content), 0644)
}

func execCommand(cmd string) (string, error) {
	out, err := exec.Command("/bin/sh", "-c", cmd).CombinedOutput()
	return string(out), err
}

func getEnvDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
