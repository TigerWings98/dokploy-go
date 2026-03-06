package backup

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/dokploy/dokploy/internal/db/schema"
	"github.com/dokploy/dokploy/internal/process"
)

// RestoreBackup downloads a backup from S3 and restores it to the database.
func (s *Service) RestoreBackup(backupID string, filename string) error {
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

	// Download from S3 via rclone
	tmpDir := "/tmp/dokploy-restores"
	os.MkdirAll(tmpDir, 0755)
	localPath := filepath.Join(tmpDir, filename)
	defer os.Remove(localPath)

	dest := backup.Destination
	rcloneEnv := fmt.Sprintf(
		"RCLONE_CONFIG_S3_TYPE=s3 RCLONE_CONFIG_S3_PROVIDER=Other "+
			"RCLONE_CONFIG_S3_ACCESS_KEY_ID=%s RCLONE_CONFIG_S3_SECRET_ACCESS_KEY=%s "+
			"RCLONE_CONFIG_S3_REGION=%s RCLONE_CONFIG_S3_ENDPOINT=%s",
		dest.AccessKey, dest.SecretAccessKey, dest.Region, dest.Endpoint,
	)

	downloadCmd := fmt.Sprintf("%s rclone copy s3:%s/%s/%s %s",
		rcloneEnv, dest.Bucket, backup.Prefix, filename, tmpDir)

	if _, err := process.ExecAsync(downloadCmd); err != nil {
		return fmt.Errorf("failed to download backup: %w", err)
	}

	// Build restore command based on database type
	var restoreCmd string
	var serverID *string
	var server *schema.Server

	switch backup.DatabaseType {
	case schema.DatabaseTypePostgres:
		if backup.Postgres == nil {
			return fmt.Errorf("postgres instance not found")
		}
		restoreCmd = fmt.Sprintf(
			"gunzip -c %s | docker exec -i $(docker ps -q -f name=%s) psql -U %s",
			localPath, backup.Postgres.AppName, backup.Postgres.DatabaseUser,
		)
		serverID = backup.Postgres.ServerID
		server = backup.Postgres.Server
	case schema.DatabaseTypeMySQL:
		if backup.MySQL == nil {
			return fmt.Errorf("mysql instance not found")
		}
		restoreCmd = fmt.Sprintf(
			"gunzip -c %s | docker exec -i $(docker ps -q -f name=%s) mysql -u %s -p%s",
			localPath, backup.MySQL.AppName, backup.MySQL.DatabaseUser, backup.MySQL.DatabasePassword,
		)
		serverID = backup.MySQL.ServerID
		server = backup.MySQL.Server
	case schema.DatabaseTypeMariaDB:
		if backup.MariaDB == nil {
			return fmt.Errorf("mariadb instance not found")
		}
		restoreCmd = fmt.Sprintf(
			"gunzip -c %s | docker exec -i $(docker ps -q -f name=%s) mariadb -u %s -p%s",
			localPath, backup.MariaDB.AppName, backup.MariaDB.DatabaseUser, backup.MariaDB.DatabasePassword,
		)
		serverID = backup.MariaDB.ServerID
		server = backup.MariaDB.Server
	case schema.DatabaseTypeMongo:
		if backup.Mongo == nil {
			return fmt.Errorf("mongo instance not found")
		}
		restoreCmd = fmt.Sprintf(
			"gunzip -c %s | docker exec -i $(docker ps -q -f name=%s) mongorestore --username %s --password %s --archive",
			localPath, backup.Mongo.AppName, backup.Mongo.DatabaseUser, backup.Mongo.DatabasePassword,
		)
		serverID = backup.Mongo.ServerID
		server = backup.Mongo.Server
	default:
		return fmt.Errorf("unsupported database type for restore: %s", backup.DatabaseType)
	}

	// Execute restore (local or remote)
	if serverID != nil && server != nil && server.SSHKey != nil {
		conn := process.SSHConnection{
			Host:       server.IPAddress,
			Port:       server.Port,
			Username:   server.Username,
			PrivateKey: server.SSHKey.PrivateKey,
		}
		if _, err := process.ExecAsyncRemote(conn, restoreCmd, nil); err != nil {
			return fmt.Errorf("failed to restore database: %w", err)
		}
	} else {
		if _, err := process.ExecAsync(restoreCmd); err != nil {
			return fmt.Errorf("failed to restore database: %w", err)
		}
	}

	return nil
}

// ListBackupFiles lists available backup files from S3 for a backup config.
func (s *Service) ListBackupFiles(backupID string) ([]string, error) {
	var backup schema.Backup
	if err := s.db.Preload("Destination").First(&backup, "\"backupId\" = ?", backupID).Error; err != nil {
		return nil, fmt.Errorf("backup not found: %w", err)
	}

	if backup.Destination == nil {
		return nil, fmt.Errorf("backup destination not configured")
	}

	dest := backup.Destination
	rcloneEnv := fmt.Sprintf(
		"RCLONE_CONFIG_S3_TYPE=s3 RCLONE_CONFIG_S3_PROVIDER=Other "+
			"RCLONE_CONFIG_S3_ACCESS_KEY_ID=%s RCLONE_CONFIG_S3_SECRET_ACCESS_KEY=%s "+
			"RCLONE_CONFIG_S3_REGION=%s RCLONE_CONFIG_S3_ENDPOINT=%s",
		dest.AccessKey, dest.SecretAccessKey, dest.Region, dest.Endpoint,
	)

	listCmd := fmt.Sprintf("%s rclone lsf s3:%s/%s/", rcloneEnv, dest.Bucket, backup.Prefix)

	result, err := process.ExecAsync(listCmd)
	if err != nil {
		return nil, fmt.Errorf("failed to list backup files: %w", err)
	}

	var files []string
	if result != nil && result.Stdout != "" {
		for _, line := range splitLines(result.Stdout) {
			if line != "" {
				files = append(files, line)
			}
		}
	}

	return files, nil
}

func splitLines(s string) []string {
	var lines []string
	for _, line := range filepath.SplitList(s) {
		lines = append(lines, line)
	}
	// filepath.SplitList uses OS path separator, use manual split for newlines
	lines = nil
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			line := s[start:i]
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}
			lines = append(lines, line)
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
