// Input: handler 依赖 (db/middleware/schema)
// Output: buildRegistry() 注册 351 个 tRPC procedure，getDefaultMember() 提供组织成员上下文
// Role: tRPC 路由注册中心，将所有领域 handler 的 procedure 汇聚到统一的 procedureRegistry
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package handler

import (
	"github.com/dokploy/dokploy/internal/db/schema"
	mw "github.com/dokploy/dokploy/internal/middleware"
	"github.com/labstack/echo/v4"
)

// getDefaultMember returns the default org member for the current user.
func (h *Handler) getDefaultMember(c echo.Context) (*schema.Member, error) {
	user := mw.GetUser(c)
	if user == nil {
		return nil, &trpcErr{"Authentication required", "UNAUTHORIZED", 401}
	}
	session := mw.GetSession(c)

	var member schema.Member
	q := h.DB.Where("user_id = ?", user.ID)
	if session != nil && session.ActiveOrganizationID != nil && *session.ActiveOrganizationID != "" {
		q = q.Where("organization_id = ?", *session.ActiveOrganizationID)
	}
	if err := q.Order("is_default DESC, created_at DESC").First(&member).Error; err != nil {
		return nil, &trpcErr{"No organization membership found", "BAD_REQUEST", 400}
	}
	return &member, nil
}

// buildRegistry creates the full procedure registry by calling each domain's registration.
func (h *Handler) buildRegistry() procedureRegistry {
	r := make(procedureRegistry)

	h.registerProjectTRPC(r)
	h.registerApplicationTRPC(r)
	h.registerComposeTRPC(r)
	h.registerDatabaseTRPC(r)
	h.registerDeploymentTRPC(r)
	h.registerServerTRPC(r)
	h.registerOrganizationTRPC(r)
	h.registerSettingsTRPC(r)
	h.registerUserTRPC(r)
	h.registerNotificationTRPC(r)
	h.registerGitProviderTRPC(r)
	h.registerSSHKeyTRPC(r)
	h.registerDockerTRPC(r)
	h.registerScheduleTRPC(r)
	h.registerBackupTRPC(r)
	h.registerMiscTRPC(r)
	h.registerPatchTRPC(r)
	h.registerSSOTRPC(r)
	h.registerStubsTRPC(r)

	return r
}

// findDatabaseService queries a database service by ID with all relations preloaded.
func (h *Handler) findDatabaseService(model interface{}, idField, id string) error {
	quotedID := "\"" + idField + "\""
	query := h.DB.
		Preload("Environment").
		Preload("Server").
		Preload("Mounts")

	switch model.(type) {
	case *schema.Postgres, *schema.MySQL, *schema.MariaDB, *schema.Mongo, *schema.Redis:
		query = query.Preload("Backups").Preload("Backups.Destination")
	}

	if err := query.Where(quotedID+" = ?", id).First(model).Error; err != nil {
		return err
	}

	// Manually load Environment.Project (camelCase PK workaround)
	type withEnv interface{ GetEnvironmentID() string }
	if we, ok := model.(withEnv); ok {
		envID := we.GetEnvironmentID()
		if envID != "" {
			var env schema.Environment
			if err := h.DB.First(&env, "\"environmentId\" = ?", envID).Error; err == nil {
				if env.ProjectID != "" {
					var project schema.Project
					h.DB.First(&project, "\"projectId\" = ?", env.ProjectID)
					env.Project = &project
				}
				switch m := model.(type) {
				case *schema.Postgres:
					m.Environment = &env
				case *schema.MySQL:
					m.Environment = &env
				case *schema.MariaDB:
					m.Environment = &env
				case *schema.Mongo:
					m.Environment = &env
				case *schema.Redis:
					m.Environment = &env
				}
			}
		}
	}

	// Ensure slices are never null in JSON response
	switch m := model.(type) {
	case *schema.Postgres:
		if m.Mounts == nil { m.Mounts = []schema.Mount{} }
		if m.Backups == nil { m.Backups = []schema.Backup{} }
	case *schema.MySQL:
		if m.Mounts == nil { m.Mounts = []schema.Mount{} }
		if m.Backups == nil { m.Backups = []schema.Backup{} }
	case *schema.MariaDB:
		if m.Mounts == nil { m.Mounts = []schema.Mount{} }
		if m.Backups == nil { m.Backups = []schema.Backup{} }
	case *schema.Mongo:
		if m.Mounts == nil { m.Mounts = []schema.Mount{} }
		if m.Backups == nil { m.Backups = []schema.Backup{} }
	case *schema.Redis:
		if m.Mounts == nil { m.Mounts = []schema.Mount{} }
		if m.Backups == nil { m.Backups = []schema.Backup{} }
	}

	return nil
}
