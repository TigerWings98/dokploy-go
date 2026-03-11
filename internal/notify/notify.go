// Input: Notification 主表 + 11 子表 (通过 GORM Preload 加载)
// Output: Notifier (Send 多渠道分发, SendTest 测试), 支持 Slack/Telegram/Discord/Email/Resend/Gotify/Ntfy/Custom/Lark/Pushover/Teams
// Role: 多渠道通知分发器，从子表读取渠道配置，根据 NotificationType 选择对应渠道发送通知
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/smtp"
	"strings"
	"time"

	"github.com/dokploy/dokploy/internal/db"
	"github.com/dokploy/dokploy/internal/db/schema"
	"gorm.io/gorm"
)

// EventType represents a notification event type.
type EventType string

const (
	EventAppDeploy       EventType = "app_deploy"
	EventAppBuildError   EventType = "app_build_error"
	EventDatabaseBackup  EventType = "database_backup"
	EventDokployRestart  EventType = "dokploy_restart"
	EventDockerCleanup   EventType = "docker_cleanup"
	EventServerThreshold EventType = "server_threshold"
	EventVolumeBackup    EventType = "volume_backup"
)

// NotificationPayload is the data sent to notification channels.
type NotificationPayload struct {
	Event    EventType `json:"event"`
	Title    string    `json:"title"`
	Message  string    `json:"message"`
	AppName  string    `json:"appName,omitempty"`
	HTMLBody string    `json:"-"` // HTML content for email notifications
}

// Notifier handles sending notifications.
type Notifier struct {
	db *db.DB
}

// NewNotifier creates a new Notifier.
func NewNotifier(database *db.DB) *Notifier {
	return &Notifier{db: database}
}

// preloadAll returns a gorm query with all 11 sub-table preloads.
func (n *Notifier) preloadAll() *gorm.DB {
	return n.db.DB.
		Preload("Slack").
		Preload("Telegram").
		Preload("Discord").
		Preload("Email").
		Preload("Resend").
		Preload("Gotify").
		Preload("Ntfy").
		Preload("Custom").
		Preload("Lark").
		Preload("Pushover").
		Preload("Teams")
}

// Send sends a notification to all enabled channels for the given organization.
func (n *Notifier) Send(orgID string, payload NotificationPayload) {
	var notifications []schema.Notification
	n.preloadAll().Where("\"organizationId\" = ?", orgID).Find(&notifications)

	for _, notif := range notifications {
		if !shouldSend(&notif, payload.Event) {
			continue
		}
		go n.sendToChannel(&notif, payload)
	}
}

// SendTest sends a test notification to a specific channel.
func (n *Notifier) SendTest(notif *schema.Notification) error {
	// 重新加载带子表的完整数据
	var full schema.Notification
	if err := n.preloadAll().First(&full, "\"notificationId\" = ?", notif.NotificationID).Error; err != nil {
		return fmt.Errorf("notification not found: %w", err)
	}
	payload := NotificationPayload{
		Event:   EventDokployRestart,
		Title:   "Test Notification",
		Message: "This is a test notification from Dokploy.",
	}
	return n.sendToChannelSync(&full, payload)
}

func (n *Notifier) sendToChannelSync(notif *schema.Notification, payload NotificationPayload) error {
	switch notif.NotificationType {
	case schema.NotificationTypeSlack:
		return sendSlack(notif, payload)
	case schema.NotificationTypeDiscord:
		return sendDiscord(notif, payload)
	case schema.NotificationTypeTelegram:
		return sendTelegram(notif, payload)
	case schema.NotificationTypeEmail:
		return sendEmail(notif, payload)
	case schema.NotificationTypeResend:
		return sendResend(notif, payload)
	case schema.NotificationTypeGotify:
		return sendGotify(notif, payload)
	case schema.NotificationTypeNtfy:
		return sendNtfy(notif, payload)
	case schema.NotificationTypeCustom:
		return sendCustom(notif, payload)
	case schema.NotificationTypeLark:
		return sendLark(notif, payload)
	case schema.NotificationTypePushover:
		return sendPushover(notif, payload)
	case schema.NotificationTypeTeams:
		return sendTeams(notif, payload)
	default:
		return fmt.Errorf("unsupported notification type: %s", notif.NotificationType)
	}
}

func shouldSend(notif *schema.Notification, event EventType) bool {
	switch event {
	case EventAppDeploy:
		return notif.AppDeploy != nil && *notif.AppDeploy
	case EventAppBuildError:
		return notif.AppBuildError != nil && *notif.AppBuildError
	case EventDatabaseBackup:
		return notif.DatabaseBackup != nil && *notif.DatabaseBackup
	case EventDokployRestart:
		return notif.DokployRestart != nil && *notif.DokployRestart
	case EventDockerCleanup:
		return notif.DockerCleanup != nil && *notif.DockerCleanup
	case EventServerThreshold:
		return notif.ServerThreshold != nil && *notif.ServerThreshold
	case EventVolumeBackup:
		return notif.VolumeBackup != nil && *notif.VolumeBackup
	}
	return false
}

func (n *Notifier) sendToChannel(notif *schema.Notification, payload NotificationPayload) {
	if err := n.sendToChannelSync(notif, payload); err != nil {
		fmt.Printf("Failed to send %s notification: %v\n", notif.NotificationType, err)
	}
}

// ── 各渠道发送实现 ──────────────────────────────────

func sendSlack(notif *schema.Notification, payload NotificationPayload) error {
	if notif.Slack == nil {
		return fmt.Errorf("slack configuration not loaded")
	}
	s := notif.Slack
	body := map[string]interface{}{
		"text": fmt.Sprintf("*%s*\n%s", payload.Title, payload.Message),
	}
	if s.Channel != nil {
		body["channel"] = *s.Channel
	}
	return postJSON(s.WebhookURL, body)
}

func sendDiscord(notif *schema.Notification, payload NotificationPayload) error {
	if notif.Discord == nil {
		return fmt.Errorf("discord configuration not loaded")
	}
	body := map[string]interface{}{
		"embeds": []map[string]interface{}{
			{
				"title":       payload.Title,
				"description": payload.Message,
				"color":       5814783,
			},
		},
	}
	return postJSON(notif.Discord.WebhookURL, body)
}

func sendTelegram(notif *schema.Notification, payload NotificationPayload) error {
	if notif.Telegram == nil {
		return fmt.Errorf("telegram configuration not loaded")
	}
	t := notif.Telegram
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", t.BotToken)
	body := map[string]interface{}{
		"chat_id":    t.ChatID,
		"text":       fmt.Sprintf("<b>%s</b>\n%s", payload.Title, payload.Message),
		"parse_mode": "HTML",
	}
	if t.MessageThreadID != nil {
		body["message_thread_id"] = *t.MessageThreadID
	}
	return postJSON(url, body)
}

func sendEmail(notif *schema.Notification, payload NotificationPayload) error {
	if notif.Email == nil {
		return fmt.Errorf("email configuration not loaded")
	}
	e := notif.Email
	addr := fmt.Sprintf("%s:%d", e.SmtpServer, e.SmtpPort)

	subject := payload.Title
	body := payload.Message
	contentType := "text/plain"
	if payload.HTMLBody != "" {
		body = payload.HTMLBody
		contentType = "text/html"
	}

	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: %s; charset=UTF-8\r\n\r\n%s",
		e.FromAddress,
		strings.Join(e.ToAddresses, ","),
		subject,
		contentType,
		body,
	)

	var auth smtp.Auth
	if e.Username != "" && e.Password != "" {
		auth = smtp.PlainAuth("", e.Username, e.Password, e.SmtpServer)
	}

	return smtp.SendMail(addr, auth, e.FromAddress, e.ToAddresses, []byte(msg))
}

func sendResend(notif *schema.Notification, payload NotificationPayload) error {
	if notif.Resend == nil {
		return fmt.Errorf("resend configuration not loaded")
	}
	r := notif.Resend

	body := payload.Message
	if payload.HTMLBody != "" {
		body = payload.HTMLBody
	}

	reqBody := map[string]interface{}{
		"from":    r.FromAddress,
		"to":      r.ToAddresses,
		"subject": payload.Title,
		"html":    body,
	}

	data, _ := json.Marshal(reqBody)
	req, err := http.NewRequest("POST", "https://api.resend.com/emails", bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+r.APIKey)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("resend API returned status %d", resp.StatusCode)
	}
	return nil
}

func sendGotify(notif *schema.Notification, payload NotificationPayload) error {
	if notif.Gotify == nil {
		return fmt.Errorf("gotify configuration not loaded")
	}
	g := notif.Gotify
	url := fmt.Sprintf("%s/message?token=%s", strings.TrimRight(g.ServerURL, "/"), g.AppToken)
	body := map[string]interface{}{
		"title":    payload.Title,
		"message":  payload.Message,
		"priority": g.Priority,
	}
	return postJSON(url, body)
}

func sendNtfy(notif *schema.Notification, payload NotificationPayload) error {
	if notif.Ntfy == nil {
		return fmt.Errorf("ntfy configuration not loaded")
	}
	n := notif.Ntfy
	url := fmt.Sprintf("%s/%s", strings.TrimRight(n.ServerURL, "/"), n.Topic)

	req, err := http.NewRequest("POST", url, strings.NewReader(payload.Message))
	if err != nil {
		return err
	}
	req.Header.Set("Title", payload.Title)
	req.Header.Set("Priority", fmt.Sprintf("%d", n.Priority))
	if n.AccessToken != nil {
		req.Header.Set("Authorization", "Bearer "+*n.AccessToken)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("ntfy returned status %d", resp.StatusCode)
	}
	return nil
}

func sendCustom(notif *schema.Notification, payload NotificationPayload) error {
	if notif.Custom == nil {
		return fmt.Errorf("custom webhook configuration not loaded")
	}
	c := notif.Custom

	data, _ := json.Marshal(payload)
	req, err := http.NewRequest("POST", c.Endpoint, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	// 应用自定义 headers
	if c.Headers != nil {
		if headers, ok := c.Headers.Data.(map[string]interface{}); ok {
			for k, v := range headers {
				if sv, ok := v.(string); ok {
					req.Header.Set(k, sv)
				}
			}
		}
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("custom webhook returned status %d", resp.StatusCode)
	}
	return nil
}

func sendLark(notif *schema.Notification, payload NotificationPayload) error {
	if notif.Lark == nil {
		return fmt.Errorf("lark configuration not loaded")
	}
	body := map[string]interface{}{
		"msg_type": "text",
		"content": map[string]string{
			"text": fmt.Sprintf("%s\n%s", payload.Title, payload.Message),
		},
	}
	return postJSON(notif.Lark.WebhookURL, body)
}

func sendPushover(notif *schema.Notification, payload NotificationPayload) error {
	if notif.Pushover == nil {
		return fmt.Errorf("pushover configuration not loaded")
	}
	p := notif.Pushover
	body := map[string]interface{}{
		"token":    p.APIToken,
		"user":     p.UserKey,
		"title":    payload.Title,
		"message":  payload.Message,
		"priority": p.Priority,
	}
	if p.Priority == 2 {
		if p.Retry != nil {
			body["retry"] = *p.Retry
		}
		if p.Expire != nil {
			body["expire"] = *p.Expire
		}
	}
	return postJSON("https://api.pushover.net/1/messages.json", body)
}

func sendTeams(notif *schema.Notification, payload NotificationPayload) error {
	if notif.Teams == nil {
		return fmt.Errorf("teams configuration not loaded")
	}
	body := map[string]interface{}{
		"@type":      "MessageCard",
		"@context":   "http://schema.org/extensions",
		"summary":    payload.Title,
		"themeColor": "0076D7",
		"title":      payload.Title,
		"sections": []map[string]interface{}{
			{
				"activityTitle": "Dokploy Notification",
				"text":          payload.Message,
			},
		},
	}
	return postJSON(notif.Teams.WebhookURL, body)
}

// SendEmailToRecipient 通过指定通知渠道的 Email/Resend 配置发送邮件到指定收件人
// 用于邀请邮件等场景，覆盖通知原有的 toAddresses
func (n *Notifier) SendEmailToRecipient(notificationID string, toEmail string, subject string, htmlContent string) error {
	var notif schema.Notification
	if err := n.preloadAll().First(&notif, "\"notificationId\" = ?", notificationID).Error; err != nil {
		return fmt.Errorf("notification not found: %w", err)
	}

	payload := NotificationPayload{
		Title:    subject,
		Message:  htmlContent,
		HTMLBody: htmlContent,
	}

	if notif.Email != nil {
		// 临时覆盖收件人
		origAddrs := notif.Email.ToAddresses
		notif.Email.ToAddresses = []string{toEmail}
		err := sendEmail(&notif, payload)
		notif.Email.ToAddresses = origAddrs
		return err
	}

	if notif.Resend != nil {
		origAddrs := notif.Resend.ToAddresses
		notif.Resend.ToAddresses = []string{toEmail}
		err := sendResend(&notif, payload)
		notif.Resend.ToAddresses = origAddrs
		return err
	}

	return fmt.Errorf("notification has no email or resend provider configured")
}

func postJSON(url string, body interface{}) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}
	return nil
}
