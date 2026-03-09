// Input: gorm, go-nanoid
// Output: Destination struct (S3 备份目的地), Backup struct (数据库备份记录), VolumeBackup struct (卷备份记录)
// Role: 备份相关数据表模型，包含 S3 连接配置、Cron 表达式、备份状态追踪，关联 Organization 和各数据库类型
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package schema

import (
	"time"

	gonanoid "github.com/matoous/go-nanoid/v2"
	"gorm.io/gorm"
)

// Destination represents the destination table (S3 backup destinations).
type Destination struct {
	DestinationID  string  `gorm:"column:destinationId;primaryKey;type:text" json:"destinationId"`
	Name           string  `gorm:"column:name;type:text;not null" json:"name"`
	Provider       *string `gorm:"column:provider;type:text" json:"provider"`
	AccessKey      string  `gorm:"column:accessKey;type:text;not null" json:"accessKey"`
	SecretAccessKey string `gorm:"column:secretAccessKey;type:text;not null" json:"secretAccessKey"`
	Bucket         string  `gorm:"column:bucket;type:text;not null" json:"bucket"`
	Region         string  `gorm:"column:region;type:text;not null" json:"region"`
	Endpoint       string  `gorm:"column:endpoint;type:text;not null" json:"endpoint"`
	CreatedAt      string  `gorm:"column:createdAt;type:text;not null" json:"createdAt"`
	OrganizationID string  `gorm:"column:organizationId;type:text;not null" json:"organizationId"`

	Organization *Organization `gorm:"foreignKey:OrganizationID;references:ID" json:"organization,omitempty"`
	Backups      []Backup      `gorm:"foreignKey:DestinationID;references:DestinationID" json:"backups"`
}

func (Destination) TableName() string { return "destination" }

func (d *Destination) BeforeCreate(tx *gorm.DB) error {
	if d.DestinationID == "" {
		d.DestinationID, _ = gonanoid.New()
	}
	if d.CreatedAt == "" {
		d.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	return nil
}

// Backup represents the backup table.
type Backup struct {
	BackupID      string       `gorm:"column:backupId;primaryKey;type:text" json:"backupId"`
	Schedule      string       `gorm:"column:schedule;type:text;not null" json:"schedule"`
	Enabled       *bool        `gorm:"column:enabled" json:"enabled"`
	Prefix        string       `gorm:"column:prefix;type:text;not null" json:"prefix"`
	DatabaseType  DatabaseType `gorm:"column:database;type:text;not null" json:"database"`
	CreatedAt     string       `gorm:"column:createdAt;type:text;not null" json:"createdAt"`
	DestinationID string       `gorm:"column:destinationId;type:text;not null" json:"destinationId"`
	PostgresID    *string      `gorm:"column:postgresId;type:text" json:"postgresId"`
	MySQLID       *string      `gorm:"column:mysqlId;type:text" json:"mysqlId"`
	MariaDBID     *string      `gorm:"column:mariadbId;type:text" json:"mariadbId"`
	MongoID       *string      `gorm:"column:mongoId;type:text" json:"mongoId"`
	RedisID       *string      `gorm:"column:redisId;type:text" json:"redisId"`

	Destination *Destination `gorm:"foreignKey:DestinationID;references:DestinationID" json:"destination,omitempty"`
	Postgres    *Postgres    `gorm:"foreignKey:PostgresID;references:PostgresID" json:"postgres,omitempty"`
	MySQL       *MySQL       `gorm:"foreignKey:MySQLID;references:MySQLID" json:"mysql,omitempty"`
	MariaDB     *MariaDB     `gorm:"foreignKey:MariaDBID;references:MariaDBID" json:"mariadb,omitempty"`
	Mongo       *Mongo       `gorm:"foreignKey:MongoID;references:MongoID" json:"mongo,omitempty"`
	Redis       *Redis       `gorm:"foreignKey:RedisID;references:RedisID" json:"redis,omitempty"`
	Deployments []Deployment `gorm:"foreignKey:BackupID;references:BackupID" json:"deployments"`
}

func (Backup) TableName() string { return "backup" }

func (b *Backup) BeforeCreate(tx *gorm.DB) error {
	if b.BackupID == "" {
		b.BackupID, _ = gonanoid.New()
	}
	if b.CreatedAt == "" {
		b.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	return nil
}

// VolumeBackup represents the volume_backup table.
type VolumeBackup struct {
	VolumeBackupID string  `gorm:"column:volumeBackupId;primaryKey;type:text" json:"volumeBackupId"`
	Name           string  `gorm:"column:name;type:text;not null" json:"name"`
	VolumeName     string  `gorm:"column:volumeName;type:text;not null" json:"volumeName"`
	Prefix         string  `gorm:"column:prefix;type:text;not null" json:"prefix"`
	ServiceType    string  `gorm:"column:serviceType;type:text;not null;default:'application'" json:"serviceType"`
	AppName        string  `gorm:"column:appName;type:text;not null" json:"appName"`
	ServiceName    *string `gorm:"column:serviceName;type:text" json:"serviceName"`
	TurnOff        bool    `gorm:"column:turnOff;not null;default:false" json:"turnOff"`
	CronExpression string  `gorm:"column:cronExpression;type:text;not null" json:"cronExpression"`
	KeepLatestCount *int   `gorm:"column:keepLatestCount" json:"keepLatestCount"`
	Enabled        *bool   `gorm:"column:enabled" json:"enabled"`
	ApplicationID  *string `gorm:"column:applicationId;type:text" json:"applicationId"`
	PostgresID     *string `gorm:"column:postgresId;type:text" json:"postgresId"`
	MariaDBID      *string `gorm:"column:mariadbId;type:text" json:"mariadbId"`
	MongoID        *string `gorm:"column:mongoId;type:text" json:"mongoId"`
	MySQLID        *string `gorm:"column:mysqlId;type:text" json:"mysqlId"`
	RedisID        *string `gorm:"column:redisId;type:text" json:"redisId"`
	ComposeID      *string `gorm:"column:composeId;type:text" json:"composeId"`
	CreatedAt      string  `gorm:"column:createdAt;type:text;not null" json:"createdAt"`
	DestinationID  string  `gorm:"column:destinationId;type:text;not null" json:"destinationId"`

	Application *Application `gorm:"foreignKey:ApplicationID;references:ApplicationID" json:"application,omitempty"`
	Postgres    *Postgres    `gorm:"foreignKey:PostgresID;references:PostgresID" json:"postgres,omitempty"`
	MariaDB     *MariaDB     `gorm:"foreignKey:MariaDBID;references:MariaDBID" json:"mariadb,omitempty"`
	Mongo       *Mongo       `gorm:"foreignKey:MongoID;references:MongoID" json:"mongo,omitempty"`
	MySQL       *MySQL       `gorm:"foreignKey:MySQLID;references:MySQLID" json:"mysql,omitempty"`
	Redis       *Redis       `gorm:"foreignKey:RedisID;references:RedisID" json:"redis,omitempty"`
	Compose     *Compose     `gorm:"foreignKey:ComposeID;references:ComposeID" json:"compose,omitempty"`
	Destination *Destination `gorm:"foreignKey:DestinationID;references:DestinationID" json:"destination,omitempty"`
	Deployments []Deployment `gorm:"foreignKey:VolumeBackupID;references:VolumeBackupID" json:"deployments"`
}

func (VolumeBackup) TableName() string { return "volume_backup" }

func (v *VolumeBackup) BeforeCreate(tx *gorm.DB) error {
	if v.VolumeBackupID == "" {
		v.VolumeBackupID, _ = gonanoid.New()
	}
	if v.CreatedAt == "" {
		v.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	return nil
}
