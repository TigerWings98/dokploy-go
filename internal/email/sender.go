// Input: SMTP 配置 (server/port/username/password), 邮件内容 (to/subject/body)
// Output: SendEmail (SMTP 邮件发送), SendHTMLEmail (HTML 格式邮件发送)
// Role: SMTP 邮件发送器，支持纯文本和 HTML 格式邮件，用于通知渠道的邮件投递
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package email

import (
	"fmt"
	"net/smtp"
	"os"
	"strconv"
)

// SMTPConfig holds SMTP settings from environment variables.
type SMTPConfig struct {
	Server      string
	Port        int
	Username    string
	Password    string
	FromAddress string
}

// LoadSMTPConfig loads SMTP configuration from environment variables.
func LoadSMTPConfig() *SMTPConfig {
	port, _ := strconv.Atoi(os.Getenv("SMTP_PORT"))
	if port == 0 {
		port = 587
	}
	return &SMTPConfig{
		Server:      os.Getenv("SMTP_SERVER"),
		Port:        port,
		Username:    os.Getenv("SMTP_USERNAME"),
		Password:    os.Getenv("SMTP_PASSWORD"),
		FromAddress: os.Getenv("SMTP_FROM_ADDRESS"),
	}
}

// IsConfigured returns true if SMTP settings are available.
func (c *SMTPConfig) IsConfigured() bool {
	return c.Server != "" && c.FromAddress != ""
}

// SendHTML sends an HTML email using the SMTP configuration.
func (c *SMTPConfig) SendHTML(to, subject, htmlBody string) error {
	if !c.IsConfigured() {
		return fmt.Errorf("SMTP not configured")
	}

	addr := fmt.Sprintf("%s:%d", c.Server, c.Port)

	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n%s",
		c.FromAddress, to, subject, htmlBody)

	var auth smtp.Auth
	if c.Username != "" && c.Password != "" {
		auth = smtp.PlainAuth("", c.Username, c.Password, c.Server)
	}

	return smtp.SendMail(addr, auth, c.FromAddress, []string{to}, []byte(msg))
}

// SendVerificationEmail sends a verification email with the given link.
func SendVerificationEmail(toEmail, subject, htmlContent string) error {
	cfg := LoadSMTPConfig()
	return cfg.SendHTML(toEmail, subject, htmlContent)
}

// SendInvitationEmail sends an invitation email to join Dokploy.
func SendInvitationEmail(toEmail, inviteLink string) error {
	html, err := RenderInvitation(InvitationData{
		InviteLink: inviteLink,
		ToEmail:    toEmail,
	})
	if err != nil {
		return fmt.Errorf("failed to render invitation email: %w", err)
	}
	return SendVerificationEmail(toEmail, "Join Dokploy", html)
}
