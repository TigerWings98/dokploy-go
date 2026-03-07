package schema

import (
	"time"

	gonanoid "github.com/matoous/go-nanoid/v2"
	"gorm.io/gorm"
)

// PreviewDeployment represents the preview_deployment table.
type PreviewDeployment struct {
	PreviewDeploymentID   string            `gorm:"column:previewDeploymentId;primaryKey;type:text" json:"previewDeploymentId"`
	AppName               string            `gorm:"column:appName;type:text;not null;uniqueIndex:preview_deployment_appName_unique" json:"appName"`
	Branch                string            `gorm:"column:branch;type:text;not null" json:"branch"`
	PullRequestID         string            `gorm:"column:pullRequestId;type:text;not null" json:"pullRequestId"`
	PullRequestNumber     string            `gorm:"column:pullRequestNumber;type:text;not null" json:"pullRequestNumber"`
	PullRequestURL        string            `gorm:"column:pullRequestURL;type:text;not null" json:"pullRequestURL"`
	PullRequestTitle      string            `gorm:"column:pullRequestTitle;type:text;not null" json:"pullRequestTitle"`
	PullRequestCommentID  string            `gorm:"column:pullRequestCommentId;type:text" json:"pullRequestCommentId"`
	PreviewStatus         ApplicationStatus `gorm:"column:previewStatus;type:text;not null;default:'idle'" json:"previewStatus"`
	CreatedAt             string            `gorm:"column:createdAt;type:text;not null" json:"createdAt"`
	ExpiresAt             *string           `gorm:"column:expiresAt;type:text" json:"expiresAt"`
	ApplicationID         string            `gorm:"column:applicationId;type:text;not null" json:"applicationId"`
	DomainID              *string           `gorm:"column:domainId;type:text" json:"domainId"`

	// Relations
	Application *Application `gorm:"foreignKey:ApplicationID" json:"application,omitempty"`
	Deployments []Deployment `gorm:"foreignKey:PreviewDeploymentID" json:"deployments"`
	Domains     []Domain     `gorm:"foreignKey:PreviewDeploymentID" json:"domains"`
}

func (PreviewDeployment) TableName() string { return "preview_deployments" }

func (p *PreviewDeployment) BeforeCreate(tx *gorm.DB) error {
	if p.PreviewDeploymentID == "" {
		p.PreviewDeploymentID, _ = gonanoid.New()
	}
	if p.AppName == "" {
		p.AppName = GenerateAppName("preview")
	}
	if p.CreatedAt == "" {
		p.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	return nil
}

// Rollback represents the rollback table.
type Rollback struct {
	RollbackID    string `gorm:"column:rollbackId;primaryKey;type:text" json:"rollbackId"`
	DockerImage   string `gorm:"column:dockerImage;type:text;not null" json:"dockerImage"`
	CreatedAt     string `gorm:"column:createdAt;type:text;not null" json:"createdAt"`
	ApplicationID string `gorm:"column:applicationId;type:text;not null" json:"applicationId"`
	DeploymentID  string `gorm:"column:deploymentId;type:text" json:"deploymentId"`

	Application *Application `gorm:"foreignKey:ApplicationID" json:"application,omitempty"`
}

func (Rollback) TableName() string { return "rollback" }

func (r *Rollback) BeforeCreate(tx *gorm.DB) error {
	if r.RollbackID == "" {
		r.RollbackID, _ = gonanoid.New()
	}
	if r.CreatedAt == "" {
		r.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	return nil
}

// Schedule represents the schedule table.
type Schedule struct {
	ScheduleID     string  `gorm:"column:scheduleId;primaryKey;type:text" json:"scheduleId"`
	Name           string  `gorm:"column:name;type:text;not null" json:"name"`
	CronExpression string  `gorm:"column:cronExpression;type:text;not null" json:"cronExpression"`
	AppName        string  `gorm:"column:appName;type:text;not null" json:"appName"`
	ServiceName    *string `gorm:"column:serviceName;type:text" json:"serviceName"`
	ShellType      string  `gorm:"column:shellType;type:text;not null;default:bash" json:"shellType"`
	ScheduleType   string  `gorm:"column:scheduleType;type:text;not null;default:application" json:"scheduleType"`
	Command        string  `gorm:"column:command;type:text;not null" json:"command"`
	Script         *string `gorm:"column:script;type:text" json:"script"`
	Enabled        bool    `gorm:"column:enabled;not null;default:true" json:"enabled"`
	Timezone       *string `gorm:"column:timezone;type:text" json:"timezone"`
	CreatedAt      string  `gorm:"column:createdAt;type:text;not null" json:"createdAt"`
	ApplicationID  *string `gorm:"column:applicationId;type:text" json:"applicationId"`
	ComposeID      *string `gorm:"column:composeId;type:text" json:"composeId"`
	ServerID       *string `gorm:"column:serverId;type:text" json:"serverId"`
	UserID         *string `gorm:"column:userId;type:text" json:"userId"`

	Application *Application `gorm:"foreignKey:ApplicationID" json:"application,omitempty"`
	Compose     *Compose     `gorm:"foreignKey:ComposeID" json:"compose,omitempty"`
	Server      *Server      `gorm:"foreignKey:ServerID" json:"server,omitempty"`
	Deployments []Deployment `gorm:"foreignKey:ScheduleID" json:"deployments"`
}

func (Schedule) TableName() string { return "schedule" }

func (s *Schedule) BeforeCreate(tx *gorm.DB) error {
	if s.ScheduleID == "" {
		s.ScheduleID, _ = gonanoid.New()
	}
	if s.CreatedAt == "" {
		s.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	return nil
}
