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

// Send sends a notification to all enabled channels for the given organization.
func (n *Notifier) Send(orgID string, payload NotificationPayload) {
	var notifications []schema.Notification
	n.db.Where("\"organizationId\" = ? AND enabled = ?", orgID, true).Find(&notifications)

	for _, notif := range notifications {
		if !shouldSend(&notif, payload.Event) {
			continue
		}
		go n.sendToChannel(&notif, payload)
	}
}

// SendTest sends a test notification to a specific channel.
func (n *Notifier) SendTest(notif *schema.Notification) error {
	payload := NotificationPayload{
		Event:   EventDokployRestart,
		Title:   "Test Notification",
		Message: "This is a test notification from Dokploy.",
	}
	return n.sendToChannelSync(notif, payload)
}

func (n *Notifier) sendToChannelSync(notif *schema.Notification, payload NotificationPayload) error {
	switch notif.Type {
	case schema.NotificationTypeSlack:
		return sendSlack(notif, payload)
	case schema.NotificationTypeDiscord:
		return sendDiscord(notif, payload)
	case schema.NotificationTypeTelegram:
		return sendTelegram(notif, payload)
	case schema.NotificationTypeWebhook:
		return sendWebhook(notif, payload)
	case schema.NotificationTypeEmail:
		return sendEmail(notif, payload)
	case schema.NotificationTypeGotify:
		return sendGotify(notif, payload)
	case schema.NotificationTypeNtfy:
		return sendNtfy(notif, payload)
	default:
		return fmt.Errorf("unsupported notification type: %s", notif.Type)
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
	var err error
	switch notif.Type {
	case schema.NotificationTypeSlack:
		err = sendSlack(notif, payload)
	case schema.NotificationTypeDiscord:
		err = sendDiscord(notif, payload)
	case schema.NotificationTypeTelegram:
		err = sendTelegram(notif, payload)
	case schema.NotificationTypeWebhook:
		err = sendWebhook(notif, payload)
	case schema.NotificationTypeEmail:
		err = sendEmail(notif, payload)
	case schema.NotificationTypeGotify:
		err = sendGotify(notif, payload)
	case schema.NotificationTypeNtfy:
		err = sendNtfy(notif, payload)
	default:
		// Unsupported channel
		return
	}
	if err != nil {
		fmt.Printf("Failed to send %s notification: %v\n", notif.Type, err)
	}
}

func sendSlack(notif *schema.Notification, payload NotificationPayload) error {
	if notif.SlackWebhookURL == nil {
		return fmt.Errorf("slack webhook URL not configured")
	}
	body := map[string]interface{}{
		"text": fmt.Sprintf("*%s*\n%s", payload.Title, payload.Message),
	}
	if notif.SlackChannel != nil {
		body["channel"] = *notif.SlackChannel
	}
	return postJSON(*notif.SlackWebhookURL, body)
}

func sendDiscord(notif *schema.Notification, payload NotificationPayload) error {
	if notif.DiscordWebhookURL == nil {
		return fmt.Errorf("discord webhook URL not configured")
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
	return postJSON(*notif.DiscordWebhookURL, body)
}

func sendTelegram(notif *schema.Notification, payload NotificationPayload) error {
	if notif.TelegramBotToken == nil || notif.TelegramChatID == nil {
		return fmt.Errorf("telegram bot token or chat ID not configured")
	}
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", *notif.TelegramBotToken)
	body := map[string]interface{}{
		"chat_id":    *notif.TelegramChatID,
		"text":       fmt.Sprintf("<b>%s</b>\n%s", payload.Title, payload.Message),
		"parse_mode": "HTML",
	}
	return postJSON(url, body)
}

func sendWebhook(notif *schema.Notification, payload NotificationPayload) error {
	if notif.WebhookURL == nil {
		return fmt.Errorf("webhook URL not configured")
	}
	return postJSON(*notif.WebhookURL, payload)
}

func sendEmail(notif *schema.Notification, payload NotificationPayload) error {
	if notif.SmtpServer == nil || notif.SmtpFromAddress == nil || len(notif.SmtpToAddress) == 0 {
		return fmt.Errorf("SMTP configuration incomplete")
	}

	port := 587
	if notif.SmtpPort != nil {
		port = *notif.SmtpPort
	}

	addr := fmt.Sprintf("%s:%d", *notif.SmtpServer, port)

	subject := payload.Title
	body := payload.Message
	contentType := "text/plain"
	if payload.HTMLBody != "" {
		body = payload.HTMLBody
		contentType = "text/html"
	}

	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: %s; charset=UTF-8\r\n\r\n%s",
		*notif.SmtpFromAddress,
		strings.Join(notif.SmtpToAddress, ","),
		subject,
		contentType,
		body,
	)

	var auth smtp.Auth
	if notif.SmtpUsername != nil && notif.SmtpPassword != nil {
		auth = smtp.PlainAuth("", *notif.SmtpUsername, *notif.SmtpPassword, *notif.SmtpServer)
	}

	return smtp.SendMail(addr, auth, *notif.SmtpFromAddress, notif.SmtpToAddress, []byte(msg))
}

func sendGotify(notif *schema.Notification, payload NotificationPayload) error {
	if notif.GotifyURL == nil || notif.GotifyToken == nil {
		return fmt.Errorf("gotify URL or token not configured")
	}
	url := fmt.Sprintf("%s/message?token=%s", strings.TrimRight(*notif.GotifyURL, "/"), *notif.GotifyToken)
	body := map[string]interface{}{
		"title":    payload.Title,
		"message":  payload.Message,
		"priority": 5,
	}
	return postJSON(url, body)
}

func sendNtfy(notif *schema.Notification, payload NotificationPayload) error {
	if notif.NtfyURL == nil || notif.NtfyTopic == nil {
		return fmt.Errorf("ntfy URL or topic not configured")
	}
	url := fmt.Sprintf("%s/%s", strings.TrimRight(*notif.NtfyURL, "/"), *notif.NtfyTopic)

	req, err := http.NewRequest("POST", url, strings.NewReader(payload.Message))
	if err != nil {
		return err
	}
	req.Header.Set("Title", payload.Title)

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
