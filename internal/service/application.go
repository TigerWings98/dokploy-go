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

	// Step 1: Clone/prepare source code
	writeLog("--- Preparing source code ---")
	buildDir := filepath.Join(s.cfg.Paths.ApplicationsPath, app.AppName, "code")
	if err := s.prepareSource(app, buildDir, writeLog); err != nil {
		s.failDeployment(deployment.DeploymentID, app.ApplicationID, err.Error())
		return
	}

	// Step 2: Build
	writeLog("--- Building application ---")
	if err := s.buildApplication(app, buildDir, writeLog); err != nil {
		s.failDeployment(deployment.DeploymentID, app.ApplicationID, err.Error())
		return
	}

	// Step 3: Create/update Docker service
	writeLog("--- Creating Docker service ---")
	if err := s.createService(app, writeLog); err != nil {
		s.failDeployment(deployment.DeploymentID, app.ApplicationID, err.Error())
		return
	}

	// Mark deployment as done
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

func (s *ApplicationService) prepareSource(app *schema.Application, buildDir string, writeLog func(string)) error {
	if err := os.MkdirAll(buildDir, 0755); err != nil {
		return fmt.Errorf("failed to create build directory: %w", err)
	}

	switch app.SourceType {
	case schema.SourceTypeGithub, schema.SourceTypeGitlab, schema.SourceTypeBitbucket, schema.SourceTypeGitea:
		return s.cloneGitRepo(app, buildDir, writeLog)
	case schema.SourceTypeGit:
		return s.cloneCustomGit(app, buildDir, writeLog)
	case schema.SourceTypeDocker:
		writeLog("Using Docker image directly, no source to clone")
		return nil
	case schema.SourceTypeDrop:
		writeLog("Using uploaded source (drop)")
		return nil
	default:
		return fmt.Errorf("unsupported source type: %s", app.SourceType)
	}
}

func (s *ApplicationService) cloneGitRepo(app *schema.Application, buildDir string, writeLog func(string)) error {
	var repoURL, branch string

	switch app.SourceType {
	case schema.SourceTypeGithub:
		if app.Repository == nil || app.Owner == nil || app.Branch == nil {
			return fmt.Errorf("github source requires repository, owner, and branch")
		}
		repoURL = fmt.Sprintf("https://github.com/%s/%s.git", *app.Owner, *app.Repository)
		branch = *app.Branch
	case schema.SourceTypeGitlab:
		if app.GitlabRepository == nil || app.GitlabBranch == nil {
			return fmt.Errorf("gitlab source requires repository and branch")
		}
		repoURL = *app.GitlabRepository
		branch = *app.GitlabBranch
	case schema.SourceTypeBitbucket:
		if app.BitbucketOwner == nil || app.BitbucketRepository == nil || app.BitbucketBranch == nil {
			return fmt.Errorf("bitbucket source requires owner, repository, and branch")
		}
		repoURL = fmt.Sprintf("https://bitbucket.org/%s/%s.git", *app.BitbucketOwner, *app.BitbucketRepository)
		branch = *app.BitbucketBranch
	case schema.SourceTypeGitea:
		if app.GiteaRepository == nil || app.GiteaBranch == nil {
			return fmt.Errorf("gitea source requires repository and branch")
		}
		repoURL = *app.GiteaRepository
		branch = *app.GiteaBranch
	}

	cmd := fmt.Sprintf("git clone --branch %s --depth 1 %s %s", branch, repoURL, buildDir)
	if app.EnableSubmodules {
		cmd = fmt.Sprintf("git clone --branch %s --depth 1 --recurse-submodules %s %s", branch, repoURL, buildDir)
	}

	writeLog(fmt.Sprintf("Cloning %s (branch: %s)", repoURL, branch))
	_, err := process.ExecAsyncStream(cmd, writeLog)
	return err
}

func (s *ApplicationService) cloneCustomGit(app *schema.Application, buildDir string, writeLog func(string)) error {
	if app.CustomGitURL == nil || app.CustomGitBranch == nil {
		return fmt.Errorf("custom git source requires URL and branch")
	}

	cmd := fmt.Sprintf("git clone --branch %s --depth 1 %s %s", *app.CustomGitBranch, *app.CustomGitURL, buildDir)
	writeLog(fmt.Sprintf("Cloning %s (branch: %s)", *app.CustomGitURL, *app.CustomGitBranch))
	_, err := process.ExecAsyncStream(cmd, writeLog)
	return err
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

	s.updateStatus(appID, schema.ApplicationStatusRunning)
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
