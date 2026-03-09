// Input: gorm, go-nanoid, pq
// Output: Postgres/MySQL/MariaDB/Mongo/Redis struct + SwarmConfig 嵌入结构体
// Role: 5 种数据库服务的数据表模型，共享 SwarmConfig (Swarm 编排字段)，关联 Server/Environment/Backup/Deployment
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package schema

import (
	"time"

	"github.com/lib/pq"
	gonanoid "github.com/matoous/go-nanoid/v2"
	"gorm.io/gorm"
)

// ========== Shared Swarm fields (embedded struct) ==========

// SwarmConfig contains Docker Swarm orchestration fields shared by all database services.
type SwarmConfig struct {
	HealthCheckSwarm     JSONField[interface{}]   `gorm:"column:healthCheckSwarm;type:jsonb" json:"healthCheckSwarm"`
	RestartPolicySwarm   JSONField[interface{}]   `gorm:"column:restartPolicySwarm;type:jsonb" json:"restartPolicySwarm"`
	PlacementSwarm       JSONField[interface{}]   `gorm:"column:placementSwarm;type:jsonb" json:"placementSwarm"`
	UpdateConfigSwarm    JSONField[interface{}]   `gorm:"column:updateConfigSwarm;type:jsonb" json:"updateConfigSwarm"`
	RollbackConfigSwarm  JSONField[interface{}]   `gorm:"column:rollbackConfigSwarm;type:jsonb" json:"rollbackConfigSwarm"`
	ModeSwarm            JSONField[interface{}]   `gorm:"column:modeSwarm;type:jsonb" json:"modeSwarm"`
	LabelsSwarm          JSONField[interface{}]   `gorm:"column:labelsSwarm;type:jsonb" json:"labelsSwarm"`
	NetworkSwarm         JSONField[[]interface{}] `gorm:"column:networkSwarm;type:jsonb" json:"networkSwarm"`
	StopGracePeriodSwarm *int64                   `gorm:"column:stopGracePeriodSwarm" json:"stopGracePeriodSwarm"`
	EndpointSpecSwarm    JSONField[interface{}]   `gorm:"column:endpointSpecSwarm;type:jsonb" json:"endpointSpecSwarm"`
	UlimitsSwarm         JSONField[interface{}]   `gorm:"column:ulimitsSwarm;type:jsonb" json:"ulimitsSwarm"`
	Replicas             int                      `gorm:"column:replicas;not null;default:1" json:"replicas"`
}

// ========== Postgres ==========

type Postgres struct {
	PostgresID        string            `gorm:"column:postgresId;primaryKey;type:text" json:"postgresId"`
	Name              string            `gorm:"column:name;type:text;not null" json:"name"`
	AppName           string            `gorm:"column:appName;type:text;not null;uniqueIndex:postgres_appName_unique" json:"appName"`
	Description       *string           `gorm:"column:description;type:text" json:"description"`
	DatabaseName      string            `gorm:"column:databaseName;type:text;not null" json:"databaseName"`
	DatabaseUser      string            `gorm:"column:databaseUser;type:text;not null" json:"databaseUser"`
	DatabasePassword  string            `gorm:"column:databasePassword;type:text;not null" json:"databasePassword"`
	DockerImage       string            `gorm:"column:dockerImage;type:text;not null" json:"dockerImage"`
	Command           *string           `gorm:"column:command;type:text" json:"command"`
	Args              pq.StringArray    `gorm:"column:args;type:text[]" json:"args"`
	Env               *string           `gorm:"column:env;type:text" json:"env"`
	MemoryReservation *string           `gorm:"column:memoryReservation;type:text" json:"memoryReservation"`
	MemoryLimit       *string           `gorm:"column:memoryLimit;type:text" json:"memoryLimit"`
	CPUReservation    *string           `gorm:"column:cpuReservation;type:text" json:"cpuReservation"`
	CPULimit          *string           `gorm:"column:cpuLimit;type:text" json:"cpuLimit"`
	ExternalPort      *int              `gorm:"column:externalPort" json:"externalPort"`
	ApplicationStatus ApplicationStatus `gorm:"column:applicationStatus;type:text;not null;default:'idle'" json:"applicationStatus"`
	CreatedAt         string            `gorm:"column:createdAt;type:text;not null" json:"createdAt"`
	EnvironmentID     string            `gorm:"column:environmentId;type:text;not null" json:"environmentId"`
	ServerID          *string           `gorm:"column:serverId;type:text" json:"serverId"`

	SwarmConfig

	// Relations
	Environment *Environment `gorm:"foreignKey:EnvironmentID;references:EnvironmentID" json:"environment,omitempty"`
	Server      *Server      `gorm:"foreignKey:ServerID;references:ServerID" json:"server,omitempty"`
	Mounts      []Mount      `gorm:"foreignKey:PostgresID;references:PostgresID" json:"mounts"`
	Backups     []Backup     `gorm:"foreignKey:PostgresID;references:PostgresID" json:"backups"`
}

func (Postgres) TableName() string              { return "postgres" }
func (p *Postgres) GetEnvironmentID() string     { return p.EnvironmentID }

func (p *Postgres) BeforeCreate(tx *gorm.DB) error {
	if p.PostgresID == "" {
		p.PostgresID, _ = gonanoid.New()
	}
	if p.AppName == "" {
		p.AppName = GenerateAppName("postgres")
	}
	if p.CreatedAt == "" {
		p.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	return nil
}

// ========== MySQL ==========

type MySQL struct {
	MySQLID              string            `gorm:"column:mysqlId;primaryKey;type:text" json:"mysqlId"`
	Name                 string            `gorm:"column:name;type:text;not null" json:"name"`
	AppName              string            `gorm:"column:appName;type:text;not null;uniqueIndex:mysql_appName_unique" json:"appName"`
	Description          *string           `gorm:"column:description;type:text" json:"description"`
	DatabaseName         string            `gorm:"column:databaseName;type:text;not null" json:"databaseName"`
	DatabaseUser         string            `gorm:"column:databaseUser;type:text;not null" json:"databaseUser"`
	DatabasePassword     string            `gorm:"column:databasePassword;type:text;not null" json:"databasePassword"`
	DatabaseRootPassword string            `gorm:"column:rootPassword;type:text;not null" json:"databaseRootPassword"`
	DockerImage          string            `gorm:"column:dockerImage;type:text;not null" json:"dockerImage"`
	Command              *string           `gorm:"column:command;type:text" json:"command"`
	Args                 pq.StringArray    `gorm:"column:args;type:text[]" json:"args"`
	Env                  *string           `gorm:"column:env;type:text" json:"env"`
	MemoryReservation    *string           `gorm:"column:memoryReservation;type:text" json:"memoryReservation"`
	MemoryLimit          *string           `gorm:"column:memoryLimit;type:text" json:"memoryLimit"`
	CPUReservation       *string           `gorm:"column:cpuReservation;type:text" json:"cpuReservation"`
	CPULimit             *string           `gorm:"column:cpuLimit;type:text" json:"cpuLimit"`
	ExternalPort         *int              `gorm:"column:externalPort" json:"externalPort"`
	ApplicationStatus    ApplicationStatus `gorm:"column:applicationStatus;type:text;not null;default:'idle'" json:"applicationStatus"`
	CreatedAt            string            `gorm:"column:createdAt;type:text;not null" json:"createdAt"`
	EnvironmentID        string            `gorm:"column:environmentId;type:text;not null" json:"environmentId"`
	ServerID             *string           `gorm:"column:serverId;type:text" json:"serverId"`

	SwarmConfig

	Environment *Environment `gorm:"foreignKey:EnvironmentID;references:EnvironmentID" json:"environment,omitempty"`
	Server      *Server      `gorm:"foreignKey:ServerID;references:ServerID" json:"server,omitempty"`
	Mounts      []Mount      `gorm:"foreignKey:MySQLID;references:MySQLID" json:"mounts"`
	Backups     []Backup     `gorm:"foreignKey:MySQLID;references:MySQLID" json:"backups"`
}

func (MySQL) TableName() string            { return "mysql" }
func (m *MySQL) GetEnvironmentID() string  { return m.EnvironmentID }

func (m *MySQL) BeforeCreate(tx *gorm.DB) error {
	if m.MySQLID == "" {
		m.MySQLID, _ = gonanoid.New()
	}
	if m.AppName == "" {
		m.AppName = GenerateAppName("mysql")
	}
	if m.CreatedAt == "" {
		m.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	return nil
}

// ========== MariaDB ==========

type MariaDB struct {
	MariaDBID            string            `gorm:"column:mariadbId;primaryKey;type:text" json:"mariadbId"`
	Name                 string            `gorm:"column:name;type:text;not null" json:"name"`
	AppName              string            `gorm:"column:appName;type:text;not null;uniqueIndex:mariadb_appName_unique" json:"appName"`
	Description          *string           `gorm:"column:description;type:text" json:"description"`
	DatabaseName         string            `gorm:"column:databaseName;type:text;not null" json:"databaseName"`
	DatabaseUser         string            `gorm:"column:databaseUser;type:text;not null" json:"databaseUser"`
	DatabasePassword     string            `gorm:"column:databasePassword;type:text;not null" json:"databasePassword"`
	DatabaseRootPassword string            `gorm:"column:rootPassword;type:text;not null" json:"databaseRootPassword"`
	DockerImage          string            `gorm:"column:dockerImage;type:text;not null" json:"dockerImage"`
	Command              *string           `gorm:"column:command;type:text" json:"command"`
	Args                 pq.StringArray    `gorm:"column:args;type:text[]" json:"args"`
	Env                  *string           `gorm:"column:env;type:text" json:"env"`
	MemoryReservation    *string           `gorm:"column:memoryReservation;type:text" json:"memoryReservation"`
	MemoryLimit          *string           `gorm:"column:memoryLimit;type:text" json:"memoryLimit"`
	CPUReservation       *string           `gorm:"column:cpuReservation;type:text" json:"cpuReservation"`
	CPULimit             *string           `gorm:"column:cpuLimit;type:text" json:"cpuLimit"`
	ExternalPort         *int              `gorm:"column:externalPort" json:"externalPort"`
	ApplicationStatus    ApplicationStatus `gorm:"column:applicationStatus;type:text;not null;default:'idle'" json:"applicationStatus"`
	CreatedAt            string            `gorm:"column:createdAt;type:text;not null" json:"createdAt"`
	EnvironmentID        string            `gorm:"column:environmentId;type:text;not null" json:"environmentId"`
	ServerID             *string           `gorm:"column:serverId;type:text" json:"serverId"`

	SwarmConfig

	Environment *Environment `gorm:"foreignKey:EnvironmentID;references:EnvironmentID" json:"environment,omitempty"`
	Server      *Server      `gorm:"foreignKey:ServerID;references:ServerID" json:"server,omitempty"`
	Mounts      []Mount      `gorm:"foreignKey:MariaDBID;references:MariaDBID" json:"mounts"`
	Backups     []Backup     `gorm:"foreignKey:MariaDBID;references:MariaDBID" json:"backups"`
}

func (MariaDB) TableName() string              { return "mariadb" }
func (m *MariaDB) GetEnvironmentID() string    { return m.EnvironmentID }

func (m *MariaDB) BeforeCreate(tx *gorm.DB) error {
	if m.MariaDBID == "" {
		m.MariaDBID, _ = gonanoid.New()
	}
	if m.AppName == "" {
		m.AppName = GenerateAppName("mariadb")
	}
	if m.CreatedAt == "" {
		m.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	return nil
}

// ========== Mongo ==========

type Mongo struct {
	MongoID           string            `gorm:"column:mongoId;primaryKey;type:text" json:"mongoId"`
	Name              string            `gorm:"column:name;type:text;not null" json:"name"`
	AppName           string            `gorm:"column:appName;type:text;not null;uniqueIndex:mongo_appName_unique" json:"appName"`
	Description       *string           `gorm:"column:description;type:text" json:"description"`
	DatabaseUser      string            `gorm:"column:databaseUser;type:text;not null" json:"databaseUser"`
	DatabasePassword  string            `gorm:"column:databasePassword;type:text;not null" json:"databasePassword"`
	DockerImage       string            `gorm:"column:dockerImage;type:text;not null" json:"dockerImage"`
	Command           *string           `gorm:"column:command;type:text" json:"command"`
	Args              pq.StringArray    `gorm:"column:args;type:text[]" json:"args"`
	Env               *string           `gorm:"column:env;type:text" json:"env"`
	MemoryReservation *string           `gorm:"column:memoryReservation;type:text" json:"memoryReservation"`
	MemoryLimit       *string           `gorm:"column:memoryLimit;type:text" json:"memoryLimit"`
	CPUReservation    *string           `gorm:"column:cpuReservation;type:text" json:"cpuReservation"`
	CPULimit          *string           `gorm:"column:cpuLimit;type:text" json:"cpuLimit"`
	ExternalPort      *int              `gorm:"column:externalPort" json:"externalPort"`
	ApplicationStatus ApplicationStatus `gorm:"column:applicationStatus;type:text;not null;default:'idle'" json:"applicationStatus"`
	CreatedAt         string            `gorm:"column:createdAt;type:text;not null" json:"createdAt"`
	EnvironmentID     string            `gorm:"column:environmentId;type:text;not null" json:"environmentId"`
	ServerID          *string           `gorm:"column:serverId;type:text" json:"serverId"`
	ReplicaSets       bool              `gorm:"column:replicaSets;not null;default:false" json:"replicaSets"`

	SwarmConfig

	Environment *Environment `gorm:"foreignKey:EnvironmentID;references:EnvironmentID" json:"environment,omitempty"`
	Server      *Server      `gorm:"foreignKey:ServerID;references:ServerID" json:"server,omitempty"`
	Mounts      []Mount      `gorm:"foreignKey:MongoID;references:MongoID" json:"mounts"`
	Backups     []Backup     `gorm:"foreignKey:MongoID;references:MongoID" json:"backups"`
}

func (Mongo) TableName() string              { return "mongo" }
func (m *Mongo) GetEnvironmentID() string    { return m.EnvironmentID }

func (m *Mongo) BeforeCreate(tx *gorm.DB) error {
	if m.MongoID == "" {
		m.MongoID, _ = gonanoid.New()
	}
	if m.AppName == "" {
		m.AppName = GenerateAppName("mongo")
	}
	if m.CreatedAt == "" {
		m.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	return nil
}

// ========== Redis ==========

type Redis struct {
	RedisID           string            `gorm:"column:redisId;primaryKey;type:text" json:"redisId"`
	Name              string            `gorm:"column:name;type:text;not null" json:"name"`
	AppName           string            `gorm:"column:appName;type:text;not null;uniqueIndex:redis_appName_unique" json:"appName"`
	Description       *string           `gorm:"column:description;type:text" json:"description"`
	DatabasePassword  string            `gorm:"column:password;type:text;not null" json:"databasePassword"`
	DockerImage       string            `gorm:"column:dockerImage;type:text;not null" json:"dockerImage"`
	Command           *string           `gorm:"column:command;type:text" json:"command"`
	Args              pq.StringArray    `gorm:"column:args;type:text[]" json:"args"`
	Env               *string           `gorm:"column:env;type:text" json:"env"`
	MemoryReservation *string           `gorm:"column:memoryReservation;type:text" json:"memoryReservation"`
	MemoryLimit       *string           `gorm:"column:memoryLimit;type:text" json:"memoryLimit"`
	CPUReservation    *string           `gorm:"column:cpuReservation;type:text" json:"cpuReservation"`
	CPULimit          *string           `gorm:"column:cpuLimit;type:text" json:"cpuLimit"`
	ExternalPort      *int              `gorm:"column:externalPort" json:"externalPort"`
	ApplicationStatus ApplicationStatus `gorm:"column:applicationStatus;type:text;not null;default:'idle'" json:"applicationStatus"`
	CreatedAt         string            `gorm:"column:createdAt;type:text;not null" json:"createdAt"`
	EnvironmentID     string            `gorm:"column:environmentId;type:text;not null" json:"environmentId"`
	ServerID          *string           `gorm:"column:serverId;type:text" json:"serverId"`

	SwarmConfig

	Environment *Environment `gorm:"foreignKey:EnvironmentID;references:EnvironmentID" json:"environment,omitempty"`
	Server      *Server      `gorm:"foreignKey:ServerID;references:ServerID" json:"server,omitempty"`
	Mounts      []Mount      `gorm:"foreignKey:RedisID;references:RedisID" json:"mounts"`
	Backups     []Backup     `gorm:"foreignKey:RedisID;references:RedisID" json:"backups"`
}

func (Redis) TableName() string              { return "redis" }
func (r *Redis) GetEnvironmentID() string    { return r.EnvironmentID }

func (r *Redis) BeforeCreate(tx *gorm.DB) error {
	if r.RedisID == "" {
		r.RedisID, _ = gonanoid.New()
	}
	if r.AppName == "" {
		r.AppName = GenerateAppName("redis")
	}
	if r.CreatedAt == "" {
		r.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	return nil
}
