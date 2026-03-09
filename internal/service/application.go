// Input: db, docker, git, builder, traefik, notify, config
// Output: ApplicationService (DeployApplication 全流程: clone→build→service create→traefik config)
// Role: 应用部署编排服务，实现完整的 CI/CD 流水线：Git 克隆 → 构建 → Docker Service 创建 → 路由配置
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/dokploy/dokploy/internal/builder"
	"github.com/dokploy/dokploy/internal/config"
	"github.com/dokploy/dokploy/internal/db"
	"github.com/dokploy/dokploy/internal/db/schema"
	"github.com/dokploy/dokploy/internal/docker"
	"github.com/dokploy/dokploy/internal/git"
	"github.com/dokploy/dokploy/internal/process"
	"gorm.io/gorm"
)

// ApplicationService handles application business logic.
type ApplicationService struct {
	db     *db.DB
	docker *docker.Client
	cfg    *config.Config
}

// NewApplicationService creates a new ApplicationService.
func NewApplicationService(database *db.DB, dockerClient *docker.Client, cfg *config.Config) *ApplicationService {
	return &ApplicationService{db: database, docker: dockerClient, cfg: cfg}
}

// FindByID finds an application by ID with common preloads.
func (s *ApplicationService) FindByID(appID string) (*schema.Application, error) {
	var app schema.Application
	err := s.db.
		Preload("Deployments", func(db *gorm.DB) *gorm.DB {
			return db.Order("\"createdAt\" DESC").Limit(5)
		}).
		Preload("Domains").
		Preload("Mounts").
		Preload("Redirects").
		Preload("Security").
		Preload("Ports").
		Preload("Environment").
		Preload("Server").
		Preload("Server.SSHKey").
		Preload("Registry").
		Preload("Github").
		Preload("Gitlab").
		Preload("Gitea").
		Preload("Bitbucket").
		Preload("CustomGitSSHKey").
		First(&app, "\"applicationId\" = ?", appID).Error
	if err != nil {
		return nil, err
	}
	return &app, nil
}

// Deploy performs the full deployment pipeline for an application.
func (s *ApplicationService) Deploy(appID string, titleOverride, descOverride *string) error {
	app, err := s.FindByID(appID)
	if err != nil {
		return fmt.Errorf("application not found: %w", err)
	}

	// Create deployment record
	title := fmt.Sprintf("Deploy %s", app.Name)
	if titleOverride != nil {
		title = *titleOverride
	}

	now := time.Now().UTC()
	logPath := filepath.Join(s.cfg.Paths.LogsPath, app.AppName, fmt.Sprintf("%d.log", now.UnixMilli()))

	// Ensure log directory exists
	if err := os.MkdirAll(filepath.Dir(logPath), 0755); err != nil {
		return fmt.Errorf("failed to create log directory: %w", err)
	}

	deployment := &schema.Deployment{
		Title:         title,
		Description:   descOverride,
		LogPath:       logPath,
		ApplicationID: &app.ApplicationID,
		ServerID:      app.ServerID,
	}

	if err := s.db.Create(deployment).Error; err != nil {
		return fmt.Errorf("failed to create deployment: %w", err)
	}

	// Update application status
	s.updateStatus(app.ApplicationID, schema.ApplicationStatusRunning)

	// Start deployment in background
	go s.runDeploy(app, deployment)

	return nil
}

func (s *ApplicationService) runDeploy(app *schema.Application, deployment *schema.Deployment) {
	logFile, err := os.OpenFile(deployment.LogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		s.failDeployment(deployment.DeploymentID, app.ApplicationID, fmt.Sprintf("Failed to open log file: %v", err))
		return
	}
	defer logFile.Close()

	writeLog := func(msg string) {
		logFile.WriteString(msg + "\n")
	}

	writeLog(fmt.Sprintf("Starting deployment for %s (%s)", app.Name, app.AppName))
	writeLog(fmt.Sprintf("Build type: %s, Source: %s", app.BuildType, app.SourceType))

	isRemote := app.ServerID != nil && app.Server != nil && app.Server.SSHKey != nil

	if isRemote {
		// 远程部署：与 TS 版一致，将整个流水线（clone + build）作为 SSH 命令发送到远程服务器
		if err := s.runDeployRemote(app, deployment, writeLog); err != nil {
			s.failDeployment(deployment.DeploymentID, app.ApplicationID, err.Error())
			return
		}
	} else {
		// 本地部署
		buildDir := filepath.Join(s.cfg.Paths.ApplicationsPath, app.AppName, "code")

		writeLog("--- Preparing source code ---")
		if err := s.prepareSource(app, buildDir, writeLog); err != nil {
			s.failDeployment(deployment.DeploymentID, app.ApplicationID, err.Error())
			return
		}

		writeLog("--- Building application ---")
		if err := s.buildApplication(app, buildDir, writeLog); err != nil {
			s.failDeployment(deployment.DeploymentID, app.ApplicationID, err.Error())
			return
		}

		writeLog("--- Creating Docker service ---")
		if err := s.createService(app, writeLog); err != nil {
			s.failDeployment(deployment.DeploymentID, app.ApplicationID, err.Error())
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

	s.updateStatus(app.ApplicationID, schema.ApplicationStatusDone)
	writeLog("Deployment completed successfully!")
}

// runDeployRemote 远程部署：与 TS 版完全一致，将 clone + build 作为 SSH 命令发送
func (s *ApplicationService) runDeployRemote(app *schema.Application, deployment *schema.Deployment, writeLog func(string)) error {
	conn := process.SSHConnection{
		Host:       app.Server.IPAddress,
		Port:       app.Server.Port,
		Username:   app.Server.Username,
		PrivateKey: app.Server.SSHKey.PrivateKey,
	}

	buildDir := fmt.Sprintf("/etc/dokploy/applications/%s/code", app.AppName)

	// 与 TS 版一致：构建 set -e; clone; build 的完整命令
	command := "set -e;"

	if app.SourceType == schema.SourceTypeDocker {
		// Docker 源类型：直接 pull 镜像
		if app.DockerImage != nil {
			command += fmt.Sprintf(" docker pull %s;", *app.DockerImage)
		}
	} else {
		// Git 源类型：生成 clone 命令
		cloneCmd, err := git.GenerateCloneCommand(app, buildDir)
		if err != nil {
			return fmt.Errorf("failed to generate clone command: %w", err)
		}
		if cloneCmd != "" {
			command += " " + cloneCmd + ";"
		}

		// 生成 build 命令
		buildPath := buildDir
		if app.BuildPath != nil && *app.BuildPath != "/" {
			buildPath = filepath.Join(buildDir, *app.BuildPath)
		}

		opts := builder.BuildOptions{
			AppName:          app.AppName,
			BuildType:        string(app.BuildType),
			BuildPath:        buildPath,
			Dockerfile:       safeStr(app.Dockerfile),
			DockerContext:    safeStr(app.DockerContextPath),
			DockerBuildStage: safeStr(app.DockerBuildStage),
			HerokuVersion:    safeStr(app.HerokuVersion),
			RailpackVersion:  safeStr(app.RailpackVersion),
			PublishDir:       safeStr(app.PublishDirectory),
			CleanCache:       app.CleanCache != nil && *app.CleanCache,
		}
		if app.BuildArgs != nil && *app.BuildArgs != "" {
			opts.BuildArgs = parseEnvString(*app.BuildArgs)
		}

		buildCmd, err := builder.GenerateBuildCommand(opts)
		if err != nil {
			return fmt.Errorf("failed to generate build command: %w", err)
		}
		command += " " + buildCmd + ";"
	}

	// 通过 SSH 执行完整命令（与 TS 版一致：包含日志重定向）
	commandWithLog := fmt.Sprintf("(%s) >> %s 2>&1", command, deployment.LogPath)
	writeLog("--- Running deploy on remote server ---")
	_, err := process.ExecAsyncRemote(conn, commandWithLog, writeLog)
	if err != nil {
		return fmt.Errorf("remote deploy failed: %w", err)
	}

	// 服务创建/更新（远程）
	writeLog("--- Creating Docker service on remote ---")
	return s.createServiceRemote(app, writeLog)
}

func (s *ApplicationService) prepareSource(app *schema.Application, buildDir string, writeLog func(string)) error {
	result, err := git.CloneWithAuth(app, buildDir, writeLog)
	if err != nil {
		return err
	}

	if result != nil && result.CommitHash != "" {
		writeLog(fmt.Sprintf("Commit: %s %s", result.CommitHash[:12], result.CommitMessage))
	}

	return nil
}

func (s *ApplicationService) buildApplication(app *schema.Application, buildDir string, writeLog func(string)) error {
	if app.SourceType == schema.SourceTypeDocker {
		writeLog(fmt.Sprintf("Using Docker image: %s", safeStr(app.DockerImage)))
		return nil
	}

	buildPath := buildDir
	if app.BuildPath != nil && *app.BuildPath != "/" {
		buildPath = filepath.Join(buildDir, *app.BuildPath)
	}

	opts := builder.BuildOptions{
		AppName:         app.AppName,
		BuildType:       string(app.BuildType),
		BuildPath:       buildPath,
		Dockerfile:      safeStr(app.Dockerfile),
		DockerContext:    safeStr(app.DockerContextPath),
		DockerBuildStage: safeStr(app.DockerBuildStage),
		HerokuVersion:   safeStr(app.HerokuVersion),
		RailpackVersion: safeStr(app.RailpackVersion),
		PublishDir:      safeStr(app.PublishDirectory),
		CleanCache:      app.CleanCache != nil && *app.CleanCache,
	}

	// Parse build args
	if app.BuildArgs != nil && *app.BuildArgs != "" {
		opts.BuildArgs = parseEnvString(*app.BuildArgs)
	}

	cmd, err := builder.GenerateBuildCommand(opts)
	if err != nil {
		return err
	}

	writeLog(fmt.Sprintf("Build command: %s", cmd))
	_, err = process.ExecAsyncStream(cmd, writeLog)
	return err
}

func (s *ApplicationService) createService(app *schema.Application, writeLog func(string)) error {
	if app.ServerID != nil {
		return s.createServiceRemote(app, writeLog)
	}
	return s.createServiceLocal(app, writeLog)
}

func (s *ApplicationService) createServiceLocal(app *schema.Application, writeLog func(string)) error {
	imageName := app.AppName
	if app.SourceType == schema.SourceTypeDocker && app.DockerImage != nil {
		imageName = *app.DockerImage
	}

	// Build docker service create command
	cmd := fmt.Sprintf("docker service create --name %s --network dokploy-network", app.AppName)

	// Environment variables
	if app.Env != nil && *app.Env != "" {
		envMap := parseEnvString(*app.Env)
		for k, v := range envMap {
			cmd += fmt.Sprintf(" --env %s=%s", k, v)
		}
	}

	// Resource limits
	if app.MemoryLimit != nil && *app.MemoryLimit != "" {
		cmd += fmt.Sprintf(" --limit-memory %s", *app.MemoryLimit)
	}
	if app.CPULimit != nil && *app.CPULimit != "" {
		cmd += fmt.Sprintf(" --limit-cpu %s", *app.CPULimit)
	}

	// Replicas
	cmd += fmt.Sprintf(" --replicas %d", app.Replicas)

	// Command override
	if app.Command != nil && *app.Command != "" {
		cmd += fmt.Sprintf(" %s %s", imageName, *app.Command)
	} else {
		cmd += " " + imageName
	}

	writeLog(fmt.Sprintf("Creating service: %s", app.AppName))

	// Check if service already exists, if so update instead
	_, err := process.ExecAsync(fmt.Sprintf("docker service inspect %s", app.AppName))
	if err == nil {
		// Service exists, update it
		updateCmd := fmt.Sprintf("docker service update --force --image %s %s", imageName, app.AppName)
		writeLog("Service exists, updating...")
		_, err = process.ExecAsyncStream(updateCmd, writeLog)
		return err
	}

	_, err = process.ExecAsyncStream(cmd, writeLog)
	return err
}

func (s *ApplicationService) createServiceRemote(app *schema.Application, writeLog func(string)) error {
	if app.Server == nil || app.Server.SSHKey == nil {
		return fmt.Errorf("server or SSH key not found")
	}

	imageName := app.AppName
	if app.SourceType == schema.SourceTypeDocker && app.DockerImage != nil {
		imageName = *app.DockerImage
	}

	cmd := fmt.Sprintf("docker service update --force --image %s %s || docker service create --name %s --network dokploy-network %s",
		imageName, app.AppName, app.AppName, imageName)

	conn := process.SSHConnection{
		Host:       app.Server.IPAddress,
		Port:       app.Server.Port,
		Username:   app.Server.Username,
		PrivateKey: app.Server.SSHKey.PrivateKey,
	}

	_, err := process.ExecAsyncRemote(conn, cmd, writeLog)
	return err
}

// Rebuild rebuilds an application without re-cloning source code.
// 对应 TS 版 rebuildApplication：跳过 git clone，仅从已有代码 build + 重启服务。
func (s *ApplicationService) Rebuild(appID string, titleOverride, descOverride *string) error {
	app, err := s.FindByID(appID)
	if err != nil {
		return fmt.Errorf("application not found: %w", err)
	}

	title := fmt.Sprintf("Rebuild %s", app.Name)
	if titleOverride != nil {
		title = *titleOverride
	}

	now := time.Now().UTC()
	logPath := filepath.Join(s.cfg.Paths.LogsPath, app.AppName, fmt.Sprintf("%d.log", now.UnixMilli()))
	os.MkdirAll(filepath.Dir(logPath), 0755)

	deployment := &schema.Deployment{
		Title:         title,
		Description:   descOverride,
		LogPath:       logPath,
		ApplicationID: &app.ApplicationID,
		ServerID:      app.ServerID,
	}

	if err := s.db.Create(deployment).Error; err != nil {
		return fmt.Errorf("failed to create deployment: %w", err)
	}

	s.updateStatus(app.ApplicationID, schema.ApplicationStatusRunning)
	go s.runRebuild(app, deployment)
	return nil
}

func (s *ApplicationService) runRebuild(app *schema.Application, deployment *schema.Deployment) {
	logFile, err := os.OpenFile(deployment.LogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		s.failDeployment(deployment.DeploymentID, app.ApplicationID, fmt.Sprintf("Failed to open log file: %v", err))
		return
	}
	defer logFile.Close()

	writeLog := func(msg string) {
		logFile.WriteString(msg + "\n")
	}

	writeLog(fmt.Sprintf("Rebuilding %s (%s) - skipping source clone", app.Name, app.AppName))

	isRemote := app.ServerID != nil && app.Server != nil && app.Server.SSHKey != nil

	if isRemote {
		// 远程 rebuild：跳过 clone，仅 build + 重建服务
		conn := process.SSHConnection{
			Host:       app.Server.IPAddress,
			Port:       app.Server.Port,
			Username:   app.Server.Username,
			PrivateKey: app.Server.SSHKey.PrivateKey,
		}

		buildDir := fmt.Sprintf("/etc/dokploy/applications/%s/code", app.AppName)
		buildPath := buildDir
		if app.BuildPath != nil && *app.BuildPath != "/" {
			buildPath = filepath.Join(buildDir, *app.BuildPath)
		}

		opts := builder.BuildOptions{
			AppName:          app.AppName,
			BuildType:        string(app.BuildType),
			BuildPath:        buildPath,
			Dockerfile:       safeStr(app.Dockerfile),
			DockerContext:    safeStr(app.DockerContextPath),
			DockerBuildStage: safeStr(app.DockerBuildStage),
			HerokuVersion:    safeStr(app.HerokuVersion),
			RailpackVersion:  safeStr(app.RailpackVersion),
			PublishDir:       safeStr(app.PublishDirectory),
			CleanCache:       app.CleanCache != nil && *app.CleanCache,
		}
		if app.BuildArgs != nil && *app.BuildArgs != "" {
			opts.BuildArgs = parseEnvString(*app.BuildArgs)
		}

		buildCmd, err := builder.GenerateBuildCommand(opts)
		if err != nil {
			s.failDeployment(deployment.DeploymentID, app.ApplicationID, err.Error())
			return
		}

		command := fmt.Sprintf("set -e; %s", buildCmd)
		commandWithLog := fmt.Sprintf("(%s) >> %s 2>&1", command, deployment.LogPath)
		_, err = process.ExecAsyncRemote(conn, commandWithLog, writeLog)
		if err != nil {
			s.failDeployment(deployment.DeploymentID, app.ApplicationID, err.Error())
			return
		}

		if err := s.createServiceRemote(app, writeLog); err != nil {
			s.failDeployment(deployment.DeploymentID, app.ApplicationID, err.Error())
			return
		}
	} else {
		// 本地 rebuild
		buildDir := filepath.Join(s.cfg.Paths.ApplicationsPath, app.AppName, "code")
		writeLog("--- Building application ---")
		if err := s.buildApplication(app, buildDir, writeLog); err != nil {
			s.failDeployment(deployment.DeploymentID, app.ApplicationID, err.Error())
			return
		}

		writeLog("--- Creating Docker service ---")
		if err := s.createService(app, writeLog); err != nil {
			s.failDeployment(deployment.DeploymentID, app.ApplicationID, err.Error())
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
	s.updateStatus(app.ApplicationID, schema.ApplicationStatusDone)
	writeLog("Rebuild completed successfully!")
}

// Reload restarts the Docker service without rebuilding.
// 对应 TS 版 reload：同步操作，仅 docker service update --force 重启容器。
func (s *ApplicationService) Reload(appID string) error {
	app, err := s.FindByID(appID)
	if err != nil {
		return err
	}

	s.updateStatus(appID, schema.ApplicationStatusIdle)

	if app.ServerID != nil && app.Server != nil && app.Server.SSHKey != nil {
		conn := process.SSHConnection{
			Host:       app.Server.IPAddress,
			Port:       app.Server.Port,
			Username:   app.Server.Username,
			PrivateKey: app.Server.SSHKey.PrivateKey,
		}
		_, err = process.ExecAsyncRemote(conn, fmt.Sprintf("docker service update --force %s", app.AppName), nil)
	} else {
		_, err = process.ExecAsync(fmt.Sprintf("docker service update --force %s", app.AppName))
	}

	if err != nil {
		s.updateStatus(appID, schema.ApplicationStatusError)
		return err
	}

	s.updateStatus(appID, schema.ApplicationStatusDone)
	return nil
}

// Stop stops an application's Docker service.
func (s *ApplicationService) Stop(appID string) error {
	app, err := s.FindByID(appID)
	if err != nil {
		return err
	}

	if app.ServerID != nil && app.Server != nil && app.Server.SSHKey != nil {
		conn := process.SSHConnection{
			Host:       app.Server.IPAddress,
			Port:       app.Server.Port,
			Username:   app.Server.Username,
			PrivateKey: app.Server.SSHKey.PrivateKey,
		}
		_, err = process.ExecAsyncRemote(conn, fmt.Sprintf("docker service scale %s=0", app.AppName), nil)
	} else {
		err = s.docker.ScaleService(context.Background(), app.AppName, 0)
	}

	if err != nil {
		return err
	}

	s.updateStatus(appID, schema.ApplicationStatusIdle)
	return nil
}

// Start starts an application's Docker service.
func (s *ApplicationService) Start(appID string) error {
	app, err := s.FindByID(appID)
	if err != nil {
		return err
	}

	replicas := uint64(app.Replicas)
	if replicas == 0 {
		replicas = 1
	}

	if app.ServerID != nil && app.Server != nil && app.Server.SSHKey != nil {
		conn := process.SSHConnection{
			Host:       app.Server.IPAddress,
			Port:       app.Server.Port,
			Username:   app.Server.Username,
			PrivateKey: app.Server.SSHKey.PrivateKey,
		}
		_, err = process.ExecAsyncRemote(conn, fmt.Sprintf("docker service scale %s=%d", app.AppName, replicas), nil)
	} else {
		err = s.docker.ScaleService(context.Background(), app.AppName, replicas)
	}

	if err != nil {
		return err
	}

	s.updateStatus(appID, schema.ApplicationStatusDone)
	return nil
}

// Delete removes an application and its Docker service.
func (s *ApplicationService) Delete(appID string) error {
	app, err := s.FindByID(appID)
	if err != nil {
		return err
	}

	// Remove Docker service
	if app.ServerID != nil && app.Server != nil && app.Server.SSHKey != nil {
		conn := process.SSHConnection{
			Host:       app.Server.IPAddress,
			Port:       app.Server.Port,
			Username:   app.Server.Username,
			PrivateKey: app.Server.SSHKey.PrivateKey,
		}
		process.ExecAsyncRemote(conn, fmt.Sprintf("docker service rm %s", app.AppName), nil)
	} else {
		s.docker.RemoveService(context.Background(), app.AppName)
	}

	// Remove from database (cascades to deployments, domains, etc.)
	return s.db.Delete(&schema.Application{}, "\"applicationId\" = ?", appID).Error
}

func (s *ApplicationService) updateStatus(appID string, status schema.ApplicationStatus) {
	s.db.Model(&schema.Application{}).
		Where("\"applicationId\" = ?", appID).
		Update("applicationStatus", status)
}

func (s *ApplicationService) failDeployment(deploymentID, appID, errMsg string) {
	now := time.Now().UTC().Format(time.RFC3339)
	s.db.Model(&schema.Deployment{}).
		Where("\"deploymentId\" = ?", deploymentID).
		Updates(map[string]interface{}{
			"status":       schema.DeploymentStatusError,
			"errorMessage": errMsg,
			"finishedAt":   now,
		})
	s.updateStatus(appID, schema.ApplicationStatusError)
}

func safeStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func parseEnvString(envStr string) map[string]string {
	result := make(map[string]string)
	for _, line := range splitLines(envStr) {
		line = trimSpace(line)
		if line == "" || line[0] == '#' {
			continue
		}
		idx := indexOf(line, '=')
		if idx > 0 {
			result[line[:idx]] = line[idx+1:]
		}
	}
	return result
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func trimSpace(s string) string {
	start := 0
	for start < len(s) && (s[start] == ' ' || s[start] == '\t' || s[start] == '\r') {
		start++
	}
	end := len(s)
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}

func indexOf(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}
