// Input: 环境变量 DATABASE_URL/POSTGRES_*/DOCKER_*/BETTER_AUTH_SECRET/IS_CLOUD/GO_ENV, 文件系统 /.dockerenv
// Output: Config struct (含 DatabaseURL, Docker 配置, 认证密钥, Paths 路径体系), CleanupCronJob 常量
// Role: 全局配置中心，从环境变量加载数据库/Docker/认证配置，根据运行环境选择 /etc/dokploy 或 .docker/ 路径
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Config holds all application configuration.
type Config struct {
	// Database
	DatabaseURL string

	// Docker
	DockerAPIVersion string
	DockerHost       string
	DockerPort       string

	// Auth
	BetterAuthSecret string

	// Cloud mode
	IsCloud bool

	// Paths
	Paths Paths
}

// Paths holds all file system paths used by dokploy.
type Paths struct {
	BasePath            string
	MainTraefikPath     string
	DynamicTraefikPath  string
	LogsPath            string
	ApplicationsPath    string
	ComposePath         string
	SSHPath             string
	CertificatesPath    string
	MonitoringPath      string
	RegistryPath        string
	SchedulesPath       string
	VolumeBackupsPath   string
	VolumeBackupLockPath string
	PatchReposPath      string
}

func buildPaths(basePath string) Paths {
	mainTraefik := filepath.Join(basePath, "traefik")
	dynamicTraefik := filepath.Join(mainTraefik, "dynamic")
	return Paths{
		BasePath:            basePath,
		MainTraefikPath:     mainTraefik,
		DynamicTraefikPath:  dynamicTraefik,
		LogsPath:            filepath.Join(basePath, "logs"),
		ApplicationsPath:    filepath.Join(basePath, "applications"),
		ComposePath:         filepath.Join(basePath, "compose"),
		SSHPath:             filepath.Join(basePath, "ssh"),
		CertificatesPath:    filepath.Join(dynamicTraefik, "certificates"),
		MonitoringPath:      filepath.Join(basePath, "monitoring"),
		RegistryPath:        filepath.Join(basePath, "registry"),
		SchedulesPath:       filepath.Join(basePath, "schedules"),
		VolumeBackupsPath:   filepath.Join(basePath, "volume-backups"),
		VolumeBackupLockPath: filepath.Join(basePath, "volume-backup-lock"),
		PatchReposPath:      filepath.Join(basePath, "patch-repos"),
	}
}

// Load reads configuration from environment variables.
func Load() *Config {
	isCloud := os.Getenv("IS_CLOUD") == "true"

	cfg := &Config{
		DatabaseURL:      buildDatabaseURL(),
		DockerAPIVersion: os.Getenv("DOCKER_API_VERSION"),
		DockerHost:       os.Getenv("DOCKER_HOST"),
		DockerPort:       os.Getenv("DOCKER_PORT"),
		BetterAuthSecret: getEnvDefault("BETTER_AUTH_SECRET", "better-auth-secret-123456789"),
		IsCloud:          isCloud,
	}

	// In production use /etc/dokploy, in development use .docker/ in CWD
	env := os.Getenv("GO_ENV")
	if env == "production" || isRunningInContainer() {
		cfg.Paths = buildPaths("/etc/dokploy")
	} else {
		cwd, _ := os.Getwd()
		cfg.Paths = buildPaths(filepath.Join(cwd, ".docker"))
	}

	return cfg
}

func buildDatabaseURL() string {
	// Priority 1: DATABASE_URL env var
	if url := os.Getenv("DATABASE_URL"); url != "" {
		return url
	}

	// Priority 2: POSTGRES_PASSWORD_FILE (Docker Secrets)
	if pwFile := os.Getenv("POSTGRES_PASSWORD_FILE"); pwFile != "" {
		data, err := os.ReadFile(pwFile)
		if err != nil {
			panic(fmt.Sprintf("cannot read secret at %s: %v", pwFile, err))
		}
		password := strings.TrimSpace(string(data))
		user := getEnvDefault("POSTGRES_USER", "dokploy")
		dbName := getEnvDefault("POSTGRES_DB", "dokploy")
		host := getEnvDefault("POSTGRES_HOST", "dokploy-postgres")
		port := getEnvDefault("POSTGRES_PORT", "5432")
		return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", user, password, host, port, dbName)
	}

	// Priority 3: Legacy default
	if isRunningInContainer() {
		return "postgres://dokploy:amukds4wi9001583845717ad2@dokploy-postgres:5432/dokploy?sslmode=disable"
	}
	return "postgres://dokploy:amukds4wi9001583845717ad2@localhost:5432/dokploy?sslmode=disable"
}

func getEnvDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// isRunningInContainer checks if we're running inside a container.
func isRunningInContainer() bool {
	if os.Getenv("GO_ENV") == "production" {
		return true
	}
	_, err := os.Stat("/.dockerenv")
	return err == nil
}

// CleanupCronJob is the default cron expression for Docker cleanup.
const CleanupCronJob = "50 23 * * *"
