// Input: gorm, go-nanoid
// Output: Mount struct (含 type/hostPath/mountPath/content 等字段) + Port struct (含 publishedPort/targetPort/protocol)
// Role: 挂载和端口映射数据表模型，关联 Application/Compose/PreviewDeployment
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package schema

import (
	"time"

	gonanoid "github.com/matoous/go-nanoid/v2"
	"gorm.io/gorm"
)

// Mount represents the mount table.
type Mount struct {
	MountID       string    `gorm:"column:mountId;primaryKey;type:text" json:"mountId"`
	Type          MountType `gorm:"column:type;type:text;not null" json:"type"`
	HostPath      *string   `gorm:"column:hostPath;type:text" json:"hostPath"`
	VolumeName    *string   `gorm:"column:volumeName;type:text" json:"volumeName"`
	Content       *string   `gorm:"column:content;type:text" json:"content"`
	MountPath     string    `gorm:"column:mountPath;type:text;not null" json:"mountPath"`
	ServiceName   *string   `gorm:"column:serviceName;type:text" json:"serviceName"`
	FilePath      *string   `gorm:"column:filePath;type:text" json:"filePath"`
	CreatedAt     string    `gorm:"column:createdAt;type:text;not null" json:"createdAt"`
	ApplicationID *string   `gorm:"column:applicationId;type:text" json:"applicationId"`
	PostgresID    *string   `gorm:"column:postgresId;type:text" json:"postgresId"`
	MariaDBID     *string   `gorm:"column:mariadbId;type:text" json:"mariadbId"`
	MongoID       *string   `gorm:"column:mongoId;type:text" json:"mongoId"`
	MySQLID       *string   `gorm:"column:mysqlId;type:text" json:"mysqlId"`
	RedisID       *string   `gorm:"column:redisId;type:text" json:"redisId"`
	ComposeID     *string   `gorm:"column:composeId;type:text" json:"composeId"`

	// Relations
	Application *Application `gorm:"foreignKey:ApplicationID" json:"application,omitempty"`
	Postgres    *Postgres    `gorm:"foreignKey:PostgresID" json:"postgres,omitempty"`
	MariaDB     *MariaDB     `gorm:"foreignKey:MariaDBID" json:"mariadb,omitempty"`
	Mongo       *Mongo       `gorm:"foreignKey:MongoID" json:"mongo,omitempty"`
	MySQL       *MySQL       `gorm:"foreignKey:MySQLID" json:"mysql,omitempty"`
	Redis       *Redis       `gorm:"foreignKey:RedisID" json:"redis,omitempty"`
	Compose     *Compose     `gorm:"foreignKey:ComposeID" json:"compose,omitempty"`
}

func (Mount) TableName() string { return "mount" }

func (m *Mount) BeforeCreate(tx *gorm.DB) error {
	if m.MountID == "" {
		m.MountID, _ = gonanoid.New()
	}
	if m.CreatedAt == "" {
		m.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	return nil
}
