// Input: gorm, go-nanoid
// Output: Compose struct (含 sourceType/composeType/composeFile 等字段和 Git/Domain/Deployment 关系)
// Role: Docker Compose 服务数据表模型，支持 docker-compose 和 stack 两种部署模式，关联 Environment/Server/Git 提供商
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
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
	AppName       string            `gorm:"column:appName;type:text;not null;uniqueIndex:compose_appName_unique" json:"appName"`
	Description   *string           `gorm:"column:description;type:text" json:"description"`
	Env           *string           `gorm:"column:env;type:text" json:"env"`
	ComposeFile   string            `gorm:"column:composeFile;type:text;not null;default:''" json:"composeFile"`
	ComposeType   ComposeType       `gorm:"column:composeType;type:text;not null;default:'docker-compose'" json:"composeType"`
	SourceType    SourceTypeCompose `gorm:"column:sourceType;type:text;not null;default:'raw'" json:"sourceType"`
	RefreshToken  *string           `gorm:"column:refreshToken;type:text" json:"refreshToken"`
	ComposeStatus ApplicationStatus `gorm:"column:composeStatus;type:text;not null;default:'idle'" json:"composeStatus"`
	ComposePath   string            `gorm:"column:composePath;type:text;not null;default:'./docker-compose.yml'" json:"composePath"`
	Command       string            `gorm:"column:command;type:text;not null;default:''" json:"command"`
	AutoDeploy    *bool             `gorm:"column:autoDeploy" json:"autoDeploy"`
	RandomizeCompose bool            `gorm:"column:randomize;not null;default:false" json:"randomize"`
	CreatedAt     string            `gorm:"column:createdAt;type:text;not null" json:"createdAt"`
	EnvironmentID string            `gorm:"column:environmentId;type:text;not null" json:"environmentId"`
	ServerID      *string           `gorm:"column:serverId;type:text" json:"serverId"`

	// GitHub 字段
	Repository *string `gorm:"column:repository;type:text" json:"repository"`
	Owner      *string `gorm:"column:owner;type:text" json:"owner"`
	Branch     *string `gorm:"column:branch;type:text" json:"branch"`

	// GitLab 字段
	GitlabProjectID     *int    `gorm:"column:gitlabProjectId" json:"gitlabProjectId"`
	GitlabRepository    *string `gorm:"column:gitlabRepository;type:text" json:"gitlabRepository"`
	GitlabOwner         *string `gorm:"column:gitlabOwner;type:text" json:"gitlabOwner"`
	GitlabBranch        *string `gorm:"column:gitlabBranch;type:text" json:"gitlabBranch"`
	GitlabPathNamespace *string `gorm:"column:gitlabPathNamespace;type:text" json:"gitlabPathNamespace"`

	// Bitbucket 字段
	BitbucketRepository     *string `gorm:"column:bitbucketRepository;type:text" json:"bitbucketRepository"`
	BitbucketRepositorySlug *string `gorm:"column:bitbucketRepositorySlug;type:text" json:"bitbucketRepositorySlug"`
	BitbucketOwner          *string `gorm:"column:bitbucketOwner;type:text" json:"bitbucketOwner"`
	BitbucketBranch         *string `gorm:"column:bitbucketBranch;type:text" json:"bitbucketBranch"`

	// Gitea 字段
	GiteaRepository *string `gorm:"column:giteaRepository;type:text" json:"giteaRepository"`
	GiteaOwner      *string `gorm:"column:giteaOwner;type:text" json:"giteaOwner"`
	GiteaBranch     *string `gorm:"column:giteaBranch;type:text" json:"giteaBranch"`

	// 自定义 Git 字段
	CustomGitURL      *string `gorm:"column:customGitUrl;type:text" json:"customGitUrl"`
	CustomGitBranch   *string `gorm:"column:customGitBranch;type:text" json:"customGitBranch"`
	CustomGitSSHKeyID *string `gorm:"column:customGitSSHKeyId;type:text" json:"customGitSSHKeyId"`

	// 子模块 & 触发器
	EnableSubmodules bool    `gorm:"column:enableSubmodules;not null;default:false" json:"enableSubmodules"`
	TriggerType      *string `gorm:"column:triggerType;type:text;default:'push'" json:"triggerType"`
	WatchPaths       StringArray `gorm:"column:watchPaths;type:text[]" json:"watchPaths"`

	// Suffix for compose project name
	ComposeSuffix string `gorm:"column:suffix;type:text;not null;default:''" json:"suffix"`

	// 隔离部署：为 compose 创建独立 Docker 网络
	IsolatedDeployment        bool `gorm:"column:isolatedDeployment;not null;default:false" json:"isolatedDeployment"`
	IsolatedDeploymentsVolume bool `gorm:"column:isolatedDeploymentsVolume;not null;default:false" json:"isolatedDeploymentsVolume"`

	// 提供商外键
	GithubID    *string `gorm:"column:githubId;type:text" json:"githubId"`
	GitlabID    *string `gorm:"column:gitlabId;type:text" json:"gitlabId"`
	BitbucketID *string `gorm:"column:bitbucketId;type:text" json:"bitbucketId"`
	GiteaID     *string `gorm:"column:giteaId;type:text" json:"giteaId"`

	// Relations
	Environment     *Environment `gorm:"foreignKey:EnvironmentID;references:EnvironmentID" json:"environment,omitempty"`
	Server          *Server      `gorm:"foreignKey:ServerID;references:ServerID" json:"server,omitempty"`
	Deployments     []Deployment `gorm:"foreignKey:ComposeID;references:ComposeID" json:"deployments"`
	Domains         []Domain     `gorm:"foreignKey:ComposeID;references:ComposeID" json:"domains"`
	Mounts          []Mount      `gorm:"foreignKey:ComposeID;references:ComposeID" json:"mounts"`
	Security        []Security   `gorm:"foreignKey:ComposeID;references:ComposeID" json:"security"`
	Redirects       []Redirect   `gorm:"foreignKey:ComposeID;references:ComposeID" json:"redirects"`
	Github          *Github      `gorm:"foreignKey:GithubID;references:GithubID" json:"github,omitempty"`
	Gitlab          *Gitlab      `gorm:"foreignKey:GitlabID;references:GitlabID" json:"gitlab,omitempty"`
	Gitea           *Gitea       `gorm:"foreignKey:GiteaID;references:GiteaID" json:"gitea,omitempty"`
	Bitbucket       *Bitbucket   `gorm:"foreignKey:BitbucketID;references:BitbucketID" json:"bitbucket,omitempty"`
	CustomGitSSHKey *SSHKey      `gorm:"foreignKey:CustomGitSSHKeyID;references:SSHKeyID" json:"customGitSSHKey,omitempty"`
	Backups         []Backup     `gorm:"foreignKey:ComposeID;references:ComposeID" json:"backups"`
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
