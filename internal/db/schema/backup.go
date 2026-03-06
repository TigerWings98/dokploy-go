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
	AccessKey      string  `gorm:"column:accessKey;type:text;not null" json:"accessKey"`
	SecretAccessKey string `gorm:"column:secretAccessKey;type:text;not null" json:"secretAccessKey"`
	Bucket         string  `gorm:"column:bucket;type:text;not null" json:"bucket"`
	Region         string  `gorm:"column:region;type:text;not null" json:"region"`
	Endpoint       string  `gorm:"column:endpoint;type:text;not null" json:"endpoint"`
	CreatedAt      string  `gorm:"column:createdAt;type:text;not null" json:"createdAt"`
	OrganizationID string  `gorm:"column:organizationId;type:text;not null" json:"organizationId"`

	Organization *Organization `gorm:"foreignKey:OrganizationID" json:"organization,omitempty"`
	Backups      []Backup      `gorm:"foreignKey:DestinationID" json:"backups,omitempty"`
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

	Destination *Destination `gorm:"foreignKey:DestinationID" json:"destination,omitempty"`
	Postgres    *Postgres    `gorm:"foreignKey:PostgresID" json:"postgres,omitempty"`
	MySQL       *MySQL       `gorm:"foreignKey:MySQLID" json:"mysql,omitempty"`
	MariaDB     *MariaDB     `gorm:"foreignKey:MariaDBID" json:"mariadb,omitempty"`
	Mongo       *Mongo       `gorm:"foreignKey:MongoID" json:"mongo,omitempty"`
	Deployments []Deployment `gorm:"foreignKey:BackupID" json:"deployments,omitempty"`
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
	AppName        string  `gorm:"column:appName;type:text;not null" json:"appName"`
	ServiceName    string  `gorm:"column:serviceName;type:text;not null" json:"serviceName"`
	ServiceType    string  `gorm:"column:serviceType;type:text;not null" json:"serviceType"`
	SourcePath     string  `gorm:"column:sourcePath;type:text;not null" json:"sourcePath"`
	Schedule       string  `gorm:"column:schedule;type:text;not null" json:"schedule"`
	Prefix         string  `gorm:"column:prefix;type:text;not null" json:"prefix"`
	Enabled        *bool   `gorm:"column:enabled" json:"enabled"`
	DestinationID  string  `gorm:"column:destinationId;type:text;not null" json:"destinationId"`
	CreatedAt      string  `gorm:"column:createdAt;type:text;not null" json:"createdAt"`
	ServerID       *string `gorm:"column:serverId;type:text" json:"serverId"`

	Destination *Destination `gorm:"foreignKey:DestinationID" json:"destination,omitempty"`
	Server      *Server      `gorm:"foreignKey:ServerID" json:"server,omitempty"`
	Deployments []Deployment `gorm:"foreignKey:VolumeBackupID" json:"deployments,omitempty"`
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
