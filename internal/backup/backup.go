// Input: db (Backup/Destination 表), rclone CLI, Docker SDK (exec 备份命令)
// Output: Service (RunBackup/ScheduleBackup/CancelBackup/ListBackupFiles)
// Role: 数据库备份服务，通过 Docker exec 导出数据 + rclone 上传到 S3 兼容存储
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package backup

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/dokploy/dokploy/internal/config"
	"github.com/dokploy/dokploy/internal/db"
	"github.com/dokploy/dokploy/internal/db/schema"
	"github.com/dokploy/dokploy/internal/notify"
	"github.com/dokploy/dokploy/internal/process"
	"github.com/robfig/cron/v3"
)

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

// RunBackup executes a backup for the given backup configuration.
func (s *Service) RunBackup(backupID string) error {
	var backup schema.Backup
	if err := s.db.
		Preload("Destination").
		Preload("Compose").
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

	if backup.Destination == nil {
		return fmt.Errorf("backup destination not configured")
	}

	timestamp := time.Now().UTC().Format("2006-01-02T15-04-05")
	filename := fmt.Sprintf("%s-%s-%s.sql.gz", backup.Prefix, string(backup.DatabaseType), timestamp)
	tmpDir := "/tmp/dokploy-backups"
	os.MkdirAll(tmpDir, 0755)
	dumpPath := filepath.Join(tmpDir, filename)

	// Build dump command based on database type
	var dumpCmd string
	var serverID *string
	var server *schema.Server

	switch backup.DatabaseType {
	case schema.DatabaseTypePostgres:
		if backup.Postgres == nil {
			return fmt.Errorf("postgres instance not found")
		}
		dumpCmd = fmt.Sprintf(
			"docker exec $(docker ps -q -f name=%s) pg_dumpall -U %s | gzip > %s",
			backup.Postgres.AppName, backup.Postgres.DatabaseUser, dumpPath,
		)
		serverID = backup.Postgres.ServerID
		server = backup.Postgres.Server
	case schema.DatabaseTypeMySQL:
		if backup.MySQL == nil {
			return fmt.Errorf("mysql instance not found")
		}
		dumpCmd = fmt.Sprintf(
			"docker exec $(docker ps -q -f name=%s) mysqldump -u %s -p%s --all-databases | gzip > %s",
			backup.MySQL.AppName, backup.MySQL.DatabaseUser, backup.MySQL.DatabasePassword, dumpPath,
		)
		serverID = backup.MySQL.ServerID
		server = backup.MySQL.Server
	case schema.DatabaseTypeMariaDB:
		if backup.MariaDB == nil {
			return fmt.Errorf("mariadb instance not found")
		}
		dumpCmd = fmt.Sprintf(
			"docker exec $(docker ps -q -f name=%s) mariadb-dump -u %s -p%s --all-databases | gzip > %s",
			backup.MariaDB.AppName, backup.MariaDB.DatabaseUser, backup.MariaDB.DatabasePassword, dumpPath,
		)
		serverID = backup.MariaDB.ServerID
		server = backup.MariaDB.Server
	case schema.DatabaseTypeMongo:
		if backup.Mongo == nil {
			return fmt.Errorf("mongo instance not found")
		}
		dumpCmd = fmt.Sprintf(
			"docker exec $(docker ps -q -f name=%s) mongodump --username %s --password %s --archive | gzip > %s",
			backup.Mongo.AppName, backup.Mongo.DatabaseUser, backup.Mongo.DatabasePassword, dumpPath,
		)
		serverID = backup.Mongo.ServerID
		server = backup.Mongo.Server
	default:
		return fmt.Errorf("unsupported database type for backup: %s", backup.DatabaseType)
	}

	// Execute dump (local or remote)
	if serverID != nil && server != nil && server.SSHKey != nil {
		conn := process.SSHConnection{
			Host:       server.IPAddress,
			Port:       server.Port,
			Username:   server.Username,
			PrivateKey: server.SSHKey.PrivateKey,
		}
		if _, err := process.ExecAsyncRemote(conn, dumpCmd, nil); err != nil {
			return fmt.Errorf("failed to create database dump: %w", err)
		}
	} else {
		if _, err := process.ExecAsync(dumpCmd); err != nil {
			return fmt.Errorf("failed to create database dump: %w", err)
		}
	}

	// Upload to S3 using rclone (inline config to avoid temp file leaks)
	dest := backup.Destination
	rcloneEnv := fmt.Sprintf(
		"RCLONE_CONFIG_S3_TYPE=s3 RCLONE_CONFIG_S3_PROVIDER=Other "+
			"RCLONE_CONFIG_S3_ACCESS_KEY_ID=%s RCLONE_CONFIG_S3_SECRET_ACCESS_KEY=%s "+
			"RCLONE_CONFIG_S3_REGION=%s RCLONE_CONFIG_S3_ENDPOINT=%s",
		dest.AccessKey, dest.SecretAccessKey, dest.Region, dest.Endpoint,
	)

	// S3 路径包含 appName 子目录，便于按服务分类组织备份文件
	appName := getServiceAppName(&backup)
	uploadCmd := fmt.Sprintf("%s rclone copy %s s3:%s/%s/%s",
		rcloneEnv, dumpPath, dest.Bucket, appName, backup.Prefix)

	if _, err := process.ExecAsync(uploadCmd); err != nil {
		os.Remove(dumpPath)
		return fmt.Errorf("failed to upload backup: %w", err)
	}

	// Cleanup local dump file immediately
	os.Remove(dumpPath)

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

	if vb.Destination == nil {
		return fmt.Errorf("volume backup destination not configured")
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
			return fmt.Errorf("failed to create volume backup: %w", err)
		}
	} else {
		if _, err := process.ExecAsync(backupCmd); err != nil {
			return fmt.Errorf("failed to create volume backup: %w", err)
		}
	}

	// Upload to S3 using rclone
	dest := vb.Destination
	rcloneEnv := fmt.Sprintf(
		"RCLONE_CONFIG_S3_TYPE=s3 RCLONE_CONFIG_S3_PROVIDER=Other "+
			"RCLONE_CONFIG_S3_ACCESS_KEY_ID=%s RCLONE_CONFIG_S3_SECRET_ACCESS_KEY=%s "+
			"RCLONE_CONFIG_S3_REGION=%s RCLONE_CONFIG_S3_ENDPOINT=%s",
		dest.AccessKey, dest.SecretAccessKey, dest.Region, dest.Endpoint,
	)

	// S3 路径包含服务 appName 子目录（Compose 类型会包含 serviceName）
	s3AppName := getVolumeServiceAppName(&vb)
	uploadCmd := fmt.Sprintf("%s rclone copy %s s3:%s/%s/%s",
		rcloneEnv, backupPath, dest.Bucket, s3AppName, vb.Prefix)

	if _, err := process.ExecAsync(uploadCmd); err != nil {
		os.Remove(backupPath)
		return fmt.Errorf("failed to upload volume backup: %w", err)
	}

	os.Remove(backupPath)
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

	rcloneEnv := fmt.Sprintf(
		"RCLONE_CONFIG_S3_TYPE=s3 RCLONE_CONFIG_S3_PROVIDER=Other "+
			"RCLONE_CONFIG_S3_ACCESS_KEY_ID=%s RCLONE_CONFIG_S3_SECRET_ACCESS_KEY=%s "+
			"RCLONE_CONFIG_S3_REGION=%s RCLONE_CONFIG_S3_ENDPOINT=%s",
		dest.AccessKey, dest.SecretAccessKey, dest.Region, dest.Endpoint,
	)

	// S3 路径包含服务 appName 子目录，与上传路径保持一致
	s3AppName := getVolumeServiceAppName(&vb)
	downloadCmd := fmt.Sprintf("%s rclone copy s3:%s/%s/%s/%s %s",
		rcloneEnv, dest.Bucket, s3AppName, vb.Prefix, filename, tmpDir)

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
