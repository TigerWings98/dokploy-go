package db

import (
	"log"

	"github.com/dokploy/dokploy/internal/db/schema"
)

// AutoMigrate runs GORM auto-migration for all schema models.
// If the database already has tables (existing Dokploy installation managed by Drizzle),
// we skip migration to avoid constraint name conflicts between GORM and Drizzle conventions.
// AutoMigrate only runs on fresh databases where tables don't exist yet.
func (d *DB) AutoMigrate() error {
	// Check if this is an existing database by looking for a core table
	var count int64
	d.DB.Raw("SELECT count(*) FROM information_schema.tables WHERE table_schema = CURRENT_SCHEMA() AND table_name = 'user' AND table_type = 'BASE TABLE'").Scan(&count)
	if count > 0 {
		log.Println("Existing database detected, skipping auto-migration")
		return nil
	}

	log.Println("Fresh database detected, running auto-migration...")

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
