// Input: gorm, go-nanoid
// Output: GitProvider struct (含 providerType/apiUrl/accessToken 等字段)
// Role: Git 提供商配置数据表模型，存储 GitHub/GitLab/Gitea/Bitbucket 的 API 凭证和连接信息
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package schema

import (
	"time"

	gonanoid "github.com/matoous/go-nanoid/v2"
	"gorm.io/gorm"
)

// GitProvider represents the git_provider table.
type GitProvider struct {
	GitProviderID  string          `gorm:"column:gitProviderId;primaryKey;type:text" json:"gitProviderId"`
	Name           string          `gorm:"column:name;type:text;not null" json:"name"`
	ProviderType   GitProviderType `gorm:"column:providerType;type:text;not null" json:"providerType"`
	CreatedAt      string          `gorm:"column:createdAt;type:text;not null" json:"createdAt"`
	OrganizationID string          `gorm:"column:organizationId;type:text;not null" json:"organizationId"`
	UserID         string          `gorm:"column:userId;type:text;not null" json:"userId"`

	Organization *Organization `gorm:"foreignKey:OrganizationID" json:"organization,omitempty"`
	User         *User         `gorm:"foreignKey:UserID" json:"user,omitempty"`
	Github       *Github       `gorm:"foreignKey:GitProviderID" json:"github,omitempty"`
	Gitlab       *Gitlab       `gorm:"foreignKey:GitProviderID" json:"gitlab,omitempty"`
	Bitbucket    *Bitbucket    `gorm:"foreignKey:GitProviderID" json:"bitbucket,omitempty"`
	Gitea        *Gitea        `gorm:"foreignKey:GitProviderID" json:"gitea,omitempty"`
}

func (GitProvider) TableName() string { return "git_provider" }

func (g *GitProvider) BeforeCreate(tx *gorm.DB) error {
	if g.GitProviderID == "" {
		g.GitProviderID, _ = gonanoid.New()
	}
	if g.CreatedAt == "" {
		g.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	return nil
}

// Github represents the github table.
type Github struct {
	GithubID        string  `gorm:"column:githubId;primaryKey;type:text" json:"githubId"`
	GithubAppID     *int    `gorm:"column:githubAppId" json:"githubAppId"`
	GithubAppName   *string `gorm:"column:githubAppName;type:text" json:"githubAppName"`
	GithubClientID  *string `gorm:"column:githubClientId;type:text" json:"githubClientId"`
	GithubClientSecret *string `gorm:"column:githubClientSecret;type:text" json:"githubClientSecret"`
	GithubInstallationID *string `gorm:"column:githubInstallationId;type:text" json:"githubInstallationId"`
	GithubPrivateKey *string `gorm:"column:githubPrivateKey;type:text" json:"githubPrivateKey"`
	GithubWebhookSecret *string `gorm:"column:githubWebhookSecret;type:text" json:"githubWebhookSecret"`
	GitProviderID   string  `gorm:"column:gitProviderId;type:text;not null" json:"gitProviderId"`

	GitProvider  *GitProvider  `gorm:"foreignKey:GitProviderID" json:"gitProvider,omitempty"`
	Applications []Application `gorm:"foreignKey:GithubID" json:"applications"`
}

func (Github) TableName() string { return "github" }

func (g *Github) BeforeCreate(tx *gorm.DB) error {
	if g.GithubID == "" {
		g.GithubID, _ = gonanoid.New()
	}
	return nil
}

// Gitlab represents the gitlab table.
type Gitlab struct {
	GitlabID          string  `gorm:"column:gitlabId;primaryKey;type:text" json:"gitlabId"`
	GitlabURL         string  `gorm:"column:gitlabUrl;type:text;not null;default:'https://gitlab.com'" json:"gitlabUrl"`
	GitlabInternalURL *string `gorm:"column:gitlabInternalUrl;type:text" json:"gitlabInternalUrl"`
	ApplicationID     *string `gorm:"column:applicationId;type:text" json:"applicationId"`
	RedirectURI       *string `gorm:"column:redirectUri;type:text" json:"redirectUri"`
	Secret            *string `gorm:"column:secret;type:text" json:"secret"`
	GroupName         *string `gorm:"column:groupName;type:text" json:"groupName"`
	AccessToken       *string `gorm:"column:accessToken;type:text" json:"accessToken"`
	RefreshToken      *string `gorm:"column:refreshToken;type:text" json:"refreshToken"`
	ExpiresAt         *int    `gorm:"column:expiresAt" json:"expiresAt"`
	GitProviderID     string  `gorm:"column:gitProviderId;type:text;not null" json:"gitProviderId"`

	GitProvider *GitProvider `gorm:"foreignKey:GitProviderID" json:"gitProvider,omitempty"`
}

func (Gitlab) TableName() string { return "gitlab" }

func (g *Gitlab) BeforeCreate(tx *gorm.DB) error {
	if g.GitlabID == "" {
		g.GitlabID, _ = gonanoid.New()
	}
	return nil
}

// Bitbucket represents the bitbucket table.
type Bitbucket struct {
	BitbucketID            string  `gorm:"column:bitbucketId;primaryKey;type:text" json:"bitbucketId"`
	BitbucketUsername      *string `gorm:"column:bitbucketUsername;type:text" json:"bitbucketUsername"`
	BitbucketEmail         *string `gorm:"column:bitbucketEmail;type:text" json:"bitbucketEmail"`
	AppPassword            *string `gorm:"column:appPassword;type:text" json:"appPassword"`
	APIToken               *string `gorm:"column:apiToken;type:text" json:"apiToken"`
	BitbucketWorkspaceName *string `gorm:"column:bitbucketWorkspaceName;type:text" json:"bitbucketWorkspaceName"`
	GitProviderID          string  `gorm:"column:gitProviderId;type:text;not null" json:"gitProviderId"`

	GitProvider *GitProvider `gorm:"foreignKey:GitProviderID" json:"gitProvider,omitempty"`
}

func (Bitbucket) TableName() string { return "bitbucket" }

func (b *Bitbucket) BeforeCreate(tx *gorm.DB) error {
	if b.BitbucketID == "" {
		b.BitbucketID, _ = gonanoid.New()
	}
	return nil
}

// Gitea represents the gitea table.
type Gitea struct {
	GiteaID             string  `gorm:"column:giteaId;primaryKey;type:text" json:"giteaId"`
	GiteaURL            string  `gorm:"column:giteaUrl;type:text;not null;default:'https://gitea.com'" json:"giteaUrl"`
	GiteaInternalURL    *string `gorm:"column:giteaInternalUrl;type:text" json:"giteaInternalUrl"`
	RedirectURI         *string `gorm:"column:redirectUri;type:text" json:"redirectUri"`
	ClientID            *string `gorm:"column:clientId;type:text" json:"clientId"`
	ClientSecret        *string `gorm:"column:clientSecret;type:text" json:"clientSecret"`
	AccessToken         *string `gorm:"column:accessToken;type:text" json:"accessToken"`
	RefreshToken        *string `gorm:"column:refreshToken;type:text" json:"refreshToken"`
	ExpiresAt           *int    `gorm:"column:expiresAt" json:"expiresAt"`
	Scopes              *string `gorm:"column:scopes;type:text;default:'repo,repo:status,read:user,read:org'" json:"scopes"`
	LastAuthenticatedAt *int    `gorm:"column:lastAuthenticatedAt" json:"lastAuthenticatedAt"`
	GitProviderID       string  `gorm:"column:gitProviderId;type:text;not null" json:"gitProviderId"`

	GitProvider *GitProvider `gorm:"foreignKey:GitProviderID" json:"gitProvider,omitempty"`
}

func (Gitea) TableName() string { return "gitea" }

func (g *Gitea) BeforeCreate(tx *gorm.DB) error {
	if g.GiteaID == "" {
		g.GiteaID, _ = gonanoid.New()
	}
	return nil
}
