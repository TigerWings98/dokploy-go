package service

import (
	"context"
	"fmt"
	"time"

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

// DeployPostgres deploys a PostgreSQL service.
func (s *DatabaseService) DeployPostgres(postgresID string) error {
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
		pg.MemoryLimit, pg.CPULimit, pg.ExternalPort, 5432)
	if err != nil {
		s.updatePostgresStatus(postgresID, schema.ApplicationStatusError)
		return err
	}

	s.updatePostgresStatus(postgresID, schema.ApplicationStatusDone)
	return nil
}

// DeployMySQL deploys a MySQL service.
func (s *DatabaseService) DeployMySQL(mysqlID string) error {
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
		my.MemoryLimit, my.CPULimit, my.ExternalPort, 3306)
	if err != nil {
		s.updateMySQLStatus(mysqlID, schema.ApplicationStatusError)
		return err
	}

	s.updateMySQLStatus(mysqlID, schema.ApplicationStatusDone)
	return nil
}

// DeployMariaDB deploys a MariaDB service.
func (s *DatabaseService) DeployMariaDB(mariadbID string) error {
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
		mdb.MemoryLimit, mdb.CPULimit, mdb.ExternalPort, 3306)
	if err != nil {
		s.updateMariaDBStatus(mariadbID, schema.ApplicationStatusError)
		return err
	}

	s.updateMariaDBStatus(mariadbID, schema.ApplicationStatusDone)
	return nil
}

// DeployMongo deploys a MongoDB service.
func (s *DatabaseService) DeployMongo(mongoID string) error {
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
		mongo.MemoryLimit, mongo.CPULimit, mongo.ExternalPort, 27017)
	if err != nil {
		s.updateMongoStatus(mongoID, schema.ApplicationStatusError)
		return err
	}

	s.updateMongoStatus(mongoID, schema.ApplicationStatusDone)
	return nil
}

// DeployRedis deploys a Redis service.
func (s *DatabaseService) DeployRedis(redisID string) error {
	var redis schema.Redis
	if err := s.db.Preload("Mounts").Preload("Server").Preload("Server.SSHKey").
		First(&redis, "\"redisId\" = ?", redisID).Error; err != nil {
		return err
	}

	s.updateRedisStatus(redisID, schema.ApplicationStatusRunning)

	envVars := map[string]string{}
	// Redis password is set via command, not env
	command := redis.Command
	if redis.DatabasePassword != "" {
		pw := redis.DatabasePassword
		if command != nil {
			cmdStr := fmt.Sprintf("%s --requirepass %s", *command, pw)
			command = &cmdStr
		} else {
			cmdStr := fmt.Sprintf("redis-server --requirepass %s", pw)
			command = &cmdStr
		}
	}

	err := s.deployDatabaseService(redis.AppName, redis.DockerImage, redis.ServerID, redis.Server,
		envVars, command, redis.Env, redis.Mounts,
		redis.MemoryLimit, redis.CPULimit, redis.ExternalPort, 6379)
	if err != nil {
		s.updateRedisStatus(redisID, schema.ApplicationStatusError)
		return err
	}

	s.updateRedisStatus(redisID, schema.ApplicationStatusDone)
	return nil
}

// RebuildDatabase removes and re-deploys a database service.
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

	// Remove service
	s.removeServiceByName(appName, serverID)
	time.Sleep(6 * time.Second)

	// Remove volumes
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

	// Re-deploy
	switch dbType {
	case schema.DatabaseTypePostgres:
		return s.DeployPostgres(databaseID)
	case schema.DatabaseTypeMySQL:
		return s.DeployMySQL(databaseID)
	case schema.DatabaseTypeMariaDB:
		return s.DeployMariaDB(databaseID)
	case schema.DatabaseTypeMongo:
		return s.DeployMongo(databaseID)
	case schema.DatabaseTypeRedis:
		return s.DeployRedis(databaseID)
	}
	return nil
}

// deployDatabaseService is the shared logic for deploying any database service.
func (s *DatabaseService) deployDatabaseService(
	appName, dockerImage string,
	serverID *string, server *schema.Server,
	envVars map[string]string,
	command, extraEnv *string,
	mounts []schema.Mount,
	memoryLimit, cpuLimit *string,
	externalPort *int, defaultPort int,
) error {
	// Build docker service create command
	cmd := fmt.Sprintf("docker service create --name %s --network dokploy-network", appName)

	// Environment variables
	for k, v := range envVars {
		cmd += fmt.Sprintf(" --env %s=%s", k, v)
	}

	// Extra env from user
	if extraEnv != nil && *extraEnv != "" {
		for k, v := range parseEnvString(*extraEnv) {
			cmd += fmt.Sprintf(" --env %s=%s", k, v)
		}
	}

	// Mounts
	for _, mount := range mounts {
		switch mount.Type {
		case schema.MountTypeVolume:
			if mount.VolumeName != nil {
				cmd += fmt.Sprintf(" --mount type=volume,source=%s,target=%s", *mount.VolumeName, mount.MountPath)
			}
		case schema.MountTypeBind:
			if mount.HostPath != nil {
				cmd += fmt.Sprintf(" --mount type=bind,source=%s,target=%s", *mount.HostPath, mount.MountPath)
			}
		}
	}

	// Resource limits
	if memoryLimit != nil && *memoryLimit != "" {
		cmd += fmt.Sprintf(" --limit-memory %s", *memoryLimit)
	}
	if cpuLimit != nil && *cpuLimit != "" {
		cmd += fmt.Sprintf(" --limit-cpu %s", *cpuLimit)
	}

	// External port
	if externalPort != nil && *externalPort > 0 {
		cmd += fmt.Sprintf(" --publish %d:%d", *externalPort, defaultPort)
	}

	// Image
	cmd += " " + dockerImage

	// Command override
	if command != nil && *command != "" {
		cmd += " " + *command
	}

	// First try to remove existing service
	s.removeServiceByName(appName, serverID)
	time.Sleep(2 * time.Second)

	// Execute
	if serverID != nil && server != nil && server.SSHKey != nil {
		conn := process.SSHConnection{
			Host:       server.IPAddress,
			Port:       server.Port,
			Username:   server.Username,
			PrivateKey: server.SSHKey.PrivateKey,
		}
		_, err := process.ExecAsyncRemote(conn, cmd, nil)
		return err
	}

	_, err := process.ExecAsync(cmd)
	return err
}

func (s *DatabaseService) removeServiceByName(appName string, serverID *string) {
	cmd := fmt.Sprintf("docker service rm %s", appName)
	if serverID != nil {
		s.execRemoteByServerID(*serverID, cmd)
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
	conn := process.SSHConnection{
		Host:       server.IPAddress,
		Port:       server.Port,
		Username:   server.Username,
		PrivateKey: server.SSHKey.PrivateKey,
	}
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
