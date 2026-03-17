// Input: db, docker, config
// Output: DatabaseService (数据库部署/停止/重建全流程，与 TS 版完全对齐)
// Role: 数据库服务部署编排，内联执行（不走队列），支持 onData 回调实时日志
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package service

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	containertypes "github.com/docker/docker/api/types/container"
	dockermount "github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/swarm"
	"github.com/dokploy/dokploy/internal/config"
	"github.com/dokploy/dokploy/internal/db"
	"github.com/dokploy/dokploy/internal/db/schema"
	"github.com/dokploy/dokploy/internal/docker"
	"github.com/dokploy/dokploy/internal/process"
)

// DatabaseService handles database service deployment and management.
type DatabaseService struct {
	db     *db.DB
	docker *docker.Client
	cfg    *config.Config
}

// NewDatabaseService creates a new DatabaseService.
func NewDatabaseService(database *db.DB, dockerClient *docker.Client, cfg *config.Config) *DatabaseService {
	return &DatabaseService{db: database, docker: dockerClient, cfg: cfg}
}

// DeployPostgres 部署 PostgreSQL 服务（与 TS 版 deployPostgres 对齐：内联执行，支持日志回调）
func (s *DatabaseService) DeployPostgres(postgresID string, onData func(string)) error {
	var pg schema.Postgres
	if err := s.db.Preload("Mounts").Preload("Server").Preload("Server.SSHKey").
		Preload("Environment").Preload("Environment.Project").
		First(&pg, "\"postgresId\" = ?", postgresID).Error; err != nil {
		return err
	}
	s.updatePostgresStatus(postgresID, schema.ApplicationStatusRunning)

	// 与 TS 版一致：构建默认环境变量字符串，然后通过 prepareEnvironmentVariables 解析模板
	defaultEnv := fmt.Sprintf("POSTGRES_DB=%s\nPOSTGRES_USER=%s\nPOSTGRES_PASSWORD=%s",
		pg.DatabaseName, pg.DatabaseUser, pg.DatabasePassword)
	if pg.Env != nil && *pg.Env != "" {
		defaultEnv += "\n" + *pg.Env
	}

	err := s.deployDatabaseService(pg.AppName, pg.DockerImage, pg.ServerID, pg.Server,
		defaultEnv, pg.Command, []string(pg.Args), pg.Mounts,
		pg.MemoryLimit, pg.MemoryReservation, pg.CPULimit, pg.CPUReservation,
		pg.ExternalPort, 5432, &pg.SwarmConfig, pg.Environment, onData)
	if err != nil {
		s.updatePostgresStatus(postgresID, schema.ApplicationStatusError)
		return err
	}

	s.updatePostgresStatus(postgresID, schema.ApplicationStatusDone)
	return nil
}

// DeployMySQL 部署 MySQL 服务（与 TS 版对齐：root 用户不设 MYSQL_USER/MYSQL_PASSWORD）
func (s *DatabaseService) DeployMySQL(mysqlID string, onData func(string)) error {
	var my schema.MySQL
	if err := s.db.Preload("Mounts").Preload("Server").Preload("Server.SSHKey").
		Preload("Environment").Preload("Environment.Project").
		First(&my, "\"mysqlId\" = ?", mysqlID).Error; err != nil {
		return err
	}
	s.updateMySQLStatus(mysqlID, schema.ApplicationStatusRunning)

	// 与 TS 版一致：root 用户只设 MYSQL_DATABASE + MYSQL_ROOT_PASSWORD，非 root 才加 MYSQL_USER/MYSQL_PASSWORD
	var defaultEnv string
	if my.DatabaseUser != "root" {
		defaultEnv = fmt.Sprintf("MYSQL_USER=%s\nMYSQL_DATABASE=%s\nMYSQL_PASSWORD=%s\nMYSQL_ROOT_PASSWORD=%s",
			my.DatabaseUser, my.DatabaseName, my.DatabasePassword, my.DatabaseRootPassword)
	} else {
		defaultEnv = fmt.Sprintf("MYSQL_DATABASE=%s\nMYSQL_ROOT_PASSWORD=%s",
			my.DatabaseName, my.DatabaseRootPassword)
	}
	if my.Env != nil && *my.Env != "" {
		defaultEnv += "\n" + *my.Env
	}

	err := s.deployDatabaseService(my.AppName, my.DockerImage, my.ServerID, my.Server,
		defaultEnv, my.Command, []string(my.Args), my.Mounts,
		my.MemoryLimit, my.MemoryReservation, my.CPULimit, my.CPUReservation,
		my.ExternalPort, 3306, &my.SwarmConfig, my.Environment, onData)
	if err != nil {
		s.updateMySQLStatus(mysqlID, schema.ApplicationStatusError)
		return err
	}

	s.updateMySQLStatus(mysqlID, schema.ApplicationStatusDone)
	return nil
}

// DeployMariaDB 部署 MariaDB 服务（与 TS 版对齐：始终设置全部 4 个环境变量）
func (s *DatabaseService) DeployMariaDB(mariadbID string, onData func(string)) error {
	var mdb schema.MariaDB
	if err := s.db.Preload("Mounts").Preload("Server").Preload("Server.SSHKey").
		Preload("Environment").Preload("Environment.Project").
		First(&mdb, "\"mariadbId\" = ?", mariadbID).Error; err != nil {
		return err
	}
	s.updateMariaDBStatus(mariadbID, schema.ApplicationStatusRunning)

	defaultEnv := fmt.Sprintf("MARIADB_DATABASE=%s\nMARIADB_USER=%s\nMARIADB_PASSWORD=%s\nMARIADB_ROOT_PASSWORD=%s",
		mdb.DatabaseName, mdb.DatabaseUser, mdb.DatabasePassword, mdb.DatabaseRootPassword)
	if mdb.Env != nil && *mdb.Env != "" {
		defaultEnv += "\n" + *mdb.Env
	}

	err := s.deployDatabaseService(mdb.AppName, mdb.DockerImage, mdb.ServerID, mdb.Server,
		defaultEnv, mdb.Command, []string(mdb.Args), mdb.Mounts,
		mdb.MemoryLimit, mdb.MemoryReservation, mdb.CPULimit, mdb.CPUReservation,
		mdb.ExternalPort, 3306, &mdb.SwarmConfig, mdb.Environment, onData)
	if err != nil {
		s.updateMariaDBStatus(mariadbID, schema.ApplicationStatusError)
		return err
	}

	s.updateMariaDBStatus(mariadbID, schema.ApplicationStatusDone)
	return nil
}

// DeployMongo 部署 MongoDB 服务（与 TS 版对齐：支持 ReplicaSet 副本集初始化）
func (s *DatabaseService) DeployMongo(mongoID string, onData func(string)) error {
	var mongo schema.Mongo
	if err := s.db.Preload("Mounts").Preload("Server").Preload("Server.SSHKey").
		Preload("Environment").Preload("Environment.Project").
		First(&mongo, "\"mongoId\" = ?", mongoID).Error; err != nil {
		return err
	}
	s.updateMongoStatus(mongoID, schema.ApplicationStatusRunning)

	defaultEnv := fmt.Sprintf("MONGO_INITDB_ROOT_USERNAME=%s\nMONGO_INITDB_ROOT_PASSWORD=%s",
		mongo.DatabaseUser, mongo.DatabasePassword)

	// 与 TS 版一致：ReplicaSet 模式额外添加 MONGO_INITDB_DATABASE=admin
	if mongo.ReplicaSets {
		defaultEnv += "\nMONGO_INITDB_DATABASE=admin"
	}
	if mongo.Env != nil && *mongo.Env != "" {
		defaultEnv += "\n" + *mongo.Env
	}

	// 与 TS 版一致：ReplicaSet 模式覆盖 command，使用启动脚本初始化副本集
	command := mongo.Command
	if mongo.ReplicaSets {
		// 与 TS 版 buildMongo 完全对齐的副本集启动脚本
		replicaScript := fmt.Sprintf(`mongod --port 27017 --replSet rs0 --bind_ip_all &
MONGOD_PID=$!
until mongosh --port 27017 --eval "db.adminCommand('ping')" > /dev/null 2>&1; do
  sleep 1
done
if ! mongosh --port 27017 --eval "rs.status().ok" > /dev/null 2>&1; then
  mongosh --port 27017 --eval "rs.initiate({_id: 'rs0', members: [{_id: 0, host: '%s:27017'}]})"
  until mongosh --port 27017 --eval "rs.isMaster().ismaster" | grep -q "true"; do
    sleep 1
  done
  mongosh --port 27017 --eval "db.getSiblingDB('admin').createUser({user: '%s', pwd: '%s', roles: [{role: 'root', db: 'admin'}]})"
fi
wait $MONGOD_PID`, mongo.AppName, mongo.DatabaseUser, mongo.DatabasePassword)
		// 与 TS 版一致：ReplicaSet 模式 command 固定为 /bin/bash -c script
		// 通过 deployDatabaseService 传入 command="/bin/bash"，但实际使用 containerSpec.Args
		// 需要特殊处理：这里用 command 传完整的 /bin/bash -c '...'
		bashCmd := "/bin/bash"
		command = &bashCmd
		// 将脚本存储在一个临时变量中，通过 special deploy 逻辑处理
		// 实际上在 buildServiceSpec 中 command 会 split by space，但 /bin/bash 不会有问题
		// args 需要单独处理 — 我们通过设置 mongo.Args 来传递
		mongo.Args = []string{"-c", replicaScript}
	}

	err := s.deployDatabaseService(mongo.AppName, mongo.DockerImage, mongo.ServerID, mongo.Server,
		defaultEnv, command, []string(mongo.Args), mongo.Mounts,
		mongo.MemoryLimit, mongo.MemoryReservation, mongo.CPULimit, mongo.CPUReservation,
		mongo.ExternalPort, 27017, &mongo.SwarmConfig, mongo.Environment, onData)
	if err != nil {
		s.updateMongoStatus(mongoID, schema.ApplicationStatusError)
		return err
	}

	s.updateMongoStatus(mongoID, schema.ApplicationStatusDone)
	return nil
}

// DeployRedis 部署 Redis 服务（与 TS 版对齐：REDIS_PASSWORD env + 默认 requirepass 命令）
func (s *DatabaseService) DeployRedis(redisID string, onData func(string)) error {
	var redis schema.Redis
	if err := s.db.Preload("Mounts").Preload("Server").Preload("Server.SSHKey").
		Preload("Environment").Preload("Environment.Project").
		First(&redis, "\"redisId\" = ?", redisID).Error; err != nil {
		return err
	}
	s.updateRedisStatus(redisID, schema.ApplicationStatusRunning)

	// 与 TS 版一致：REDIS_PASSWORD 作为环境变量
	defaultEnv := fmt.Sprintf("REDIS_PASSWORD=%s", redis.DatabasePassword)
	if redis.Env != nil && *redis.Env != "" {
		defaultEnv += "\n" + *redis.Env
	}

	// 与 TS 版一致：如果有自定义 command 或 args，直接使用；否则默认 /bin/sh -c "redis-server --requirepass pw"
	command := redis.Command
	if (command == nil || *command == "") && len(redis.Args) == 0 {
		bashCmd := "/bin/sh"
		command = &bashCmd
		redis.Args = []string{"-c", fmt.Sprintf("redis-server --requirepass %s", redis.DatabasePassword)}
	}

	err := s.deployDatabaseService(redis.AppName, redis.DockerImage, redis.ServerID, redis.Server,
		defaultEnv, command, []string(redis.Args), redis.Mounts,
		redis.MemoryLimit, redis.MemoryReservation, redis.CPULimit, redis.CPUReservation,
		redis.ExternalPort, 6379, &redis.SwarmConfig, redis.Environment, onData)
	if err != nil {
		s.updateRedisStatus(redisID, schema.ApplicationStatusError)
		return err
	}

	s.updateRedisStatus(redisID, schema.ApplicationStatusDone)
	return nil
}

// DeployByType 按类型分发部署（供 handler 层直接调用）
func (s *DatabaseService) DeployByType(databaseID, dbType string, onData func(string)) error {
	switch schema.DatabaseType(dbType) {
	case schema.DatabaseTypePostgres:
		return s.DeployPostgres(databaseID, onData)
	case schema.DatabaseTypeMySQL:
		return s.DeployMySQL(databaseID, onData)
	case schema.DatabaseTypeMariaDB:
		return s.DeployMariaDB(databaseID, onData)
	case schema.DatabaseTypeMongo:
		return s.DeployMongo(databaseID, onData)
	case schema.DatabaseTypeRedis:
		return s.DeployRedis(databaseID, onData)
	default:
		return fmt.Errorf("unsupported database type: %s", dbType)
	}
}

// RebuildDatabase 重建数据库服务（与 TS 版 rebuildDatabase 对齐：删除服务+卷→重新部署）
func (s *DatabaseService) RebuildDatabase(databaseID string, dbType schema.DatabaseType) error {
	var appName string
	var serverID *string
	var mounts []schema.Mount

	switch dbType {
	case schema.DatabaseTypePostgres:
		var pg schema.Postgres
		if err := s.db.Preload("Mounts").First(&pg, "\"postgresId\" = ?", databaseID).Error; err != nil {
			return err
		}
		appName = pg.AppName
		serverID = pg.ServerID
		mounts = pg.Mounts
	case schema.DatabaseTypeMySQL:
		var my schema.MySQL
		if err := s.db.Preload("Mounts").First(&my, "\"mysqlId\" = ?", databaseID).Error; err != nil {
			return err
		}
		appName = my.AppName
		serverID = my.ServerID
		mounts = my.Mounts
	case schema.DatabaseTypeMariaDB:
		var mdb schema.MariaDB
		if err := s.db.Preload("Mounts").First(&mdb, "\"mariadbId\" = ?", databaseID).Error; err != nil {
			return err
		}
		appName = mdb.AppName
		serverID = mdb.ServerID
		mounts = mdb.Mounts
	case schema.DatabaseTypeMongo:
		var mongo schema.Mongo
		if err := s.db.Preload("Mounts").First(&mongo, "\"mongoId\" = ?", databaseID).Error; err != nil {
			return err
		}
		appName = mongo.AppName
		serverID = mongo.ServerID
		mounts = mongo.Mounts
	case schema.DatabaseTypeRedis:
		var redis schema.Redis
		if err := s.db.Preload("Mounts").First(&redis, "\"redisId\" = ?", databaseID).Error; err != nil {
			return err
		}
		appName = redis.AppName
		serverID = redis.ServerID
		mounts = redis.Mounts
	default:
		return fmt.Errorf("unsupported database type: %s", dbType)
	}

	// 与 TS 版一致：删除服务
	s.removeServiceByName(appName, serverID)
	// 与 TS 版一致：等待 6 秒
	time.Sleep(6 * time.Second)

	// 与 TS 版一致：删除所有 volume
	for _, mount := range mounts {
		if mount.Type == schema.MountTypeVolume && mount.VolumeName != nil {
			cmd := fmt.Sprintf("docker volume rm %s --force", *mount.VolumeName)
			if serverID != nil {
				s.execRemoteByServerID(*serverID, cmd)
			} else {
				process.ExecAsync(cmd)
			}
		}
	}

	// 重新部署（不传 onData，rebuild 不需要实时日志）
	return s.DeployByType(databaseID, string(dbType), nil)
}

// StopDatabase 停止数据库服务（与 TS 版对齐：docker service scale=0 + 状态设为 idle）
func (s *DatabaseService) StopDatabase(databaseID string, dbType schema.DatabaseType) error {
	var appName string
	var serverID *string

	switch dbType {
	case schema.DatabaseTypePostgres:
		var pg schema.Postgres
		if err := s.db.First(&pg, "\"postgresId\" = ?", databaseID).Error; err != nil {
			return fmt.Errorf("postgres not found: %w", err)
		}
		appName = pg.AppName
		serverID = pg.ServerID
		defer s.updatePostgresStatus(databaseID, schema.ApplicationStatusIdle)
	case schema.DatabaseTypeMySQL:
		var my schema.MySQL
		if err := s.db.First(&my, "\"mysqlId\" = ?", databaseID).Error; err != nil {
			return fmt.Errorf("mysql not found: %w", err)
		}
		appName = my.AppName
		serverID = my.ServerID
		defer s.updateMySQLStatus(databaseID, schema.ApplicationStatusIdle)
	case schema.DatabaseTypeMariaDB:
		var mdb schema.MariaDB
		if err := s.db.First(&mdb, "\"mariadbId\" = ?", databaseID).Error; err != nil {
			return fmt.Errorf("mariadb not found: %w", err)
		}
		appName = mdb.AppName
		serverID = mdb.ServerID
		defer s.updateMariaDBStatus(databaseID, schema.ApplicationStatusIdle)
	case schema.DatabaseTypeMongo:
		var m schema.Mongo
		if err := s.db.First(&m, "\"mongoId\" = ?", databaseID).Error; err != nil {
			return fmt.Errorf("mongo not found: %w", err)
		}
		appName = m.AppName
		serverID = m.ServerID
		defer s.updateMongoStatus(databaseID, schema.ApplicationStatusIdle)
	case schema.DatabaseTypeRedis:
		var r schema.Redis
		if err := s.db.First(&r, "\"redisId\" = ?", databaseID).Error; err != nil {
			return fmt.Errorf("redis not found: %w", err)
		}
		appName = r.AppName
		serverID = r.ServerID
		defer s.updateRedisStatus(databaseID, schema.ApplicationStatusIdle)
	default:
		return fmt.Errorf("unsupported database type: %s", dbType)
	}

	cmd := fmt.Sprintf("docker service scale %s=0", appName)
	if serverID != nil {
		s.execRemoteByServerID(*serverID, cmd)
	} else {
		process.ExecAsyncStream(cmd, nil)
	}

	return nil
}

// StartDatabase 启动数据库服务（与 TS 版对齐：docker service scale=1 + 状态设为 done）
func (s *DatabaseService) StartDatabase(databaseID string, dbType schema.DatabaseType) error {
	var appName string
	var serverID *string

	switch dbType {
	case schema.DatabaseTypePostgres:
		var pg schema.Postgres
		if err := s.db.First(&pg, "\"postgresId\" = ?", databaseID).Error; err != nil {
			return fmt.Errorf("postgres not found: %w", err)
		}
		appName = pg.AppName
		serverID = pg.ServerID
		defer s.updatePostgresStatus(databaseID, schema.ApplicationStatusDone)
	case schema.DatabaseTypeMySQL:
		var my schema.MySQL
		if err := s.db.First(&my, "\"mysqlId\" = ?", databaseID).Error; err != nil {
			return fmt.Errorf("mysql not found: %w", err)
		}
		appName = my.AppName
		serverID = my.ServerID
		defer s.updateMySQLStatus(databaseID, schema.ApplicationStatusDone)
	case schema.DatabaseTypeMariaDB:
		var mdb schema.MariaDB
		if err := s.db.First(&mdb, "\"mariadbId\" = ?", databaseID).Error; err != nil {
			return fmt.Errorf("mariadb not found: %w", err)
		}
		appName = mdb.AppName
		serverID = mdb.ServerID
		defer s.updateMariaDBStatus(databaseID, schema.ApplicationStatusDone)
	case schema.DatabaseTypeMongo:
		var m schema.Mongo
		if err := s.db.First(&m, "\"mongoId\" = ?", databaseID).Error; err != nil {
			return fmt.Errorf("mongo not found: %w", err)
		}
		appName = m.AppName
		serverID = m.ServerID
		defer s.updateMongoStatus(databaseID, schema.ApplicationStatusDone)
	case schema.DatabaseTypeRedis:
		var r schema.Redis
		if err := s.db.First(&r, "\"redisId\" = ?", databaseID).Error; err != nil {
			return fmt.Errorf("redis not found: %w", err)
		}
		appName = r.AppName
		serverID = r.ServerID
		defer s.updateRedisStatus(databaseID, schema.ApplicationStatusDone)
	default:
		return fmt.Errorf("unsupported database type: %s", dbType)
	}

	if serverID != nil {
		s.execRemoteByServerID(*serverID, fmt.Sprintf("docker service scale %s=1", appName))
	} else {
		s.docker.ScaleService(context.Background(), appName, 1)
	}

	return nil
}

// deployDatabaseService 数据库部署核心逻辑
// 与 TS 版对齐：使用 Docker SDK update-first / create-fallback（幂等，多次点击不冲突）
// defaultEnv: 已合并的环境变量字符串（默认 + 自定义），将通过 prepareEnvironmentVariables 解析模板
func (s *DatabaseService) deployDatabaseService(
	appName, dockerImage string,
	serverID *string, server *schema.Server,
	defaultEnv string,
	command *string, args []string,
	mounts []schema.Mount,
	memoryLimit, memoryReservation, cpuLimit, cpuReservation *string,
	externalPort *int, defaultPort int,
	swarmConfig *schema.SwarmConfig,
	environment *schema.Environment,
	onData func(string),
) error {
	emit := func(msg string) {
		if onData != nil {
			onData(msg)
		}
	}

	emit(fmt.Sprintf("Deploying %s: 🔄\n", appName))

	// Step 1: Pull image
	emit(fmt.Sprintf("Pulling image: %s\n", dockerImage))
	pullArgs := []string{"docker", "pull", dockerImage}
	if serverID != nil && server != nil && server.SSHKey != nil {
		conn := sshConnFromServer(server)
		process.ExecAsyncRemote(conn, strings.Join(pullArgs, " "), func(line string) { emit(line + "\n") })
	} else {
		s.runLocalCommand(pullArgs, func(line string) { emit(line + "\n") })
	}

	emit(fmt.Sprintf("App name: %s\n", appName))

	// 与 TS 版一致：通过 prepareEnvironmentVariables 解析模板变量
	var projectEnv, environmentEnv string
	if environment != nil {
		environmentEnv = environment.Env
		if environment.Project != nil {
			projectEnv = environment.Project.Env
		}
	}
	envVars := prepareEnvironmentVariables(defaultEnv, projectEnv, environmentEnv)

	// Step 2: 构建 ServiceSpec（与 TS 版 createService + generateConfigContainer 对齐）
	spec := s.buildServiceSpec(appName, dockerImage, envVars, command, args, mounts,
		memoryLimit, memoryReservation, cpuLimit, cpuReservation,
		externalPort, defaultPort, swarmConfig)

	// Step 3: 本地文件挂载准备（写入文件内容到磁盘）
	for _, m := range mounts {
		if m.Type == schema.MountTypeFile && m.FilePath != nil && m.Content != nil {
			dir := filepath.Join("/etc/dokploy/applications", appName, "files")
			filePath := filepath.Join(dir, *m.FilePath)
			mkdirCmd := fmt.Sprintf("mkdir -p '%s'", dir)
			writeCmd := fmt.Sprintf("echo '%s' | base64 -d > '%s'", base64Encode(*m.Content), filePath)
			if serverID != nil && server != nil && server.SSHKey != nil {
				conn := sshConnFromServer(server)
				process.ExecAsyncRemote(conn, mkdirCmd+" && "+writeCmd, nil)
			} else {
				process.ExecAsync(mkdirCmd + " && " + writeCmd)
			}
		}
	}

	// Step 4: 部署（update-first / create-fallback）
	if serverID != nil && server != nil && server.SSHKey != nil {
		// 远程部署：通过 SSH CLI（rm + create 模式）
		return s.deployRemoteService(server, appName, dockerImage, envVars, command, args,
			mounts, memoryLimit, memoryReservation, cpuLimit, cpuReservation,
			externalPort, defaultPort, swarmConfig, emit)
	}

	// 本地部署：使用 Docker SDK（与 TS 版完全一致的 update-first 模式）
	return s.deployLocalService(appName, spec, emit)
}

// buildServiceSpec 构建 Docker Swarm ServiceSpec
// 与 TS 版 createService + generateConfigContainer 完全对齐：包含所有 Swarm JSONB 配置
func (s *DatabaseService) buildServiceSpec(
	appName, dockerImage string,
	envVars []string,
	command *string, args []string,
	mounts []schema.Mount,
	memoryLimit, memoryReservation, cpuLimit, cpuReservation *string,
	externalPort *int, defaultPort int,
	swarmConfig *schema.SwarmConfig,
) swarm.ServiceSpec {
	applicationsPath := "/etc/dokploy/applications"

	// ===== 挂载列表（volume/bind/file 三种类型，与 TS 版对齐） =====
	var mountList []dockermount.Mount
	for _, m := range mounts {
		switch m.Type {
		case schema.MountTypeVolume:
			if m.VolumeName != nil && *m.VolumeName != "" {
				mountList = append(mountList, dockermount.Mount{
					Type:   dockermount.TypeVolume,
					Source: *m.VolumeName,
					Target: m.MountPath,
				})
			}
		case schema.MountTypeBind:
			if m.HostPath != nil && *m.HostPath != "" {
				mountList = append(mountList, dockermount.Mount{
					Type:   dockermount.TypeBind,
					Source: *m.HostPath,
					Target: m.MountPath,
				})
			}
		case schema.MountTypeFile:
			if m.FilePath != nil {
				sourcePath := filepath.Join(applicationsPath, appName, "files", *m.FilePath)
				mountList = append(mountList, dockermount.Mount{
					Type:   dockermount.TypeBind,
					Source: sourcePath,
					Target: m.MountPath,
				})
			}
		}
	}

	// ===== 资源限制与预留（与 TS 版 calculateResources 对齐） =====
	var resources *swarm.ResourceRequirements
	var limits swarm.Limit
	var reservations swarm.Resources
	hasLimits := false
	hasReservations := false
	if memoryLimit != nil && *memoryLimit != "" {
		limits.MemoryBytes = parseMemoryString(*memoryLimit)
		hasLimits = true
	}
	if cpuLimit != nil && *cpuLimit != "" {
		limits.NanoCPUs = parseCPUString(*cpuLimit)
		hasLimits = true
	}
	if memoryReservation != nil && *memoryReservation != "" {
		reservations.MemoryBytes = parseMemoryString(*memoryReservation)
		hasReservations = true
	}
	if cpuReservation != nil && *cpuReservation != "" {
		reservations.NanoCPUs = parseCPUString(*cpuReservation)
		hasReservations = true
	}
	if hasLimits || hasReservations {
		resources = &swarm.ResourceRequirements{}
		if hasLimits {
			resources.Limits = &limits
		}
		if hasReservations {
			resources.Reservations = &reservations
		}
	}

	// ===== ContainerSpec =====
	containerSpec := &swarm.ContainerSpec{
		Image:  dockerImage,
		Env:    envVars,
		Mounts: mountList,
	}

	// Command override（与 TS 版一致：command.split(" ") → ContainerSpec.Command）
	if command != nil && *command != "" {
		containerSpec.Command = strings.Fields(*command)
	}
	// Args（与 TS 版一致：args → ContainerSpec.Args）
	if len(args) > 0 {
		containerSpec.Args = args
	}

	// ===== 以下从 SwarmConfig 的 interface{} 字段中解析 Swarm 配置 =====
	// SwarmConfig 使用 JSONField[interface{}]，需要 JSON 重编码后反序列化为类型化结构体

	// 默认值
	replicas := uint64(1)
	networks := []swarm.NetworkAttachmentConfig{{Target: "dokploy-network"}}
	var placement *swarm.Placement
	var restartPolicy *swarm.RestartPolicy
	var updateConfig *swarm.UpdateConfig
	var rollbackConfig *swarm.UpdateConfig
	var endpointSpec *swarm.EndpointSpec
	mode := swarm.ServiceMode{
		Replicated: &swarm.ReplicatedService{Replicas: &replicas},
	}

	// 默认 EndpointSpec（与 TS 版一致：dnsrr + host mode ports）
	var ports []swarm.PortConfig
	if externalPort != nil && *externalPort > 0 {
		ports = append(ports, swarm.PortConfig{
			Protocol:      swarm.PortConfigProtocolTCP,
			TargetPort:    uint32(defaultPort),
			PublishedPort: uint32(*externalPort),
			PublishMode:   swarm.PortConfigPublishModeHost,
		})
	}
	endpointSpec = &swarm.EndpointSpec{
		Mode:  swarm.ResolutionModeDNSRR,
		Ports: ports,
	}

	// 默认 UpdateConfig（与 TS 版一致）
	updateConfig = &swarm.UpdateConfig{
		Parallelism: 1,
		Order:       "start-first",
	}

	// 默认 Placement（与 TS 版一致：有挂载时约束到 manager）
	if len(mounts) > 0 {
		placement = &swarm.Placement{Constraints: []string{"node.role==manager"}}
	}

	if swarmConfig != nil {
		// ===== HealthCheck =====
		if hcData := swarmConfig.HealthCheckSwarm.Data; hcData != nil {
			var hc schema.HealthCheckSwarm
			if remarshal(hcData, &hc) == nil && len(hc.Test) > 0 {
				containerSpec.Healthcheck = &containertypes.HealthConfig{
					Test: hc.Test,
				}
				if hc.Interval != nil {
					containerSpec.Healthcheck.Interval = time.Duration(*hc.Interval)
				}
				if hc.Timeout != nil {
					containerSpec.Healthcheck.Timeout = time.Duration(*hc.Timeout)
				}
				if hc.StartPeriod != nil {
					containerSpec.Healthcheck.StartPeriod = time.Duration(*hc.StartPeriod)
				}
				if hc.Retries != nil {
					containerSpec.Healthcheck.Retries = *hc.Retries
				}
			}
		}

		// ===== RestartPolicy =====
		if rpData := swarmConfig.RestartPolicySwarm.Data; rpData != nil {
			var rp schema.RestartPolicySwarm
			if remarshal(rpData, &rp) == nil {
				restartPolicy = &swarm.RestartPolicy{}
				if rp.Condition != nil {
					cond := swarm.RestartPolicyCondition(*rp.Condition)
					restartPolicy.Condition = cond
				}
				if rp.Delay != nil {
					d := time.Duration(*rp.Delay)
					restartPolicy.Delay = &d
				}
				if rp.MaxAttempts != nil {
					ma := uint64(*rp.MaxAttempts)
					restartPolicy.MaxAttempts = &ma
				}
				if rp.Window != nil {
					d := time.Duration(*rp.Window)
					restartPolicy.Window = &d
				}
			}
		}

		// ===== Placement =====
		if psData := swarmConfig.PlacementSwarm.Data; psData != nil {
			var ps schema.PlacementSwarm
			if remarshal(psData, &ps) == nil && (len(ps.Constraints) > 0 || len(ps.Preferences) > 0) {
				placement = &swarm.Placement{Constraints: ps.Constraints}
				for _, pref := range ps.Preferences {
					placement.Preferences = append(placement.Preferences, swarm.PlacementPreference{
						Spread: &swarm.SpreadOver{SpreadDescriptor: pref.Spread.SpreadDescriptor},
					})
				}
				if ps.MaxReplicas != nil {
					placement.MaxReplicas = uint64(*ps.MaxReplicas)
				}
				for _, p := range ps.Platforms {
					placement.Platforms = append(placement.Platforms, swarm.Platform{
						Architecture: p.Architecture, OS: p.OS,
					})
				}
			}
		}

		// ===== Labels =====
		if lblData := swarmConfig.LabelsSwarm.Data; lblData != nil {
			var labels schema.LabelsSwarm
			if remarshal(lblData, &labels) == nil && len(labels) > 0 {
				containerSpec.Labels = map[string]string(labels)
			}
		}

		// ===== Mode =====
		if modeData := swarmConfig.ModeSwarm.Data; modeData != nil {
			var sm schema.ServiceModeSwarm
			if remarshal(modeData, &sm) == nil {
				if sm.Global != nil {
					mode = swarm.ServiceMode{Global: &swarm.GlobalService{}}
				} else if sm.Replicated != nil && sm.Replicated.Replicas != nil {
					r := uint64(*sm.Replicated.Replicas)
					mode = swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: &r}}
				} else {
					r := uint64(swarmConfig.Replicas)
					mode = swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: &r}}
				}
			}
		}

		// ===== UpdateConfig =====
		if ucData := swarmConfig.UpdateConfigSwarm.Data; ucData != nil {
			var uc schema.UpdateConfigSwarm
			if remarshal(ucData, &uc) == nil {
				updateConfig = &swarm.UpdateConfig{
					Parallelism: uint64(uc.Parallelism),
					Order:       uc.Order,
				}
				if uc.Delay != nil {
					updateConfig.Delay = time.Duration(*uc.Delay)
				}
				if uc.FailureAction != nil {
					updateConfig.FailureAction = *uc.FailureAction
				}
				if uc.Monitor != nil {
					updateConfig.Monitor = time.Duration(*uc.Monitor)
				}
				if uc.MaxFailureRatio != nil {
					updateConfig.MaxFailureRatio = float32(*uc.MaxFailureRatio)
				}
			}
		}

		// ===== RollbackConfig =====
		if rcData := swarmConfig.RollbackConfigSwarm.Data; rcData != nil {
			var rc schema.UpdateConfigSwarm
			if remarshal(rcData, &rc) == nil {
				rollbackConfig = &swarm.UpdateConfig{
					Parallelism: uint64(rc.Parallelism),
					Order:       rc.Order,
				}
				if rc.Delay != nil {
					rollbackConfig.Delay = time.Duration(*rc.Delay)
				}
				if rc.FailureAction != nil {
					rollbackConfig.FailureAction = *rc.FailureAction
				}
				if rc.Monitor != nil {
					rollbackConfig.Monitor = time.Duration(*rc.Monitor)
				}
				if rc.MaxFailureRatio != nil {
					rollbackConfig.MaxFailureRatio = float32(*rc.MaxFailureRatio)
				}
			}
		}

		// ===== StopGracePeriod =====
		if swarmConfig.StopGracePeriodSwarm != nil && *swarmConfig.StopGracePeriodSwarm > 0 {
			d := time.Duration(*swarmConfig.StopGracePeriodSwarm)
			containerSpec.StopGracePeriod = &d
		}

		// ===== Ulimits =====
		if ulData := swarmConfig.UlimitsSwarm.Data; ulData != nil {
			var uls schema.UlimitsSwarm
			if remarshal(ulData, &uls) == nil && len(uls) > 0 {
				for _, ul := range uls {
					containerSpec.Ulimits = append(containerSpec.Ulimits, &containertypes.Ulimit{
						Name: ul.Name,
						Soft: int64(ul.Soft),
						Hard: int64(ul.Hard),
					})
				}
			}
		}

		// ===== Networks =====
		if netData := swarmConfig.NetworkSwarm.Data; len(netData) > 0 {
			var nets []schema.NetworkSwarm
			if remarshal(netData, &nets) == nil && len(nets) > 0 {
				networks = nil
				for _, net := range nets {
					if net.Target != nil && *net.Target != "" {
						nac := swarm.NetworkAttachmentConfig{Target: *net.Target}
						if len(net.Aliases) > 0 {
							nac.Aliases = net.Aliases
						}
						if len(net.DriverOpts) > 0 {
							nac.DriverOpts = net.DriverOpts
						}
						networks = append(networks, nac)
					}
				}
			}
		}

		// ===== EndpointSpec =====
		if esData := swarmConfig.EndpointSpecSwarm.Data; esData != nil {
			var es schema.EndpointSpecSwarm
			if remarshal(esData, &es) == nil && (es.Mode != nil || len(es.Ports) > 0) {
				endpointSpec = &swarm.EndpointSpec{}
				if es.Mode != nil && *es.Mode != "" {
					endpointSpec.Mode = swarm.ResolutionMode(*es.Mode)
				}
				for _, port := range es.Ports {
					pc := swarm.PortConfig{
						Protocol:    swarm.PortConfigProtocol(safeStrPtr(port.Protocol, "tcp")),
						PublishMode: swarm.PortConfigPublishMode(safeStrPtr(port.PublishMode, "host")),
					}
					if port.TargetPort != nil {
						pc.TargetPort = uint32(*port.TargetPort)
					}
					if port.PublishedPort != nil {
						pc.PublishedPort = uint32(*port.PublishedPort)
					}
					endpointSpec.Ports = append(endpointSpec.Ports, pc)
				}
			}
		}
	}

	return swarm.ServiceSpec{
		Annotations: swarm.Annotations{Name: appName},
		TaskTemplate: swarm.TaskSpec{
			ContainerSpec: containerSpec,
			Networks:      networks,
			RestartPolicy: restartPolicy,
			Placement:     placement,
			Resources:     resources,
		},
		Mode:           mode,
		UpdateConfig:   updateConfig,
		RollbackConfig: rollbackConfig,
		EndpointSpec:   endpointSpec,
	}
}

// remarshal 将 interface{} 通过 JSON 重编码转换为目标类型
// 用于 SwarmConfig 的 JSONField[interface{}] 字段转换为类型化结构体
func remarshal(src interface{}, dst interface{}) error {
	data, err := json.Marshal(src)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dst)
}

// deployLocalService 本地部署：Docker SDK update-first / create-fallback（与 TS 版完全一致）
func (s *DatabaseService) deployLocalService(appName string, spec swarm.ServiceSpec, emit func(string)) error {
	ctx := context.Background()

	// 与 TS 版一致：先尝试 inspect + update，失败则 create
	svc, _, err := s.docker.DockerClient().ServiceInspectWithRaw(ctx, appName, types.ServiceInspectOptions{})
	if err == nil {
		// 服务已存在 → update（ForceUpdate++ 强制重启任务）
		emit("Updating existing service...\n")
		spec.TaskTemplate.ForceUpdate = svc.Spec.TaskTemplate.ForceUpdate + 1
		_, err = s.docker.DockerClient().ServiceUpdate(ctx, svc.ID, svc.Version, spec, types.ServiceUpdateOptions{})
		if err != nil {
			emit(fmt.Sprintf("Error updating service: %v\n", err))
			return err
		}
		emit("Service updated successfully!\n")
		return nil
	}

	// 服务不存在 → create
	emit("Creating new service...\n")
	_, err = s.docker.DockerClient().ServiceCreate(ctx, spec, types.ServiceCreateOptions{})
	if err != nil {
		emit(fmt.Sprintf("Error creating service: %v\n", err))
		return err
	}
	emit("Deployment completed successfully!\n")
	return nil
}

// deployRemoteService 远程部署：通过 SSH CLI（rm + create 模式）
// 与 TS 版对齐：TS 版远程数据库也使用 getRemoteDocker(serverId) SSH 隧道 SDK 整体替换 spec。
// Go 远程无法使用 SDK，因此用 rm + create 确保 spec 完全替换，避免 --env-add 等累积问题。
// 数据库数据在 volume 中，rm 不会丢失数据。
func (s *DatabaseService) deployRemoteService(
	server *schema.Server,
	appName, dockerImage string,
	envVars []string,
	command *string, args []string,
	mounts []schema.Mount,
	memoryLimit, memoryReservation, cpuLimit, cpuReservation *string,
	externalPort *int, defaultPort int,
	swarmConfig *schema.SwarmConfig,
	emit func(string),
) error {
	conn := sshConnFromServer(server)

	// 构建 create 参数（完整 spec，不使用 update 追加模式）
	createArgs := []string{"docker", "service", "create",
		"--name", appName,
	}

	// ===== 网络（与 TS 版一致：networkSwarm 优先，否则默认 dokploy-network） =====
	hasCustomNetwork := false
	if swarmConfig != nil {
		if netData := swarmConfig.NetworkSwarm.Data; len(netData) > 0 {
			var nets []schema.NetworkSwarm
			if remarshal(netData, &nets) == nil {
				for _, net := range nets {
					if net.Target != nil && *net.Target != "" {
						createArgs = append(createArgs, "--network", *net.Target)
						hasCustomNetwork = true
					}
				}
			}
		}
	}
	if !hasCustomNetwork {
		createArgs = append(createArgs, "--network", "dokploy-network")
	}

	// ===== 环境变量 =====
	for _, env := range envVars {
		createArgs = append(createArgs, "--env", env)
	}

	// ===== 挂载（volume/bind/file 三种类型，与 TS 版对齐） =====
	applicationsPath := "/etc/dokploy/applications"
	for _, m := range mounts {
		var mountStr string
		switch m.Type {
		case schema.MountTypeVolume:
			if m.VolumeName != nil && *m.VolumeName != "" {
				mountStr = fmt.Sprintf("type=volume,source=%s,target=%s", *m.VolumeName, m.MountPath)
			}
		case schema.MountTypeBind:
			if m.HostPath != nil && *m.HostPath != "" {
				mountStr = fmt.Sprintf("type=bind,source=%s,target=%s", *m.HostPath, m.MountPath)
			}
		case schema.MountTypeFile:
			if m.FilePath != nil {
				sourcePath := fmt.Sprintf("%s/%s/files/%s", applicationsPath, appName, *m.FilePath)
				mountStr = fmt.Sprintf("type=bind,source=%s,target=%s", sourcePath, m.MountPath)
			}
		}
		if mountStr != "" {
			createArgs = append(createArgs, "--mount", mountStr)
		}
	}

	// ===== 资源限制与预留 =====
	if memoryLimit != nil && *memoryLimit != "" {
		createArgs = append(createArgs, "--limit-memory", *memoryLimit)
	}
	if cpuLimit != nil && *cpuLimit != "" {
		createArgs = append(createArgs, "--limit-cpu", *cpuLimit)
	}
	if memoryReservation != nil && *memoryReservation != "" {
		createArgs = append(createArgs, "--reserve-memory", *memoryReservation)
	}
	if cpuReservation != nil && *cpuReservation != "" {
		createArgs = append(createArgs, "--reserve-cpu", *cpuReservation)
	}

	// ===== Swarm 配置 CLI flags =====
	if swarmConfig != nil {
		// Mode
		if modeData := swarmConfig.ModeSwarm.Data; modeData != nil {
			var sm schema.ServiceModeSwarm
			if remarshal(modeData, &sm) == nil {
				if sm.Global != nil {
					createArgs = append(createArgs, "--mode", "global")
				} else if sm.Replicated != nil && sm.Replicated.Replicas != nil {
					createArgs = append(createArgs, "--replicas", fmt.Sprintf("%d", *sm.Replicated.Replicas))
				} else {
					createArgs = append(createArgs, "--replicas", fmt.Sprintf("%d", swarmConfig.Replicas))
				}
			}
		} else {
			createArgs = append(createArgs, "--replicas", fmt.Sprintf("%d", swarmConfig.Replicas))
		}

		// HealthCheck
		if hcData := swarmConfig.HealthCheckSwarm.Data; hcData != nil {
			var hc schema.HealthCheckSwarm
			if remarshal(hcData, &hc) == nil && len(hc.Test) > 0 {
				if hc.Test[0] == "NONE" {
					createArgs = append(createArgs, "--no-healthcheck")
				} else {
					var healthCmd string
					if hc.Test[0] == "CMD-SHELL" && len(hc.Test) > 1 {
						healthCmd = hc.Test[1]
					} else if hc.Test[0] == "CMD" && len(hc.Test) > 1 {
						healthCmd = strings.Join(hc.Test[1:], " ")
					} else {
						healthCmd = strings.Join(hc.Test, " ")
					}
					createArgs = append(createArgs, "--health-cmd", healthCmd)
				}
				if hc.Interval != nil {
					createArgs = append(createArgs, "--health-interval", nsToDuration(*hc.Interval))
				}
				if hc.Timeout != nil {
					createArgs = append(createArgs, "--health-timeout", nsToDuration(*hc.Timeout))
				}
				if hc.StartPeriod != nil {
					createArgs = append(createArgs, "--health-start-period", nsToDuration(*hc.StartPeriod))
				}
				if hc.Retries != nil {
					createArgs = append(createArgs, "--health-retries", fmt.Sprintf("%d", *hc.Retries))
				}
			}
		}

		// RestartPolicy
		if rpData := swarmConfig.RestartPolicySwarm.Data; rpData != nil {
			var rp schema.RestartPolicySwarm
			if remarshal(rpData, &rp) == nil {
				if rp.Condition != nil && *rp.Condition != "" {
					createArgs = append(createArgs, "--restart-condition", *rp.Condition)
				}
				if rp.Delay != nil {
					createArgs = append(createArgs, "--restart-delay", nsToDuration(*rp.Delay))
				}
				if rp.MaxAttempts != nil {
					createArgs = append(createArgs, "--restart-max-attempts", fmt.Sprintf("%d", *rp.MaxAttempts))
				}
				if rp.Window != nil {
					createArgs = append(createArgs, "--restart-window", nsToDuration(*rp.Window))
				}
			}
		}

		// Placement
		if psData := swarmConfig.PlacementSwarm.Data; psData != nil {
			var ps schema.PlacementSwarm
			if remarshal(psData, &ps) == nil {
				for _, c := range ps.Constraints {
					createArgs = append(createArgs, "--constraint", c)
				}
				for _, pref := range ps.Preferences {
					if pref.Spread.SpreadDescriptor != "" {
						createArgs = append(createArgs, "--placement-pref", fmt.Sprintf("spread=%s", pref.Spread.SpreadDescriptor))
					}
				}
			}
		} else if len(mounts) > 0 {
			createArgs = append(createArgs, "--constraint", "node.role==manager")
		}

		// UpdateConfig
		if ucData := swarmConfig.UpdateConfigSwarm.Data; ucData != nil {
			var uc schema.UpdateConfigSwarm
			if remarshal(ucData, &uc) == nil {
				createArgs = append(createArgs, "--update-parallelism", fmt.Sprintf("%d", uc.Parallelism))
				if uc.Delay != nil {
					createArgs = append(createArgs, "--update-delay", nsToDuration(*uc.Delay))
				}
				if uc.FailureAction != nil && *uc.FailureAction != "" {
					createArgs = append(createArgs, "--update-failure-action", *uc.FailureAction)
				}
				if uc.Monitor != nil {
					createArgs = append(createArgs, "--update-monitor", nsToDuration(*uc.Monitor))
				}
				if uc.MaxFailureRatio != nil {
					createArgs = append(createArgs, "--update-max-failure-ratio", fmt.Sprintf("%g", *uc.MaxFailureRatio))
				}
				if uc.Order != "" {
					createArgs = append(createArgs, "--update-order", uc.Order)
				}
			}
		} else {
			createArgs = append(createArgs, "--update-parallelism", "1", "--update-order", "start-first")
		}

		// RollbackConfig
		if rcData := swarmConfig.RollbackConfigSwarm.Data; rcData != nil {
			var rc schema.UpdateConfigSwarm
			if remarshal(rcData, &rc) == nil {
				createArgs = append(createArgs, "--rollback-parallelism", fmt.Sprintf("%d", rc.Parallelism))
				if rc.Delay != nil {
					createArgs = append(createArgs, "--rollback-delay", nsToDuration(*rc.Delay))
				}
				if rc.FailureAction != nil && *rc.FailureAction != "" {
					createArgs = append(createArgs, "--rollback-failure-action", *rc.FailureAction)
				}
				if rc.Monitor != nil {
					createArgs = append(createArgs, "--rollback-monitor", nsToDuration(*rc.Monitor))
				}
				if rc.MaxFailureRatio != nil {
					createArgs = append(createArgs, "--rollback-max-failure-ratio", fmt.Sprintf("%g", *rc.MaxFailureRatio))
				}
				if rc.Order != "" {
					createArgs = append(createArgs, "--rollback-order", rc.Order)
				}
			}
		}

		// Labels
		if lblData := swarmConfig.LabelsSwarm.Data; lblData != nil {
			var labels schema.LabelsSwarm
			if remarshal(lblData, &labels) == nil {
				for k, v := range labels {
					createArgs = append(createArgs, "--container-label", fmt.Sprintf("%s=%s", k, v))
				}
			}
		}

		// StopGracePeriod
		if swarmConfig.StopGracePeriodSwarm != nil && *swarmConfig.StopGracePeriodSwarm > 0 {
			createArgs = append(createArgs, "--stop-grace-period", nsToDuration(*swarmConfig.StopGracePeriodSwarm))
		}

		// Ulimits
		if ulData := swarmConfig.UlimitsSwarm.Data; ulData != nil {
			var uls schema.UlimitsSwarm
			if remarshal(ulData, &uls) == nil {
				for _, ul := range uls {
					createArgs = append(createArgs, "--ulimit", fmt.Sprintf("%s=%d:%d", ul.Name, ul.Soft, ul.Hard))
				}
			}
		}

		// EndpointSpec
		if esData := swarmConfig.EndpointSpecSwarm.Data; esData != nil {
			var es schema.EndpointSpecSwarm
			if remarshal(esData, &es) == nil {
				if es.Mode != nil && *es.Mode != "" {
					createArgs = append(createArgs, "--endpoint-mode", *es.Mode)
				}
				for _, port := range es.Ports {
					publishMode := "host"
					if port.PublishMode != nil && *port.PublishMode != "" {
						publishMode = *port.PublishMode
					}
					protocol := "tcp"
					if port.Protocol != nil && *port.Protocol != "" {
						protocol = *port.Protocol
					}
					targetPort := 0
					if port.TargetPort != nil {
						targetPort = *port.TargetPort
					}
					publishedPort := 0
					if port.PublishedPort != nil {
						publishedPort = *port.PublishedPort
					}
					createArgs = append(createArgs, "--publish",
						fmt.Sprintf("mode=%s,target=%d,published=%d,protocol=%s", publishMode, targetPort, publishedPort, protocol))
				}
			}
		} else {
			// 默认端口映射
			if externalPort != nil && *externalPort > 0 {
				portStr := fmt.Sprintf("published=%d,target=%d,protocol=tcp,mode=host", *externalPort, defaultPort)
				createArgs = append(createArgs, "--publish", portStr)
			}
		}
	} else {
		// 无 SwarmConfig 时的默认配置
		createArgs = append(createArgs, "--replicas", "1",
			"--update-parallelism", "1", "--update-order", "start-first")
		if len(mounts) > 0 {
			createArgs = append(createArgs, "--constraint", "node.role==manager")
		}
		if externalPort != nil && *externalPort > 0 {
			portStr := fmt.Sprintf("published=%d,target=%d,protocol=tcp,mode=host", *externalPort, defaultPort)
			createArgs = append(createArgs, "--publish", portStr)
		}
	}

	// 镜像 (create 时放在最后面)
	createArgs = append(createArgs, dockerImage)

	// Command override
	if command != nil && *command != "" {
		createArgs = append(createArgs, strings.Fields(*command)...)
	}
	// Args（与 TS 版一致：作为 CMD 参数附加在镜像后面）
	if len(args) > 0 {
		createArgs = append(createArgs, args...)
	}

	// rm + create 模式：先删除旧服务（忽略错误），再创建新服务
	emit("Deploying service...\n")
	createCmd := argsToShellCommand(createArgs)
	fullCmd := fmt.Sprintf("docker service rm %s 2>/dev/null; sleep 2; %s", appName, createCmd)
	_, err := process.ExecAsyncRemote(conn, fullCmd, func(line string) { emit(line + "\n") })
	if err != nil {
		emit(fmt.Sprintf("Error: %v\n", err))
		return err
	}
	emit("Deployment completed successfully!\n")
	return nil
}

// runLocalCommand 本地执行命令并流式输出（用 exec.Command args 数组，避免 shell 解析问题）
func (s *DatabaseService) runLocalCommand(args []string, onLine func(string)) error {
	cmd := exec.Command(args[0], args[1:]...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return err
	}

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		if onLine != nil {
			onLine(scanner.Text())
		}
	}

	return cmd.Wait()
}

// argsToShellCommand 将参数数组转换为 shell 安全的命令字符串（用于 SSH 远程执行）
func argsToShellCommand(args []string) string {
	quoted := make([]string, len(args))
	for i, arg := range args {
		if needsQuoting(arg) {
			quoted[i] = "'" + strings.ReplaceAll(arg, "'", "'\\''") + "'"
		} else {
			quoted[i] = arg
		}
	}
	return strings.Join(quoted, " ")
}

// needsQuoting 判断参数是否需要 shell 引用
func needsQuoting(s string) bool {
	for _, c := range s {
		if c == ' ' || c == '"' || c == '\'' || c == '\\' || c == '$' || c == '!' || c == '`' || c == '(' || c == ')' || c == '{' || c == '}' || c == '[' || c == ']' || c == '|' || c == '&' || c == ';' || c == '<' || c == '>' || c == '*' || c == '?' || c == '#' || c == '~' {
			return true
		}
	}
	return false
}

// splitEnvLines 将环境变量字符串按行分割
func splitEnvLines(env string) []string {
	return strings.Split(env, "\n")
}

// parseMemoryString 将 Docker 内存限制字符串转为字节数（如 "512m" → 536870912）
func parseMemoryString(s string) int64 {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return 0
	}
	multiplier := int64(1)
	if strings.HasSuffix(s, "g") {
		multiplier = 1024 * 1024 * 1024
		s = s[:len(s)-1]
	} else if strings.HasSuffix(s, "m") {
		multiplier = 1024 * 1024
		s = s[:len(s)-1]
	} else if strings.HasSuffix(s, "k") {
		multiplier = 1024
		s = s[:len(s)-1]
	}
	n, _ := strconv.ParseFloat(s, 64)
	return int64(n * float64(multiplier))
}

// parseCPUString 将 Docker CPU 限制字符串转为纳核数（如 "0.5" → 500000000）
func parseCPUString(s string) int64 {
	n, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return int64(n * 1e9)
}

// sshConnFromServer 从 Server 模型构建 SSH 连接信息
func sshConnFromServer(server *schema.Server) process.SSHConnection {
	return process.SSHConnection{
		Host:       server.IPAddress,
		Port:       server.Port,
		Username:   server.Username,
		PrivateKey: server.SSHKey.PrivateKey,
	}
}

func (s *DatabaseService) removeServiceByName(appName string, serverID *string) {
	if serverID != nil {
		s.execRemoteByServerID(*serverID, fmt.Sprintf("docker service rm %s", appName))
	} else {
		s.docker.RemoveService(context.Background(), appName)
	}
}

func (s *DatabaseService) execRemoteByServerID(serverID, cmd string) {
	var server schema.Server
	if err := s.db.Preload("SSHKey").First(&server, "\"serverId\" = ?", serverID).Error; err != nil {
		return
	}
	if server.SSHKey == nil {
		return
	}
	conn := sshConnFromServer(&server)
	process.ExecAsyncRemote(conn, cmd, nil)
}

func (s *DatabaseService) updatePostgresStatus(id string, status schema.ApplicationStatus) {
	s.db.Model(&schema.Postgres{}).Where("\"postgresId\" = ?", id).Update("applicationStatus", status)
}
func (s *DatabaseService) updateMySQLStatus(id string, status schema.ApplicationStatus) {
	s.db.Model(&schema.MySQL{}).Where("\"mysqlId\" = ?", id).Update("applicationStatus", status)
}
func (s *DatabaseService) updateMariaDBStatus(id string, status schema.ApplicationStatus) {
	s.db.Model(&schema.MariaDB{}).Where("\"mariadbId\" = ?", id).Update("applicationStatus", status)
}
func (s *DatabaseService) updateMongoStatus(id string, status schema.ApplicationStatus) {
	s.db.Model(&schema.Mongo{}).Where("\"mongoId\" = ?", id).Update("applicationStatus", status)
}
func (s *DatabaseService) updateRedisStatus(id string, status schema.ApplicationStatus) {
	s.db.Model(&schema.Redis{}).Where("\"redisId\" = ?", id).Update("applicationStatus", status)
}
