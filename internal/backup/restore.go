// Input: rclone CLI, Docker SDK (exec 恢复命令), S3 文件路径
// Output: RestoreBackup (从 S3 下载+Docker exec 导入), ListS3Files (列出备份文件)
// Role: 数据库备份恢复服务，从 S3 下载备份文件并通过 Docker exec 导入到数据库容器
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package backup

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/dokploy/dokploy/internal/db/schema"
	"github.com/dokploy/dokploy/internal/process"
)

// RestoreBackup downloads a backup from S3 and restores it to the database.
func (s *Service) RestoreBackup(backupID string, filename string) error {
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

	dest := backup.Destination
	rcloneFlags := getRcloneFlags(dest)
	appName := getServiceAppName(&backup)
	prefix := normalizeS3Path(backup.Prefix)
	s3Path := fmt.Sprintf(":s3:%s/%s/%s%s", dest.Bucket, appName, prefix, filename)

	// 构建容器查找 + 恢复命令（与 TS 版 getRestoreCommand 完全一致）
	var containerSearch string
	var restoreCommand string
	var serverID *string
	var server *schema.Server
	isMongo := false

	switch backup.DatabaseType {
	case schema.DatabaseTypePostgres:
		if backup.Postgres == nil {
			return fmt.Errorf("postgres instance not found")
		}
		containerSearch = getServiceContainerCommand(backup.Postgres.AppName)
		restoreCommand = fmt.Sprintf(
			`docker exec -i $CONTAINER_ID sh -c "pg_restore -U '%s' -d %s -O --clean --if-exists"`,
			backup.Postgres.DatabaseUser, backup.Database,
		)
		serverID = backup.Postgres.ServerID
		server = backup.Postgres.Server
	case schema.DatabaseTypeMySQL:
		if backup.MySQL == nil {
			return fmt.Errorf("mysql instance not found")
		}
		containerSearch = getServiceContainerCommand(backup.MySQL.AppName)
		restoreCommand = fmt.Sprintf(
			`docker exec -i $CONTAINER_ID sh -c "mysql -u root -p'%s' %s"`,
			backup.MySQL.DatabaseRootPassword, backup.Database,
		)
		serverID = backup.MySQL.ServerID
		server = backup.MySQL.Server
	case schema.DatabaseTypeMariaDB:
		if backup.MariaDB == nil {
			return fmt.Errorf("mariadb instance not found")
		}
		containerSearch = getServiceContainerCommand(backup.MariaDB.AppName)
		restoreCommand = fmt.Sprintf(
			`docker exec -i $CONTAINER_ID sh -c "mariadb -u '%s' -p'%s' %s"`,
			backup.MariaDB.DatabaseUser, backup.MariaDB.DatabasePassword, backup.Database,
		)
		serverID = backup.MariaDB.ServerID
		server = backup.MariaDB.Server
	case schema.DatabaseTypeMongo:
		if backup.Mongo == nil {
			return fmt.Errorf("mongo instance not found")
		}
		containerSearch = getServiceContainerCommand(backup.Mongo.AppName)
		restoreCommand = fmt.Sprintf(
			`docker exec -i $CONTAINER_ID sh -c "mongorestore --username '%s' --password '%s' --authenticationDatabase admin --db %s --archive --drop"`,
			backup.Mongo.DatabaseUser, backup.Mongo.DatabasePassword, backup.Database,
		)
		serverID = backup.Mongo.ServerID
		server = backup.Mongo.Server
		isMongo = true
	default:
		return fmt.Errorf("unsupported database type for restore: %s", backup.DatabaseType)
	}

	// 组装完整恢复命令
	var fullCmd string
	if isMongo {
		// MongoDB 特殊处理：先下载到临时目录，解压后恢复（与 TS 版 getMongoSpecificCommand 一致）
		tempDir := "/tmp/dokploy-restore"
		baseName := filepath.Base(filename)
		decompressedName := strings.TrimSuffix(baseName, ".gz")
		rcloneDownload := fmt.Sprintf("rclone copy %s \"%s\" %s", rcloneFlags, s3Path, tempDir)
		fullCmd = fmt.Sprintf(
			`CONTAINER_ID=$(%s) && rm -rf %s && mkdir -p %s && %s && cd %s && gunzip -f "%s" && %s < "%s" && rm -rf %s`,
			containerSearch, tempDir, tempDir, rcloneDownload, tempDir, baseName, restoreCommand, decompressedName, tempDir,
		)
	} else {
		// 其他数据库：rclone cat → gunzip → 恢复命令（流式管道）
		rcloneDownload := fmt.Sprintf("rclone cat %s \"%s\"", rcloneFlags, s3Path)
		fullCmd = fmt.Sprintf(
			`CONTAINER_ID=$(%s) && %s | gunzip | %s`,
			containerSearch, rcloneDownload, restoreCommand,
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
			return fmt.Errorf("failed to restore database: %w", err)
		}
	} else {
		if _, err := process.ExecAsync(fullCmd); err != nil {
			return fmt.Errorf("failed to restore database: %w", err)
		}
	}

	return nil
}

// ListBackupFiles lists available backup files from S3 for a backup config.
func (s *Service) ListBackupFiles(backupID string) ([]string, error) {
	var backup schema.Backup
	if err := s.db.Preload("Destination").Preload("Compose").Preload("Postgres").Preload("MySQL").Preload("MariaDB").Preload("Mongo").First(&backup, "\"backupId\" = ?", backupID).Error; err != nil {
		return nil, fmt.Errorf("backup not found: %w", err)
	}

	if backup.Destination == nil {
		return nil, fmt.Errorf("backup destination not configured")
	}

	dest := backup.Destination
	rcloneFlags := getRcloneFlags(dest)

	// S3 路径包含 appName 子目录
	appName := getServiceAppName(&backup)
	prefix := normalizeS3Path(backup.Prefix)
	listCmd := fmt.Sprintf("rclone lsf %s \":s3:%s/%s/%s\"", rcloneFlags, dest.Bucket, appName, prefix)

	result, err := process.ExecAsync(listCmd)
	if err != nil {
		return nil, fmt.Errorf("failed to list backup files: %w", err)
	}

	var files []string
	if result != nil && result.Stdout != "" {
		for _, line := range strings.Split(result.Stdout, "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				files = append(files, line)
			}
		}
	}

	return files, nil
}
