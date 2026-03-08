// Input: db, docker, git, compose/transform, config
// Output: ComposeService (DeployCompose 全流程: clone→compose transform→docker compose up/stack deploy)
// Role: Compose 部署编排服务，支持 docker-compose 和 stack 两种部署模式
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package service

import (
	"fmt"
	"os"
	"path/filepath"
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

	// Prepare compose directory
	composeDir := filepath.Join(s.cfg.Paths.ComposePath, compose.AppName)
	os.MkdirAll(composeDir, 0755)

	// Step 1: Prepare source (clone or write raw compose file)
	if err := s.prepareComposeSource(compose, composeDir, writeLog); err != nil {
		s.failDeployment(deployment.DeploymentID, compose.ComposeID, err.Error())
		return
	}

	// Step 2: Run compose up
	if err := s.composeUp(compose, composeDir, writeLog); err != nil {
		s.failDeployment(deployment.DeploymentID, compose.ComposeID, err.Error())
		return
	}

	// Mark as done
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

func (s *ComposeService) composeUp(compose *schema.Compose, composeDir string, writeLog func(string)) error {
	var cmd string

	switch compose.ComposeType {
	case schema.ComposeTypeStack:
		// Docker stack deploy
		stackName := compose.AppName
		if compose.ComposeSuffix != nil {
			stackName = *compose.ComposeSuffix
		}
		cmd = fmt.Sprintf("docker stack deploy -c docker-compose.yml %s --prune", stackName)
	default:
		// Docker compose
		projectName := compose.AppName
		cmd = fmt.Sprintf("docker compose -p %s up -d --build --remove-orphans", projectName)
	}

	if compose.ServerID != nil && compose.Server != nil && compose.Server.SSHKey != nil {
		// Remote execution
		conn := process.SSHConnection{
			Host:       compose.Server.IPAddress,
			Port:       compose.Server.Port,
			Username:   compose.Server.Username,
			PrivateKey: compose.Server.SSHKey.PrivateKey,
		}
		fullCmd := fmt.Sprintf("cd %s && %s", composeDir, cmd)
		_, err := process.ExecAsyncRemote(conn, fullCmd, writeLog)
		return err
	}

	// Local execution
	_, err := process.ExecAsyncStream(cmd, writeLog, process.WithDir(composeDir))
	return err
}

// Stop stops a compose project (与 TS 版一致：docker compose stop 或 docker stack rm)。
func (s *ComposeService) Stop(composeID string) error {
	compose, err := s.FindByID(composeID)
	if err != nil {
		return err
	}

	composeDir := filepath.Join(s.cfg.Paths.ComposePath, compose.AppName)

	switch compose.ComposeType {
	case schema.ComposeTypeStack:
		// Stack 模式：docker stack rm
		cmd := fmt.Sprintf("docker stack rm %s", compose.AppName)
		if compose.ServerID != nil && compose.Server != nil && compose.Server.SSHKey != nil {
			conn := process.SSHConnection{
				Host:       compose.Server.IPAddress,
				Port:       compose.Server.Port,
				Username:   compose.Server.Username,
				PrivateKey: compose.Server.SSHKey.PrivateKey,
			}
			_, err = process.ExecAsyncRemote(conn, cmd, nil)
		} else {
			_, err = process.ExecAsyncStream(cmd, nil)
		}
	default:
		// docker-compose 模式：docker compose stop（仅停止，不删除容器，与 TS 版一致）
		cmd := fmt.Sprintf("docker compose -p %s stop", compose.AppName)
		if compose.ServerID != nil && compose.Server != nil && compose.Server.SSHKey != nil {
			conn := process.SSHConnection{
				Host:       compose.Server.IPAddress,
				Port:       compose.Server.Port,
				Username:   compose.Server.Username,
				PrivateKey: compose.Server.SSHKey.PrivateKey,
			}
			_, err = process.ExecAsyncRemote(conn, fmt.Sprintf("cd %s && %s", composeDir, cmd), nil)
		} else {
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

	// Stack 模式无 start 概念（需要重新 deploy），仅支持 docker-compose 模式
	if compose.ComposeType == schema.ComposeTypeStack {
		return fmt.Errorf("stack compose does not support start, use deploy instead")
	}

	composeDir := filepath.Join(s.cfg.Paths.ComposePath, compose.AppName, "code")
	// 使用数据库中的 composePath（与 TS 版一致）
	composePath := compose.ComposePath
	if composePath == "" {
		composePath = "./docker-compose.yml"
	}
	cmd := fmt.Sprintf("docker compose -p %s -f %s up -d", compose.AppName, composePath)

	if compose.ServerID != nil && compose.Server != nil && compose.Server.SSHKey != nil {
		conn := process.SSHConnection{
			Host:       compose.Server.IPAddress,
			Port:       compose.Server.Port,
			Username:   compose.Server.Username,
			PrivateKey: compose.Server.SSHKey.PrivateKey,
		}
		_, err = process.ExecAsyncRemote(conn, fmt.Sprintf("cd %s && %s", composeDir, cmd), nil)
	} else {
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
