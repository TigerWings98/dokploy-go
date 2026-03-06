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

// RunBackup executes a backup for the given backup configuration.
func (s *Service) RunBackup(backupID string) error {
	var backup schema.Backup
	if err := s.db.
		Preload("Destination").
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

	uploadCmd := fmt.Sprintf("%s rclone copy %s s3:%s/%s",
		rcloneEnv, dumpPath, dest.Bucket, backup.Prefix)

	if _, err := process.ExecAsync(uploadCmd); err != nil {
		os.Remove(dumpPath)
		return fmt.Errorf("failed to upload backup: %w", err)
	}

	// Cleanup local dump file immediately
	os.Remove(dumpPath)

	return nil
}
