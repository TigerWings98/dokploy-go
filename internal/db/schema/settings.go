// Input: gorm, go-nanoid
// Output: WebServerSettings struct (全局设置), Admin struct, SSHKey struct, Certificate struct, Registry struct, Environment struct, Destination struct, Schedule struct
// Role: 系统级配置和资源管理表模型集合，涵盖服务器设置/SSH密钥/证书/Registry/环境变量/备份目标/定时任务
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package schema

import (
	"time"

	gonanoid "github.com/matoous/go-nanoid/v2"
	"gorm.io/gorm"
)

// WebServerSettings represents the singleton web server settings table.
type WebServerSettings struct {
	ID                       string          `gorm:"column:id;primaryKey;type:text" json:"id"`
	ServerIP                 *string         `gorm:"column:serverIp;type:text" json:"serverIp"`
	CertificateType          CertificateType `gorm:"column:certificateType;type:text;not null;default:'none'" json:"certificateType"`
	HTTPS                    bool            `gorm:"column:https;not null;default:false" json:"https"`
	Host                     *string         `gorm:"column:host;type:text" json:"host"`
	LetsEncryptEmail         *string         `gorm:"column:letsEncryptEmail;type:text" json:"letsEncryptEmail"`
	SSHPrivateKey            *string         `gorm:"column:sshPrivateKey;type:text" json:"sshPrivateKey"`
	EnableDockerCleanup      bool            `gorm:"column:enableDockerCleanup;not null;default:true" json:"enableDockerCleanup"`
	LogCleanupCron           *string         `gorm:"column:logCleanupCron;type:text" json:"logCleanupCron"`
	MetricsConfig            JSONField[any]  `gorm:"column:metricsConfig;type:jsonb" json:"metricsConfig"`
	CleanupCacheApplications bool            `gorm:"column:cleanupCacheApplications;not null;default:false" json:"cleanupCacheApplications"`
	CleanupCacheOnPreviews   bool            `gorm:"column:cleanupCacheOnPreviews;not null;default:false" json:"cleanupCacheOnPreviews"`
	CleanupCacheOnCompose    bool            `gorm:"column:cleanupCacheOnCompose;not null;default:false" json:"cleanupCacheOnCompose"`
	// v0.28.6 新增：企业版白标配置（jsonb）
	WhitelabelingConfig      JSONField[any]  `gorm:"column:whitelabelingConfig;type:jsonb" json:"whitelabelingConfig"`
	CreatedAt                time.Time       `gorm:"column:createdAt;not null;default:now()" json:"createdAt"`
	UpdatedAt                time.Time       `gorm:"column:updatedAt;not null;default:now()" json:"updatedAt"`
}

func (WebServerSettings) TableName() string { return "webServerSettings" }

func (w *WebServerSettings) BeforeCreate(tx *gorm.DB) error {
	if w.ID == "" {
		w.ID, _ = gonanoid.New()
	}
	return nil
}

// GoConfig 是 Go 版专属的单行配置表（go_ 前缀，不与 TS 版冲突）。
// 以后新增 Go 专属配置直接加列即可，和 TS 版 webServerSettings 同样的模式。
type GoConfig struct {
	ID            string    `gorm:"column:id;primaryKey;type:text;default:'default'" json:"id"`
	RegistryImage string    `gorm:"column:registry_image;type:text;not null;default:''" json:"registryImage"` // 镜像名（不含 tag），如 ghcr.io/your-org/dokploy-go
	RegistryID    *string   `gorm:"column:registry_id;type:text" json:"registryId"`                          // 关联已有的 Registry 记录（用其 username/password 认证）
	ServiceName   string    `gorm:"column:service_name;type:text;not null;default:'dokploy'" json:"serviceName"`
	CreatedAt     time.Time `gorm:"column:created_at;not null;default:now()" json:"createdAt"`
	UpdatedAt     time.Time `gorm:"column:updated_at;not null;default:now()" json:"updatedAt"`

	// Relations
	Registry *Registry `gorm:"foreignKey:RegistryID;references:RegistryID" json:"registry,omitempty"`
}

func (GoConfig) TableName() string { return "go_config" }

func (g *GoConfig) BeforeCreate(tx *gorm.DB) error {
	if g.ID == "" {
		g.ID = "default"
	}
	return nil
}
