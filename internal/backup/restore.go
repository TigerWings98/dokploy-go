// Input: rclone CLI, Docker SDK (exec 恢复命令), S3 文件路径
// Output: RestoreBackup (数据库恢复), RestoreComposeBackup (Compose 恢复), RestoreWebServerBackup (全服务器恢复), ListBackupFiles (列出备份文件)
// Role: 数据库/Compose/Web Server 备份恢复服务，从 S3 下载备份文件并恢复到本地或远程服务器
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package backup

import (
	"encoding/json"
	"fmt"
	"os"
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

// restoreMetadata 用于解析恢复请求中的 metadata JSON（与 TS 版 apiRestoreBackup.metadata 一致）
type restoreMetadata struct {
	ServiceName string `json:"serviceName"`
	Postgres    *struct {
		DatabaseUser string `json:"databaseUser"`
	} `json:"postgres"`
	MariaDB *struct {
		DatabaseUser     string `json:"databaseUser"`
		DatabasePassword string `json:"databasePassword"`
	} `json:"mariadb"`
	Mongo *struct {
		DatabaseUser     string `json:"databaseUser"`
		DatabasePassword string `json:"databasePassword"`
	} `json:"mongo"`
	MySQL *struct {
		DatabaseRootPassword string `json:"databaseRootPassword"`
	} `json:"mysql"`
}

// RestoreComposeBackup 恢复 Compose 类型的备份。
// 与 TS 版 restoreComposeBackup 完全一致：从 metadata 提取凭据，用 compose label 查找容器。
func (s *Service) RestoreComposeBackup(composeID string, destinationID string, databaseType string, databaseName string, backupFile string, metadataJSON string, emit func(string)) error {
	var compose schema.Compose
	if err := s.db.Preload("Server").Preload("Server.SSHKey").First(&compose, "\"composeId\" = ?", composeID).Error; err != nil {
		return fmt.Errorf("compose not found: %w", err)
	}

	if databaseType == "web-server" {
		return nil // TS 版也直接 return
	}

	var dest schema.Destination
	if err := s.db.First(&dest, "\"destinationId\" = ?", destinationID).Error; err != nil {
		return fmt.Errorf("destination not found: %w", err)
	}

	rcloneFlags := getRcloneFlags(&dest)
	bucketPath := fmt.Sprintf(":s3:%s", dest.Bucket)
	backupPath := fmt.Sprintf("%s/%s", bucketPath, backupFile)

	// 解析 metadata
	var meta restoreMetadata
	if metadataJSON != "" {
		json.Unmarshal([]byte(metadataJSON), &meta)
	}

	// 构建 rclone 命令（Mongo 特殊处理：用 rclone copy 下载到临时目录）
	isMongo := databaseType == "mongo"
	rcloneCommand := fmt.Sprintf(`rclone cat %s "%s" | gunzip`, rcloneFlags, backupPath)
	if isMongo {
		rcloneCommand = fmt.Sprintf(`rclone copy %s "%s"`, rcloneFlags, backupPath)
	}

	// 构建恢复命令
	var restoreCommand string
	switch databaseType {
	case "postgres":
		user := ""
		if meta.Postgres != nil {
			user = meta.Postgres.DatabaseUser
		}
		restoreCommand = fmt.Sprintf(
			`docker exec -i $CONTAINER_ID sh -c "pg_restore -U '%s' -d %s -O --clean --if-exists"`,
			user, databaseName,
		)
	case "mysql":
		pass := ""
		if meta.MySQL != nil {
			pass = meta.MySQL.DatabaseRootPassword
		}
		restoreCommand = fmt.Sprintf(
			`docker exec -i $CONTAINER_ID sh -c "mysql -u root -p'%s' %s"`,
			pass, databaseName,
		)
	case "mariadb":
		user, pass := "", ""
		if meta.MariaDB != nil {
			user = meta.MariaDB.DatabaseUser
			pass = meta.MariaDB.DatabasePassword
		}
		restoreCommand = fmt.Sprintf(
			`docker exec -i $CONTAINER_ID sh -c "mariadb -u '%s' -p'%s' %s"`,
			user, pass, databaseName,
		)
	case "mongo":
		user, pass := "", ""
		if meta.Mongo != nil {
			user = meta.Mongo.DatabaseUser
			pass = meta.Mongo.DatabasePassword
		}
		restoreCommand = fmt.Sprintf(
			`docker exec -i $CONTAINER_ID sh -c "mongorestore --username '%s' --password '%s' --authenticationDatabase admin --db %s --archive --drop"`,
			user, pass, databaseName,
		)
	default:
		return fmt.Errorf("unsupported database type for compose restore: %s", databaseType)
	}

	// 容器查找：使用 compose label（与 TS 版 getComposeSearchCommand 一致）
	serviceName := meta.ServiceName
	composeType := string(compose.ComposeType)
	containerSearch := getComposeContainerCommand(compose.AppName, serviceName, composeType)

	// 组装完整命令
	var fullCmd string
	if isMongo {
		tempDir := "/tmp/dokploy-restore"
		baseName := filepath.Base(backupFile)
		decompressedName := strings.TrimSuffix(baseName, ".gz")
		fullCmd = fmt.Sprintf(
			`CONTAINER_ID=$(%s) && rm -rf %s && mkdir -p %s && %s %s && cd %s && gunzip -f "%s" && %s < "%s" && rm -rf %s`,
			containerSearch, tempDir, tempDir, rcloneCommand, tempDir, tempDir, baseName, restoreCommand, decompressedName, tempDir,
		)
	} else {
		fullCmd = fmt.Sprintf(
			`CONTAINER_ID=$(%s) && %s | %s`,
			containerSearch, rcloneCommand, restoreCommand,
		)
	}

	emit("Starting restore...")
	emit(fmt.Sprintf("Backup path: %s", backupPath))
	emit(fmt.Sprintf("Executing command: %s", fullCmd))

	// 执行（本地或远程 SSH）
	if compose.ServerID != nil && compose.Server != nil && compose.Server.SSHKey != nil {
		conn := process.SSHConnection{
			Host:       compose.Server.IPAddress,
			Port:       compose.Server.Port,
			Username:   compose.Server.Username,
			PrivateKey: compose.Server.SSHKey.PrivateKey,
		}
		if _, err := process.ExecAsyncRemote(conn, fullCmd, nil); err != nil {
			return fmt.Errorf("failed to restore compose backup: %w", err)
		}
	} else {
		if _, err := process.ExecAsync(fullCmd); err != nil {
			return fmt.Errorf("failed to restore compose backup: %w", err)
		}
	}

	emit("Restore completed successfully!")
	return nil
}

// cleanupManagedServices 在 webserver restore 之前，清理所有由 Dokploy 管辖的 Docker 服务。
// 通过查询当前数据库获取所有 appName，逐个移除对应的 Docker Swarm 服务/Stack/Compose 项目。
// 跳过 Dokploy 系统服务（dokploy-traefik、dokploy-postgres）。
func (s *Service) cleanupManagedServices(emit func(string)) {
	emit("=== Pre-restore cleanup: removing all Dokploy-managed services ===")

	// 1. 清理 Application 的 Swarm 服务
	var apps []schema.Application
	s.db.Select("\"appName\"").Find(&apps)
	for _, app := range apps {
		emit(fmt.Sprintf("Removing application service: %s", app.AppName))
		process.ExecAsync(fmt.Sprintf("docker service rm %s 2>/dev/null || true", app.AppName))
	}

	// 2. 清理 Preview Deployment 的 Swarm 服务
	var previews []schema.PreviewDeployment
	s.db.Select("\"appName\"").Find(&previews)
	for _, pd := range previews {
		emit(fmt.Sprintf("Removing preview deployment service: %s", pd.AppName))
		process.ExecAsync(fmt.Sprintf("docker service rm %s 2>/dev/null || true", pd.AppName))
	}

	// 3. 清理 Database 的 Swarm 服务 (postgres/mysql/mariadb/mongo/redis)
	type dbRecord struct {
		AppName string
	}
	dbTables := []struct {
		table string
		model interface{}
	}{
		{"postgres", &[]schema.Postgres{}},
		{"mysql", &[]schema.MySQL{}},
		{"mariadb", &[]schema.MariaDB{}},
		{"mongo", &[]schema.Mongo{}},
		{"redis", &[]schema.Redis{}},
	}
	for _, dt := range dbTables {
		var names []dbRecord
		s.db.Table(dt.table).Select("\"appName\"").Find(&names)
		for _, n := range names {
			emit(fmt.Sprintf("Removing %s database service: %s", dt.table, n.AppName))
			process.ExecAsync(fmt.Sprintf("docker service rm %s 2>/dev/null || true", n.AppName))
		}
	}

	// 4. 清理 Compose 项目（区分 stack 和 docker-compose 类型）
	var composes []schema.Compose
	s.db.Select("\"appName\"", "\"composeType\"", "\"suffix\"").Find(&composes)
	composePath := s.cfg.Paths.ComposePath
	for _, comp := range composes {
		projectPath := filepath.Join(composePath, comp.AppName, "code")
		if comp.ComposeType == schema.ComposeTypeStack {
			emit(fmt.Sprintf("Removing stack: %s", comp.AppName))
			process.ExecAsync(fmt.Sprintf(
				"docker network disconnect %s dokploy-traefik 2>/dev/null || true; docker stack rm %s 2>/dev/null || true",
				comp.AppName, comp.AppName,
			))
		} else {
			emit(fmt.Sprintf("Removing compose project: %s", comp.AppName))
			process.ExecAsync(fmt.Sprintf(
				"docker network disconnect %s dokploy-traefik 2>/dev/null || true; cd %s 2>/dev/null && docker compose -p %s down --remove-orphans 2>/dev/null || true",
				comp.AppName, projectPath, comp.AppName,
			))
		}
	}

	// 5. 清理 Traefik 动态路由配置文件（restore 会覆盖整个 /etc/dokploy，但这里先确保运行中的 Traefik 不再路由到已删服务）
	dynamicPath := s.cfg.Paths.DynamicTraefikPath
	emit(fmt.Sprintf("Cleaning Traefik dynamic configs: %s", dynamicPath))
	process.ExecAsync(fmt.Sprintf("rm -f %s/*.yml 2>/dev/null || true", dynamicPath))

	// 6. 停止所有 backup cron 任务
	emit("Stopping all backup cron jobs...")
	s.mu.Lock()
	for id, entryID := range s.jobs {
		s.cron.Remove(entryID)
		delete(s.jobs, id)
	}
	s.mu.Unlock()

	emit("=== Pre-restore cleanup completed ===")
}

// RestoreWebServerBackup 从 S3 下载 Web Server 备份 zip 并恢复文件系统 + Dokploy PostgreSQL 数据库。
// 增强版：restore 前先清理所有 Dokploy 管辖的 Docker 服务，避免孤儿容器。
func (s *Service) RestoreWebServerBackup(destinationID string, backupFile string, emit func(string)) error {
	var dest schema.Destination
	if err := s.db.First(&dest, "\"destinationId\" = ?", destinationID).Error; err != nil {
		return fmt.Errorf("destination not found: %w", err)
	}

	// ★ 在恢复之前，清理所有 Dokploy 管辖的服务（防止孤儿容器）
	s.cleanupManagedServices(emit)

	rcloneFlags := getRcloneFlags(&dest)
	bucketPath := fmt.Sprintf(":s3:%s", dest.Bucket)
	backupPath := fmt.Sprintf("%s/%s", bucketPath, backupFile)
	basePath := s.cfg.Paths.BasePath

	// 创建临时目录
	tempDir, err := os.MkdirTemp("", "dokploy-restore-")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer func() {
		emit("Cleaning up temporary files...")
		os.RemoveAll(tempDir)
	}()

	emit("Starting restore...")
	emit(fmt.Sprintf("Backup path: %s", backupPath))
	emit(fmt.Sprintf("Temp directory: %s", tempDir))

	// 从 S3 下载备份
	emit("Downloading backup from S3...")
	dlCmd := fmt.Sprintf(`rclone copyto %s "%s" "%s/%s"`, rcloneFlags, backupPath, tempDir, backupFile)
	if _, err := process.ExecAsync(dlCmd); err != nil {
		return fmt.Errorf("failed to download backup: %w", err)
	}

	// 列出文件
	emit("Listing files before extraction...")
	if result, err := process.ExecAsync(fmt.Sprintf("ls -la %s", tempDir)); err == nil && result != nil {
		emit(fmt.Sprintf("Files before extraction: %s", result.Stdout))
	}

	// 解压备份
	emit("Extracting backup...")
	if _, err := process.ExecAsync(fmt.Sprintf("cd %s && unzip %s > /dev/null 2>&1", tempDir, backupFile)); err != nil {
		return fmt.Errorf("failed to extract backup: %w", err)
	}

	// 恢复文件系统
	emit("Restoring filesystem...")
	emit(fmt.Sprintf("Copying from %s/filesystem/* to %s/", tempDir, basePath))

	emit("Cleaning target directory...")
	if _, err := process.ExecAsync(fmt.Sprintf(`rm -rf "%s/"*`, basePath)); err != nil {
		return fmt.Errorf("failed to clean target directory: %w", err)
	}

	emit("Setting up target directory...")
	if _, err := process.ExecAsync(fmt.Sprintf(`mkdir -p "%s"`, basePath)); err != nil {
		return fmt.Errorf("failed to create target directory: %w", err)
	}

	emit("Copying files...")
	if _, err := process.ExecAsync(fmt.Sprintf(`cp -rp "%s/filesystem/"* "%s/"`, tempDir, basePath)); err != nil {
		return fmt.Errorf("failed to copy filesystem: %w", err)
	}

	// 数据库恢复
	emit("Starting database restore...")

	// 检查是否有压缩的数据库文件
	if result, _ := process.ExecAsync(fmt.Sprintf("ls %s/database.sql.gz || true", tempDir)); result != nil && strings.Contains(result.Stdout, "database.sql.gz") {
		emit("Found compressed database file, decompressing...")
		if _, err := process.ExecAsync(fmt.Sprintf("cd %s && gunzip database.sql.gz", tempDir)); err != nil {
			return fmt.Errorf("failed to decompress database file: %w", err)
		}
	}

	// 验证数据库文件存在
	if result, _ := process.ExecAsync(fmt.Sprintf("ls %s/database.sql || true", tempDir)); result == nil || !strings.Contains(result.Stdout, "database.sql") {
		return fmt.Errorf("database file not found after extraction")
	}

	// 查找 dokploy-postgres 容器
	containerResult, err := process.ExecAsync(`docker ps --filter "name=dokploy-postgres" --filter "status=running" -q | head -n 1`)
	if err != nil || containerResult == nil || strings.TrimSpace(containerResult.Stdout) == "" {
		return fmt.Errorf("dokploy postgres container not found")
	}
	postgresContainerID := strings.TrimSpace(containerResult.Stdout)

	// 断开所有连接
	emit("Disconnecting all users from database...")
	disconnectCmd := fmt.Sprintf(
		`docker exec %s psql -U dokploy postgres -c "SELECT pg_terminate_backend(pg_stat_activity.pid) FROM pg_stat_activity WHERE pg_stat_activity.datname = 'dokploy' AND pid <> pg_backend_pid();"`,
		postgresContainerID,
	)
	process.ExecAsync(disconnectCmd) // 忽略错误，可能没有活跃连接

	// 删除并重建数据库
	emit("Dropping existing database...")
	if _, err := process.ExecAsync(fmt.Sprintf(
		`docker exec %s psql -U dokploy postgres -c "DROP DATABASE IF EXISTS dokploy;"`,
		postgresContainerID,
	)); err != nil {
		return fmt.Errorf("failed to drop database: %w", err)
	}

	emit("Creating fresh database...")
	if _, err := process.ExecAsync(fmt.Sprintf(
		`docker exec %s psql -U dokploy postgres -c "CREATE DATABASE dokploy;"`,
		postgresContainerID,
	)); err != nil {
		return fmt.Errorf("failed to create database: %w", err)
	}

	// 复制备份文件到容器
	emit("Copying backup file into container...")
	if _, err := process.ExecAsync(fmt.Sprintf(
		`docker cp %s/database.sql %s:/tmp/database.sql`,
		tempDir, postgresContainerID,
	)); err != nil {
		return fmt.Errorf("failed to copy backup file to container: %w", err)
	}

	// 验证容器内文件
	emit("Verifying file in container...")
	if _, err := process.ExecAsync(fmt.Sprintf(
		`docker exec %s ls -l /tmp/database.sql`,
		postgresContainerID,
	)); err != nil {
		return fmt.Errorf("backup file not found in container: %w", err)
	}

	// 恢复数据库
	emit("Running database restore...")
	if _, err := process.ExecAsync(fmt.Sprintf(
		`docker exec %s pg_restore -v -U dokploy -d dokploy /tmp/database.sql`,
		postgresContainerID,
	)); err != nil {
		return fmt.Errorf("failed to restore database: %w", err)
	}

	// 清理容器内临时文件
	emit("Cleaning up container temp file...")
	process.ExecAsync(fmt.Sprintf(`docker exec %s rm /tmp/database.sql`, postgresContainerID))

	// ★ 重置所有服务状态为 idle（restore 后所有服务都未运行，状态必须回到初始值）
	emit("Resetting all service statuses to idle...")
	resetStatuses := []string{
		`UPDATE application SET "applicationStatus" = 'idle'`,
		`UPDATE compose SET "composeStatus" = 'idle'`,
		`UPDATE postgres SET "applicationStatus" = 'idle'`,
		`UPDATE mysql SET "applicationStatus" = 'idle'`,
		`UPDATE mariadb SET "applicationStatus" = 'idle'`,
		`UPDATE mongo SET "applicationStatus" = 'idle'`,
		`UPDATE redis SET "applicationStatus" = 'idle'`,
	}
	for _, sql := range resetStatuses {
		resetCmd := fmt.Sprintf(
			`docker exec %s psql -U dokploy -d dokploy -c "%s"`,
			postgresContainerID, sql,
		)
		if _, err := process.ExecAsync(resetCmd); err != nil {
			emit(fmt.Sprintf("Warning: failed to reset status: %v", err))
		}
	}
	emit("All service statuses reset to idle.")

	emit("Restore completed successfully!")
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
