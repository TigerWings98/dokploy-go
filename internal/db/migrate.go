package db

import (
	"log"

	"github.com/dokploy/dokploy/internal/db/schema"
)

// AutoMigrate runs GORM auto-migration for all schema models.
// This creates tables that don't exist and adds missing columns.
// It does NOT drop columns or change types (safe for production).
func (d *DB) AutoMigrate() error {
	log.Println("Running database auto-migration...")

	err := d.DB.AutoMigrate(
		// Auth tables
		&schema.User{},
		&schema.Account{},
		&schema.Session{},
		&schema.Verification{},
		&schema.TwoFactor{},
		&schema.APIKey{},

		// Organization
		&schema.Organization{},
		&schema.Member{},
		&schema.Invitation{},

		// Core
		&schema.Project{},
		&schema.Environment{},
		&schema.Application{},
		&schema.Compose{},
		&schema.Server{},

		// Settings
		&schema.WebServerSettings{},

		// Deployment
		&schema.Deployment{},
		&schema.Domain{},

		// Database services
		&schema.Postgres{},
		&schema.MySQL{},
		&schema.MariaDB{},
		&schema.Mongo{},
		&schema.Redis{},

		// Git providers
		&schema.GitProvider{},
		&schema.Github{},
		&schema.Gitlab{},
		&schema.Gitea{},
		&schema.Bitbucket{},

		// Supporting entities
		&schema.SSHKey{},
		&schema.Certificate{},
		&schema.Registry{},
		&schema.Destination{},
		&schema.Backup{},
		&schema.Notification{},
		&schema.Mount{},
		&schema.Redirect{},
		&schema.Security{},
		&schema.Port{},
		&schema.Schedule{},
		&schema.Rollback{},
		&schema.VolumeBackup{},
		&schema.PreviewDeployment{},
	)
	if err != nil {
		return err
	}

	log.Println("Database migration complete")
	return nil
}

// IsAdminPresent checks if any admin/owner member exists in the database.
func (d *DB) IsAdminPresent() bool {
	var count int64
	d.DB.Model(&schema.Member{}).Where("role = ?", "owner").Count(&count)
	return count > 0
}
