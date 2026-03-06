package backup

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/dokploy/dokploy/internal/db"
	"github.com/dokploy/dokploy/internal/db/schema"
	"github.com/dokploy/dokploy/internal/process"
)

// BackupService handles database backup operations.
type BackupService struct {
	db *db.DB
}

// NewBackupService creates a new BackupService.
func NewBackupService(database *db.DB) *BackupService {
	return &BackupService{db: database}
}

// RunBackup executes a backup for the given backup configuration.
func (s *BackupService) RunBackup(backupID string) error {
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

	// Step 1: Create database dump
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

	// Execute dump
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

	// Step 2: Upload to S3 using rclone
	dest := backup.Destination
	rcloneConfig := fmt.Sprintf(
		"[s3]\ntype = s3\nprovider = Other\naccess_key_id = %s\nsecret_access_key = %s\nregion = %s\nendpoint = %s",
		dest.AccessKey, dest.SecretAccessKey, dest.Region, dest.Endpoint,
	)

	rcloneConfigPath := filepath.Join(tmpDir, "rclone.conf")
	if err := os.WriteFile(rcloneConfigPath, []byte(rcloneConfig), 0600); err != nil {
		return fmt.Errorf("failed to write rclone config: %w", err)
	}
	defer os.Remove(rcloneConfigPath)

	uploadCmd := fmt.Sprintf(
		"rclone copy %s s3:%s/%s --config %s",
		dumpPath, dest.Bucket, backup.Prefix, rcloneConfigPath,
	)

	if _, err := process.ExecAsync(uploadCmd); err != nil {
		return fmt.Errorf("failed to upload backup: %w", err)
	}

	// Cleanup
	os.Remove(dumpPath)

	return nil
}
