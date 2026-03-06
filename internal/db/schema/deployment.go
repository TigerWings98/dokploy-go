package schema

import (
	"time"

	gonanoid "github.com/matoous/go-nanoid/v2"
	"gorm.io/gorm"
)

// Deployment represents the deployment table.
type Deployment struct {
	DeploymentID        string            `gorm:"column:deploymentId;primaryKey;type:text" json:"deploymentId"`
	Title               string            `gorm:"column:title;type:text;not null" json:"title"`
	Description         *string           `gorm:"column:description;type:text" json:"description"`
	Status              *DeploymentStatus `gorm:"column:status;type:text;default:'running'" json:"status"`
	LogPath             string            `gorm:"column:logPath;type:text;not null" json:"logPath"`
	PID                 *string           `gorm:"column:pid;type:text" json:"pid"`
	ApplicationID       *string           `gorm:"column:applicationId;type:text" json:"applicationId"`
	ComposeID           *string           `gorm:"column:composeId;type:text" json:"composeId"`
	ServerID            *string           `gorm:"column:serverId;type:text" json:"serverId"`
	IsPreviewDeployment *bool             `gorm:"column:isPreviewDeployment;default:false" json:"isPreviewDeployment"`
	PreviewDeploymentID *string           `gorm:"column:previewDeploymentId;type:text" json:"previewDeploymentId"`
	CreatedAt           string            `gorm:"column:createdAt;type:text;not null" json:"createdAt"`
	StartedAt           *string           `gorm:"column:startedAt;type:text" json:"startedAt"`
	FinishedAt          *string           `gorm:"column:finishedAt;type:text" json:"finishedAt"`
	ErrorMessage        *string           `gorm:"column:errorMessage;type:text" json:"errorMessage"`
	ScheduleID          *string           `gorm:"column:scheduleId;type:text" json:"scheduleId"`
	BackupID            *string           `gorm:"column:backupId;type:text" json:"backupId"`
	RollbackID          *string           `gorm:"column:rollbackId;type:text" json:"rollbackId"`
	VolumeBackupID      *string           `gorm:"column:volumeBackupId;type:text" json:"volumeBackupId"`
	BuildServerID       *string           `gorm:"column:buildServerId;type:text" json:"buildServerId"`

	// Relations
	Application       *Application       `gorm:"foreignKey:ApplicationID" json:"application,omitempty"`
	Compose           *Compose           `gorm:"foreignKey:ComposeID" json:"compose,omitempty"`
	Server            *Server            `gorm:"foreignKey:ServerID" json:"server,omitempty"`
	BuildServer       *Server            `gorm:"foreignKey:BuildServerID" json:"buildServer,omitempty"`
	PreviewDeployment *PreviewDeployment `gorm:"foreignKey:PreviewDeploymentID" json:"previewDeployment,omitempty"`
}

func (Deployment) TableName() string { return "deployment" }

func (d *Deployment) BeforeCreate(tx *gorm.DB) error {
	if d.DeploymentID == "" {
		d.DeploymentID, _ = gonanoid.New()
	}
	if d.CreatedAt == "" {
		d.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	return nil
}
