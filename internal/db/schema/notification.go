// Input: gorm, go-nanoid
// Output: Notification struct (含扁平化的多渠道通知字段：Slack/Telegram/Discord/Email/Gotify/Ntfy 等)
// Role: 通知配置数据表模型，⚠️ 与 TS 版架构差异大（TS 用 11 个子表，Go 扁平化到主表）
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package schema

import (
	"time"

	gonanoid "github.com/matoous/go-nanoid/v2"
	"gorm.io/gorm"
)

// Notification represents the notification table.
type Notification struct {
	NotificationID string           `gorm:"column:notificationId;primaryKey;type:text" json:"notificationId"`
	Name           string           `gorm:"column:name;type:text;not null" json:"name"`
	Type           NotificationType `gorm:"column:type;type:text;not null" json:"type"`
	Enabled        bool             `gorm:"column:enabled;not null;default:true" json:"enabled"`
	CreatedAt      string           `gorm:"column:createdAt;type:text;not null" json:"createdAt"`
	OrganizationID string           `gorm:"column:organizationId;type:text;not null" json:"organizationId"`

	// Type-specific fields
	AppDeploy         *bool `gorm:"column:appDeploy" json:"appDeploy"`
	AppBuildError     *bool `gorm:"column:appBuildError" json:"appBuildError"`
	DatabaseBackup    *bool `gorm:"column:databaseBackup" json:"databaseBackup"`
	DokployRestart    *bool `gorm:"column:dokployRestart" json:"dokployRestart"`
	DockerCleanup     *bool `gorm:"column:dockerCleanup" json:"dockerCleanup"`
	ServerThreshold   *bool `gorm:"column:serverThreshold" json:"serverThreshold"`
	VolumeBackup      *bool `gorm:"column:volumeBackup" json:"volumeBackup"`

	// Slack
	SlackWebhookURL *string `gorm:"column:slackWebhookUrl;type:text" json:"slackWebhookUrl"`
	SlackChannel    *string `gorm:"column:slackChannel;type:text" json:"slackChannel"`

	// Telegram
	TelegramBotToken *string `gorm:"column:telegramBotToken;type:text" json:"telegramBotToken"`
	TelegramChatID   *string `gorm:"column:telegramChatId;type:text" json:"telegramChatId"`

	// Discord
	DiscordWebhookURL *string `gorm:"column:discordWebhookUrl;type:text" json:"discordWebhookUrl"`

	// Email
	SmtpServer   *string `gorm:"column:smtpServer;type:text" json:"smtpServer"`
	SmtpPort     *int    `gorm:"column:smtpPort" json:"smtpPort"`
	SmtpUsername *string `gorm:"column:smtpUsername;type:text" json:"smtpUsername"`
	SmtpPassword *string `gorm:"column:smtpPassword;type:text" json:"smtpPassword"`
	SmtpFromAddress *string `gorm:"column:smtpFromAddress;type:text" json:"smtpFromAddress"`
	SmtpToAddress StringArray `gorm:"column:smtpToAddress;type:text[]" json:"smtpToAddress"`

	// Gotify
	GotifyURL   *string `gorm:"column:gotifyUrl;type:text" json:"gotifyUrl"`
	GotifyToken *string `gorm:"column:gotifyToken;type:text" json:"gotifyToken"`

	// Ntfy
	NtfyURL   *string `gorm:"column:ntfyUrl;type:text" json:"ntfyUrl"`
	NtfyTopic *string `gorm:"column:ntfyTopic;type:text" json:"ntfyTopic"`

	// Webhook
	WebhookURL *string `gorm:"column:webhookUrl;type:text" json:"webhookUrl"`

	Organization *Organization `gorm:"foreignKey:OrganizationID" json:"organization,omitempty"`
}

func (Notification) TableName() string { return "notification" }

func (n *Notification) BeforeCreate(tx *gorm.DB) error {
	if n.NotificationID == "" {
		n.NotificationID, _ = gonanoid.New()
	}
	if n.CreatedAt == "" {
		n.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	return nil
}
