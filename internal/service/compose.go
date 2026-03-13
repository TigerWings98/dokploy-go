// Input: db, docker, git, compose/transform, config
// Output: ComposeService (DeployCompose 全流程: clone→compose transform→docker compose up/stack deploy)
// Role: Compose 部署编排服务，支持 docker-compose 和 stack 两种部署模式
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package service

import (
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	composepkg "github.com/dokploy/dokploy/internal/compose"
	"github.com/dokploy/dokploy/internal/config"
	"github.com/dokploy/dokploy/internal/db"
	"github.com/dokploy/dokploy/internal/db/schema"
	"github.com/dokploy/dokploy/internal/docker"
	"github.com/dokploy/dokploy/internal/email"
	"github.com/dokploy/dokploy/internal/notify"
	"github.com/dokploy/dokploy/internal/process"
	"gorm.io/gorm"
)

// ComposeService handles Docker Compose business logic.
type ComposeService struct {
	db       *db.DB
	docker   *docker.Client
	cfg      *config.Config
	notifier *notify.Notifier
}

// NewComposeService creates a new ComposeService.
func NewComposeService(database *db.DB, dockerClient *docker.Client, cfg *config.Config, notifier *notify.Notifier) *ComposeService {
	return &ComposeService{db: database, docker: dockerClient, cfg: cfg, notifier: notifier}
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
		Preload("Environment").
		Preload("Environment.Project").
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
		// 远程 rebuild：与 TS 版一致
		// Raw 类型：重新写入最新 compose 文件；Git 类型：使用已有代码
		// 然后通过 buildAndRunRemoteCompose 统一处理转换 + env + docker up
		conn := process.SSHConnection{
			Host:       compose.Server.IPAddress,
			Port:       compose.Server.Port,
			Username:   compose.Server.Username,
			PrivateKey: compose.Server.SSHKey.PrivateKey,
		}

		projectPath := fmt.Sprintf("/etc/dokploy/compose/%s/code", compose.AppName)

		// Raw 类型：重新写入 compose file（未转换的原始内容，转换在 buildAndRunRemoteCompose 统一处理）
		if compose.SourceType == schema.SourceTypeComposeRaw {
			encoded := base64Encode(compose.ComposeFile)
			writeCmd := fmt.Sprintf(`mkdir -p %s && echo "%s" | base64 -d > "%s/docker-compose.yml"`, projectPath, encoded, projectPath)
			_, err := process.ExecAsyncRemote(conn, writeCmd, writeLog)
			if err != nil {
				s.failDeployment(deployment.DeploymentID, compose.ComposeID, err.Error())
				return
			}
		}

		// 与 TS 版 getBuildComposeCommand 一致：读取 → 转换 → 写回 + env + docker up
		if err := s.buildAndRunRemoteCompose(conn, compose, deployment, projectPath, writeLog); err != nil {
			s.failDeployment(deployment.DeploymentID, compose.ComposeID, err.Error())
			return
		}
	} else {
		// 本地 rebuild：与 TS 版一致，源码在 ComposePath/appName/code/ 下
		composeDir := filepath.Join(s.cfg.Paths.ComposePath, compose.AppName, "code")
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

	// 发送重建成功通知
	s.sendComposeSuccessNotification(compose)
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
		// 本地部署：与 TS 版一致，源码放在 ComposePath/appName/code/ 下
		composeDir := filepath.Join(s.cfg.Paths.ComposePath, compose.AppName, "code")
		os.MkdirAll(composeDir, 0755)

		if err := s.prepareComposeSource(compose, composeDir, writeLog); err != nil {
			s.failDeployment(deployment.DeploymentID, compose.ComposeID, err.Error())
			return
		}

		// 应用补丁（与 TS 版 generateApplyPatchesCommand 一致）
		// 仅 Git 源码类型需要补丁，Raw 类型每次都重新写入完整文件
		if compose.SourceType != schema.SourceTypeComposeRaw {
			s.applyPatches(compose.ComposeID, composeDir, writeLog)
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

	// 发送部署成功通知
	s.sendComposeSuccessNotification(compose)
}

// runDeployRemote 远程部署：与 TS 版完全一致
// TS 版流程：1. 克隆/写入源码 → 2. 应用补丁 → 3. 读取 compose 文件 → 4. 转换（suffix/network） → 5. 写回 + env + docker up
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
		// Raw 类型：直接写入 compose 文件（网络注入在后续统一处理）
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

	// Step 2: 应用补丁（与 TS 版 generateApplyPatchesCommand 一致，仅 Git 源码类型需要）
	if compose.SourceType != schema.SourceTypeComposeRaw {
		patchCmd := s.generateRemotePatchCommand(compose.ComposeID, projectPath)
		if patchCmd != "" {
			writeLog("--- Applying patches on remote server ---")
			patchWithLog := fmt.Sprintf("(%s) >> %s 2>&1", patchCmd, deployment.LogPath)
			_, err = process.ExecAsyncRemote(conn, patchWithLog, writeLog)
			if err != nil {
				return fmt.Errorf("failed to apply patches on remote: %w", err)
			}
		}
	}

	// Step 3: 构建 compose up 命令（与 TS 版 getBuildComposeCommand 一致）
	// 读取远程 compose 文件 → 本地转换（suffix/network） → 生成写回命令
	return s.buildAndRunRemoteCompose(conn, compose, deployment, projectPath, writeLog)
}

// buildAndRunRemoteCompose 构建并执行远程 compose 部署命令
// 与 TS 版 getBuildComposeCommand + writeDomainsToCompose 一致：
// 1. 从远程读取 compose 文件（cat via SSH）
// 2. 在本地进行 suffix/network 转换
// 3. 生成完整 bash 命令：写回转换后的 compose + 创建 .env + docker up
func (s *ComposeService) buildAndRunRemoteCompose(conn process.SSHConnection, compose *schema.Compose, deployment *schema.Deployment, projectPath string, writeLog func(string)) error {
	composePth := "docker-compose.yml"
	if compose.ComposePath != "" && compose.SourceType != schema.SourceTypeComposeRaw {
		composePth = compose.ComposePath
	}
	remoteComposeFile := filepath.Join(projectPath, composePth)

	// 与 TS 版 loadDockerComposeRemote 一致：通过 SSH 读取远程 compose 文件
	catResult, err := process.ExecAsyncRemote(conn, fmt.Sprintf("cat %s", remoteComposeFile), nil)
	if err != nil {
		return fmt.Errorf("failed to read compose file from remote: %w", err)
	}
	composeContent := []byte(catResult.Stdout)

	// 与 TS 版 addDomainToCompose 中的转换逻辑一致：
	// 1. isolated → randomizeDeployableSpecificationFile (collision prevention + volume isolation)
	// 2. randomize → randomizeSpecificationFile (suffix to all properties)
	// 3. 网络注入（dokploy-network 或 isolated network）
	if compose.IsolatedDeployment {
		suffix := compose.AppName
		if compose.ComposeSuffix != nil && *compose.ComposeSuffix != "" {
			suffix = *compose.ComposeSuffix
		}
		// InjectIsolatedNetwork 包含了 appName 网络注入 + 可选的 volume suffix
		if transformed, err := composepkg.InjectIsolatedNetwork(composeContent, suffix, compose.IsolatedDeploymentsVolume); err == nil {
			composeContent = transformed
		}
	} else {
		// 非隔离：先 randomize suffix，再注入 dokploy-network
		if compose.RandomizeCompose != nil && *compose.RandomizeCompose && compose.ComposeSuffix != nil {
			if transformed, err := composepkg.AddSuffixToAll(composeContent, *compose.ComposeSuffix); err == nil {
				composeContent = transformed
			}
		}
		if transformed, err := composepkg.InjectDokployNetwork(composeContent); err == nil {
			composeContent = transformed
		}
	}

	// 与 TS 版 writeDomainsToCompose 一致：将转换后的 compose 内容编码为 base64 写回命令
	encodedCompose := base64Encode(string(composeContent))
	writeComposeCmd := fmt.Sprintf(`echo "%s" | base64 -d > "%s";`, encodedCompose, remoteComposeFile)

	// 生成 docker 命令（与 TS 版 createCommand 一致）
	var dockerCmd string
	if compose.Command != nil && *compose.Command != "" {
		dockerCmd = *compose.Command
	} else {
		switch compose.ComposeType {
		case schema.ComposeTypeStack:
			exportCmd := s.getExportEnvCommand(compose)
			dockerCmd = fmt.Sprintf("%sstack deploy -c %s %s --prune --with-registry-auth", exportCmd, composePth, compose.AppName)
		default:
			dockerCmd = fmt.Sprintf("compose -p %s -f %s up -d --build --remove-orphans", compose.AppName, composePth)
		}
	}

	// 构建 .env 文件命令（与 TS 版 getCreateEnvFileCommand 一致）
	envContent := fmt.Sprintf("APP_NAME=%s\n", compose.AppName)
	if compose.Env != nil && *compose.Env != "" {
		envContent += *compose.Env + "\n"
	}
	if !strings.Contains(envContent, "DOCKER_CONFIG") {
		envContent += "DOCKER_CONFIG=/root/.docker\n"
	}
	if compose.RandomizeCompose != nil && *compose.RandomizeCompose && compose.ComposeSuffix != nil {
		envContent += fmt.Sprintf("COMPOSE_PREFIX=%s\n", *compose.ComposeSuffix)
	}
	var projectEnv, environmentEnv string
	if compose.Environment != nil {
		environmentEnv = compose.Environment.Env
		if compose.Environment.Project != nil {
			projectEnv = compose.Environment.Project.Env
		}
	}
	resolvedLines := prepareEnvironmentVariables(envContent, projectEnv, environmentEnv)
	encodedEnv := base64Encode(strings.Join(resolvedLines, "\n") + "\n")

	envFilePath := filepath.Join(filepath.Dir(remoteComposeFile), ".env")

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
%s
touch %s;
echo "%s" | base64 -d > "%s";
cd "%s";
%s
env -i PATH="$PATH" docker %s 2>&1 || { echo "Error: ❌ Docker command failed"; exit 1; }
%s
echo "Docker Compose Deployed: ✅";`, writeComposeCmd, envFilePath, encodedEnv, envFilePath, projectPath, networkCmd, dockerCmd, networkConnectCmd)

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
	// 确定 compose 文件路径
	composePth := compose.ComposePath
	if composePth == "" || compose.SourceType == schema.SourceTypeComposeRaw {
		composePth = "docker-compose.yml"
	}

	// 创建 .env 文件（与 TS 版 getCreateEnvFileCommand 一致）
	s.createEnvFile(compose, composeDir, writeLog)

	// 应用 randomize 后缀转换（与 TS 版 randomizeSpecificationFile 一致）
	// 非隔离模式下，randomize=true 时给 services/volumes/networks/configs/secrets 加后缀
	if compose.RandomizeCompose != nil && *compose.RandomizeCompose && compose.ComposeSuffix != nil && !compose.IsolatedDeployment {
		s.applySuffixToComposeFile(compose, composeDir, composePth, *compose.ComposeSuffix, writeLog)
	}

	// 注入网络到 compose 文件（与 TS 版 addDomainToCompose 一致）
	// 非隔离部署：加入共享 dokploy-network（external: true）
	// 隔离部署：加入以 appName 命名的独立网络
	s.injectNetworkToComposeFile(compose, composeDir, writeLog)

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

	// 生成 docker 命令（与 TS 版 createCommand 一致）
	var cmd string
	if compose.Command != nil && *compose.Command != "" {
		// 用户自定义命令
		cmd = fmt.Sprintf("docker %s", *compose.Command)
	} else {
		switch compose.ComposeType {
		case schema.ComposeTypeStack:
			stackName := compose.AppName
			if compose.ComposeSuffix != nil {
				stackName = *compose.ComposeSuffix
			}
			// Stack deploy：需要 export 环境变量（与 TS 版 getExportEnvCommand 一致）
			exportCmd := s.getExportEnvCommand(compose)
			cmd = fmt.Sprintf("%sdocker stack deploy -c %s %s --prune --with-registry-auth", exportCmd, composePth, stackName)
		default:
			cmd = fmt.Sprintf("docker compose -p %s -f %s up -d --build --remove-orphans", compose.AppName, composePth)
		}
	}

	// 使用 env -i PATH="$PATH" 隔离环境变量（与 TS 版一致）
	// 确保 docker compose 只使用 .env 文件中的变量，不受宿主环境污染
	shellCmd := fmt.Sprintf(`env -i PATH="$PATH" %s`, cmd)

	_, err := process.ExecAsyncStream(shellCmd, writeLog, process.WithDir(composeDir))
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

// applySuffixToComposeFile 对 compose 文件应用 randomize 后缀
// 与 TS 版 randomizeSpecificationFile → addSuffixToAll 一致
func (s *ComposeService) applySuffixToComposeFile(compose *schema.Compose, composeDir, composePth, suffix string, writeLog func(string)) {
	composeFilePath := filepath.Join(composeDir, composePth)
	data, err := os.ReadFile(composeFilePath)
	if err != nil {
		if writeLog != nil {
			writeLog(fmt.Sprintf("Warning: failed to read compose file for suffix: %v", err))
		}
		return
	}

	result, err := composepkg.AddSuffixToAll(data, suffix)
	if err != nil {
		if writeLog != nil {
			writeLog(fmt.Sprintf("Warning: failed to apply suffix to compose file: %v", err))
		}
		return
	}

	if err := os.WriteFile(composeFilePath, result, 0644); err != nil {
		if writeLog != nil {
			writeLog(fmt.Sprintf("Warning: failed to write suffixed compose file: %v", err))
		}
		return
	}
	if writeLog != nil {
		writeLog(fmt.Sprintf("Applied suffix '%s' to compose file ✅", suffix))
	}
}

// getExportEnvCommand 为 Docker Stack deploy 生成环境变量 export 前缀
// 与 TS 版 getExportEnvCommand 一致：Stack 不支持 .env 文件，需要通过 shell export 传递
func (s *ComposeService) getExportEnvCommand(compose *schema.Compose) string {
	if compose.ComposeType != schema.ComposeTypeStack {
		return ""
	}

	envContent := ""
	if compose.Env != nil && *compose.Env != "" {
		envContent = *compose.Env
	}

	var projectEnv, environmentEnv string
	if compose.Environment != nil {
		environmentEnv = compose.Environment.Env
		if compose.Environment.Project != nil {
			projectEnv = compose.Environment.Project.Env
		}
	}

	resolved := prepareEnvironmentVariables(envContent, projectEnv, environmentEnv)
	if len(resolved) == 0 {
		return ""
	}

	// 构建 KEY=VALUE 格式的 export 前缀
	return strings.Join(resolved, " ") + " "
}

// applyPatches 应用数据库中的补丁到 compose 项目目录
// 与 TS 版 generateApplyPatchesCommand 一致：
// - create/update 类型：写入文件内容
// - delete 类型：删除文件
func (s *ComposeService) applyPatches(composeID, composeDir string, writeLog func(string)) {
	var patches []schema.Patch
	s.db.Where("\"composeId\" = ? AND enabled = true", composeID).Find(&patches)
	if len(patches) == 0 {
		return
	}

	writeLog(fmt.Sprintf("Applying %d patch(es)...", len(patches)))
	for _, p := range patches {
		filePath := filepath.Join(composeDir, p.FilePath)
		if p.Type == schema.PatchTypeDelete {
			os.Remove(filePath)
			writeLog(fmt.Sprintf("  Deleted: %s", p.FilePath))
		} else {
			// create 或 update：确保目录存在，写入内容
			os.MkdirAll(filepath.Dir(filePath), 0755)
			if err := os.WriteFile(filePath, []byte(p.Content), 0644); err != nil {
				writeLog(fmt.Sprintf("  Warning: failed to apply patch %s: %v", p.FilePath, err))
			} else {
				writeLog(fmt.Sprintf("  Applied: %s", p.FilePath))
			}
		}
	}
}

// generateRemotePatchCommand 生成远程补丁 shell 命令
// 与 TS 版 generateApplyPatchesCommand 完全一致：
// - create/update：base64 编码写入文件
// - delete：rm -f 删除文件
func (s *ComposeService) generateRemotePatchCommand(composeID, codePath string) string {
	var patches []schema.Patch
	s.db.Where("\"composeId\" = ? AND enabled = true", composeID).Find(&patches)
	if len(patches) == 0 {
		return ""
	}

	cmd := fmt.Sprintf(`set -e; echo "Applying %d patch(es)...";`, len(patches))
	for _, p := range patches {
		filePath := filepath.Join(codePath, p.FilePath)
		if p.Type == schema.PatchTypeDelete {
			cmd += fmt.Sprintf(`rm -f "%s";`, filePath)
		} else {
			encoded := base64Encode(p.Content)
			cmd += fmt.Sprintf(`
file="%s"
dir="$(dirname "$file")"
mkdir -p "$dir"
echo "%s" | base64 -d > "$file"
`, filePath, encoded)
		}
	}
	return cmd
}

// injectNetworkToComposeFile 注入网络配置到 compose 文件
// 与 TS 版 addDomainToCompose 中的网络注入逻辑一致：
// - 非隔离部署：注入 dokploy-network（external: true），使容器加入 Dokploy 共享网络
// - 隔离部署：注入以 appName 命名的独立网络（external: true）
func (s *ComposeService) injectNetworkToComposeFile(comp *schema.Compose, composeDir string, writeLog func(string)) {
	composePth := comp.ComposePath
	if composePth == "" {
		composePth = "docker-compose.yml"
	}
	composeFilePath := filepath.Join(composeDir, composePth)

	data, err := os.ReadFile(composeFilePath)
	if err != nil {
		if writeLog != nil {
			writeLog(fmt.Sprintf("Warning: failed to read compose file for network injection: %v", err))
		}
		return
	}

	var result []byte
	if comp.IsolatedDeployment {
		result, err = composepkg.InjectIsolatedNetwork(data, comp.AppName, comp.IsolatedDeploymentsVolume)
	} else {
		result, err = composepkg.InjectDokployNetwork(data)
	}
	if err != nil {
		if writeLog != nil {
			writeLog(fmt.Sprintf("Warning: failed to inject network into compose file: %v", err))
		}
		return
	}

	if err := os.WriteFile(composeFilePath, result, 0644); err != nil {
		if writeLog != nil {
			writeLog(fmt.Sprintf("Warning: failed to write network-injected compose file: %v", err))
		}
		return
	}
	if writeLog != nil {
		if comp.IsolatedDeployment {
			writeLog(fmt.Sprintf("Injected isolated network '%s' into compose file ✅", comp.AppName))
		} else {
			writeLog("Injected dokploy-network (external) into compose file ✅")
		}
	}
}

// createEnvFile 在 compose 项目目录创建 .env 文件
// 与 TS 版 getCreateEnvFileCommand 完全一致：
// 1. APP_NAME={appName}
// 2. 用户定义的环境变量（compose.Env）
// 3. DOCKER_CONFIG=/root/.docker（如果用户未设置）
// 4. COMPOSE_PREFIX={suffix}（如果 randomize 为 true）
// 5. 通过 prepareEnvironmentVariables 解析模板变量（${{project.X}}/${{environment.X}}/${{X}}）
func (s *ComposeService) createEnvFile(compose *schema.Compose, composeDir string, writeLog func(string)) {
	// 构建 env 内容（与 TS 版 getCreateEnvFileCommand 一致）
	envContent := fmt.Sprintf("APP_NAME=%s\n", compose.AppName)
	if compose.Env != nil && *compose.Env != "" {
		envContent += *compose.Env + "\n"
	}
	if !strings.Contains(envContent, "DOCKER_CONFIG") {
		envContent += "DOCKER_CONFIG=/root/.docker\n"
	}
	if compose.RandomizeCompose != nil && *compose.RandomizeCompose && compose.ComposeSuffix != nil {
		envContent += fmt.Sprintf("COMPOSE_PREFIX=%s\n", *compose.ComposeSuffix)
	}

	// 通过 prepareEnvironmentVariables 解析模板变量
	var projectEnv, environmentEnv string
	if compose.Environment != nil {
		environmentEnv = compose.Environment.Env
		if compose.Environment.Project != nil {
			projectEnv = compose.Environment.Project.Env
		}
	}
	resolvedLines := prepareEnvironmentVariables(envContent, projectEnv, environmentEnv)
	resolvedContent := strings.Join(resolvedLines, "\n") + "\n"

	// 写入 .env 文件到 compose file 所在目录
	composePth := compose.ComposePath
	if composePth == "" {
		composePth = "docker-compose.yml"
	}
	envFilePath := filepath.Join(composeDir, filepath.Dir(composePth), ".env")
	os.MkdirAll(filepath.Dir(envFilePath), 0755)

	if err := os.WriteFile(envFilePath, []byte(resolvedContent), 0644); err != nil {
		if writeLog != nil {
			writeLog(fmt.Sprintf("Warning: failed to write .env file: %v", err))
		}
		return
	}
	if writeLog != nil {
		writeLog("Created .env file with environment variables ✅")
	}
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

	// 发送构建失败通知
	s.sendComposeErrorNotification(composeID, errMsg)
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

// getComposeOrgID 从 compose 关联链获取 organizationId
func (s *ComposeService) getComposeOrgID(compose *schema.Compose) string {
	if compose.Environment != nil && compose.Environment.Project != nil {
		return compose.Environment.Project.OrganizationID
	}
	var env schema.Environment
	if err := s.db.Preload("Project").First(&env, "\"environmentId\" = ?", compose.EnvironmentID).Error; err != nil {
		return ""
	}
	if env.Project != nil {
		return env.Project.OrganizationID
	}
	return ""
}

func (s *ComposeService) getComposeProjectName(compose *schema.Compose) string {
	if compose.Environment != nil && compose.Environment.Project != nil {
		return compose.Environment.Project.Name
	}
	return ""
}

func (s *ComposeService) getComposeEnvName(compose *schema.Compose) string {
	if compose.Environment != nil {
		return compose.Environment.Name
	}
	return ""
}

// sendComposeSuccessNotification 发送 Compose 部署成功通知
func (s *ComposeService) sendComposeSuccessNotification(compose *schema.Compose) {
	if s.notifier == nil {
		return
	}
	orgID := s.getComposeOrgID(compose)
	if orgID == "" {
		return
	}

	projectName := s.getComposeProjectName(compose)
	envName := s.getComposeEnvName(compose)

	htmlBody, err := email.RenderBuildSuccess(email.BuildSuccessData{
		ProjectName:     projectName,
		ApplicationName: compose.Name,
		ApplicationType: "compose",
		EnvironmentName: envName,
	})
	if err != nil {
		log.Printf("Failed to render compose success email: %v", err)
		htmlBody = ""
	}

	s.notifier.Send(orgID, notify.NotificationPayload{
		Event:    notify.EventAppDeploy,
		Title:    "Build Successful",
		Message:  fmt.Sprintf("Compose %s deployed successfully in project %s", compose.Name, projectName),
		AppName:  compose.AppName,
		HTMLBody: htmlBody,
	})
}

// sendComposeErrorNotification 发送 Compose 构建失败通知
func (s *ComposeService) sendComposeErrorNotification(composeID, errMsg string) {
	if s.notifier == nil {
		return
	}

	var compose schema.Compose
	if err := s.db.Preload("Environment").Preload("Environment.Project").First(&compose, "\"composeId\" = ?", composeID).Error; err != nil {
		return
	}

	orgID := s.getComposeOrgID(&compose)
	if orgID == "" {
		return
	}

	projectName := s.getComposeProjectName(&compose)
	envName := s.getComposeEnvName(&compose)

	htmlBody, err := email.RenderBuildFailed(email.BuildFailedData{
		ProjectName:     projectName,
		ApplicationName: compose.Name,
		ApplicationType: "compose",
		EnvironmentName: envName,
		ErrorMessage:    errMsg,
	})
	if err != nil {
		log.Printf("Failed to render compose error email: %v", err)
		htmlBody = ""
	}

	s.notifier.Send(orgID, notify.NotificationPayload{
		Event:    notify.EventAppBuildError,
		Title:    "Build Failed",
		Message:  fmt.Sprintf("Compose %s build failed: %s", compose.Name, errMsg),
		AppName:  compose.AppName,
		HTMLBody: htmlBody,
	})
}

