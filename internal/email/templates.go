package email

import (
	"bytes"
	"fmt"
	"html/template"
	"time"
)

const brandColor = "#007291"

// BuildSuccessData holds data for the build success email template.
type BuildSuccessData struct {
	ProjectName     string
	ApplicationName string
	ApplicationType string
	BuildLink       string
	Date            string
	EnvironmentName string
}

// BuildFailedData holds data for the build failed email template.
type BuildFailedData struct {
	ProjectName     string
	ApplicationName string
	ApplicationType string
	BuildLink       string
	Date            string
	EnvironmentName string
	ErrorMessage    string
}

// DatabaseBackupData holds data for the database backup email template.
type DatabaseBackupData struct {
	ProjectName     string
	ApplicationName string
	DatabaseType    string
	Type            string // "success" or "error"
	ErrorMessage    string
	Date            string
}

// DockerCleanupData holds data for the docker cleanup email template.
type DockerCleanupData struct {
	Date string
}

// ServerRestartData holds data for the server restart email template.
type ServerRestartData struct {
	Date string
}

// VolumeBackupData holds data for the volume backup email template.
type VolumeBackupData struct {
	ProjectName     string
	ApplicationName string
	VolumeName      string
	ServiceType     string
	Type            string // "success" or "error"
	ErrorMessage    string
	BackupSize      string
	Date            string
}

// InvitationData holds data for the invitation email template.
type InvitationData struct {
	InviteLink string
	ToEmail    string
}

// RenderBuildSuccess renders the build success email to HTML.
func RenderBuildSuccess(data BuildSuccessData) (string, error) {
	if data.Date == "" {
		data.Date = time.Now().UTC().Format(time.RFC3339)
	}
	return renderTemplate("Build Successful", buildSuccessTmpl, data)
}

// RenderBuildFailed renders the build failed email to HTML.
func RenderBuildFailed(data BuildFailedData) (string, error) {
	if data.Date == "" {
		data.Date = time.Now().UTC().Format(time.RFC3339)
	}
	return renderTemplate("Build Failed", buildFailedTmpl, data)
}

// RenderDatabaseBackup renders the database backup email to HTML.
func RenderDatabaseBackup(data DatabaseBackupData) (string, error) {
	if data.Date == "" {
		data.Date = time.Now().UTC().Format(time.RFC3339)
	}
	title := "Database Backup Successful"
	if data.Type == "error" {
		title = "Database Backup Failed"
	}
	return renderTemplate(title, databaseBackupTmpl, data)
}

// RenderDockerCleanup renders the docker cleanup email to HTML.
func RenderDockerCleanup(data DockerCleanupData) (string, error) {
	if data.Date == "" {
		data.Date = time.Now().UTC().Format(time.RFC3339)
	}
	return renderTemplate("Docker Cleanup Completed", dockerCleanupTmpl, data)
}

// RenderServerRestart renders the server restart email to HTML.
func RenderServerRestart(data ServerRestartData) (string, error) {
	if data.Date == "" {
		data.Date = time.Now().UTC().Format(time.RFC3339)
	}
	return renderTemplate("Server Restarted", serverRestartTmpl, data)
}

// RenderVolumeBackup renders the volume backup email to HTML.
func RenderVolumeBackup(data VolumeBackupData) (string, error) {
	if data.Date == "" {
		data.Date = time.Now().UTC().Format(time.RFC3339)
	}
	title := "Volume Backup Successful"
	if data.Type == "error" {
		title = "Volume Backup Failed"
	}
	return renderTemplate(title, volumeBackupTmpl, data)
}

// RenderInvitation renders the invitation email to HTML.
func RenderInvitation(data InvitationData) (string, error) {
	return renderTemplate("Join Dokploy", invitationTmpl, data)
}

func renderTemplate(title, tmplStr string, data interface{}) (string, error) {
	tmpl, err := template.New("email").Parse(fmt.Sprintf(baseLayout, title, brandColor) + tmplStr + layoutFooter)
	if err != nil {
		return "", fmt.Errorf("failed to parse email template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to render email template: %w", err)
	}

	return buf.String(), nil
}

const baseLayout = `<!DOCTYPE html>
<html>
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>%s</title>
<style>
body { margin: 0; padding: 0; background-color: #f6f6f6; font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; }
.container { max-width: 465px; margin: 40px auto; background: #ffffff; border: 1px solid #eaeaea; border-radius: 8px; padding: 32px; }
.logo { text-align: center; margin-bottom: 24px; }
h1 { font-size: 24px; font-weight: 700; color: #1a1a1a; text-align: center; margin: 0 0 16px 0; }
p { font-size: 14px; line-height: 24px; color: #333333; margin: 0 0 12px 0; }
.details { background: #f4f4f5; border-radius: 6px; padding: 16px; margin: 16px 0; }
.details p { margin: 4px 0; }
.error { background: #fef2f2; border: 1px solid #fecaca; border-radius: 6px; padding: 16px; margin: 16px 0; }
.error p { color: #991b1b; }
.btn { display: inline-block; background: %s; color: #ffffff; padding: 12px 24px; border-radius: 6px; text-decoration: none; font-size: 14px; font-weight: 600; }
.btn-container { text-align: center; margin: 24px 0; }
.footer { text-align: center; margin-top: 32px; padding-top: 16px; border-top: 1px solid #eaeaea; }
.footer p { font-size: 12px; color: #999999; }
</style>
</head>
<body>
<div class="container">
<div class="logo">
<strong style="font-size:20px;color:#1a1a1a;">Dokploy</strong>
</div>
`

const layoutFooter = `
<div class="footer">
<p>Powered by Dokploy</p>
</div>
</div>
</body>
</html>`

const buildSuccessTmpl = `
<h1>Build Successful</h1>
<p>Your application has been built and deployed successfully.</p>
<div class="details">
<p><strong>Project:</strong> {{.ProjectName}}</p>
<p><strong>Application:</strong> {{.ApplicationName}}</p>
<p><strong>Type:</strong> {{.ApplicationType}}</p>
<p><strong>Environment:</strong> {{.EnvironmentName}}</p>
<p><strong>Date:</strong> {{.Date}}</p>
</div>
{{if .BuildLink}}
<div class="btn-container">
<a href="{{.BuildLink}}" class="btn">View Build</a>
</div>
{{end}}
`

const buildFailedTmpl = `
<h1>Build Failed</h1>
<p>Your application build has failed.</p>
<div class="details">
<p><strong>Project:</strong> {{.ProjectName}}</p>
<p><strong>Application:</strong> {{.ApplicationName}}</p>
<p><strong>Type:</strong> {{.ApplicationType}}</p>
<p><strong>Environment:</strong> {{.EnvironmentName}}</p>
<p><strong>Date:</strong> {{.Date}}</p>
</div>
{{if .ErrorMessage}}
<div class="error">
<p><strong>Error:</strong> {{.ErrorMessage}}</p>
</div>
{{end}}
{{if .BuildLink}}
<div class="btn-container">
<a href="{{.BuildLink}}" class="btn">View Build Logs</a>
</div>
{{end}}
`

const databaseBackupTmpl = `
{{if eq .Type "success"}}
<h1>Database Backup Successful</h1>
<p>Your database backup completed successfully.</p>
{{else}}
<h1>Database Backup Failed</h1>
<p>Your database backup has failed.</p>
{{end}}
<div class="details">
<p><strong>Project:</strong> {{.ProjectName}}</p>
<p><strong>Database:</strong> {{.ApplicationName}}</p>
<p><strong>Type:</strong> {{.DatabaseType}}</p>
<p><strong>Date:</strong> {{.Date}}</p>
</div>
{{if .ErrorMessage}}
<div class="error">
<p><strong>Error:</strong> {{.ErrorMessage}}</p>
</div>
{{end}}
`

const dockerCleanupTmpl = `
<h1>Docker Cleanup Completed</h1>
<p>Docker system cleanup has been performed on your server.</p>
<div class="details">
<p><strong>Date:</strong> {{.Date}}</p>
</div>
<p>Unused images, containers, and volumes have been removed.</p>
`

const serverRestartTmpl = `
<h1>Server Restarted</h1>
<p>Your Dokploy server has been restarted.</p>
<div class="details">
<p><strong>Date:</strong> {{.Date}}</p>
</div>
<p>All services should be back online.</p>
`

const volumeBackupTmpl = `
{{if eq .Type "success"}}
<h1>Volume Backup Successful</h1>
<p>Your volume backup for <strong>{{.ApplicationName}}</strong> completed successfully.</p>
{{else}}
<h1>Volume Backup Failed</h1>
<p>Your volume backup for <strong>{{.ApplicationName}}</strong> has failed.</p>
{{end}}
<div class="details">
<p><strong>Project:</strong> {{.ProjectName}}</p>
<p><strong>Application:</strong> {{.ApplicationName}}</p>
<p><strong>Volume Name:</strong> {{.VolumeName}}</p>
<p><strong>Service Type:</strong> {{.ServiceType}}</p>
{{if .BackupSize}}<p><strong>Backup Size:</strong> {{.BackupSize}}</p>{{end}}
<p><strong>Date:</strong> {{.Date}}</p>
</div>
{{if .ErrorMessage}}
<div class="error">
<p><strong>Error:</strong> {{.ErrorMessage}}</p>
</div>
{{end}}
`

const invitationTmpl = `
<h1>Join <strong>Dokploy</strong></h1>
<p>Hello,</p>
<p>You have been invited to join <strong>Dokploy</strong>, a platform that helps for deploying your apps to the cloud.</p>
<div class="btn-container">
<a href="{{.InviteLink}}" class="btn">Join the team</a>
</div>
<p style="font-size:12px;color:#666666;">This invitation was intended for {{.ToEmail}}. If you were not expecting this invitation, you can ignore this email.</p>
`
