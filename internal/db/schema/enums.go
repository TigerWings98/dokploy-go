package schema

// ApplicationStatus represents the status of an application/service.
type ApplicationStatus string

const (
	ApplicationStatusIdle    ApplicationStatus = "idle"
	ApplicationStatusRunning ApplicationStatus = "running"
	ApplicationStatusDone    ApplicationStatus = "done"
	ApplicationStatusError   ApplicationStatus = "error"
)

// CertificateType represents the type of SSL certificate.
type CertificateType string

const (
	CertificateTypeLetsencrypt CertificateType = "letsencrypt"
	CertificateTypeNone        CertificateType = "none"
	CertificateTypeCustom      CertificateType = "custom"
)

// TriggerType represents how deployments are triggered.
type TriggerType string

const (
	TriggerTypePush TriggerType = "push"
	TriggerTypeTag  TriggerType = "tag"
)

// SourceType represents the source of application code.
type SourceType string

const (
	SourceTypeDocker    SourceType = "docker"
	SourceTypeGit       SourceType = "git"
	SourceTypeGithub    SourceType = "github"
	SourceTypeGitlab    SourceType = "gitlab"
	SourceTypeBitbucket SourceType = "bitbucket"
	SourceTypeGitea     SourceType = "gitea"
	SourceTypeDrop      SourceType = "drop"
)

// BuildType represents the build method.
type BuildType string

const (
	BuildTypeDockerfile        BuildType = "dockerfile"
	BuildTypeHerokuBuildpacks  BuildType = "heroku_buildpacks"
	BuildTypePaketoBuildpacks  BuildType = "paketo_buildpacks"
	BuildTypeNixpacks          BuildType = "nixpacks"
	BuildTypeStatic            BuildType = "static"
	BuildTypeRailpack          BuildType = "railpack"
)

// DeploymentStatus represents the status of a deployment.
type DeploymentStatus string

const (
	DeploymentStatusRunning   DeploymentStatus = "running"
	DeploymentStatusDone      DeploymentStatus = "done"
	DeploymentStatusError     DeploymentStatus = "error"
	DeploymentStatusCancelled DeploymentStatus = "cancelled"
)

// ServerStatus represents the status of a server.
type ServerStatus string

const (
	ServerStatusActive   ServerStatus = "active"
	ServerStatusInactive ServerStatus = "inactive"
)

// ServerType represents the type of server.
type ServerType string

const (
	ServerTypeDeploy ServerType = "deploy"
	ServerTypeBuild  ServerType = "build"
)

// ComposeType represents the type of compose deployment.
type ComposeType string

const (
	ComposeTypeDocker ComposeType = "docker-compose"
	ComposeTypeStack  ComposeType = "stack"
)

// SourceTypeCompose represents compose source types.
type SourceTypeCompose string

const (
	SourceTypeComposeDocker    SourceTypeCompose = "docker"
	SourceTypeComposeGit       SourceTypeCompose = "git"
	SourceTypeComposeGithub    SourceTypeCompose = "github"
	SourceTypeComposeGitlab    SourceTypeCompose = "gitlab"
	SourceTypeComposeBitbucket SourceTypeCompose = "bitbucket"
	SourceTypeComposeGitea     SourceTypeCompose = "gitea"
	SourceTypeComposeRaw       SourceTypeCompose = "raw"
)

// DomainType represents domain configuration type.
type DomainType string

const (
	DomainTypeApplication      DomainType = "application"
	DomainTypeCompose          DomainType = "compose"
	DomainTypePreviewDeployment DomainType = "preview"
)

// MountType represents the type of mount.
type MountType string

const (
	MountTypeBind   MountType = "bind"
	MountTypeVolume MountType = "volume"
	MountTypeFile   MountType = "file"
)

// ProtocolType for ports.
type ProtocolType string

const (
	ProtocolTypeTCP ProtocolType = "tcp"
	ProtocolTypeUDP ProtocolType = "udp"
)

// DatabaseType represents a database service type.
type DatabaseType string

const (
	DatabaseTypePostgres DatabaseType = "postgres"
	DatabaseTypeMySQL    DatabaseType = "mysql"
	DatabaseTypeMariaDB  DatabaseType = "mariadb"
	DatabaseTypeMongo    DatabaseType = "mongo"
	DatabaseTypeRedis    DatabaseType = "redis"
)

// DestinationType for backups.
type DestinationType string

const (
	DestinationTypeS3 DestinationType = "s3"
)

// NotificationType represents notification channels.
type NotificationType string

const (
	NotificationTypeSlack     NotificationType = "slack"
	NotificationTypeTelegram  NotificationType = "telegram"
	NotificationTypeDiscord   NotificationType = "discord"
	NotificationTypeEmail     NotificationType = "email"
	NotificationTypeGotify    NotificationType = "gotify"
	NotificationTypeNtfy      NotificationType = "ntfy"
	NotificationTypePushover  NotificationType = "pushover"
	NotificationTypeWebhook   NotificationType = "webhook"
	NotificationTypeDingtalk  NotificationType = "dingtalk"
	NotificationTypeFeishu    NotificationType = "feishu"
	NotificationTypeMatrix    NotificationType = "matrix"
	NotificationTypeMattermost NotificationType = "mattermost"
)

// MemberRole represents a member's role in an organization.
type MemberRole string

const (
	MemberRoleOwner  MemberRole = "owner"
	MemberRoleMember MemberRole = "member"
	MemberRoleAdmin  MemberRole = "admin"
)

// RegistryType represents a container registry type.
type RegistryType string

const (
	RegistryTypeSelfHosted RegistryType = "selfHosted"
	RegistryTypeCloud      RegistryType = "cloud"
)

// ScheduleType represents the type of scheduled job.
type ScheduleType string

const (
	ScheduleTypeApp     ScheduleType = "app"
	ScheduleTypeCompose ScheduleType = "compose"
	ScheduleTypeServer  ScheduleType = "server"
)

// GitProviderType represents the git provider type.
type GitProviderType string

const (
	GitProviderTypeGithub    GitProviderType = "github"
	GitProviderTypeGitlab    GitProviderType = "gitlab"
	GitProviderTypeBitbucket GitProviderType = "bitbucket"
	GitProviderTypeGitea     GitProviderType = "gitea"
)
