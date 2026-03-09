// Input: db, docker, git, compose/transform, config
// Output: ComposeService (DeployCompose 全流程: clone→compose transform→docker compose up/stack deploy)
// Role: Compose 部署编排服务，支持 docker-compose 和 stack 两种部署模式
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package service

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dokploy/dokploy/internal/config"
	"github.com/dokploy/dokploy/internal/db"
	"github.com/dokploy/dokploy/internal/db/schema"
	"github.com/dokploy/dokploy/internal/docker"
	"github.com/dokploy/dokploy/internal/process"
	"gorm.io/gorm"
)

// ComposeService handles Docker Compose business logic.
type ComposeService struct {
	db     *db.DB
	docker *docker.Client
	cfg    *config.Config
}

// NewComposeService creates a new ComposeService.
func NewComposeService(database *db.DB, dockerClient *docker.Client, cfg *config.Config) *ComposeService {
	return &ComposeService{db: database, docker: dockerClient, cfg: cfg}
}

// FindByID finds a compose by ID with preloads.
func (s *ComposeService) FindByID(composeID string) (*schema.Compose, error) {
	var compose schema.Compose
	err := s.db.
		Preload("Deployments", func(db *gorm.DB) *gorm.DB {
			return db.Order("\"createdAt\" DESC").Limit(5)
		}).
		Preload("Domains").
		Preload("Mounts").
		Preload("Server").
		Preload("Server.SSHKey").
		First(&compose, "\"composeId\" = ?", composeID).Error
	if err != nil {
		return nil, err
	}
	return &compose, nil
}

// Deploy deploys a compose project.
func (s *ComposeService) Deploy(composeID string, titleOverride *string) error {
	compose, err := s.FindByID(composeID)
	if err != nil {
		return fmt.Errorf("compose not found: %w", err)
	}

	title := fmt.Sprintf("Deploy %s", compose.Name)
	if titleOverride != nil {
		title = *titleOverride
	}

	now := time.Now().UTC()
	logPath := filepath.Join(s.cfg.Paths.LogsPath, compose.AppName, fmt.Sprintf("%d.log", now.UnixMilli()))
	os.MkdirAll(filepath.Dir(logPath), 0755)

	deployment := &schema.Deployment{
		Title:     title,
		LogPath:   logPath,
		ComposeID: &compose.ComposeID,
		ServerID:  compose.ServerID,
	}

	if err := s.db.Create(deployment).Error; err != nil {
		return fmt.Errorf("failed to create deployment: %w", err)
	}

	s.updateStatus(compose.ComposeID, schema.ApplicationStatusRunning)
	go s.runDeploy(compose, deployment)

	return nil
}

// Rebuild rebuilds a compose project without re-cloning source code.
// 对应 TS 版 rebuildCompose：跳过 git clone，直接 compose up。
// Raw 类型仍然写入 compose file（因为内容可能已更新）。
func (s *ComposeService) Rebuild(composeID string, titleOverride *string) error {
	compose, err := s.FindByID(composeID)
	if err != nil {
		return fmt.Errorf("compose not found: %w", err)
	}

	title := fmt.Sprintf("Rebuild %s", compose.Name)
	if titleOverride != nil {
		title = *titleOverride
	}

	now := time.Now().UTC()
	logPath := filepath.Join(s.cfg.Paths.LogsPath, compose.AppName, fmt.Sprintf("%d.log", now.UnixMilli()))
	os.MkdirAll(filepath.Dir(logPath), 0755)

	deployment := &schema.Deployment{
		Title:     title,
		LogPath:   logPath,
		ComposeID: &compose.ComposeID,
		ServerID:  compose.ServerID,
	}

	if err := s.db.Create(deployment).Error; err != nil {
		return fmt.Errorf("failed to create deployment: %w", err)
	}

	s.updateStatus(compose.ComposeID, schema.ApplicationStatusRunning)
	go s.runRebuild(compose, deployment)
	return nil
}

func (s *ComposeService) runRebuild(compose *schema.Compose, deployment *schema.Deployment) {
	logFile, err := os.OpenFile(deployment.LogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		s.failDeployment(deployment.DeploymentID, compose.ComposeID, err.Error())
		return
	}
	defer logFile.Close()

	writeLog := func(msg string) {
		logFile.WriteString(msg + "\n")
	}

	writeLog(fmt.Sprintf("Rebuilding compose %s (%s) - skipping source clone", compose.Name, compose.AppName))

	isRemote := compose.ServerID != nil && compose.Server != nil && compose.Server.SSHKey != nil

	if isRemote {
		// 远程 rebuild：与 TS 版一致，raw 类型仍然写入最新文件，然后 compose up
		conn := process.SSHConnection{
			Host:       compose.Server.IPAddress,
			Port:       compose.Server.Port,
			Username:   compose.Server.Username,
			PrivateKey: compose.Server.SSHKey.PrivateKey,
		}

		projectPath := fmt.Sprintf("/etc/dokploy/compose/%s/code", compose.AppName)

		// Raw 类型：重新写入 compose file
		if compose.SourceType == schema.SourceTypeComposeRaw {
			encoded := base64Encode(compose.ComposeFile)
			writeCmd := fmt.Sprintf(`mkdir -p %s && echo "%s" | base64 -d > "%s/docker-compose.yml"`, projectPath, encoded, projectPath)
			_, err := process.ExecAsyncRemote(conn, writeCmd, writeLog)
			if err != nil {
				s.failDeployment(deployment.DeploymentID, compose.ComposeID, err.Error())
				return
			}
		}

		// Compose up
		composePth := "docker-compose.yml"
		if compose.ComposePath != "" {
			composePth = compose.ComposePath
		}
		var dockerCmd string
		switch compose.ComposeType {
		case schema.ComposeTypeStack:
			dockerCmd = fmt.Sprintf("stack deploy -c %s %s --prune --with-registry-auth", composePth, compose.AppName)
		default:
			dockerCmd = fmt.Sprintf("compose -p %s -f %s up -d --build --remove-orphans", compose.AppName, composePth)
		}

		buildCmd := fmt.Sprintf(`set -e; cd "%s" && docker %s 2>&1`, projectPath, dockerCmd)
		_, err := process.ExecAsyncRemote(conn, buildCmd, writeLog)
		if err != nil {
			s.failDeployment(deployment.DeploymentID, compose.ComposeID, err.Error())
			return
		}
	} else {
		// 本地 rebuild
		composeDir := filepath.Join(s.cfg.Paths.ComposePath, compose.AppName)
		os.MkdirAll(composeDir, 0755)

		if compose.SourceType == schema.SourceTypeComposeRaw {
			composePath := filepath.Join(composeDir, "docker-compose.yml")
			writeLog("Writing compose file from raw content")
			if err := os.WriteFile(composePath, []byte(compose.ComposeFile), 0644); err != nil {
				s.failDeployment(deployment.DeploymentID, compose.ComposeID, err.Error())
				return
			}
		}

		if err := s.composeUpLocal(compose, composeDir, writeLog); err != nil {
			s.failDeployment(deployment.DeploymentID, compose.ComposeID, err.Error())
			return
		}
	}

	now := time.Now().UTC().Format(time.RFC3339)
	s.db.Model(&schema.Deployment{}).
		Where("\"deploymentId\" = ?", deployment.DeploymentID).
		Updates(map[string]interface{}{
			"status":     schema.DeploymentStatusDone,
			"finishedAt": now,
		})
	s.updateStatus(compose.ComposeID, schema.ApplicationStatusDone)
	writeLog("Compose rebuild completed successfully!")
}

func (s *ComposeService) runDeploy(compose *schema.Compose, deployment *schema.Deployment) {
	logFile, err := os.OpenFile(deployment.LogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		s.failDeployment(deployment.DeploymentID, compose.ComposeID, err.Error())
		return
	}
	defer logFile.Close()

	writeLog := func(msg string) {
		logFile.WriteString(msg + "\n")
	}

	writeLog(fmt.Sprintf("Starting compose deployment for %s (%s)", compose.Name, compose.AppName))

	isRemote := compose.ServerID != nil && compose.Server != nil && compose.Server.SSHKey != nil

	if isRemote {
		// 远程部署：与 TS 版一致，将整个流水线（准备源码 + compose up）作为 SSH 命令发送到远程服务器
		if err := s.runDeployRemote(compose, deployment, writeLog); err != nil {
			s.failDeployment(deployment.DeploymentID, compose.ComposeID, err.Error())
			return
		}
	} else {
		// 本地部署
		composeDir := filepath.Join(s.cfg.Paths.ComposePath, compose.AppName)
		os.MkdirAll(composeDir, 0755)

		if err := s.prepareComposeSource(compose, composeDir, writeLog); err != nil {
			s.failDeployment(deployment.DeploymentID, compose.ComposeID, err.Error())
			return
		}

		if err := s.composeUpLocal(compose, composeDir, writeLog); err != nil {
			s.failDeployment(deployment.DeploymentID, compose.ComposeID, err.Error())
			return
		}
	}

	now := time.Now().UTC().Format(time.RFC3339)
	s.db.Model(&schema.Deployment{}).
		Where("\"deploymentId\" = ?", deployment.DeploymentID).
		Updates(map[string]interface{}{
			"status":     schema.DeploymentStatusDone,
			"finishedAt": now,
		})

	s.updateStatus(compose.ComposeID, schema.ApplicationStatusDone)
	writeLog("Compose deployment completed successfully!")
}

// runDeployRemote 远程部署：与 TS 版完全一致，将准备源码和 compose up 作为 SSH 命令发送
func (s *ComposeService) runDeployRemote(compose *schema.Compose, deployment *schema.Deployment, writeLog func(string)) error {
	conn := process.SSHConnection{
		Host:       compose.Server.IPAddress,
		Port:       compose.Server.Port,
		Username:   compose.Server.Username,
		PrivateKey: compose.Server.SSHKey.PrivateKey,
	}

	composePath := "/etc/dokploy/compose"
	projectPath := fmt.Sprintf("%s/%s/code", composePath, compose.AppName)

	// Step 1: 准备源码（与 TS 版一致，通过 SSH 在远程服务器上执行）
	var prepareCmd string
	switch compose.SourceType {
	case schema.SourceTypeComposeRaw:
		// 与 TS 版 getCreateComposeFileCommand 一致：base64 编码写入文件
		encoded := base64Encode(compose.ComposeFile)
		prepareCmd = fmt.Sprintf(`set -e;
rm -rf %s;
mkdir -p %s;
echo "%s" | base64 -d > "%s/docker-compose.yml";
echo "File 'docker-compose.yml' created: ✅";`, projectPath, projectPath, encoded, projectPath)

	case schema.SourceTypeComposeGithub, schema.SourceTypeComposeGitlab,
		schema.SourceTypeComposeGitea, schema.SourceTypeComposeBitbucket,
		schema.SourceTypeComposeGit:
		// Git 克隆直接在远程服务器上执行
		var repoURL, branch string
		if compose.SourceType == schema.SourceTypeComposeGit {
			if compose.CustomGitURL == nil || compose.CustomGitBranch == nil {
				return fmt.Errorf("custom git requires URL and branch")
			}
			repoURL = *compose.CustomGitURL
			branch = *compose.CustomGitBranch
		} else {
			if compose.Repository == nil || compose.Branch == nil {
				return fmt.Errorf("repository and branch are required")
			}
			if compose.Owner != nil {
				repoURL = fmt.Sprintf("https://github.com/%s/%s.git", *compose.Owner, *compose.Repository)
			} else {
				repoURL = *compose.Repository
			}
			branch = *compose.Branch
		}
		prepareCmd = fmt.Sprintf(`set -e;
rm -rf %s;
mkdir -p %s;
git clone --branch %s --depth 1 %s %s;
echo "Repository cloned: ✅";`, projectPath, filepath.Dir(projectPath), branch, repoURL, projectPath)

	default:
		return fmt.Errorf("unsupported compose source type: %s", compose.SourceType)
	}

	// 执行源码准备
	writeLog("--- Preparing source on remote server ---")
	prepareWithLog := fmt.Sprintf("(%s) >> %s 2>&1", prepareCmd, deployment.LogPath)
	_, err := process.ExecAsyncRemote(conn, prepareWithLog, writeLog)
	if err != nil {
		return fmt.Errorf("failed to prepare source on remote: %w", err)
	}

	// Step 2: 构建 env 文件 + compose up（与 TS 版 getBuildComposeCommand 一致）
	var dockerCmd string
	composePth := "docker-compose.yml"
	if compose.ComposePath != "" {
		composePth = compose.ComposePath
	}

	switch compose.ComposeType {
	case schema.ComposeTypeStack:
		dockerCmd = fmt.Sprintf("stack deploy -c %s %s --prune --with-registry-auth", composePth, compose.AppName)
	default:
		dockerCmd = fmt.Sprintf("compose -p %s -f %s up -d --build --remove-orphans", compose.AppName, composePth)
	}

	// 构建 .env 文件命令（与 TS 版 getCreateEnvFileCommand 一致）
	envContent := fmt.Sprintf("APP_NAME=%s\n", compose.AppName)
	if compose.Env != nil && *compose.Env != "" {
		envContent += *compose.Env + "\n"
	}
	if !strings.Contains(envContent, "DOCKER_CONFIG") {
		envContent += "DOCKER_CONFIG=/root/.docker\n"
	}
	encodedEnv := base64Encode(envContent)

	envFilePath := filepath.Join(filepath.Dir(filepath.Join(projectPath, composePth)), ".env")

	// 隔离部署：创建独立网络（stack 使用 overlay 驱动），部署后连接 Traefik
	var networkCmd, networkConnectCmd string
	if compose.IsolatedDeployment {
		driverFlag := ""
		if compose.ComposeType == schema.ComposeTypeStack {
			driverFlag = "--driver overlay "
		}
		networkCmd = fmt.Sprintf(`docker network inspect %s >/dev/null 2>&1 || docker network create %s--attachable %s;`,
			compose.AppName, driverFlag, compose.AppName)
		networkConnectCmd = fmt.Sprintf(`docker network connect %s $(docker ps --filter "name=dokploy-traefik" -q) >/dev/null 2>&1 || true;`,
			compose.AppName)
	}

	buildCmd := fmt.Sprintf(`set -e;
touch %s;
echo "%s" | base64 -d > "%s";
cd "%s";
%s
docker %s 2>&1 || { echo "Error: ❌ Docker command failed"; exit 1; }
%s
echo "Docker Compose Deployed: ✅";`, envFilePath, encodedEnv, envFilePath, projectPath, networkCmd, dockerCmd, networkConnectCmd)

	writeLog("--- Running compose up on remote server ---")
	buildWithLog := fmt.Sprintf("(%s) >> %s 2>&1", buildCmd, deployment.LogPath)
	_, err = process.ExecAsyncRemote(conn, buildWithLog, writeLog)
	if err != nil {
		return fmt.Errorf("failed to run compose on remote: %w", err)
	}

	return nil
}

func (s *ComposeService) prepareComposeSource(compose *schema.Compose, composeDir string, writeLog func(string)) error {
	switch compose.SourceType {
	case schema.SourceTypeComposeRaw:
		// Write compose file directly
		composePath := filepath.Join(composeDir, "docker-compose.yml")
		writeLog("Writing compose file from raw content")
		return os.WriteFile(composePath, []byte(compose.ComposeFile), 0644)

	case schema.SourceTypeComposeGithub, schema.SourceTypeComposeGitlab,
		schema.SourceTypeComposeGitea, schema.SourceTypeComposeBitbucket:
		return s.cloneComposeRepo(compose, composeDir, writeLog)

	case schema.SourceTypeComposeGit:
		if compose.CustomGitURL == nil || compose.CustomGitBranch == nil {
			return fmt.Errorf("custom git requires URL and branch")
		}
		cmd := fmt.Sprintf("git clone --branch %s --depth 1 %s %s", *compose.CustomGitBranch, *compose.CustomGitURL, composeDir)
		writeLog(fmt.Sprintf("Cloning %s", *compose.CustomGitURL))
		_, err := process.ExecAsyncStream(cmd, writeLog)
		return err

	default:
		return fmt.Errorf("unsupported compose source type: %s", compose.SourceType)
	}
}

func (s *ComposeService) cloneComposeRepo(compose *schema.Compose, composeDir string, writeLog func(string)) error {
	if compose.Repository == nil || compose.Branch == nil {
		return fmt.Errorf("repository and branch are required")
	}

	var repoURL string
	if compose.Owner != nil {
		repoURL = fmt.Sprintf("https://github.com/%s/%s.git", *compose.Owner, *compose.Repository)
	} else {
		repoURL = *compose.Repository
	}

	cmd := fmt.Sprintf("git clone --branch %s --depth 1 %s %s", *compose.Branch, repoURL, composeDir)
	writeLog(fmt.Sprintf("Cloning %s (branch: %s)", repoURL, *compose.Branch))
	_, err := process.ExecAsyncStream(cmd, writeLog)
	return err
}

func (s *ComposeService) composeUpLocal(compose *schema.Compose, composeDir string, writeLog func(string)) error {
	// 隔离部署：创建独立网络（stack 使用 overlay 驱动）
	if compose.IsolatedDeployment {
		driverFlag := ""
		if compose.ComposeType == schema.ComposeTypeStack {
			driverFlag = "--driver overlay "
		}
		networkCmd := fmt.Sprintf("docker network inspect %s >/dev/null 2>&1 || docker network create %s--attachable %s",
			compose.AppName, driverFlag, compose.AppName)
		process.ExecAsyncStream(networkCmd, writeLog)
	}

	var cmd string

	switch compose.ComposeType {
	case schema.ComposeTypeStack:
		stackName := compose.AppName
		if compose.ComposeSuffix != nil {
			stackName = *compose.ComposeSuffix
		}
		cmd = fmt.Sprintf("docker stack deploy -c docker-compose.yml %s --prune", stackName)
	default:
		cmd = fmt.Sprintf("docker compose -p %s up -d --build --remove-orphans", compose.AppName)
	}

	_, err := process.ExecAsyncStream(cmd, writeLog, process.WithDir(composeDir))
	if err != nil {
		return err
	}

	// 隔离部署：部署后连接 Traefik 到隔离网络
	if compose.IsolatedDeployment {
		connectCmd := fmt.Sprintf(`docker network connect %s $(docker ps --filter "name=dokploy-traefik" -q) 2>/dev/null || true`,
			compose.AppName)
		process.ExecAsyncStream(connectCmd, nil)
	}

	return nil
}

// Stop stops a compose project (与 TS 版一致：docker compose stop 或 docker stack rm)。
func (s *ComposeService) Stop(composeID string) error {
	compose, err := s.FindByID(composeID)
	if err != nil {
		return err
	}

	isRemote := compose.ServerID != nil && compose.Server != nil && compose.Server.SSHKey != nil

	switch compose.ComposeType {
	case schema.ComposeTypeStack:
		cmd := fmt.Sprintf("docker stack rm %s", compose.AppName)
		if isRemote {
			conn := s.sshConn(compose.Server)
			_, err = process.ExecAsyncRemote(conn, cmd, nil)
		} else {
			_, err = process.ExecAsyncStream(cmd, nil)
		}
	default:
		cmd := fmt.Sprintf("docker compose -p %s stop", compose.AppName)
		if isRemote {
			// 远程：使用远程路径
			remoteDir := fmt.Sprintf("/etc/dokploy/compose/%s/code", compose.AppName)
			conn := s.sshConn(compose.Server)
			_, err = process.ExecAsyncRemote(conn, fmt.Sprintf("cd %s && %s", remoteDir, cmd), nil)
		} else {
			composeDir := filepath.Join(s.cfg.Paths.ComposePath, compose.AppName)
			_, err = process.ExecAsyncStream(cmd, nil, process.WithDir(composeDir))
		}
	}

	if err != nil {
		s.updateStatus(composeID, schema.ApplicationStatusError)
		return err
	}

	s.updateStatus(composeID, schema.ApplicationStatusIdle)
	return nil
}

// Start starts a stopped compose project (与 TS 版一致：docker compose up -d，不 rebuild)。
func (s *ComposeService) Start(composeID string) error {
	compose, err := s.FindByID(composeID)
	if err != nil {
		return err
	}

	if compose.ComposeType == schema.ComposeTypeStack {
		return fmt.Errorf("stack compose does not support start, use deploy instead")
	}

	composePath := compose.ComposePath
	if composePath == "" {
		composePath = "./docker-compose.yml"
	}
	cmd := fmt.Sprintf("docker compose -p %s -f %s up -d", compose.AppName, composePath)

	isRemote := compose.ServerID != nil && compose.Server != nil && compose.Server.SSHKey != nil
	if isRemote {
		remoteDir := fmt.Sprintf("/etc/dokploy/compose/%s/code", compose.AppName)
		conn := s.sshConn(compose.Server)
		_, err = process.ExecAsyncRemote(conn, fmt.Sprintf("cd %s && %s", remoteDir, cmd), nil)
	} else {
		composeDir := filepath.Join(s.cfg.Paths.ComposePath, compose.AppName, "code")
		_, err = process.ExecAsyncStream(cmd, nil, process.WithDir(composeDir))
	}

	if err != nil {
		s.updateStatus(composeID, schema.ApplicationStatusIdle)
		return err
	}

	s.updateStatus(composeID, schema.ApplicationStatusDone)
	return nil
}

func (s *ComposeService) updateStatus(composeID string, status schema.ApplicationStatus) {
	s.db.Model(&schema.Compose{}).
		Where("\"composeId\" = ?", composeID).
		Update("composeStatus", status)
}

func (s *ComposeService) failDeployment(deploymentID, composeID, errMsg string) {
	now := time.Now().UTC().Format(time.RFC3339)
	s.db.Model(&schema.Deployment{}).
		Where("\"deploymentId\" = ?", deploymentID).
		Updates(map[string]interface{}{
			"status":       schema.DeploymentStatusError,
			"errorMessage": errMsg,
			"finishedAt":   now,
		})
	s.updateStatus(composeID, schema.ApplicationStatusError)
}

// sshConn 从 Server 构建 SSH 连接参数
func (s *ComposeService) sshConn(server *schema.Server) process.SSHConnection {
	return process.SSHConnection{
		Host:       server.IPAddress,
		Port:       server.Port,
		Username:   server.Username,
		PrivateKey: server.SSHKey.PrivateKey,
	}
}

// base64Encode 将字符串 base64 编码（用于通过 SSH 安全传输文件内容）
func base64Encode(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}

