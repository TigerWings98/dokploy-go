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
