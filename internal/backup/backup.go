// Input: db (Backup/Destination 表), rclone CLI, Docker SDK (exec 备份命令)
// Output: Service (RunBackup/ScheduleBackup/CancelBackup/ListBackupFiles)
// Role: 数据库备份服务，通过 Docker exec 导出数据 + rclone 上传到 S3 兼容存储
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package backup

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dokploy/dokploy/internal/config"
	"github.com/dokploy/dokploy/internal/db"
	"github.com/dokploy/dokploy/internal/db/schema"
	"github.com/dokploy/dokploy/internal/email"
	"github.com/dokploy/dokploy/internal/notify"
	"github.com/dokploy/dokploy/internal/process"
	"github.com/robfig/cron/v3"
)

// composeMetadata 表示 backup.metadata JSON 中 Compose 备份的凭据信息
type composeMetadata struct {
	Postgres *struct {
		DatabaseUser string `json:"databaseUser"`
	} `json:"postgres"`
	MySQL *struct {
		DatabaseRootPassword string `json:"databaseRootPassword"`
	} `json:"mysql"`
	MariaDB *struct {
		DatabaseUser     string `json:"databaseUser"`
		DatabasePassword string `json:"databasePassword"`
	} `json:"mariadb"`
	Mongo *struct {
		DatabaseUser     string `json:"databaseUser"`
		DatabasePassword string `json:"databasePassword"`
	} `json:"mongo"`
	ServiceName string `json:"serviceName"`
}

// Service handles database backup operations with cron scheduling.
type Service struct {
	db       *db.DB
	cfg      *config.Config
	notifier *notify.Notifier
	cron     *cron.Cron
	mu       sync.Mutex
	jobs     map[string]cron.EntryID
}

// NewService creates a new backup Service.
func NewService(database *db.DB, cfg *config.Config, notifier *notify.Notifier) *Service {
	return &Service{
		db:       database,
		cfg:      cfg,
		notifier: notifier,
		cron:     cron.New(),
		jobs:     make(map[string]cron.EntryID),
	}
}

// InitCronJobs loads all enabled backups from DB and schedules them.
func (s *Service) InitCronJobs() {
	var backups []schema.Backup
	enabled := true
	if err := s.db.Where("enabled = ?", &enabled).Find(&backups).Error; err != nil {
		log.Printf("Warning: failed to load backup schedules: %v", err)
		return
	}

	for _, b := range backups {
		if err := s.ScheduleBackup(b); err != nil {
			log.Printf("Failed to schedule backup %s: %v", b.BackupID, err)
		}
	}

	s.cron.Start()
	log.Printf("Backup scheduler started with %d jobs", len(s.jobs))
}

// Stop stops the backup cron scheduler.
func (s *Service) Stop() {
	ctx := s.cron.Stop()
	<-ctx.Done()
}

// ScheduleBackup adds or updates a backup cron job.
func (s *Service) ScheduleBackup(backup schema.Backup) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Remove existing
	if entryID, ok := s.jobs[backup.BackupID]; ok {
		s.cron.Remove(entryID)
		delete(s.jobs, backup.BackupID)
	}

	if backup.Enabled == nil || !*backup.Enabled {
		return nil
	}

	backupID := backup.BackupID
	entryID, err := s.cron.AddFunc(backup.Schedule, func() {
		if err := s.RunBackup(backupID); err != nil {
			log.Printf("Backup %s failed: %v", backupID, err)
		}
	})
	if err != nil {
		return fmt.Errorf("invalid schedule %q: %w", backup.Schedule, err)
	}

	s.jobs[backup.BackupID] = entryID
	return nil
}

// RemoveBackup removes a scheduled backup job.
func (s *Service) RemoveBackup(backupID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if entryID, ok := s.jobs[backupID]; ok {
		s.cron.Remove(entryID)
		delete(s.jobs, backupID)
	}
}

// getServiceAppName 返回备份对应的服务 appName，用于 S3 路径前缀。
// 如果是 Compose 类型，且有 serviceName，则返回 composeAppName_serviceName。
func getServiceAppName(backup *schema.Backup) string {
	if backup.Compose != nil && backup.Compose.AppName != "" {
		if backup.ServiceName != nil && *backup.ServiceName != "" {
			return backup.Compose.AppName + "_" + *backup.ServiceName
		}
		return backup.Compose.AppName
	}
	// 按优先级尝试各数据库服务的 appName
	if backup.Postgres != nil && backup.Postgres.AppName != "" {
		return backup.Postgres.AppName
	}
	if backup.MySQL != nil && backup.MySQL.AppName != "" {
		return backup.MySQL.AppName
	}
	if backup.MariaDB != nil && backup.MariaDB.AppName != "" {
		return backup.MariaDB.AppName
	}
	if backup.Mongo != nil && backup.Mongo.AppName != "" {
		return backup.Mongo.AppName
	}
	return backup.AppName
}

// getVolumeServiceAppName 返回卷备份对应的服务 appName，用于 S3 路径前缀。
// 如果是 Compose 类型且有 serviceName，则返回 composeAppName_serviceName。
func getVolumeServiceAppName(vb *schema.VolumeBackup) string {
	if vb.Compose != nil && vb.Compose.AppName != "" {
		if vb.ServiceName != nil && *vb.ServiceName != "" {
			return vb.Compose.AppName + "_" + *vb.ServiceName
		}
		return vb.Compose.AppName
	}
	if vb.Application != nil && vb.Application.AppName != "" {
		return vb.Application.AppName
	}
	return vb.AppName
}

// createBackupDeployment 创建备份执行的 Deployment 记录（与 TS 版 createDeploymentBackup 对齐）
// 每次备份执行会生成一条 Deployment，记录状态和日志路径，供前端展示备份历史
func (s *Service) createBackupDeployment(backup *schema.Backup, title string) *schema.Deployment {
	appName := getServiceAppName(backup)
	logsPath := s.cfg.Paths.LogsPath
	logDir := filepath.Join(logsPath, appName)
	os.MkdirAll(logDir, 0755)

	formattedTime := time.Now().UTC().Format("2006-01-02:15:04:05")
	logFile := filepath.Join(logDir, fmt.Sprintf("%s-%s.log", appName, formattedTime))
	os.WriteFile(logFile, []byte("Initializing backup\n"), 0644)

	now := time.Now().UTC().Format(time.RFC3339)
	status := schema.DeploymentStatusRunning
	desc := title
	backupID := backup.BackupID
	deployment := &schema.Deployment{
		BackupID:    &backupID,
		Title:       title,
		Description: &desc,
		Status:      &status,
		LogPath:     logFile,
		StartedAt:   &now,
	}

	// 清理旧的部署记录，只保留最近 10 条（与 TS 版 removeLastTenDeployments 对齐）
	var oldDeployments []schema.Deployment
	s.db.Where("\"backupId\" = ?", backup.BackupID).Order("\"createdAt\" DESC").Offset(10).Find(&oldDeployments)
	for _, old := range oldDeployments {
		os.Remove(old.LogPath)
		s.db.Delete(&old)
	}

	if err := s.db.Create(deployment).Error; err != nil {
		log.Printf("Warning: failed to create backup deployment record: %v", err)
		return nil
	}
	return deployment
}

// extractExecError 从 process.ExecError 中提取 stderr 详细信息，避免只返回 "exit status 2" 这种模糊错误
func extractExecError(prefix string, err error) error {
	var execErr *process.ExecError
	if errors.As(err, &execErr) {
		detail := execErr.Stderr
		if detail == "" {
			detail = execErr.Stdout
		}
		if detail != "" {
			return fmt.Errorf("%s: %s (exit code %d)\nDetail: %s", prefix, err.Error(), execErr.ExitCode, detail)
		}
	}
	return fmt.Errorf("%s: %w", prefix, err)
}

// appendLog 追加一行日志到备份日志文件
func appendLog(logPath string, msg string) {
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	f.WriteString(msg + "\n")
}

// updateDeploymentStatus 更新 Deployment 记录状态（与 TS 版 updateDeploymentStatus 对齐）
func (s *Service) updateDeploymentStatus(deployment *schema.Deployment, status schema.DeploymentStatus, errMsg string) {
	if deployment == nil {
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	updates := map[string]interface{}{
		"status":     status,
		"finishedAt": now,
	}
	if errMsg != "" {
		updates["errorMessage"] = errMsg
	}
	s.db.Model(deployment).Updates(updates)
}

// RunBackup executes a backup for the given backup configuration.
func (s *Service) RunBackup(backupID string) error {
	var backup schema.Backup
	if err := s.db.
		Preload("Destination").
		Preload("Compose").
		Preload("Compose.Server").
		Preload("Compose.Server.SSHKey").
		Preload("Postgres").
		Preload("Postgres.Server").
		Preload("Postgres.Server.SSHKey").
		Preload("MySQL").
		Preload("MySQL.Server").
		Preload("MySQL.Server.SSHKey").
		Preload("MariaDB").
		Preload("MariaDB.Server").
		Preload("MariaDB.Server.SSHKey").
		Preload("Mongo").
		Preload("Mongo.Server").
		Preload("Mongo.Server.SSHKey").
		First(&backup, "\"backupId\" = ?", backupID).Error; err != nil {
		return fmt.Errorf("backup not found: %w", err)
	}

	// 创建 Deployment 记录（与 TS 版对齐：每次备份执行都有一条记录）
	deployment := s.createBackupDeployment(&backup, "Initializing Backup")

	// 使用 defer 在出错时发送备份失败通知并更新 Deployment 状态
	var backupErr error
	defer func() {
		if backupErr != nil {
			// 把错误信息追加到日志文件，确保前端能看到
			if deployment != nil && deployment.LogPath != "" {
				appendLog(deployment.LogPath, fmt.Sprintf("[error] %s", backupErr.Error()))
			}
			s.sendBackupNotification(&backup, "error", backupErr.Error())
			s.updateDeploymentStatus(deployment, schema.DeploymentStatusError, backupErr.Error())
		}
	}()

	if backup.Destination == nil {
		backupErr = fmt.Errorf("backup destination not configured")
		return backupErr
	}

	timestamp := time.Now().UTC().Format(time.RFC3339)
	filename := fmt.Sprintf("%s.sql.gz", timestamp)

	// 获取 rclone flags 和 S3 路径
	dest := backup.Destination
	rcloneFlags := getRcloneFlags(dest)
	appName := getServiceAppName(&backup)
	prefix := normalizeS3Path(backup.Prefix)
	rcloneDestination := fmt.Sprintf(":s3:%s/%s/%s%s", dest.Bucket, appName, prefix, filename)
	rcloneCommand := fmt.Sprintf("rclone rcat %s \"%s\"", rcloneFlags, rcloneDestination)

	// Web Server 备份走独立流程
	if string(backup.DatabaseType) == "web-server" {
		backupErr = s.runWebServerBackup(&backup)
		if backupErr == nil {
			s.sendBackupNotification(&backup, "success", "")
			s.keepLatestNBackups(&backup, nil)
			s.updateDeploymentStatus(deployment, schema.DeploymentStatusDone, "")
		}
		return backupErr
	}

	// 构建备份命令：查找容器 → dump → gzip → pipe 到 rclone
	var containerSearch string
	var dumpCommand string
	var serverID *string
	var server *schema.Server

	if backup.BackupType == "compose" {
		// Compose 备份：凭据从 metadata JSON 中获取，容器用 Compose label 查找
		if backup.Compose == nil {
			backupErr = fmt.Errorf("compose instance not found")
			return backupErr
		}
		var meta composeMetadata
		if backup.Metadata != nil {
			json.Unmarshal([]byte(*backup.Metadata), &meta)
		}
		serviceName := ""
		if backup.ServiceName != nil {
			serviceName = *backup.ServiceName
		}
		if serviceName == "" && meta.ServiceName != "" {
			serviceName = meta.ServiceName
		}
		composeType := string(backup.Compose.ComposeType)
		containerSearch = getComposeContainerCommand(backup.Compose.AppName, serviceName, composeType)

		switch backup.DatabaseType {
		case schema.DatabaseTypePostgres:
			user := ""
			if meta.Postgres != nil {
				user = meta.Postgres.DatabaseUser
			}
			dumpCommand = fmt.Sprintf(
				`docker exec -i $CONTAINER_ID bash -c "set -o pipefail; pg_dump -Fc --no-acl --no-owner -h localhost -U %s --no-password '%s' | gzip"`,
				user, backup.Database,
			)
		case schema.DatabaseTypeMySQL:
			pass := ""
			if meta.MySQL != nil {
				pass = meta.MySQL.DatabaseRootPassword
			}
			dumpCommand = fmt.Sprintf(
				`docker exec -i $CONTAINER_ID bash -c "set -o pipefail; mysqldump --default-character-set=utf8mb4 -u 'root' --password='%s' --single-transaction --no-tablespaces --quick '%s' | gzip"`,
				pass, backup.Database,
			)
		case schema.DatabaseTypeMariaDB:
			user, pass := "", ""
			if meta.MariaDB != nil {
				user = meta.MariaDB.DatabaseUser
				pass = meta.MariaDB.DatabasePassword
			}
			dumpCommand = fmt.Sprintf(
				`docker exec -i $CONTAINER_ID bash -c "set -o pipefail; mariadb-dump --user='%s' --password='%s' --single-transaction --quick --databases %s | gzip"`,
				user, pass, backup.Database,
			)
		case schema.DatabaseTypeMongo:
			user, pass := "", ""
			if meta.Mongo != nil {
				user = meta.Mongo.DatabaseUser
				pass = meta.Mongo.DatabasePassword
			}
			dumpCommand = fmt.Sprintf(
				`docker exec -i $CONTAINER_ID bash -c "set -o pipefail; mongodump -d '%s' -u '%s' -p '%s' --archive --authenticationDatabase admin --gzip"`,
				backup.Database, user, pass,
			)
		default:
			backupErr = fmt.Errorf("unsupported database type for compose backup: %s", backup.DatabaseType)
			return backupErr
		}

		serverID = backup.Compose.ServerID
		if backup.Compose.Server != nil {
			server = backup.Compose.Server
		}
	} else {
		// Database 备份：凭据从关联的 DB 记录获取，容器用 Swarm service label 查找
		switch backup.DatabaseType {
		case schema.DatabaseTypePostgres:
			if backup.Postgres == nil {
				backupErr = fmt.Errorf("postgres instance not found")
				return backupErr
			}
			containerSearch = getServiceContainerCommand(backup.Postgres.AppName)
			dumpCommand = fmt.Sprintf(
				`docker exec -i $CONTAINER_ID bash -c "set -o pipefail; pg_dump -Fc --no-acl --no-owner -h localhost -U %s --no-password '%s' | gzip"`,
				backup.Postgres.DatabaseUser, backup.Database,
			)
			serverID = backup.Postgres.ServerID
			server = backup.Postgres.Server
		case schema.DatabaseTypeMySQL:
			if backup.MySQL == nil {
				backupErr = fmt.Errorf("mysql instance not found")
				return backupErr
			}
			containerSearch = getServiceContainerCommand(backup.MySQL.AppName)
			dumpCommand = fmt.Sprintf(
				`docker exec -i $CONTAINER_ID bash -c "set -o pipefail; mysqldump --default-character-set=utf8mb4 -u 'root' --password='%s' --single-transaction --no-tablespaces --quick '%s' | gzip"`,
				backup.MySQL.DatabaseRootPassword, backup.Database,
			)
			serverID = backup.MySQL.ServerID
			server = backup.MySQL.Server
		case schema.DatabaseTypeMariaDB:
			if backup.MariaDB == nil {
				backupErr = fmt.Errorf("mariadb instance not found")
				return backupErr
			}
			containerSearch = getServiceContainerCommand(backup.MariaDB.AppName)
			dumpCommand = fmt.Sprintf(
				`docker exec -i $CONTAINER_ID bash -c "set -o pipefail; mariadb-dump --user='%s' --password='%s' --single-transaction --quick --databases %s | gzip"`,
				backup.MariaDB.DatabaseUser, backup.MariaDB.DatabasePassword, backup.Database,
			)
			serverID = backup.MariaDB.ServerID
			server = backup.MariaDB.Server
		case schema.DatabaseTypeMongo:
			if backup.Mongo == nil {
				backupErr = fmt.Errorf("mongo instance not found")
				return backupErr
			}
			containerSearch = getServiceContainerCommand(backup.Mongo.AppName)
			dumpCommand = fmt.Sprintf(
				`docker exec -i $CONTAINER_ID bash -c "set -o pipefail; mongodump -d '%s' -u '%s' -p '%s' --archive --authenticationDatabase admin --gzip"`,
				backup.Database, backup.Mongo.DatabaseUser, backup.Mongo.DatabasePassword,
			)
			serverID = backup.Mongo.ServerID
			server = backup.Mongo.Server
		default:
			backupErr = fmt.Errorf("unsupported database type for backup: %s", backup.DatabaseType)
			return backupErr
		}
	}

	// 如果是 MongoDB，需要确保容器内 mongodump 版本兼容
	isMongoBackup := backup.DatabaseType == schema.DatabaseTypeMongo
	mongoInstallStep := ""
	if isMongoBackup {
		mongoInstallStep = ensureMongoToolsCmd("$CONTAINER_ID") + " && "
	}

	// 获取日志路径（与 TS 版 getBackupCommand 对齐：每一步都写入日志文件）
	logPath := ""
	if deployment != nil {
		logPath = deployment.LogPath
	}

	// 组装完整备份流水线命令（与 TS 版 getBackupCommand 完全一致，含分步日志）
	var fullCmd string
	if logPath != "" {
		fullCmd = fmt.Sprintf(`set -eo pipefail;
echo "[$(date)] Starting backup process..." >> %s;
echo "[$(date)] Executing backup command..." >> %s;
CONTAINER_ID=$(%s)
if [ -z "$CONTAINER_ID" ]; then
  echo "[$(date)] Error: Container not found" >> %s;
  exit 1;
fi
echo "[$(date)] Container Up: $CONTAINER_ID" >> %s;
%sBACKUP_OUTPUT=$(%s 2>&1 >/dev/null) || {
  echo "[$(date)] Error: Backup failed" >> %s;
  echo "Error: $BACKUP_OUTPUT" >> %s;
  exit 1;
}
echo "[$(date)] backup completed successfully" >> %s;
echo "[$(date)] Starting upload to S3..." >> %s;
UPLOAD_OUTPUT=$(%s | %s 2>&1 >/dev/null) || {
  echo "[$(date)] Error: Upload to S3 failed" >> %s;
  echo "Error: $UPLOAD_OUTPUT" >> %s;
  exit 1;
}
echo "[$(date)] Upload to S3 completed successfully" >> %s;
echo "Backup done" >> %s;`,
			logPath, logPath,
			containerSearch,
			logPath,
			logPath,
			mongoInstallStep, dumpCommand, logPath, logPath,
			logPath, logPath,
			dumpCommand, rcloneCommand, logPath, logPath,
			logPath, logPath,
		)
	} else {
		// 降级：无日志路径时使用简单命令
		fullCmd = fmt.Sprintf(`set -eo pipefail; CONTAINER_ID=$(%s); if [ -z "$CONTAINER_ID" ]; then echo "Error: Container not found"; exit 1; fi; %s%s | %s`,
			containerSearch, mongoInstallStep, dumpCommand, rcloneCommand,
		)
	}

	// 执行（本地或远程 SSH）
	if serverID != nil && server != nil && server.SSHKey != nil {
		conn := process.SSHConnection{
			Host:       server.IPAddress,
			Port:       server.Port,
			Username:   server.Username,
			PrivateKey: server.SSHKey.PrivateKey,
		}
		if _, err := process.ExecAsyncRemote(conn, fullCmd, nil); err != nil {
			backupErr = extractExecError("failed to create database dump", err)
			return backupErr
		}
	} else {
		if _, err := process.ExecAsync(fullCmd, process.WithShell("/bin/bash")); err != nil {
			backupErr = extractExecError("failed to create database dump", err)
			return backupErr
		}
	}

	// 发送备份成功通知 + 更新 Deployment 状态
	s.sendBackupNotification(&backup, "success", "")
	s.updateDeploymentStatus(deployment, schema.DeploymentStatusDone, "")

	// 清理旧备份（与 TS 版 keepLatestNBackups 对齐）
	s.keepLatestNBackups(&backup, serverID)

	return nil
}

// runWebServerBackup 备份 Dokploy 自身（PostgreSQL 数据库 + /etc/dokploy 文件系统），与 TS 版完全一致。
func (s *Service) runWebServerBackup(backup *schema.Backup) error {
	dest := backup.Destination
	rcloneFlags := getRcloneFlags(dest)
	appName := getServiceAppName(backup)
	prefix := normalizeS3Path(backup.Prefix)
	timestamp := strings.ReplaceAll(strings.ReplaceAll(time.Now().UTC().Format(time.RFC3339), ":", "-"), ".", "-")
	backupFileName := fmt.Sprintf("webserver-backup-%s.zip", timestamp)
	s3Path := fmt.Sprintf(":s3:%s/%s/%s%s", dest.Bucket, appName, prefix, backupFileName)
	basePath := s.cfg.Paths.BasePath // /etc/dokploy

	// 所有步骤在一个 shell 脚本中执行，与 TS 版逻辑对齐
	script := fmt.Sprintf(`set -e
TMPDIR=$(mktemp -d /tmp/dokploy-backup-XXXXXX)
trap 'rm -rf $TMPDIR' EXIT

mkdir -p $TMPDIR/filesystem

# 1. 查找 dokploy-postgres 容器
PGCONTAINER=$(docker ps --filter "name=dokploy-postgres" --filter "status=running" -q | head -n 1)
if [ -z "$PGCONTAINER" ]; then
  echo "Error: dokploy-postgres container not found"
  exit 1
fi

# 2. 导出数据库（pg_dump -Fc 自定义格式）
docker exec $PGCONTAINER pg_dump -v -Fc -U dokploy -d dokploy -f /tmp/database.sql
docker cp $PGCONTAINER:/tmp/database.sql $TMPDIR/database.sql
docker exec $PGCONTAINER rm -f /tmp/database.sql

# 3. 备份文件系统
rsync -a --ignore-errors --no-specials --no-devices %s/ $TMPDIR/filesystem/ || true

# 4. 打包为 zip
cd $TMPDIR && zip -r %s *.sql filesystem/ > /dev/null 2>&1

# 5. 上传到 S3
rclone copyto %s "$TMPDIR/%s" "%s"
`,
		basePath, backupFileName, rcloneFlags, backupFileName, s3Path,
	)

	if _, err := process.ExecAsync(script); err != nil {
		return fmt.Errorf("web server backup failed: %w", err)
	}
	return nil
}

// RunVolumeBackup executes a volume backup for the given volume backup configuration.
func (s *Service) RunVolumeBackup(volumeBackupID string) error {
	var vb schema.VolumeBackup
	if err := s.db.
		Preload("Destination").
		Preload("Application").
		Preload("Application.Server").
		Preload("Application.Server.SSHKey").
		Preload("Compose").
		Preload("Compose.Server").
		Preload("Compose.Server.SSHKey").
		First(&vb, "\"volumeBackupId\" = ?", volumeBackupID).Error; err != nil {
		return fmt.Errorf("volume backup not found: %w", err)
	}

	// 使用 defer 在出错时发送卷备份失败通知（与 TS v0.28.5 对齐，包裹在 try-catch 中防止级联失败）
	var volumeErr error
	defer func() {
		if volumeErr != nil {
			func() {
				defer func() {
					if r := recover(); r != nil {
						log.Printf("Failed to send volume backup error notification: %v", r)
					}
				}()
				s.sendVolumeBackupNotification(&vb, "error", volumeErr.Error())
			}()
		}
	}()

	if vb.Destination == nil {
		volumeErr = fmt.Errorf("volume backup destination not configured")
		return volumeErr
	}

	timestamp := time.Now().UTC().Format("2006-01-02T15-04-05")
	filename := fmt.Sprintf("%s-%s.tar", vb.AppName, timestamp)
	backupDir := filepath.Join("/tmp/dokploy-volume-backups", vb.AppName)
	os.MkdirAll(backupDir, 0755)
	backupPath := filepath.Join(backupDir, filename)

	// Backup the Docker volume to a tar file
	backupCmd := fmt.Sprintf(
		"docker run --rm -v %s:/volume -v %s:/backup alpine tar cf /backup/%s -C /volume .",
		vb.VolumeName, backupDir, filename,
	)

	var server *schema.Server
	if vb.Application != nil && vb.Application.Server != nil {
		server = vb.Application.Server
	} else if vb.Compose != nil && vb.Compose.Server != nil {
		server = vb.Compose.Server
	}

	if server != nil && server.SSHKey != nil {
		conn := process.SSHConnection{
			Host:       server.IPAddress,
			Port:       server.Port,
			Username:   server.Username,
			PrivateKey: server.SSHKey.PrivateKey,
		}
		if _, err := process.ExecAsyncRemote(conn, backupCmd, nil); err != nil {
			volumeErr = fmt.Errorf("failed to create volume backup: %w", err)
			return volumeErr
		}
	} else {
		if _, err := process.ExecAsync(backupCmd); err != nil {
			volumeErr = fmt.Errorf("failed to create volume backup: %w", err)
			return volumeErr
		}
	}

	// Upload to S3 using rclone（使用与 TS 版一致的 flags）
	dest := vb.Destination
	rcloneFlags := getRcloneFlags(dest)

	// S3 路径包含服务 appName 子目录（Compose 类型会包含 serviceName）
	s3AppName := getVolumeServiceAppName(&vb)
	prefix := normalizeS3Path(vb.Prefix)
	rcloneDestination := fmt.Sprintf(":s3:%s/%s/%s%s", dest.Bucket, s3AppName, prefix, filename)
	uploadCmd := fmt.Sprintf("rclone copyto %s \"%s\" \"%s\"",
		rcloneFlags, backupPath, rcloneDestination)

	if _, err := process.ExecAsync(uploadCmd); err != nil {
		os.Remove(backupPath)
		volumeErr = fmt.Errorf("failed to upload volume backup: %w", err)
		return volumeErr
	}

	os.Remove(backupPath)

	// 发送卷备份成功通知（包裹在 try-catch 中防止级联失败，与 TS v0.28.5 对齐）
	func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("Failed to send volume backup success notification: %v", r)
			}
		}()
		s.sendVolumeBackupNotification(&vb, "success", "")
	}()

	return nil
}

// RestoreVolumeBackup downloads a volume backup from S3 and restores it to the Docker volume.
func (s *Service) RestoreVolumeBackup(volumeBackupID string, filename string) error {
	var vb schema.VolumeBackup
	if err := s.db.
		Preload("Destination").
		Preload("Application").
		Preload("Application.Server").
		Preload("Application.Server.SSHKey").
		Preload("Compose").
		Preload("Compose.Server").
		Preload("Compose.Server.SSHKey").
		First(&vb, "\"volumeBackupId\" = ?", volumeBackupID).Error; err != nil {
		return fmt.Errorf("volume backup not found: %w", err)
	}

	if vb.Destination == nil {
		return fmt.Errorf("volume backup destination not configured")
	}

	dest := vb.Destination
	tmpDir := "/tmp/dokploy-volume-restores"
	os.MkdirAll(tmpDir, 0755)
	localPath := filepath.Join(tmpDir, filename)
	defer os.Remove(localPath)

	rcloneFlags := getRcloneFlags(dest)

	// S3 路径包含服务 appName 子目录，与上传路径保持一致
	s3AppName := getVolumeServiceAppName(&vb)
	prefix := normalizeS3Path(vb.Prefix)
	s3Path := fmt.Sprintf(":s3:%s/%s/%s%s", dest.Bucket, s3AppName, prefix, filename)
	downloadCmd := fmt.Sprintf("rclone copy %s \"%s\" %s", rcloneFlags, s3Path, tmpDir)

	if _, err := process.ExecAsync(downloadCmd); err != nil {
		return fmt.Errorf("failed to download volume backup: %w", err)
	}

	// Restore the tar file into the Docker volume
	restoreCmd := fmt.Sprintf(
		"docker run --rm -v %s:/volume -v %s:/backup alpine sh -c 'rm -rf /volume/* && tar xf /backup/%s -C /volume'",
		vb.VolumeName, tmpDir, filename,
	)

	var server *schema.Server
	if vb.Application != nil && vb.Application.Server != nil {
		server = vb.Application.Server
	} else if vb.Compose != nil && vb.Compose.Server != nil {
		server = vb.Compose.Server
	}

	if server != nil && server.SSHKey != nil {
		conn := process.SSHConnection{
			Host:       server.IPAddress,
			Port:       server.Port,
			Username:   server.Username,
			PrivateKey: server.SSHKey.PrivateKey,
		}
		if _, err := process.ExecAsyncRemote(conn, restoreCmd, nil); err != nil {
			return fmt.Errorf("failed to restore volume backup: %w", err)
		}
	} else {
		if _, err := process.ExecAsync(restoreCmd); err != nil {
			return fmt.Errorf("failed to restore volume backup: %w", err)
		}
	}

	return nil
}

// sendBackupNotification 发送数据库备份通知（与 TS 版 sendDatabaseBackupNotifications 对齐）
func (s *Service) sendBackupNotification(backup *schema.Backup, backupType, errMsg string) {
	if s.notifier == nil {
		return
	}

	// 获取 organizationId（通过 Destination 关联获取）
	if backup.Destination == nil {
		return
	}
	orgID := backup.Destination.OrganizationID
	if orgID == "" {
		return
	}

	appName := getServiceAppName(backup)
	dbType := string(backup.DatabaseType)
	if dbType == "mongo" {
		dbType = "mongodb"
	}

	title := "Database Backup Successful"
	message := fmt.Sprintf("Database backup for %s completed successfully", appName)
	if backupType == "error" {
		title = "Database Backup Failed"
		message = fmt.Sprintf("Database backup for %s failed: %s", appName, errMsg)
	}

	htmlBody, err := email.RenderDatabaseBackup(email.DatabaseBackupData{
		ApplicationName: appName,
		DatabaseType:    dbType,
		Type:            backupType,
		ErrorMessage:    errMsg,
	})
	if err != nil {
		log.Printf("Failed to render backup email: %v", err)
		htmlBody = ""
	}

	s.notifier.Send(orgID, notify.NotificationPayload{
		Event:    notify.EventDatabaseBackup,
		Title:    title,
		Message:  message,
		AppName:  appName,
		HTMLBody: htmlBody,
	})
}

// getServiceContainerCommand 返回按 Swarm service label 查找运行中容器的命令（与 TS 版完全一致）
func getServiceContainerCommand(appName string) string {
	return fmt.Sprintf(`docker ps -q --filter "status=running" --filter "label=com.docker.swarm.service.name=%s" | head -n 1`, appName)
}

// getComposeContainerCommand 返回按 Compose label 查找容器的命令
func getComposeContainerCommand(appName, serviceName, composeType string) string {
	if composeType == "stack" {
		return fmt.Sprintf(`docker ps -q --filter "status=running" --filter "label=com.docker.stack.namespace=%s" --filter "label=com.docker.swarm.service.name=%s_%s" | head -n 1`,
			appName, appName, serviceName)
	}
	return fmt.Sprintf(`docker ps -q --filter "status=running" --filter "label=com.docker.compose.project=%s" --filter "label=com.docker.compose.service=%s" | head -n 1`,
		appName, serviceName)
}

// ensureMongoToolsCmd 返回在容器内安装/更新 mongodb-database-tools 的命令。
// 社区 mongo 镜像（如 mongo:7.0-pi）自带的 mongodump/mongorestore 可能版本过旧，
// 无法兼容当前 MongoDB wire protocol，需要安装新版工具。
// 检测方法：新版工具 version 以 "100." 开头（如 100.10.0），旧版显示 "version: x.x.x"。
// 自动检测容器 OS 版本和 MongoDB 版本，构建正确的 repo URL。
func ensureMongoToolsCmd(containerIDVar string) string {
	return fmt.Sprintf(
		`docker exec %s bash -c "`+
			`if mongorestore --version 2>&1 | head -1 | grep -q '100\.'; then exit 0; fi; `+
			`echo 'Installing mongodb-database-tools...'; `+
			`apt-get update -qq > /dev/null 2>&1 && apt-get install -y -qq gnupg curl > /dev/null 2>&1; `+
			`CODENAME=\$(. /etc/os-release && echo \$VERSION_CODENAME); `+
			`MONGO_VER=\$(mongod --version 2>/dev/null | grep -oP 'v\\K[0-9]+\\.[0-9]+' | head -1 || echo '7.0'); `+
			`curl -fsSL https://www.mongodb.org/static/pgp/server-\${MONGO_VER}.asc | gpg --dearmor -o /usr/share/keyrings/mongodb-server.gpg 2>/dev/null; `+
			`echo \"deb [ signed-by=/usr/share/keyrings/mongodb-server.gpg ] https://repo.mongodb.org/apt/ubuntu \${CODENAME}/mongodb-org/\${MONGO_VER} multiverse\" > /etc/apt/sources.list.d/mongodb-org.list; `+
			`apt-get update -qq > /dev/null 2>&1 && apt-get install -y -qq mongodb-database-tools > /dev/null 2>&1 && `+
			`echo 'mongodb-database-tools installed successfully' || echo 'Warning: failed to install mongodb-database-tools'`+
			`"`,
		containerIDVar,
	)
}

// getRcloneFlags 生成 rclone S3 参数（与 TS 版 getS3Credentials 对齐）
func getRcloneFlags(dest *schema.Destination) string {
	flags := []string{
		fmt.Sprintf(`--s3-access-key-id="%s"`, dest.AccessKey),
		fmt.Sprintf(`--s3-secret-access-key="%s"`, dest.SecretAccessKey),
		fmt.Sprintf(`--s3-region="%s"`, dest.Region),
		fmt.Sprintf(`--s3-endpoint="%s"`, dest.Endpoint),
		"--s3-no-check-bucket",
		"--s3-force-path-style",
	}
	if dest.Provider != nil && *dest.Provider != "" {
		flags = append([]string{fmt.Sprintf(`--s3-provider="%s"`, *dest.Provider)}, flags...)
	}
	result := ""
	for i, f := range flags {
		if i > 0 {
			result += " "
		}
		result += f
	}
	return result
}

// normalizeS3Path 规范化 S3 前缀路径（与 TS 版完全一致）
func normalizeS3Path(prefix string) string {
	// 去除首尾空白和斜杠
	p := prefix
	for len(p) > 0 && (p[0] == '/' || p[0] == ' ') {
		p = p[1:]
	}
	for len(p) > 0 && (p[len(p)-1] == '/' || p[len(p)-1] == ' ') {
		p = p[:len(p)-1]
	}
	if p == "" {
		return ""
	}
	return p + "/"
}

// keepLatestNBackups 清理旧备份文件，只保留最新的 N 个（与 TS 版完全一致）
func (s *Service) keepLatestNBackups(backup *schema.Backup, serverID *string) {
	if backup.KeepLatestCount == nil || *backup.KeepLatestCount <= 0 {
		return
	}

	dest := backup.Destination
	if dest == nil {
		return
	}

	rcloneFlags := getRcloneFlags(dest)
	appName := getServiceAppName(backup)
	prefix := normalizeS3Path(backup.Prefix)
	backupFilesPath := fmt.Sprintf(":s3:%s/%s/%s", dest.Bucket, appName, prefix)

	// 文件后缀根据类型区分
	includeFilter := "*.sql.gz"
	if string(backup.DatabaseType) == "web-server" {
		includeFilter = "*.zip"
	}

	// 与 TS 版完全一致：列出 → 排序 → 取多余的 → 删除
	cleanupCmd := fmt.Sprintf(
		`rclone lsf %s --include "%s" %s | sort -r | tail -n +$((%d+1)) | xargs -I{} rclone delete %s %s{}`,
		rcloneFlags, includeFilter, backupFilesPath,
		*backup.KeepLatestCount,
		rcloneFlags, backupFilesPath,
	)

	var err error
	if serverID != nil {
		var server schema.Server
		if dbErr := s.db.First(&server, "\"serverId\" = ?", *serverID).Error; dbErr == nil && server.SSHKeyID != nil {
			var sshKey schema.SSHKey
			if dbErr := s.db.First(&sshKey, "\"sshKeyId\" = ?", *server.SSHKeyID).Error; dbErr == nil {
				conn := process.SSHConnection{
					Host:       server.IPAddress,
					Port:       server.Port,
					Username:   server.Username,
					PrivateKey: sshKey.PrivateKey,
				}
				_, err = process.ExecAsyncRemote(conn, cleanupCmd, nil)
			}
		}
	} else {
		_, err = process.ExecAsync(cleanupCmd)
	}

	if err != nil {
		log.Printf("Warning: failed to cleanup old backups for %s: %v", backup.BackupID, err)
	}
}

// sendVolumeBackupNotification 发送卷备份通知（与 TS 版 sendVolumeBackupNotifications 对齐）
func (s *Service) sendVolumeBackupNotification(vb *schema.VolumeBackup, backupType, errMsg string) {
	if s.notifier == nil {
		return
	}

	// VolumeBackup 没有直接的 organizationId，通过关联的 Application/Compose 获取
	var orgID string
	if vb.Application != nil {
		// app → environment → project → org
		var env schema.Environment
		if err := s.db.Preload("Project").First(&env, "\"environmentId\" = ?", vb.Application.EnvironmentID).Error; err == nil && env.Project != nil {
			orgID = env.Project.OrganizationID
		}
	} else if vb.Compose != nil {
		var env schema.Environment
		if err := s.db.Preload("Project").First(&env, "\"environmentId\" = ?", vb.Compose.EnvironmentID).Error; err == nil && env.Project != nil {
			orgID = env.Project.OrganizationID
		}
	}
	if orgID == "" {
		return
	}

	appName := getVolumeServiceAppName(vb)

	title := "Volume Backup Successful"
	message := fmt.Sprintf("Volume backup for %s completed successfully", appName)
	if backupType == "error" {
		title = "Volume Backup Failed"
		message = fmt.Sprintf("Volume backup for %s failed: %s", appName, errMsg)
	}

	htmlBody, err := email.RenderVolumeBackup(email.VolumeBackupData{
		ApplicationName: appName,
		VolumeName:      vb.VolumeName,
		Type:            backupType,
		ErrorMessage:    errMsg,
	})
	if err != nil {
		log.Printf("Failed to render volume backup email: %v", err)
		htmlBody = ""
	}

	s.notifier.Send(orgID, notify.NotificationPayload{
		Event:    notify.EventVolumeBackup,
		Title:    title,
		Message:  message,
		AppName:  appName,
		HTMLBody: htmlBody,
	})
}
