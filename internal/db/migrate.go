// Input: GORM DB 实例, 全部 schema 模型
// Output: IsAdminPresent 检查 + AutoMigrate 自动建表
// Role: 数据库迁移管理，确保 Go 版可独立创建所有表结构（不依赖 TS 版 Drizzle 迁移）
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package db

import (
	"fmt"

	"github.com/dokploy/dokploy/internal/db/schema"
)

// IsAdminPresent checks if any admin/owner member exists in the database.
func (d *DB) IsAdminPresent() bool {
	var count int64
	d.DB.Model(&schema.Member{}).Where("role = ?", "owner").Count(&count)
	return count > 0
}

// AutoMigrate creates or updates all database tables based on GORM model definitions.
// 建表顺序：先创建无外键依赖的表，再创建有外键引用的表。
func (d *DB) AutoMigrate() error {
	// 第一批：基础表（无外键依赖或仅自引用）
	if err := d.DB.AutoMigrate(
		// 用户/认证
		&schema.User{},
		&schema.Account{},
		&schema.Verification{},
		&schema.Session{},
		&schema.TwoFactor{},

		// 组织
		&schema.Organization{},
		&schema.Member{},
		&schema.Invitation{},
		&schema.APIKey{},

		// SSH Key (被 Server/Application 等引用)
		&schema.SSHKey{},

		// 通知子表 (先于 notification 主表)
		&schema.NotifSlack{},
		&schema.NotifTelegram{},
		&schema.NotifDiscord{},
		&schema.NotifEmail{},
		&schema.NotifResend{},
		&schema.NotifGotify{},
		&schema.NotifNtfy{},
		&schema.NotifCustom{},
		&schema.NotifLark{},
		&schema.NotifPushover{},
		&schema.NotifTeams{},

		// Registry (被 Application 引用)
		&schema.Registry{},

		// Certificate
		&schema.Certificate{},

		// Destination (被 Backup 引用)
		&schema.Destination{},

		// Git Provider 子表 (先于 GitProvider 主表)
		&schema.Github{},
		&schema.Gitlab{},
		&schema.Bitbucket{},
		&schema.Gitea{},

		// SSO
		&schema.SSOProvider{},

		// Settings
		&schema.WebServerSettings{},
	); err != nil {
		return fmt.Errorf("failed to migrate base tables: %w", err)
	}

	// 第二批：有外键依赖的表
	if err := d.DB.AutoMigrate(
		// Server (depends on SSHKey)
		&schema.Server{},

		// Notification 主表 (depends on 11 sub-tables + Organization)
		&schema.Notification{},

		// Git Provider (depends on Github/Gitlab/Bitbucket/Gitea)
		&schema.GitProvider{},

		// Project (depends on Organization)
		&schema.Project{},
		&schema.Environment{},
	); err != nil {
		return fmt.Errorf("failed to migrate dependent tables: %w", err)
	}

	// 第三批：业务实体表
	if err := d.DB.AutoMigrate(
		// Application (depends on Project, Server, Registry, GitProvider)
		&schema.Application{},

		// Compose (depends on Project, Server, GitProvider)
		&schema.Compose{},

		// Database services (depends on Project, Server)
		&schema.Postgres{},
		&schema.MySQL{},
		&schema.MariaDB{},
		&schema.Mongo{},
		&schema.Redis{},
	); err != nil {
		return fmt.Errorf("failed to migrate business tables: %w", err)
	}

	// 第四批：关联/子实体表
	if err := d.DB.AutoMigrate(
		// Domain (depends on Application/Compose/Server)
		&schema.Domain{},

		// Deployment (depends on Application/Compose)
		&schema.Deployment{},

		// Mount (depends on Application/Compose/databases)
		&schema.Mount{},

		// Port (depends on Application/databases)
		&schema.Port{},

		// Security (depends on Application/Compose)
		&schema.Security{},

		// Redirect (depends on Application/Compose)
		&schema.Redirect{},

		// Backup (depends on Destination + databases)
		&schema.Backup{},
		&schema.VolumeBackup{},

		// Preview Deployment (depends on Application)
		&schema.PreviewDeployment{},

		// Rollback/Schedule
		&schema.Rollback{},
		&schema.Schedule{},

		// Patch
		&schema.Patch{},
	); err != nil {
		return fmt.Errorf("failed to migrate relation tables: %w", err)
	}

	return nil
}
