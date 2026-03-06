package schema

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	gonanoid "github.com/matoous/go-nanoid/v2"
	"gorm.io/gorm"
)

// Application represents the application table.
type Application struct {
	ApplicationID    string            `gorm:"column:applicationId;primaryKey;type:text" json:"applicationId"`
	Name             string            `gorm:"column:name;type:text;not null" json:"name"`
	AppName          string            `gorm:"column:appName;type:text;not null;uniqueIndex:application_appName_unique" json:"appName"`
	Description      *string           `gorm:"column:description;type:text" json:"description"`
	Env              *string           `gorm:"column:env;type:text" json:"env"`
	PreviewEnv       *string           `gorm:"column:previewEnv;type:text" json:"previewEnv"`
	WatchPaths       StringArray       `gorm:"column:watchPaths;type:text[]" json:"watchPaths"`
	PreviewBuildArgs *string           `gorm:"column:previewBuildArgs;type:text" json:"previewBuildArgs"`
	PreviewBuildSecrets *string        `gorm:"column:previewBuildSecrets;type:text" json:"previewBuildSecrets"`
	PreviewLabels    StringArray       `gorm:"column:previewLabels;type:text[]" json:"previewLabels"`
	PreviewWildcard  *string           `gorm:"column:previewWildcard;type:text" json:"previewWildcard"`
	PreviewPort      *int              `gorm:"column:previewPort;default:3000" json:"previewPort"`
	PreviewHTTPS     bool              `gorm:"column:previewHttps;not null;default:false" json:"previewHttps"`
	PreviewPath      *string           `gorm:"column:previewPath;default:'/'" json:"previewPath"`
	PreviewCertificateType CertificateType `gorm:"column:certificateType;type:text;not null;default:'none'" json:"previewCertificateType"`
	PreviewCustomCertResolver *string  `gorm:"column:previewCustomCertResolver;type:text" json:"previewCustomCertResolver"`
	PreviewLimit     *int              `gorm:"column:previewLimit;default:3" json:"previewLimit"`
	IsPreviewDeploymentsActive *bool   `gorm:"column:isPreviewDeploymentsActive;default:false" json:"isPreviewDeploymentsActive"`
	PreviewRequireCollaboratorPermissions *bool `gorm:"column:previewRequireCollaboratorPermissions;default:true" json:"previewRequireCollaboratorPermissions"`
	RollbackActive   *bool             `gorm:"column:rollbackActive;default:false" json:"rollbackActive"`
	BuildArgs        *string           `gorm:"column:buildArgs;type:text" json:"buildArgs"`
	BuildSecrets     *string           `gorm:"column:buildSecrets;type:text" json:"buildSecrets"`
	MemoryReservation *string          `gorm:"column:memoryReservation;type:text" json:"memoryReservation"`
	MemoryLimit      *string           `gorm:"column:memoryLimit;type:text" json:"memoryLimit"`
	CPUReservation   *string           `gorm:"column:cpuReservation;type:text" json:"cpuReservation"`
	CPULimit         *string           `gorm:"column:cpuLimit;type:text" json:"cpuLimit"`
	Title            *string           `gorm:"column:title;type:text" json:"title"`
	Enabled          *bool             `gorm:"column:enabled" json:"enabled"`
	Subtitle         *string           `gorm:"column:subtitle;type:text" json:"subtitle"`
	Command          *string           `gorm:"column:command;type:text" json:"command"`
	Args             StringArray       `gorm:"column:args;type:text[]" json:"args"`
	RefreshToken     *string           `gorm:"column:refreshToken;type:text" json:"refreshToken"`
	SourceType       SourceType        `gorm:"column:sourceType;type:text;not null;default:'github'" json:"sourceType"`
	CleanCache       *bool             `gorm:"column:cleanCache;default:false" json:"cleanCache"`

	// GitHub
	Repository *string `gorm:"column:repository;type:text" json:"repository"`
	Owner      *string `gorm:"column:owner;type:text" json:"owner"`
	Branch     *string `gorm:"column:branch;type:text" json:"branch"`
	BuildPath  *string `gorm:"column:buildPath;type:text;default:'/'" json:"buildPath"`
	TriggerType *TriggerType `gorm:"column:triggerType;type:text;default:'push'" json:"triggerType"`
	AutoDeploy *bool   `gorm:"column:autoDeploy" json:"autoDeploy"`

	// GitLab
	GitlabProjectID    *int    `gorm:"column:gitlabProjectId" json:"gitlabProjectId"`
	GitlabRepository   *string `gorm:"column:gitlabRepository;type:text" json:"gitlabRepository"`
	GitlabOwner        *string `gorm:"column:gitlabOwner;type:text" json:"gitlabOwner"`
	GitlabBranch       *string `gorm:"column:gitlabBranch;type:text" json:"gitlabBranch"`
	GitlabBuildPath    *string `gorm:"column:gitlabBuildPath;type:text;default:'/'" json:"gitlabBuildPath"`
	GitlabPathNamespace *string `gorm:"column:gitlabPathNamespace;type:text" json:"gitlabPathNamespace"`

	// Gitea
	GiteaRepository *string `gorm:"column:giteaRepository;type:text" json:"giteaRepository"`
	GiteaOwner      *string `gorm:"column:giteaOwner;type:text" json:"giteaOwner"`
	GiteaBranch     *string `gorm:"column:giteaBranch;type:text" json:"giteaBranch"`
	GiteaBuildPath  *string `gorm:"column:giteaBuildPath;type:text;default:'/'" json:"giteaBuildPath"`

	// Bitbucket
	BitbucketRepository     *string `gorm:"column:bitbucketRepository;type:text" json:"bitbucketRepository"`
	BitbucketRepositorySlug *string `gorm:"column:bitbucketRepositorySlug;type:text" json:"bitbucketRepositorySlug"`
	BitbucketOwner          *string `gorm:"column:bitbucketOwner;type:text" json:"bitbucketOwner"`
	BitbucketBranch         *string `gorm:"column:bitbucketBranch;type:text" json:"bitbucketBranch"`
	BitbucketBuildPath      *string `gorm:"column:bitbucketBuildPath;type:text;default:'/'" json:"bitbucketBuildPath"`

	// Docker
	Username    *string `gorm:"column:username;type:text" json:"username"`
	Password    *string `gorm:"column:password;type:text" json:"password"`
	DockerImage *string `gorm:"column:dockerImage;type:text" json:"dockerImage"`
	RegistryURL *string `gorm:"column:registryUrl;type:text" json:"registryUrl"`

	// Git
	CustomGitURL      *string `gorm:"column:customGitUrl;type:text" json:"customGitUrl"`
	CustomGitBranch   *string `gorm:"column:customGitBranch;type:text" json:"customGitBranch"`
	CustomGitBuildPath *string `gorm:"column:customGitBuildPath;type:text" json:"customGitBuildPath"`
	CustomGitSSHKeyID *string `gorm:"column:customGitSSHKeyId;type:text" json:"customGitSSHKeyId"`
	EnableSubmodules  bool    `gorm:"column:enableSubmodules;not null;default:false" json:"enableSubmodules"`

	// Build
	Dockerfile       *string `gorm:"column:dockerfile;type:text;default:'Dockerfile'" json:"dockerfile"`
	DockerContextPath *string `gorm:"column:dockerContextPath;type:text" json:"dockerContextPath"`
	DockerBuildStage *string `gorm:"column:dockerBuildStage;type:text" json:"dockerBuildStage"`
	DropBuildPath    *string `gorm:"column:dropBuildPath;type:text" json:"dropBuildPath"`

	// Swarm JSON fields
	HealthCheckSwarm    *JSONField[HealthCheckSwarm]    `gorm:"column:healthCheckSwarm;type:json" json:"healthCheckSwarm"`
	RestartPolicySwarm  *JSONField[RestartPolicySwarm]  `gorm:"column:restartPolicySwarm;type:json" json:"restartPolicySwarm"`
	PlacementSwarm      *JSONField[PlacementSwarm]      `gorm:"column:placementSwarm;type:json" json:"placementSwarm"`
	UpdateConfigSwarm   *JSONField[UpdateConfigSwarm]   `gorm:"column:updateConfigSwarm;type:json" json:"updateConfigSwarm"`
	RollbackConfigSwarm *JSONField[UpdateConfigSwarm]   `gorm:"column:rollbackConfigSwarm;type:json" json:"rollbackConfigSwarm"`
	ModeSwarm           *JSONField[ServiceModeSwarm]    `gorm:"column:modeSwarm;type:json" json:"modeSwarm"`
	LabelsSwarm         *JSONField[LabelsSwarm]         `gorm:"column:labelsSwarm;type:json" json:"labelsSwarm"`
	NetworkSwarm        *JSONField[[]NetworkSwarm]      `gorm:"column:networkSwarm;type:json" json:"networkSwarm"`
	StopGracePeriodSwarm *int64                         `gorm:"column:stopGracePeriodSwarm;type:bigint" json:"stopGracePeriodSwarm"`
	EndpointSpecSwarm   *JSONField[EndpointSpecSwarm]   `gorm:"column:endpointSpecSwarm;type:json" json:"endpointSpecSwarm"`
	UlimitsSwarm        *JSONField[UlimitsSwarm]        `gorm:"column:ulimitsSwarm;type:json" json:"ulimitsSwarm"`

	// Service config
	Replicas          int               `gorm:"column:replicas;default:1;not null" json:"replicas"`
	ApplicationStatus ApplicationStatus `gorm:"column:applicationStatus;type:text;not null;default:'idle'" json:"applicationStatus"`
	BuildType         BuildType         `gorm:"column:buildType;type:text;not null;default:'nixpacks'" json:"buildType"`
	RailpackVersion   *string           `gorm:"column:railpackVersion;type:text;default:'0.15.4'" json:"railpackVersion"`
	HerokuVersion     *string           `gorm:"column:herokuVersion;type:text;default:'24'" json:"herokuVersion"`
	PublishDirectory  *string           `gorm:"column:publishDirectory;type:text" json:"publishDirectory"`
	IsStaticSpa       *bool             `gorm:"column:isStaticSpa" json:"isStaticSpa"`
	CreateEnvFile     bool              `gorm:"column:createEnvFile;not null;default:true" json:"createEnvFile"`
	CreatedAt         string            `gorm:"column:createdAt;type:text;not null" json:"createdAt"`

	// Foreign keys
	RegistryID         *string `gorm:"column:registryId;type:text" json:"registryId"`
	RollbackRegistryID *string `gorm:"column:rollbackRegistryId;type:text" json:"rollbackRegistryId"`
	EnvironmentID      string  `gorm:"column:environmentId;type:text;not null" json:"environmentId"`
	GithubID           *string `gorm:"column:githubId;type:text" json:"githubId"`
	GitlabID           *string `gorm:"column:gitlabId;type:text" json:"gitlabId"`
	GiteaID            *string `gorm:"column:giteaId;type:text" json:"giteaId"`
	BitbucketID        *string `gorm:"column:bitbucketId;type:text" json:"bitbucketId"`
	ServerID           *string `gorm:"column:serverId;type:text" json:"serverId"`
	BuildServerID      *string `gorm:"column:buildServerId;type:text" json:"buildServerId"`
	BuildRegistryID    *string `gorm:"column:buildRegistryId;type:text" json:"buildRegistryId"`

	// Relations
	Environment        *Environment        `gorm:"foreignKey:EnvironmentID" json:"environment,omitempty"`
	Deployments        []Deployment        `gorm:"foreignKey:ApplicationID" json:"deployments,omitempty"`
	Domains            []Domain            `gorm:"foreignKey:ApplicationID" json:"domains,omitempty"`
	Mounts             []Mount             `gorm:"foreignKey:ApplicationID" json:"mounts,omitempty"`
	Redirects          []Redirect          `gorm:"foreignKey:ApplicationID" json:"redirects,omitempty"`
	Security           []Security          `gorm:"foreignKey:ApplicationID" json:"security,omitempty"`
	Ports              []Port              `gorm:"foreignKey:ApplicationID" json:"ports,omitempty"`
	Registry           *Registry           `gorm:"foreignKey:RegistryID" json:"registry,omitempty"`
	BuildRegistry      *Registry           `gorm:"foreignKey:BuildRegistryID" json:"buildRegistry,omitempty"`
	RollbackRegistry   *Registry           `gorm:"foreignKey:RollbackRegistryID" json:"rollbackRegistry,omitempty"`
	Server             *Server             `gorm:"foreignKey:ServerID" json:"server,omitempty"`
	BuildServer        *Server             `gorm:"foreignKey:BuildServerID" json:"buildServer,omitempty"`
	PreviewDeployments []PreviewDeployment `gorm:"foreignKey:ApplicationID" json:"previewDeployments,omitempty"`

	// Git provider relations
	Github          *Github    `gorm:"foreignKey:GithubID" json:"github,omitempty"`
	Gitlab          *Gitlab    `gorm:"foreignKey:GitlabID" json:"gitlab,omitempty"`
	Gitea           *Gitea     `gorm:"foreignKey:GiteaID" json:"gitea,omitempty"`
	Bitbucket       *Bitbucket `gorm:"foreignKey:BitbucketID" json:"bitbucket,omitempty"`
	CustomGitSSHKey *SSHKey    `gorm:"foreignKey:CustomGitSSHKeyID" json:"customGitSSHKey,omitempty"`
}

func (Application) TableName() string { return "application" }

func (a *Application) BeforeCreate(tx *gorm.DB) error {
	if a.ApplicationID == "" {
		a.ApplicationID, _ = gonanoid.New()
	}
	if a.AppName == "" {
		a.AppName = GenerateAppName("app")
	}
	if a.RefreshToken == nil {
		token, _ := gonanoid.New()
		a.RefreshToken = &token
	}
	if a.CreatedAt == "" {
		a.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	return nil
}

// JSONField is a generic JSON column type for GORM.
type JSONField[T any] struct {
	Data T
}

func (j JSONField[T]) Value() (driver.Value, error) {
	return json.Marshal(j.Data)
}

func (j *JSONField[T]) Scan(value interface{}) error {
	if value == nil {
		return nil
	}
	var bytes []byte
	switch v := value.(type) {
	case []byte:
		bytes = v
	case string:
		bytes = []byte(v)
	default:
		return fmt.Errorf("unsupported type: %T", value)
	}
	return json.Unmarshal(bytes, &j.Data)
}
