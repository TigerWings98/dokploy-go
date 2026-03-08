// Input: db, docker, ApplicationService
// Output: PreviewService (CreatePreview/DeployPreview/RemovePreview 生命周期管理)
// Role: PR 预览部署服务，管理预览环境的创建/部署/销毁生命周期
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package service

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/dokploy/dokploy/internal/config"
	"github.com/dokploy/dokploy/internal/db"
	"github.com/dokploy/dokploy/internal/db/schema"
	"github.com/dokploy/dokploy/internal/docker"
	"github.com/dokploy/dokploy/internal/process"
	"github.com/dokploy/dokploy/internal/traefik"
)

// PreviewService handles preview deployment lifecycle.
type PreviewService struct {
	db      *db.DB
	docker  *docker.Client
	cfg     *config.Config
	traefik *traefik.Manager
}

// NewPreviewService creates a new PreviewService.
func NewPreviewService(database *db.DB, dockerClient *docker.Client, cfg *config.Config, traefikMgr *traefik.Manager) *PreviewService {
	return &PreviewService{db: database, docker: dockerClient, cfg: cfg, traefik: traefikMgr}
}

// CreatePreviewInput holds parameters for creating a preview deployment.
type CreatePreviewInput struct {
	ApplicationID    string
	Branch           string
	PullRequestID    string
	PullRequestNumber string
	PullRequestURL   string
	PullRequestTitle string
}

// CreatePreviewDeployment creates a new preview deployment for a PR.
func (s *PreviewService) CreatePreviewDeployment(input CreatePreviewInput) (*schema.PreviewDeployment, error) {
	var app schema.Application
	if err := s.db.First(&app, "\"applicationId\" = ?", input.ApplicationID).Error; err != nil {
		return nil, fmt.Errorf("application not found: %w", err)
	}

	// Check preview limit
	if app.PreviewLimit != nil {
		var count int64
		s.db.Model(&schema.PreviewDeployment{}).
			Where("\"applicationId\" = ?", input.ApplicationID).
			Count(&count)
		if int(count) >= *app.PreviewLimit {
			return nil, fmt.Errorf("preview limit reached (%d)", *app.PreviewLimit)
		}
	}

	// Generate preview domain
	domain, err := s.generatePreviewDomain(&app)
	if err != nil {
		return nil, fmt.Errorf("failed to generate preview domain: %w", err)
	}

	preview := &schema.PreviewDeployment{
		ApplicationID:     input.ApplicationID,
		Branch:            input.Branch,
		PullRequestID:     input.PullRequestID,
		PullRequestNumber: input.PullRequestNumber,
		PullRequestURL:    input.PullRequestURL,
		PullRequestTitle:  input.PullRequestTitle,
	}

	if err := s.db.Create(preview).Error; err != nil {
		return nil, fmt.Errorf("failed to create preview deployment: %w", err)
	}

	// Create domain record for preview
	if domain != "" {
		port := 3000
		if app.PreviewPort != nil {
			port = *app.PreviewPort
		}
		path := "/"
		if app.PreviewPath != nil {
			path = *app.PreviewPath
		}

		domainRecord := &schema.Domain{
			Host:                domain,
			HTTPS:               app.PreviewHTTPS,
			Port:                &port,
			Path:                &path,
			DomainType:          schema.DomainTypePreviewDeployment,
			CertificateType:     app.PreviewCertificateType,
			PreviewDeploymentID: &preview.PreviewDeploymentID,
		}

		if err := s.db.Create(domainRecord).Error; err != nil {
			log.Printf("Warning: failed to create preview domain: %v", err)
		} else {
			s.db.Model(preview).Update("domainId", domainRecord.DomainID)

			// Configure Traefik routing for the preview domain
			if s.traefik != nil {
				if err := s.traefik.ManageDomain(preview.AppName, *domainRecord, nil, nil); err != nil {
					log.Printf("Warning: failed to configure Traefik for preview: %v", err)
				}
			}
		}
	}

	return preview, nil
}

// RemovePreviewDeployment cleans up a preview deployment.
func (s *PreviewService) RemovePreviewDeployment(previewDeploymentID string) error {
	var preview schema.PreviewDeployment
	if err := s.db.
		Preload("Application").
		Preload("Application.Server").
		Preload("Application.Server.SSHKey").
		First(&preview, "\"previewDeploymentId\" = ?", previewDeploymentID).Error; err != nil {
		return fmt.Errorf("preview deployment not found: %w", err)
	}

	// 1. Remove Docker service
	s.removeService(preview.AppName, preview.Application)

	// 2. Remove deployment logs
	s.removeDeploymentLogs(preview.AppName)

	// 3. Remove source code
	s.removeSourceCode(preview.AppName)

	// 4. Remove Traefik config
	if s.traefik != nil {
		if err := s.traefik.RemoveApplicationConfig(preview.AppName); err != nil {
			log.Printf("Warning: failed to remove Traefik config for preview %s: %v", preview.AppName, err)
		}
	}

	// 5. Delete DB record (domains cascade via foreign key)
	if err := s.db.Delete(&schema.PreviewDeployment{}, "\"previewDeploymentId\" = ?", previewDeploymentID).Error; err != nil {
		return fmt.Errorf("failed to delete preview deployment: %w", err)
	}

	return nil
}

// RemovePreviewsByPullRequestID removes all preview deployments for a PR.
func (s *PreviewService) RemovePreviewsByPullRequestID(pullRequestID string) error {
	var previews []schema.PreviewDeployment
	if err := s.db.Where("\"pullRequestId\" = ?", pullRequestID).Find(&previews).Error; err != nil {
		return err
	}

	for _, preview := range previews {
		if err := s.RemovePreviewDeployment(preview.PreviewDeploymentID); err != nil {
			log.Printf("Warning: failed to remove preview %s: %v", preview.PreviewDeploymentID, err)
		}
	}

	return nil
}

// generatePreviewDomain generates a domain for a preview deployment.
func (s *PreviewService) generatePreviewDomain(app *schema.Application) (string, error) {
	if app.PreviewWildcard == nil || *app.PreviewWildcard == "" {
		return "", nil
	}

	baseDomain := *app.PreviewWildcard
	appName := schema.GenerateAppName("preview")

	if strings.Contains(baseDomain, "traefik.me") {
		ip := s.getServerIP()
		slugIP := strings.ReplaceAll(ip, ".", "-")
		suffix := appName
		if slugIP != "" {
			suffix = appName + "-" + slugIP
		}
		return strings.Replace(baseDomain, "*", suffix, 1), nil
	}

	return strings.Replace(baseDomain, "*", appName, 1), nil
}

func (s *PreviewService) getServerIP() string {
	// Try environment variable first
	if ip := os.Getenv("SERVER_IP"); ip != "" {
		return ip
	}

	// Try to get from settings
	var settings schema.WebServerSettings
	if err := s.db.First(&settings).Error; err == nil && settings.ServerIP != nil {
		return *settings.ServerIP
	}

	return ""
}

func (s *PreviewService) removeService(appName string, app *schema.Application) {
	if app != nil && app.Server != nil && app.Server.SSHKey != nil {
		conn := process.SSHConnection{
			Host:       app.Server.IPAddress,
			Port:       app.Server.Port,
			Username:   app.Server.Username,
			PrivateKey: app.Server.SSHKey.PrivateKey,
		}
		process.ExecAsyncRemote(conn, fmt.Sprintf("docker service rm %s 2>/dev/null || true", appName), nil)
	} else if s.docker != nil {
		s.docker.RemoveService(context.Background(), appName)
	}
}

func (s *PreviewService) removeDeploymentLogs(appName string) {
	logDir := filepath.Join(s.cfg.Paths.LogsPath, appName)
	os.RemoveAll(logDir)
}

func (s *PreviewService) removeSourceCode(appName string) {
	codeDir := filepath.Join(s.cfg.Paths.ApplicationsPath, appName)
	os.RemoveAll(codeDir)
}
