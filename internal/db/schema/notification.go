// Input: gorm, go-nanoid, pq (StringArray)
// Output: Notification 主表 + 11 个通知渠道子表 (Slack/Telegram/Discord/Email/Resend/Gotify/Ntfy/Custom/Lark/Pushover/Teams)
// Role: 通知配置数据模型，完全对齐 TS 版 Drizzle schema 的 11 子表 + FK 引用架构
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package schema

import (
	"time"

	gonanoid "github.com/matoous/go-nanoid/v2"
	"gorm.io/gorm"
)

// ── 主表 ──────────────────────────────────────────────

// Notification represents the notification table.
type Notification struct {
	NotificationID   string           `gorm:"column:notificationId;primaryKey;type:text" json:"notificationId"`
	Name             string           `gorm:"column:name;type:text;not null" json:"name"`
	NotificationType NotificationType `gorm:"column:notificationType;type:text;not null" json:"notificationType"`
	CreatedAt        string           `gorm:"column:createdAt;type:text;not null" json:"createdAt"`
	OrganizationID   string           `gorm:"column:organizationId;type:text;not null" json:"organizationId"`

	// 事件开关 (default false)
	AppDeploy       *bool `gorm:"column:appDeploy;default:false" json:"appDeploy"`
	AppBuildError   *bool `gorm:"column:appBuildError;default:false" json:"appBuildError"`
	DatabaseBackup  *bool `gorm:"column:databaseBackup;default:false" json:"databaseBackup"`
	DokployRestart  *bool `gorm:"column:dokployRestart;default:false" json:"dokployRestart"`
	DockerCleanup   *bool `gorm:"column:dockerCleanup;default:false" json:"dockerCleanup"`
	ServerThreshold *bool `gorm:"column:serverThreshold;default:false" json:"serverThreshold"`
	VolumeBackup    *bool `gorm:"column:volumeBackup;default:false" json:"volumeBackup"`

	// FK 引用 11 个子表 (nullable, CASCADE delete)
	SlackID    *string `gorm:"column:slackId;type:text" json:"slackId"`
	TelegramID *string `gorm:"column:telegramId;type:text" json:"telegramId"`
	DiscordID  *string `gorm:"column:discordId;type:text" json:"discordId"`
	EmailID    *string `gorm:"column:emailId;type:text" json:"emailId"`
	ResendID   *string `gorm:"column:resendId;type:text" json:"resendId"`
	GotifyID   *string `gorm:"column:gotifyId;type:text" json:"gotifyId"`
	NtfyID     *string `gorm:"column:ntfyId;type:text" json:"ntfyId"`
	CustomID   *string `gorm:"column:customId;type:text" json:"customId"`
	LarkID     *string `gorm:"column:larkId;type:text" json:"larkId"`
	PushoverID *string `gorm:"column:pushoverId;type:text" json:"pushoverId"`
	TeamsID    *string `gorm:"column:teamsId;type:text" json:"teamsId"`

	// GORM 关联 (Preload 用)
	Slack        *NotifSlack    `gorm:"foreignKey:SlackID;references:SlackID" json:"slack,omitempty"`
	Telegram     *NotifTelegram `gorm:"foreignKey:TelegramID;references:TelegramID" json:"telegram,omitempty"`
	Discord      *NotifDiscord  `gorm:"foreignKey:DiscordID;references:DiscordID" json:"discord,omitempty"`
	Email        *NotifEmail    `gorm:"foreignKey:EmailID;references:EmailID" json:"email,omitempty"`
	Resend       *NotifResend   `gorm:"foreignKey:ResendID;references:ResendID" json:"resend,omitempty"`
	Gotify       *NotifGotify   `gorm:"foreignKey:GotifyID;references:GotifyID" json:"gotify,omitempty"`
	Ntfy         *NotifNtfy     `gorm:"foreignKey:NtfyID;references:NtfyID" json:"ntfy,omitempty"`
	Custom       *NotifCustom   `gorm:"foreignKey:CustomID;references:CustomID" json:"custom,omitempty"`
	Lark         *NotifLark     `gorm:"foreignKey:LarkID;references:LarkID" json:"lark,omitempty"`
	Pushover     *NotifPushover `gorm:"foreignKey:PushoverID;references:PushoverID" json:"pushover,omitempty"`
	Teams        *NotifTeams    `gorm:"foreignKey:TeamsID;references:TeamsID" json:"teams,omitempty"`
	Organization *Organization  `gorm:"foreignKey:OrganizationID" json:"organization,omitempty"`
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

// ── 11 个子表 ─────────────────────────────────────────

// NotifSlack represents the slack table.
type NotifSlack struct {
	SlackID    string  `gorm:"column:slackId;primaryKey;type:text" json:"slackId"`
	WebhookURL string  `gorm:"column:webhookUrl;type:text;not null" json:"webhookUrl"`
	Channel    *string `gorm:"column:channel;type:text" json:"channel"`
}

func (NotifSlack) TableName() string { return "slack" }

func (s *NotifSlack) BeforeCreate(tx *gorm.DB) error {
	if s.SlackID == "" {
		s.SlackID, _ = gonanoid.New()
	}
	return nil
}

// NotifTelegram represents the telegram table.
type NotifTelegram struct {
	TelegramID      string  `gorm:"column:telegramId;primaryKey;type:text" json:"telegramId"`
	BotToken        string  `gorm:"column:botToken;type:text;not null" json:"botToken"`
	ChatID          string  `gorm:"column:chatId;type:text;not null" json:"chatId"`
	MessageThreadID *string `gorm:"column:messageThreadId;type:text" json:"messageThreadId"`
}

func (NotifTelegram) TableName() string { return "telegram" }

func (t *NotifTelegram) BeforeCreate(tx *gorm.DB) error {
	if t.TelegramID == "" {
		t.TelegramID, _ = gonanoid.New()
	}
	return nil
}

// NotifDiscord represents the discord table.
type NotifDiscord struct {
	DiscordID  string `gorm:"column:discordId;primaryKey;type:text" json:"discordId"`
	WebhookURL string `gorm:"column:webhookUrl;type:text;not null" json:"webhookUrl"`
	Decoration *bool  `gorm:"column:decoration" json:"decoration"`
}

func (NotifDiscord) TableName() string { return "discord" }

func (d *NotifDiscord) BeforeCreate(tx *gorm.DB) error {
	if d.DiscordID == "" {
		d.DiscordID, _ = gonanoid.New()
	}
	return nil
}

// NotifEmail represents the email table.
type NotifEmail struct {
	EmailID     string      `gorm:"column:emailId;primaryKey;type:text" json:"emailId"`
	SmtpServer  string      `gorm:"column:smtpServer;type:text;not null" json:"smtpServer"`
	SmtpPort    int         `gorm:"column:smtpPort;not null" json:"smtpPort"`
	Username    string      `gorm:"column:username;type:text;not null" json:"username"`
	Password    string      `gorm:"column:password;type:text;not null" json:"password"`
	FromAddress string      `gorm:"column:fromAddress;type:text;not null" json:"fromAddress"`
	ToAddresses StringArray `gorm:"column:toAddress;type:text[];not null" json:"toAddress"`
}

func (NotifEmail) TableName() string { return "email" }

func (e *NotifEmail) BeforeCreate(tx *gorm.DB) error {
	if e.EmailID == "" {
		e.EmailID, _ = gonanoid.New()
	}
	return nil
}

// NotifResend represents the resend table.
type NotifResend struct {
	ResendID    string      `gorm:"column:resendId;primaryKey;type:text" json:"resendId"`
	APIKey      string      `gorm:"column:apiKey;type:text;not null" json:"apiKey"`
	FromAddress string      `gorm:"column:fromAddress;type:text;not null" json:"fromAddress"`
	ToAddresses StringArray `gorm:"column:toAddress;type:text[];not null" json:"toAddress"`
}

func (NotifResend) TableName() string { return "resend" }

func (r *NotifResend) BeforeCreate(tx *gorm.DB) error {
	if r.ResendID == "" {
		r.ResendID, _ = gonanoid.New()
	}
	return nil
}

// NotifGotify represents the gotify table.
type NotifGotify struct {
	GotifyID   string `gorm:"column:gotifyId;primaryKey;type:text" json:"gotifyId"`
	ServerURL  string `gorm:"column:serverUrl;type:text;not null" json:"serverUrl"`
	AppToken   string `gorm:"column:appToken;type:text;not null" json:"appToken"`
	Priority   int    `gorm:"column:priority;not null;default:5" json:"priority"`
	Decoration *bool  `gorm:"column:decoration" json:"decoration"`
}

func (NotifGotify) TableName() string { return "gotify" }

func (g *NotifGotify) BeforeCreate(tx *gorm.DB) error {
	if g.GotifyID == "" {
		g.GotifyID, _ = gonanoid.New()
	}
	return nil
}

// NotifNtfy represents the ntfy table.
type NotifNtfy struct {
	NtfyID      string  `gorm:"column:ntfyId;primaryKey;type:text" json:"ntfyId"`
	ServerURL   string  `gorm:"column:serverUrl;type:text;not null" json:"serverUrl"`
	Topic       string  `gorm:"column:topic;type:text;not null" json:"topic"`
	AccessToken *string `gorm:"column:accessToken;type:text" json:"accessToken"`
	Priority    int     `gorm:"column:priority;not null;default:3" json:"priority"`
}

func (NotifNtfy) TableName() string { return "ntfy" }

func (n *NotifNtfy) BeforeCreate(tx *gorm.DB) error {
	if n.NtfyID == "" {
		n.NtfyID, _ = gonanoid.New()
	}
	return nil
}

// NotifCustom represents the custom table.
type NotifCustom struct {
	CustomID string          `gorm:"column:customId;primaryKey;type:text" json:"customId"`
	Endpoint string          `gorm:"column:endpoint;type:text;not null" json:"endpoint"`
	Headers  *JSONField[any] `gorm:"column:headers;type:jsonb" json:"headers"`
}

func (NotifCustom) TableName() string { return "custom" }

func (c *NotifCustom) BeforeCreate(tx *gorm.DB) error {
	if c.CustomID == "" {
		c.CustomID, _ = gonanoid.New()
	}
	return nil
}

// NotifLark represents the lark table.
type NotifLark struct {
	LarkID     string `gorm:"column:larkId;primaryKey;type:text" json:"larkId"`
	WebhookURL string `gorm:"column:webhookUrl;type:text;not null" json:"webhookUrl"`
}

func (NotifLark) TableName() string { return "lark" }

func (l *NotifLark) BeforeCreate(tx *gorm.DB) error {
	if l.LarkID == "" {
		l.LarkID, _ = gonanoid.New()
	}
	return nil
}

// NotifPushover represents the pushover table.
type NotifPushover struct {
	PushoverID string `gorm:"column:pushoverId;primaryKey;type:text" json:"pushoverId"`
	UserKey    string `gorm:"column:userKey;type:text;not null" json:"userKey"`
	APIToken   string `gorm:"column:apiToken;type:text;not null" json:"apiToken"`
	Priority   int    `gorm:"column:priority;not null;default:0" json:"priority"`
	Retry      *int   `gorm:"column:retry" json:"retry"`
	Expire     *int   `gorm:"column:expire" json:"expire"`
}

func (NotifPushover) TableName() string { return "pushover" }

func (p *NotifPushover) BeforeCreate(tx *gorm.DB) error {
	if p.PushoverID == "" {
		p.PushoverID, _ = gonanoid.New()
	}
	return nil
}

// NotifTeams represents the teams table.
type NotifTeams struct {
	TeamsID    string `gorm:"column:teamsId;primaryKey;type:text" json:"teamsId"`
	WebhookURL string `gorm:"column:webhookUrl;type:text;not null" json:"webhookUrl"`
}

func (NotifTeams) TableName() string { return "teams" }

func (t *NotifTeams) BeforeCreate(tx *gorm.DB) error {
	if t.TeamsID == "" {
		t.TeamsID, _ = gonanoid.New()
	}
	return nil
}
