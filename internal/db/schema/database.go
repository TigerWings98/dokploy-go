package schema

import (
	"time"

	gonanoid "github.com/matoous/go-nanoid/v2"
	"gorm.io/gorm"
)

// Postgres represents the postgres table.
type Postgres struct {
	PostgresID     string            `gorm:"column:postgresId;primaryKey;type:text" json:"postgresId"`
	Name           string            `gorm:"column:name;type:text;not null" json:"name"`
	AppName        string            `gorm:"column:appName;type:text;not null;uniqueIndex:postgres_appName_unique" json:"appName"`
	Description    *string           `gorm:"column:description;type:text" json:"description"`
	DatabaseName   string            `gorm:"column:databaseName;type:text;not null" json:"databaseName"`
	DatabaseUser   string            `gorm:"column:databaseUser;type:text;not null" json:"databaseUser"`
	DatabasePassword string          `gorm:"column:databasePassword;type:text;not null" json:"databasePassword"`
	DockerImage    string            `gorm:"column:dockerImage;type:text;not null" json:"dockerImage"`
	Command        *string           `gorm:"column:command;type:text" json:"command"`
	Env            *string           `gorm:"column:env;type:text" json:"env"`
	MemoryReservation *string        `gorm:"column:memoryReservation;type:text" json:"memoryReservation"`
	MemoryLimit    *string           `gorm:"column:memoryLimit;type:text" json:"memoryLimit"`
	CPUReservation *string           `gorm:"column:cpuReservation;type:text" json:"cpuReservation"`
	CPULimit       *string           `gorm:"column:cpuLimit;type:text" json:"cpuLimit"`
	ExternalPort   *int              `gorm:"column:externalPort" json:"externalPort"`
	ApplicationStatus ApplicationStatus `gorm:"column:applicationStatus;type:text;not null;default:'idle'" json:"applicationStatus"`
	CreatedAt      string            `gorm:"column:createdAt;type:text;not null" json:"createdAt"`
	EnvironmentID  string            `gorm:"column:environmentId;type:text;not null" json:"environmentId"`
	ServerID       *string           `gorm:"column:serverId;type:text" json:"serverId"`

	// Relations
	Environment *Environment `gorm:"foreignKey:EnvironmentID" json:"environment,omitempty"`
	Server      *Server      `gorm:"foreignKey:ServerID" json:"server,omitempty"`
	Mounts      []Mount      `gorm:"foreignKey:PostgresID" json:"mounts,omitempty"`
	Backups     []Backup     `gorm:"foreignKey:PostgresID" json:"backups,omitempty"`
}

func (Postgres) TableName() string { return "postgres" }

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

// MySQL represents the mysql table.
type MySQL struct {
	MySQLID        string            `gorm:"column:mysqlId;primaryKey;type:text" json:"mysqlId"`
	Name           string            `gorm:"column:name;type:text;not null" json:"name"`
	AppName        string            `gorm:"column:appName;type:text;not null;uniqueIndex:mysql_appName_unique" json:"appName"`
	Description    *string           `gorm:"column:description;type:text" json:"description"`
	DatabaseName   string            `gorm:"column:databaseName;type:text;not null" json:"databaseName"`
	DatabaseUser   string            `gorm:"column:databaseUser;type:text;not null" json:"databaseUser"`
	DatabasePassword string          `gorm:"column:databasePassword;type:text;not null" json:"databasePassword"`
	DatabaseRootPassword string      `gorm:"column:databaseRootPassword;type:text;not null" json:"databaseRootPassword"`
	DockerImage    string            `gorm:"column:dockerImage;type:text;not null" json:"dockerImage"`
	Command        *string           `gorm:"column:command;type:text" json:"command"`
	Env            *string           `gorm:"column:env;type:text" json:"env"`
	MemoryReservation *string        `gorm:"column:memoryReservation;type:text" json:"memoryReservation"`
	MemoryLimit    *string           `gorm:"column:memoryLimit;type:text" json:"memoryLimit"`
	CPUReservation *string           `gorm:"column:cpuReservation;type:text" json:"cpuReservation"`
	CPULimit       *string           `gorm:"column:cpuLimit;type:text" json:"cpuLimit"`
	ExternalPort   *int              `gorm:"column:externalPort" json:"externalPort"`
	ApplicationStatus ApplicationStatus `gorm:"column:applicationStatus;type:text;not null;default:'idle'" json:"applicationStatus"`
	CreatedAt      string            `gorm:"column:createdAt;type:text;not null" json:"createdAt"`
	EnvironmentID  string            `gorm:"column:environmentId;type:text;not null" json:"environmentId"`
	ServerID       *string           `gorm:"column:serverId;type:text" json:"serverId"`

	Environment *Environment `gorm:"foreignKey:EnvironmentID" json:"environment,omitempty"`
	Server      *Server      `gorm:"foreignKey:ServerID" json:"server,omitempty"`
	Mounts      []Mount      `gorm:"foreignKey:MySQLID" json:"mounts,omitempty"`
	Backups     []Backup     `gorm:"foreignKey:MySQLID" json:"backups,omitempty"`
}

func (MySQL) TableName() string { return "mysql" }

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

// MariaDB represents the mariadb table.
type MariaDB struct {
	MariaDBID      string            `gorm:"column:mariadbId;primaryKey;type:text" json:"mariadbId"`
	Name           string            `gorm:"column:name;type:text;not null" json:"name"`
	AppName        string            `gorm:"column:appName;type:text;not null;uniqueIndex:mariadb_appName_unique" json:"appName"`
	Description    *string           `gorm:"column:description;type:text" json:"description"`
	DatabaseName   string            `gorm:"column:databaseName;type:text;not null" json:"databaseName"`
	DatabaseUser   string            `gorm:"column:databaseUser;type:text;not null" json:"databaseUser"`
	DatabasePassword string          `gorm:"column:databasePassword;type:text;not null" json:"databasePassword"`
	DatabaseRootPassword string      `gorm:"column:databaseRootPassword;type:text;not null" json:"databaseRootPassword"`
	DockerImage    string            `gorm:"column:dockerImage;type:text;not null" json:"dockerImage"`
	Command        *string           `gorm:"column:command;type:text" json:"command"`
	Env            *string           `gorm:"column:env;type:text" json:"env"`
	MemoryReservation *string        `gorm:"column:memoryReservation;type:text" json:"memoryReservation"`
	MemoryLimit    *string           `gorm:"column:memoryLimit;type:text" json:"memoryLimit"`
	CPUReservation *string           `gorm:"column:cpuReservation;type:text" json:"cpuReservation"`
	CPULimit       *string           `gorm:"column:cpuLimit;type:text" json:"cpuLimit"`
	ExternalPort   *int              `gorm:"column:externalPort" json:"externalPort"`
	ApplicationStatus ApplicationStatus `gorm:"column:applicationStatus;type:text;not null;default:'idle'" json:"applicationStatus"`
	CreatedAt      string            `gorm:"column:createdAt;type:text;not null" json:"createdAt"`
	EnvironmentID  string            `gorm:"column:environmentId;type:text;not null" json:"environmentId"`
	ServerID       *string           `gorm:"column:serverId;type:text" json:"serverId"`

	Environment *Environment `gorm:"foreignKey:EnvironmentID" json:"environment,omitempty"`
	Server      *Server      `gorm:"foreignKey:ServerID" json:"server,omitempty"`
	Mounts      []Mount      `gorm:"foreignKey:MariaDBID" json:"mounts,omitempty"`
	Backups     []Backup     `gorm:"foreignKey:MariaDBID" json:"backups,omitempty"`
}

func (MariaDB) TableName() string { return "mariadb" }

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

// Mongo represents the mongo table.
type Mongo struct {
	MongoID        string            `gorm:"column:mongoId;primaryKey;type:text" json:"mongoId"`
	Name           string            `gorm:"column:name;type:text;not null" json:"name"`
	AppName        string            `gorm:"column:appName;type:text;not null;uniqueIndex:mongo_appName_unique" json:"appName"`
	Description    *string           `gorm:"column:description;type:text" json:"description"`
	DatabaseUser   string            `gorm:"column:databaseUser;type:text;not null" json:"databaseUser"`
	DatabasePassword string          `gorm:"column:databasePassword;type:text;not null" json:"databasePassword"`
	DockerImage    string            `gorm:"column:dockerImage;type:text;not null" json:"dockerImage"`
	Command        *string           `gorm:"column:command;type:text" json:"command"`
	Env            *string           `gorm:"column:env;type:text" json:"env"`
	MemoryReservation *string        `gorm:"column:memoryReservation;type:text" json:"memoryReservation"`
	MemoryLimit    *string           `gorm:"column:memoryLimit;type:text" json:"memoryLimit"`
	CPUReservation *string           `gorm:"column:cpuReservation;type:text" json:"cpuReservation"`
	CPULimit       *string           `gorm:"column:cpuLimit;type:text" json:"cpuLimit"`
	ExternalPort   *int              `gorm:"column:externalPort" json:"externalPort"`
	ApplicationStatus ApplicationStatus `gorm:"column:applicationStatus;type:text;not null;default:'idle'" json:"applicationStatus"`
	CreatedAt      string            `gorm:"column:createdAt;type:text;not null" json:"createdAt"`
	EnvironmentID  string            `gorm:"column:environmentId;type:text;not null" json:"environmentId"`
	ServerID       *string           `gorm:"column:serverId;type:text" json:"serverId"`

	Environment *Environment `gorm:"foreignKey:EnvironmentID" json:"environment,omitempty"`
	Server      *Server      `gorm:"foreignKey:ServerID" json:"server,omitempty"`
	Mounts      []Mount      `gorm:"foreignKey:MongoID" json:"mounts,omitempty"`
	Backups     []Backup     `gorm:"foreignKey:MongoID" json:"backups,omitempty"`
}

func (Mongo) TableName() string { return "mongo" }

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

// Redis represents the redis table.
type Redis struct {
	RedisID        string            `gorm:"column:redisId;primaryKey;type:text" json:"redisId"`
	Name           string            `gorm:"column:name;type:text;not null" json:"name"`
	AppName        string            `gorm:"column:appName;type:text;not null;uniqueIndex:redis_appName_unique" json:"appName"`
	Description    *string           `gorm:"column:description;type:text" json:"description"`
	DatabasePassword string          `gorm:"column:databasePassword;type:text;not null" json:"databasePassword"`
	DockerImage    string            `gorm:"column:dockerImage;type:text;not null" json:"dockerImage"`
	Command        *string           `gorm:"column:command;type:text" json:"command"`
	Env            *string           `gorm:"column:env;type:text" json:"env"`
	MemoryReservation *string        `gorm:"column:memoryReservation;type:text" json:"memoryReservation"`
	MemoryLimit    *string           `gorm:"column:memoryLimit;type:text" json:"memoryLimit"`
	CPUReservation *string           `gorm:"column:cpuReservation;type:text" json:"cpuReservation"`
	CPULimit       *string           `gorm:"column:cpuLimit;type:text" json:"cpuLimit"`
	ExternalPort   *int              `gorm:"column:externalPort" json:"externalPort"`
	ApplicationStatus ApplicationStatus `gorm:"column:applicationStatus;type:text;not null;default:'idle'" json:"applicationStatus"`
	CreatedAt      string            `gorm:"column:createdAt;type:text;not null" json:"createdAt"`
	EnvironmentID  string            `gorm:"column:environmentId;type:text;not null" json:"environmentId"`
	ServerID       *string           `gorm:"column:serverId;type:text" json:"serverId"`

	Environment *Environment `gorm:"foreignKey:EnvironmentID" json:"environment,omitempty"`
	Server      *Server      `gorm:"foreignKey:ServerID" json:"server,omitempty"`
	Mounts      []Mount      `gorm:"foreignKey:RedisID" json:"mounts,omitempty"`
}

func (Redis) TableName() string { return "redis" }

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
