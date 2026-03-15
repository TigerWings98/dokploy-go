// Input: gorm, go-nanoid
// Output: Domain struct (含 host/path/port/https/certificateType 等字段) + Redirect/Security struct
// Role: 域名路由数据表模型，关联 Application/Compose/PreviewDeployment，驱动 Traefik 动态路由配置生成
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package schema

import (
	gonanoid "github.com/matoous/go-nanoid/v2"
	"gorm.io/gorm"
	"time"
)

// Domain represents the domain table.
type Domain struct {
	DomainID          string          `gorm:"column:domainId;primaryKey;type:text" json:"domainId"`
	Host              string          `gorm:"column:host;type:text;not null" json:"host"`
	HTTPS             bool            `gorm:"column:https;not null;default:false" json:"https"`
	Port              *int            `gorm:"column:port;default:3000" json:"port"`
	Path              *string         `gorm:"column:path;type:text;default:'/'" json:"path"`
	UniqueConfigKey   *int            `gorm:"column:uniqueConfigKey" json:"uniqueConfigKey"`
	CertificateType   CertificateType `gorm:"column:certificateType;type:text;not null;default:'none'" json:"certificateType"`
	CustomCertResolver *string        `gorm:"column:customCertResolver;type:text" json:"customCertResolver"`
	DomainType        DomainType      `gorm:"column:domainType;type:text;default:'application'" json:"domainType"`
	InternalPath      *string         `gorm:"column:internalPath;type:text;default:'/'" json:"internalPath"`
	StripPath         bool            `gorm:"column:stripPath;not null;default:false" json:"stripPath"`
	CreatedAt         string          `gorm:"column:createdAt;type:text;not null" json:"createdAt"`
	ApplicationID     *string         `gorm:"column:applicationId;type:text" json:"applicationId"`
	ComposeID         *string         `gorm:"column:composeId;type:text" json:"composeId"`
	ServiceName       *string         `gorm:"column:serviceName;type:text" json:"serviceName"`
	PreviewDeploymentID *string       `gorm:"column:previewDeploymentId;type:text" json:"previewDeploymentId"`

	// Relations
	Application       *Application       `gorm:"foreignKey:ApplicationID;references:ApplicationID" json:"application,omitempty"`
	Compose           *Compose           `gorm:"foreignKey:ComposeID;references:ComposeID" json:"compose,omitempty"`
	PreviewDeployment *PreviewDeployment `gorm:"foreignKey:PreviewDeploymentID;references:PreviewDeploymentID" json:"previewDeployment,omitempty"`
}

func (Domain) TableName() string { return "domain" }

func (d *Domain) BeforeCreate(tx *gorm.DB) error {
	if d.DomainID == "" {
		d.DomainID, _ = gonanoid.New()
	}
	if d.CreatedAt == "" {
		d.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	return nil
}
