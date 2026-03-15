// Input: db, docker, config
// Output: DatabaseService (数据库部署/停止/重建全流程，与 TS 版完全对齐)
// Role: 数据库服务部署编排，内联执行（不走队列），支持 onData 回调实时日志
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package service

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
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
		First(&pg, "\"postgresId\" = ?", postgresID).Error; err != nil {
		return err
	}
	s.updatePostgresStatus(postgresID, schema.ApplicationStatusRunning)

	envVars := map[string]string{
		"POSTGRES_DB":       pg.DatabaseName,
		"POSTGRES_USER":     pg.DatabaseUser,
		"POSTGRES_PASSWORD": pg.DatabasePassword,
	}

	err := s.deployDatabaseService(pg.AppName, pg.DockerImage, pg.ServerID, pg.Server,
		envVars, pg.Command, pg.Env, pg.Mounts,
		pg.MemoryLimit, pg.CPULimit, pg.ExternalPort, 5432, onData)
	if err != nil {
		s.updatePostgresStatus(postgresID, schema.ApplicationStatusError)
		return err
	}

	s.updatePostgresStatus(postgresID, schema.ApplicationStatusDone)
	return nil
}

// DeployMySQL 部署 MySQL 服务
func (s *DatabaseService) DeployMySQL(mysqlID string, onData func(string)) error {
	var my schema.MySQL
	if err := s.db.Preload("Mounts").Preload("Server").Preload("Server.SSHKey").
		First(&my, "\"mysqlId\" = ?", mysqlID).Error; err != nil {
		return err
	}
	s.updateMySQLStatus(mysqlID, schema.ApplicationStatusRunning)

	envVars := map[string]string{
		"MYSQL_DATABASE":      my.DatabaseName,
		"MYSQL_USER":          my.DatabaseUser,
		"MYSQL_PASSWORD":      my.DatabasePassword,
		"MYSQL_ROOT_PASSWORD": my.DatabaseRootPassword,
	}

	err := s.deployDatabaseService(my.AppName, my.DockerImage, my.ServerID, my.Server,
		envVars, my.Command, my.Env, my.Mounts,
		my.MemoryLimit, my.CPULimit, my.ExternalPort, 3306, onData)
	if err != nil {
		s.updateMySQLStatus(mysqlID, schema.ApplicationStatusError)
		return err
	}

	s.updateMySQLStatus(mysqlID, schema.ApplicationStatusDone)
	return nil
}

// DeployMariaDB 部署 MariaDB 服务
func (s *DatabaseService) DeployMariaDB(mariadbID string, onData func(string)) error {
	var mdb schema.MariaDB
	if err := s.db.Preload("Mounts").Preload("Server").Preload("Server.SSHKey").
		First(&mdb, "\"mariadbId\" = ?", mariadbID).Error; err != nil {
		return err
	}
	s.updateMariaDBStatus(mariadbID, schema.ApplicationStatusRunning)

	envVars := map[string]string{
		"MARIADB_DATABASE":      mdb.DatabaseName,
		"MARIADB_USER":          mdb.DatabaseUser,
		"MARIADB_PASSWORD":      mdb.DatabasePassword,
		"MARIADB_ROOT_PASSWORD": mdb.DatabaseRootPassword,
	}

	err := s.deployDatabaseService(mdb.AppName, mdb.DockerImage, mdb.ServerID, mdb.Server,
		envVars, mdb.Command, mdb.Env, mdb.Mounts,
		mdb.MemoryLimit, mdb.CPULimit, mdb.ExternalPort, 3306, onData)
	if err != nil {
		s.updateMariaDBStatus(mariadbID, schema.ApplicationStatusError)
		return err
	}

	s.updateMariaDBStatus(mariadbID, schema.ApplicationStatusDone)
	return nil
}

// DeployMongo 部署 MongoDB 服务
func (s *DatabaseService) DeployMongo(mongoID string, onData func(string)) error {
	var mongo schema.Mongo
	if err := s.db.Preload("Mounts").Preload("Server").Preload("Server.SSHKey").
		First(&mongo, "\"mongoId\" = ?", mongoID).Error; err != nil {
		return err
	}
	s.updateMongoStatus(mongoID, schema.ApplicationStatusRunning)

	envVars := map[string]string{
		"MONGO_INITDB_ROOT_USERNAME": mongo.DatabaseUser,
		"MONGO_INITDB_ROOT_PASSWORD": mongo.DatabasePassword,
	}

	err := s.deployDatabaseService(mongo.AppName, mongo.DockerImage, mongo.ServerID, mongo.Server,
		envVars, mongo.Command, mongo.Env, mongo.Mounts,
		mongo.MemoryLimit, mongo.CPULimit, mongo.ExternalPort, 27017, onData)
	if err != nil {
		s.updateMongoStatus(mongoID, schema.ApplicationStatusError)
		return err
	}

	s.updateMongoStatus(mongoID, schema.ApplicationStatusDone)
	return nil
}

// DeployRedis 部署 Redis 服务（与 TS 版对齐：密码通过 Command 传入，不用环境变量）
func (s *DatabaseService) DeployRedis(redisID string, onData func(string)) error {
	var redis schema.Redis
	if err := s.db.Preload("Mounts").Preload("Server").Preload("Server.SSHKey").
		First(&redis, "\"redisId\" = ?", redisID).Error; err != nil {
		return err
	}
	s.updateRedisStatus(redisID, schema.ApplicationStatusRunning)

	envVars := map[string]string{}
	// 与 TS 版一致：Redis 密码通过 command 传入
	command := redis.Command
	if redis.DatabasePassword != "" {
		pw := redis.DatabasePassword
		if command != nil && *command != "" {
			cmdStr := fmt.Sprintf("%s --requirepass %s", *command, pw)
			command = &cmdStr
		} else {
			cmdStr := fmt.Sprintf("redis-server --requirepass %s", pw)
			command = &cmdStr
		}
	}

	err := s.deployDatabaseService(redis.AppName, redis.DockerImage, redis.ServerID, redis.Server,
		envVars, command, redis.Env, redis.Mounts,
		redis.MemoryLimit, redis.CPULimit, redis.ExternalPort, 6379, onData)
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
func (s *DatabaseService) deployDatabaseService(
	appName, dockerImage string,
	serverID *string, server *schema.Server,
	envVars map[string]string,
	command, extraEnv *string,
	mounts []schema.Mount,
	memoryLimit, cpuLimit *string,
	externalPort *int, defaultPort int,
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

	// Step 2: 构建 ServiceSpec（与 TS 版 createService settings 对齐）
	spec := s.buildServiceSpec(appName, dockerImage, envVars, command, extraEnv, mounts,
		memoryLimit, cpuLimit, externalPort, defaultPort)

	// Step 3: 部署（update-first / create-fallback）
	if serverID != nil && server != nil && server.SSHKey != nil {
		// 远程部署：通过 SSH CLI
		return s.deployRemoteService(server, appName, dockerImage, envVars, command, extraEnv,
			mounts, memoryLimit, cpuLimit, externalPort, defaultPort, emit)
	}

	// 本地部署：使用 Docker SDK（与 TS 版完全一致的 update-first 模式）
	return s.deployLocalService(appName, spec, emit)
}

// buildServiceSpec 构建 Docker Swarm ServiceSpec（与 TS 版 createService settings 对齐）
func (s *DatabaseService) buildServiceSpec(
	appName, dockerImage string,
	envVars map[string]string,
	command, extraEnv *string,
	mounts []schema.Mount,
	memoryLimit, cpuLimit *string,
	externalPort *int, defaultPort int,
) swarm.ServiceSpec {
	// 环境变量列表
	var envList []string
	for k, v := range envVars {
		envList = append(envList, fmt.Sprintf("%s=%s", k, v))
	}
	if extraEnv != nil && *extraEnv != "" {
		for _, line := range splitEnvLines(*extraEnv) {
			line = strings.TrimSpace(line)
			if line != "" && line[0] != '#' {
				envList = append(envList, line)
			}
		}
	}

	// 挂载列表
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
		}
	}

	// 资源限制
	var resources *swarm.ResourceRequirements
	var limits swarm.Limit
	hasLimits := false
	if memoryLimit != nil && *memoryLimit != "" {
		limits.MemoryBytes = parseMemoryString(*memoryLimit)
		hasLimits = true
	}
	if cpuLimit != nil && *cpuLimit != "" {
		limits.NanoCPUs = parseCPUString(*cpuLimit)
		hasLimits = true
	}
	if hasLimits {
		resources = &swarm.ResourceRequirements{Limits: &limits}
	}

	// 端口映射（与 TS 版一致：Mode=dnsrr，PublishMode=host）
	var ports []swarm.PortConfig
	if externalPort != nil && *externalPort > 0 {
		ports = append(ports, swarm.PortConfig{
			Protocol:      swarm.PortConfigProtocolTCP,
			TargetPort:    uint32(defaultPort),
			PublishedPort: uint32(*externalPort),
			PublishMode:   swarm.PortConfigPublishModeHost,
		})
	}

	// ContainerSpec
	containerSpec := &swarm.ContainerSpec{
		Image:  dockerImage,
		Env:    envList,
		Mounts: mountList,
	}
	// Command override（与 TS 版一致：command → ContainerSpec.Command (ENTRYPOINT)）
	if command != nil && *command != "" {
		containerSpec.Command = strings.Fields(*command)
	}

	replicas := uint64(1)
	return swarm.ServiceSpec{
		Annotations: swarm.Annotations{Name: appName},
		TaskTemplate: swarm.TaskSpec{
			ContainerSpec: containerSpec,
			Networks: []swarm.NetworkAttachmentConfig{
				{Target: "dokploy-network"},
			},
			Resources: resources,
		},
		Mode: swarm.ServiceMode{
			Replicated: &swarm.ReplicatedService{Replicas: &replicas},
		},
		EndpointSpec: &swarm.EndpointSpec{
			Mode:  swarm.ResolutionModeDNSRR,
			Ports: ports,
		},
	}
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

// deployRemoteService 远程部署：通过 SSH CLI（update-first / create-fallback）
func (s *DatabaseService) deployRemoteService(
	server *schema.Server,
	appName, dockerImage string,
	envVars map[string]string,
	command, extraEnv *string,
	mounts []schema.Mount,
	memoryLimit, cpuLimit *string,
	externalPort *int, defaultPort int,
	emit func(string),
) error {
	conn := sshConnFromServer(server)

	// 构建 create 参数
	createArgs := []string{"docker", "service", "create",
		"--name", appName,
		"--network", "dokploy-network",
	}
	// 构建 update 参数
	updateArgs := []string{"docker", "service", "update", "--force",
		"--image", dockerImage,
	}

	// 环境变量
	for k, v := range envVars {
		createArgs = append(createArgs, "--env", fmt.Sprintf("%s=%s", k, v))
		updateArgs = append(updateArgs, "--env-add", fmt.Sprintf("%s=%s", k, v))
	}
	if extraEnv != nil && *extraEnv != "" {
		for _, line := range splitEnvLines(*extraEnv) {
			line = strings.TrimSpace(line)
			if line != "" && line[0] != '#' {
				createArgs = append(createArgs, "--env", line)
				updateArgs = append(updateArgs, "--env-add", line)
			}
		}
	}

	// 挂载
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
		}
		if mountStr != "" {
			createArgs = append(createArgs, "--mount", mountStr)
			updateArgs = append(updateArgs, "--mount-add", mountStr)
		}
	}

	// 资源限制
	if memoryLimit != nil && *memoryLimit != "" {
		createArgs = append(createArgs, "--limit-memory", *memoryLimit)
		updateArgs = append(updateArgs, "--limit-memory", *memoryLimit)
	}
	if cpuLimit != nil && *cpuLimit != "" {
		createArgs = append(createArgs, "--limit-cpu", *cpuLimit)
		updateArgs = append(updateArgs, "--limit-cpu", *cpuLimit)
	}

	// 端口映射
	if externalPort != nil && *externalPort > 0 {
		portStr := fmt.Sprintf("published=%d,target=%d,protocol=tcp,mode=host", *externalPort, defaultPort)
		createArgs = append(createArgs, "--publish", portStr)
		updateArgs = append(updateArgs, "--publish-add", portStr)
	}

	// 镜像 (create 时放在最后面，update 已经用 --image 指定)
	createArgs = append(createArgs, dockerImage)

	// Command override
	if command != nil && *command != "" {
		createArgs = append(createArgs, strings.Fields(*command)...)
		// update 不支持直接改 CMD，需要 rm+create 回退
	}

	// update 的 service name
	updateArgs = append(updateArgs, appName)

	// 先尝试 update
	emit("Deploying service...\n")
	updateCmd := argsToShellCommand(updateArgs)
	_, updateErr := process.ExecAsyncRemote(conn, updateCmd, func(line string) { emit(line + "\n") })
	if updateErr == nil {
		emit("Service updated successfully!\n")
		return nil
	}

	// update 失败 → 尝试 create
	emit("Service not found, creating new...\n")
	createCmd := argsToShellCommand(createArgs)
	_, err := process.ExecAsyncRemote(conn, createCmd, func(line string) { emit(line + "\n") })
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
