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
	Port              *int            `gorm:"column:port;default:80" json:"port"`
	Path              *string         `gorm:"column:path;type:text;default:'/'" json:"path"`
	UniqueConfigKey   *int            `gorm:"column:uniqueConfigKey" json:"uniqueConfigKey"`
	CertificateType   CertificateType `gorm:"column:certificateType;type:text;not null;default:'none'" json:"certificateType"`
	CustomCertResolver *string        `gorm:"column:customCertResolver;type:text" json:"customCertResolver"`
	CreatedAt         string          `gorm:"column:createdAt;type:text;not null" json:"createdAt"`
	ApplicationID     *string         `gorm:"column:applicationId;type:text" json:"applicationId"`
	ComposeID         *string         `gorm:"column:composeId;type:text" json:"composeId"`
	ServiceName       *string         `gorm:"column:serviceName;type:text" json:"serviceName"`
	PreviewDeploymentID *string       `gorm:"column:previewDeploymentId;type:text" json:"previewDeploymentId"`

	// Relations
	Application       *Application       `gorm:"foreignKey:ApplicationID" json:"application,omitempty"`
	Compose           *Compose           `gorm:"foreignKey:ComposeID" json:"compose,omitempty"`
	PreviewDeployment *PreviewDeployment `gorm:"foreignKey:PreviewDeploymentID" json:"previewDeployment,omitempty"`
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
