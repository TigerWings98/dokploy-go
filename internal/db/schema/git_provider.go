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

	Organization *Organization `gorm:"foreignKey:OrganizationID" json:"organization,omitempty"`
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
	Applications []Application `gorm:"foreignKey:GithubID" json:"applications,omitempty"`
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
	GitlabID     string  `gorm:"column:gitlabId;primaryKey;type:text" json:"gitlabId"`
	ApplicationID *string `gorm:"column:applicationId;type:text" json:"applicationId"`
	RedirectURI  *string `gorm:"column:redirectUri;type:text" json:"redirectUri"`
	GroupName    *string `gorm:"column:groupName;type:text" json:"groupName"`
	AccessToken  *string `gorm:"column:accessToken;type:text" json:"accessToken"`
	RefreshToken *string `gorm:"column:refreshToken;type:text" json:"refreshToken"`
	ExpiresAt    *int    `gorm:"column:expiresAt" json:"expiresAt"`
	GitProviderID string `gorm:"column:gitProviderId;type:text;not null" json:"gitProviderId"`

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
	BitbucketID     string  `gorm:"column:bitbucketId;primaryKey;type:text" json:"bitbucketId"`
	BitbucketUsername *string `gorm:"column:bitbucketUsername;type:text" json:"bitbucketUsername"`
	AppPassword     *string `gorm:"column:appPassword;type:text" json:"appPassword"`
	BitbucketWorkspaceName *string `gorm:"column:bitbucketWorkspaceName;type:text" json:"bitbucketWorkspaceName"`
	GitProviderID   string  `gorm:"column:gitProviderId;type:text;not null" json:"gitProviderId"`

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
	GiteaID       string  `gorm:"column:giteaId;primaryKey;type:text" json:"giteaId"`
	AccessToken   *string `gorm:"column:accessToken;type:text" json:"accessToken"`
	GiteaURL      *string `gorm:"column:giteaUrl;type:text" json:"giteaUrl"`
	GitProviderID string  `gorm:"column:gitProviderId;type:text;not null" json:"gitProviderId"`

	GitProvider *GitProvider `gorm:"foreignKey:GitProviderID" json:"gitProvider,omitempty"`
}

func (Gitea) TableName() string { return "gitea" }

func (g *Gitea) BeforeCreate(tx *gorm.DB) error {
	if g.GiteaID == "" {
		g.GiteaID, _ = gonanoid.New()
	}
	return nil
}
