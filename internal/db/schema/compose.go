package schema

import (
	"time"

	gonanoid "github.com/matoous/go-nanoid/v2"
	"gorm.io/gorm"
)

// Compose represents the compose table.
type Compose struct {
	ComposeID     string            `gorm:"column:composeId;primaryKey;type:text" json:"composeId"`
	Name          string            `gorm:"column:name;type:text;not null" json:"name"`
	AppName       string            `gorm:"column:appName;type:text;not null;uniqueIndex" json:"appName"`
	Description   *string           `gorm:"column:description;type:text" json:"description"`
	Env           *string           `gorm:"column:env;type:text" json:"env"`
	ComposeFile   string            `gorm:"column:composeFile;type:text;not null;default:''" json:"composeFile"`
	ComposeType   ComposeType       `gorm:"column:composeType;type:text;not null;default:'docker-compose'" json:"composeType"`
	SourceType    SourceTypeCompose `gorm:"column:sourceType;type:text;not null;default:'raw'" json:"sourceType"`
	RefreshToken  *string           `gorm:"column:refreshToken;type:text" json:"refreshToken"`
	ComposeStatus ApplicationStatus `gorm:"column:composeStatus;type:text;not null;default:'idle'" json:"composeStatus"`
	Command       *string           `gorm:"column:command;type:text" json:"command"`
	AutoDeploy    *bool             `gorm:"column:autoDeploy" json:"autoDeploy"`
	RandomizeCompose *bool          `gorm:"column:randomize" json:"randomize"`
	CreatedAt     string            `gorm:"column:createdAt;type:text;not null" json:"createdAt"`
	EnvironmentID string            `gorm:"column:environmentId;type:text;not null" json:"environmentId"`
	ServerID      *string           `gorm:"column:serverId;type:text" json:"serverId"`

	// Git fields (same pattern as Application)
	Repository   *string `gorm:"column:repository;type:text" json:"repository"`
	Owner        *string `gorm:"column:owner;type:text" json:"owner"`
	Branch       *string `gorm:"column:branch;type:text" json:"branch"`
	BuildPath    *string `gorm:"column:buildPath;type:text;default:'/'" json:"buildPath"`
	GithubID     *string `gorm:"column:githubId;type:text" json:"githubId"`
	GitlabID     *string `gorm:"column:gitlabId;type:text" json:"gitlabId"`
	GiteaID      *string `gorm:"column:giteaId;type:text" json:"giteaId"`
	BitbucketID  *string `gorm:"column:bitbucketId;type:text" json:"bitbucketId"`
	CustomGitURL *string `gorm:"column:customGitUrl;type:text" json:"customGitUrl"`
	CustomGitBranch *string `gorm:"column:customGitBranch;type:text" json:"customGitBranch"`
	CustomGitBuildPath *string `gorm:"column:customGitBuildPath;type:text" json:"customGitBuildPath"`
	CustomGitSSHKeyID *string `gorm:"column:customGitSSHKeyId;type:text" json:"customGitSSHKeyId"`

	// Suffix for compose project name
	ComposeSuffix *string `gorm:"column:suffix;type:text" json:"suffix"`

	// Relations
	Environment *Environment `gorm:"foreignKey:EnvironmentID" json:"environment,omitempty"`
	Server      *Server      `gorm:"foreignKey:ServerID" json:"server,omitempty"`
	Deployments []Deployment `gorm:"foreignKey:ComposeID" json:"deployments,omitempty"`
	Domains     []Domain     `gorm:"foreignKey:ComposeID" json:"domains,omitempty"`
	Mounts      []Mount      `gorm:"foreignKey:ComposeID" json:"mounts,omitempty"`
	Security    []Security   `gorm:"foreignKey:ComposeID" json:"security,omitempty"`
	Redirects   []Redirect   `gorm:"foreignKey:ComposeID" json:"redirects,omitempty"`
}

func (Compose) TableName() string { return "compose" }

func (c *Compose) BeforeCreate(tx *gorm.DB) error {
	if c.ComposeID == "" {
		c.ComposeID, _ = gonanoid.New()
	}
	if c.AppName == "" {
		c.AppName = GenerateAppName("compose")
	}
	if c.RefreshToken == nil {
		token, _ := gonanoid.New()
		c.RefreshToken = &token
	}
	if c.CreatedAt == "" {
		c.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	return nil
}
