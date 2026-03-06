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
