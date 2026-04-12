// Input: procedureRegistry, db (User 表)
// Output: registerUserTRPC - User 领域的 tRPC procedure 注册
// Role: User tRPC 路由注册，将 user.* procedure 绑定到用户管理操作
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package handler

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/dokploy/dokploy/internal/auth"
	"github.com/dokploy/dokploy/internal/db/schema"
	mw "github.com/dokploy/dokploy/internal/middleware"
	"github.com/labstack/echo/v4"
	gonanoid "github.com/matoous/go-nanoid/v2"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

func (h *Handler) registerUserTRPC(r procedureRegistry) {
	r["user.all"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		member, err := h.getDefaultMember(c)
		if err != nil {
			return []interface{}{}, nil
		}
		var members []schema.Member
		h.DB.Preload("User").
			Where("organization_id = ?", member.OrganizationID).
			Order("created_at ASC").
			Find(&members)
		return members, nil
	}

	r["user.one"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ UserID string `json:"userId"` }
		json.Unmarshal(input, &in)
		var user schema.User
		if err := h.DB.First(&user, "id = ?", in.UserID).Error; err != nil {
			return nil, &trpcErr{"User not found", "NOT_FOUND", 404}
		}
		return user, nil
	}

	r["user.haveRootAccess"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		// haveRootAccess is a cloud-only concept.
		// In self-hosted mode, returns false (this flag is not used for access control).
		if h.Config == nil || !h.Config.IsCloud {
			return false, nil
		}
		// In cloud mode, check if user is the platform admin
		user := mw.GetUser(c)
		if user == nil {
			return false, nil
		}
		return user.Role == "admin", nil
	}

	r["user.get"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		user := mw.GetUser(c)
		if user == nil {
			return nil, &trpcErr{"Unauthorized", "UNAUTHORIZED", 401}
		}
		member, err := h.getDefaultMember(c)
		if err != nil {
			return nil, err
		}

		var fullUser schema.User
		h.DB.Preload("APIKeys").First(&fullUser, "id = ?", user.ID)
		if fullUser.APIKeys == nil {
			fullUser.APIKeys = []schema.APIKey{}
		}

		result := map[string]interface{}{
			"memberId":                member.ID,
			"userId":                  member.UserID,
			"organizationId":          member.OrganizationID,
			"role":                    member.Role,
			"isDefault":               member.IsDefault,
			"canCreateProjects":       member.CanCreateProjects,
			"canCreateServices":       member.CanCreateServices,
			"canDeleteProjects":       member.CanDeleteProjects,
			"canDeleteServices":       member.CanDeleteServices,
			"canAccessToDocker":       member.CanAccessToDocker,
			"canAccessToAPI":          member.CanAccessToAPI,
			"canAccessToSSHKeys":      member.CanAccessToSSHKeys,
			"canAccessToGitProviders": member.CanAccessToGitProviders,
			"canAccessToTraefikFiles": member.CanAccessToTraefikFiles,
			"canDeleteEnvironments":   member.CanDeleteEnvironments,
			"canCreateEnvironments":   member.CanCreateEnvironments,
			"accesedProjects":         member.AccessedProjects,
			"accesedServices":         member.AccessedServices,
			"accessedEnvironments":    member.AccessedEnvironments,
			"createdAt":               member.CreatedAt,
			"user":                    fullUser,
		}
		return result, nil
	}

	// 与 TS v0.28.7 对齐：按 resource/action 返回当前用户的完整权限 map
	// 前端用 api.user.getPermissions.useQuery() 获取后统一检查 canXxx
	r["user.getPermissions"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		member, err := h.getDefaultMember(c)
		if err != nil {
			return nil, err
		}
		return buildResolvedPermissions(member), nil
	}

	r["user.haveRootAccess"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		// self-hosted 始终返回 false（仅 Cloud 判断 USER_ADMIN_ID）
		return false, nil
	}

	r["user.getInvitations"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		user := mw.GetUser(c)
		if user == nil {
			return nil, &trpcErr{"Unauthorized", "UNAUTHORIZED", 401}
		}
		var invitations []schema.Invitation
		h.DB.Where("email = ? AND status = ?", user.Email, "pending").Find(&invitations)
		return invitations, nil
	}

	r["user.getBackups"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		user := mw.GetUser(c)
		if user == nil {
			return nil, &trpcErr{"Unauthorized", "UNAUTHORIZED", 401}
		}
		var u schema.User
		if err := h.DB.
			Preload("APIKeys").
			Preload("Backups").
			Preload("Backups.Destination").
			Preload("Backups.Deployments", func(db *gorm.DB) *gorm.DB {
				return db.Order("\"createdAt\" DESC")
			}).
			First(&u, "id = ?", user.ID).Error; err != nil {
			return nil, &trpcErr{"User not found", "NOT_FOUND", 404}
		}
		// 确保 slice 返回 [] 而非 null
		if u.Backups == nil {
			u.Backups = []schema.Backup{}
		}
		if u.APIKeys == nil {
			u.APIKeys = []schema.APIKey{}
		}
		return u, nil
	}

	r["user.update"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		user := mw.GetUser(c)
		if user == nil {
			return nil, &trpcErr{"Authentication required", "UNAUTHORIZED", 401}
		}
		var in map[string]interface{}
		json.Unmarshal(input, &in)

		// Handle password change (stored in account table, not user table)
		if pw, ok := in["password"]; ok && pw != nil {
			pwStr, _ := pw.(string)
			if pwStr != "" {
				// Verify current password
				var acct schema.Account
				if err := h.DB.Where("user_id = ?", user.ID).First(&acct).Error; err != nil {
					return nil, &trpcErr{"Account not found", "BAD_REQUEST", 400}
				}
				curPw, _ := in["currentPassword"].(string)
				if acct.Password == nil || bcrypt.CompareHashAndPassword([]byte(*acct.Password), []byte(curPw)) != nil {
					return nil, &trpcErr{"Current password is incorrect", "BAD_REQUEST", 400}
				}
				hashed, err := bcrypt.GenerateFromPassword([]byte(pwStr), 10)
				if err != nil {
					return nil, &trpcErr{"Failed to hash password", "INTERNAL_SERVER_ERROR", 500}
				}
				h.DB.Exec(`UPDATE "account" SET "password" = ? WHERE "user_id" = ?`, string(hashed), user.ID)
			}
		}

		// Update user table fields
		allowed := []string{"firstName", "lastName", "image", "email"}
		// Build raw SQL to avoid GORM NamingStrategy converting camelCase to snake_case
		var setClauses []string
		var args []interface{}
		for _, col := range allowed {
			v, ok := in[col]
			if !ok {
				continue
			}
			if v == nil {
				// Treat null as empty string for NOT NULL text columns
				v = ""
			}
			setClauses = append(setClauses, fmt.Sprintf("\"%s\" = ?", col))
			args = append(args, v)
		}
		if len(setClauses) > 0 {
			args = append(args, user.ID)
			query := fmt.Sprintf(`UPDATE "user" SET %s WHERE id = ?`, strings.Join(setClauses, ", "))
			result := h.DB.Exec(query, args...)
			if result.Error != nil {
				return nil, &trpcErr{fmt.Sprintf("Failed to update user: %v", result.Error), "INTERNAL_SERVER_ERROR", 500}
			}
		}
		return true, nil
	}

	r["user.remove"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		// In cloud mode, user removal is handled differently
		if h.Config != nil && h.Config.IsCloud {
			return true, nil
		}

		member, err := h.getDefaultMember(c)
		if err != nil {
			return nil, err
		}
		// Only admin/owner can remove users
		if member.Role != "admin" && member.Role != "owner" {
			return nil, &trpcErr{"Only owners or admins can delete users", "FORBIDDEN", 403}
		}

		var in struct {
			UserID string `json:"userId"`
		}
		json.Unmarshal(input, &in)

		// Find the target member in the same org
		var targetMember schema.Member
		if err := h.DB.Where("user_id = ? AND organization_id = ?", in.UserID, member.OrganizationID).
			First(&targetMember).Error; err != nil {
			return nil, &trpcErr{"Target user is not a member of this organization", "NOT_FOUND", 404}
		}

		// Cannot delete the org owner
		if targetMember.Role == "owner" {
			return nil, &trpcErr{"You cannot delete the organization owner", "FORBIDDEN", 403}
		}

		// Admins cannot delete themselves
		if targetMember.Role == "admin" && in.UserID == member.UserID {
			return nil, &trpcErr{"Admins cannot delete themselves", "FORBIDDEN", 403}
		}

		// Admins cannot delete other admins (only owners can)
		if member.Role == "admin" && targetMember.Role == "admin" {
			return nil, &trpcErr{"Only the organization owner can delete admins", "FORBIDDEN", 403}
		}

		// Delete user (cascades to member, account, etc.)
		h.DB.Delete(&schema.User{}, "id = ?", in.UserID)
		return true, nil
	}

	r["user.createApiKey"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		user := mw.GetUser(c)
		if user == nil {
			return nil, &trpcErr{"Authentication required", "UNAUTHORIZED", 401}
		}
		var in struct {
			Name        string  `json:"name"`
			ExpiresAt   *string `json:"expiresAt"`
			Permissions *string `json:"permissions"`
		}
		json.Unmarshal(input, &in)

		key, _ := gonanoid.Generate("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789", 48)
		prefix := "dk_"
		start := key[:4]
		fullKey := prefix + key
		// 存储哈希后的 key（与 Better Auth defaultKeyHasher 一致：SHA-256 + base64url）
		hashedKey := auth.HashAPIKey(fullKey)

		apiKey := schema.APIKey{
			ReferenceID: user.ID,
			ConfigID:    "default",
			Key:         hashedKey,
			Name:        &in.Name,
			Prefix:      &prefix,
			Start:       &start,
			Permissions: in.Permissions,
		}
		if in.ExpiresAt != nil {
			t, err := time.Parse(time.RFC3339, *in.ExpiresAt)
			if err == nil {
				apiKey.ExpiresAt = &t
			}
		}
		if err := h.DB.Create(&apiKey).Error; err != nil {
			return nil, err
		}
		return map[string]interface{}{
			"key": fullKey,
		}, nil
	}

	r["user.deleteApiKey"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			ApiKeyID string `json:"apiKeyId"`
		}
		json.Unmarshal(input, &in)
		h.DB.Delete(&schema.APIKey{}, "id = ?", in.ApiKeyID)
		return true, nil
	}

	r["user.getMetricsToken"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		user := mw.GetUser(c)
		if user == nil {
			return nil, &trpcErr{"Authentication required", "UNAUTHORIZED", 401}
		}
		token, _ := gonanoid.New()
		return map[string]string{"token": token}, nil
	}

	r["user.getUserByToken"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			Token string `json:"token"`
		}
		json.Unmarshal(input, &in)
		var user schema.User
		if err := h.DB.First(&user, "id = ?", in.Token).Error; err != nil {
			return nil, &trpcErr{"User not found", "NOT_FOUND", 404}
		}
		return user, nil
	}

	r["user.assignPermissions"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		member, err := h.getDefaultMember(c)
		if err != nil {
			return nil, err
		}
		// Only admin/owner can assign permissions
		if member.Role != "admin" && member.Role != "owner" {
			return nil, &trpcErr{"Only admins can assign permissions", "UNAUTHORIZED", 403}
		}

		var in struct {
			ID                    string      `json:"id"` // userId
			CanCreateProjects     *bool       `json:"canCreateProjects"`
			CanCreateServices     *bool       `json:"canCreateServices"`
			CanDeleteProjects     *bool       `json:"canDeleteProjects"`
			CanDeleteServices     *bool       `json:"canDeleteServices"`
			CanAccessToDocker     *bool       `json:"canAccessToDocker"`
			CanAccessToAPI        *bool       `json:"canAccessToAPI"`
			CanAccessToSSHKeys    *bool       `json:"canAccessToSSHKeys"`
			CanAccessToGitProviders *bool     `json:"canAccessToGitProviders"`
			CanAccessToTraefikFiles *bool     `json:"canAccessToTraefikFiles"`
			CanDeleteEnvironments *bool       `json:"canDeleteEnvironments"`
			CanCreateEnvironments *bool       `json:"canCreateEnvironments"`
			AccessedProjects      *[]string   `json:"accesedProjects"`
			AccessedServices      *[]string   `json:"accesedServices"`
			AccessedEnvironments  *[]string   `json:"accessedEnvironments"`
		}
		json.Unmarshal(input, &in)

		updates := map[string]interface{}{}
		if in.CanCreateProjects != nil {
			updates["\"canCreateProjects\""] = *in.CanCreateProjects
		}
		if in.CanCreateServices != nil {
			updates["\"canCreateServices\""] = *in.CanCreateServices
		}
		if in.CanDeleteProjects != nil {
			updates["\"canDeleteProjects\""] = *in.CanDeleteProjects
		}
		if in.CanDeleteServices != nil {
			updates["\"canDeleteServices\""] = *in.CanDeleteServices
		}
		if in.CanAccessToDocker != nil {
			updates["\"canAccessToDocker\""] = *in.CanAccessToDocker
		}
		if in.CanAccessToAPI != nil {
			updates["\"canAccessToAPI\""] = *in.CanAccessToAPI
		}
		if in.CanAccessToSSHKeys != nil {
			updates["\"canAccessToSSHKeys\""] = *in.CanAccessToSSHKeys
		}
		if in.CanAccessToGitProviders != nil {
			updates["\"canAccessToGitProviders\""] = *in.CanAccessToGitProviders
		}
		if in.CanAccessToTraefikFiles != nil {
			updates["\"canAccessToTraefikFiles\""] = *in.CanAccessToTraefikFiles
		}
		if in.CanDeleteEnvironments != nil {
			updates["\"canDeleteEnvironments\""] = *in.CanDeleteEnvironments
		}
		if in.CanCreateEnvironments != nil {
			updates["\"canCreateEnvironments\""] = *in.CanCreateEnvironments
		}
		if in.AccessedProjects != nil {
			updates["\"accesedProjects\""] = schema.StringArray(*in.AccessedProjects)
		}
		if in.AccessedServices != nil {
			updates["\"accesedServices\""] = schema.StringArray(*in.AccessedServices)
		}
		if in.AccessedEnvironments != nil {
			updates["\"accessedEnvironments\""] = schema.StringArray(*in.AccessedEnvironments)
		}

		if len(updates) > 0 {
			h.DB.Model(&schema.Member{}).
				Where("user_id = ? AND organization_id = ?", in.ID, member.OrganizationID).
				Updates(updates)
		}
		return true, nil
	}

	r["user.sendInvitation"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		// In cloud mode, skip (handled by cloud infrastructure)
		if h.Config != nil && h.Config.IsCloud {
			return true, nil
		}

		var in struct {
			InvitationID   string `json:"invitationId"`
			NotificationID string `json:"notificationId"`
		}
		json.Unmarshal(input, &in)

		// Look up the existing invitation
		var inv schema.Invitation
		if err := h.DB.First(&inv, "id = ?", in.InvitationID).Error; err != nil {
			return nil, &trpcErr{"Invitation not found", "NOT_FOUND", 404}
		}

		// 验证通知渠道存在
		var notifCount int64
		h.DB.Model(&schema.Notification{}).Where("\"notificationId\" = ?", in.NotificationID).Count(&notifCount)
		if notifCount == 0 {
			return nil, &trpcErr{"Notification provider not found", "NOT_FOUND", 404}
		}

		// 生成邀请链接
		scheme := "https"
		if c.Request().TLS == nil {
			scheme = "http"
		}
		host := c.Request().Host
		inviteLink := fmt.Sprintf("%s://%s/invitation?token=%s", scheme, host, in.InvitationID)

		// 查找组织名称
		orgName := "Dokploy"
		member, _ := h.getDefaultMember(c)
		if member != nil {
			var org schema.Organization
			if h.DB.First(&org, "id = ?", member.OrganizationID).Error == nil && org.Name != "" {
				orgName = org.Name
			}
		}

		// 通过通知渠道的 Email/Resend 配置发送邀请邮件
		htmlContent := fmt.Sprintf(
			`<p>You are invited to join %s on Dokploy. Click the link to accept the invitation: <a href="%s">Accept Invitation</a></p>`,
			orgName, inviteLink,
		)
		if h.Notifier != nil && inv.Email != "" {
			go h.Notifier.SendEmailToRecipient(in.NotificationID, inv.Email, "Invitation to join organization", htmlContent)
		}

		return inviteLink, nil
	}

	r["user.getContainerMetrics"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		// 代理到本地 dokploy-monitoring 容器的 /metrics/containers 端点
		monitoringURL := "http://127.0.0.1:3001/metrics/containers"
		return proxyMonitoringRequest(monitoringURL)
	}

	r["user.generateToken"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		user := mw.GetUser(c)
		if user == nil {
			return nil, &trpcErr{"Authentication required", "UNAUTHORIZED", 401}
		}
		token, _ := gonanoid.Generate("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789", 32)
		return map[string]string{"token": token}, nil
	}

	r["user.getServerMetrics"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		// 代理到本地 dokploy-monitoring 容器的 /metrics 端点
		monitoringURL := "http://127.0.0.1:3001/metrics"
		return proxyMonitoringRequest(monitoringURL)
	}

	r["user.session"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		user := mw.GetUser(c)
		session := mw.GetSession(c)
		if user == nil {
			return nil, &trpcErr{"Unauthorized", "UNAUTHORIZED", 401}
		}
		activeOrgID := ""
		if session != nil && session.ActiveOrganizationID != nil {
			activeOrgID = *session.ActiveOrganizationID
		}
		return map[string]interface{}{
			"user": map[string]interface{}{
				"id": user.ID,
			},
			"session": map[string]interface{}{
				"activeOrganizationId": activeOrgID,
			},
		}, nil
	}

	r["user.checkUserOrganizations"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		user := mw.GetUser(c)
		if user == nil {
			return nil, &trpcErr{"Authentication required", "UNAUTHORIZED", 401}
		}
		var members []schema.Member
		h.DB.Where("user_id = ?", user.ID).Find(&members)
		return len(members) > 0, nil
	}
}

// buildResolvedPermissions 根据 member 角色和 legacy 权限位构建 ResolvedPermissions
// 与 TS v0.28.7 resolvePermissions 对齐：
// - owner/admin：所有 free 资源 + 所有 enterprise 资源均 true（self-hosted 默认授权）
// - member：free 资源根据 legacy can* 位，enterprise 资源默认 false（需自定义角色，企业版才可用）
func buildResolvedPermissions(member *schema.Member) map[string]map[string]bool {
	isPrivileged := member.Role == "owner" || member.Role == "admin"

	// statements: resource -> []actions
	statements := map[string][]string{
		// better-auth organization plugin defaults
		"organization": {"update", "delete"},
		"member":       {"read", "create", "update", "delete"},
		"invitation":   {"create", "cancel"},
		"team":         {"create", "update", "delete"},
		"ac":           {"create", "read", "update", "delete"},

		// Dokploy core resources (free tier)
		"project":      {"create", "delete"},
		"service":      {"create", "read", "delete"},
		"environment":  {"create", "read", "delete"},
		"docker":       {"read"},
		"sshKeys":      {"read", "create", "delete"},
		"gitProviders": {"read", "create", "delete"},
		"traefikFiles": {"read", "write"},
		"api":          {"read"},

		// Enterprise-only resources (custom roles only)
		"volume":              {"read", "create", "delete"},
		"deployment":          {"read", "create", "cancel"},
		"envVars":             {"read", "write"},
		"projectEnvVars":      {"read", "write"},
		"environmentEnvVars":  {"read", "write"},
		"server":              {"read", "create", "delete"},
		"registry":            {"read", "create", "delete"},
		"certificate":         {"read", "create", "delete"},
		"backup":              {"read", "create", "update", "delete", "restore"},
		"volumeBackup":        {"read", "create", "update", "delete", "restore"},
		"schedule":            {"read", "create", "update", "delete"},
		"domain":              {"read", "create", "delete"},
		"destination":         {"read", "create", "delete"},
		"notification":        {"read", "create", "update", "delete"},
		"logs":                {"read"},
		"monitoring":          {"read"},
		"auditLog":            {"read"},
	}

	// enterpriseOnlyResources: 权限仅在自定义角色 + 企业许可下才有意义
	enterpriseOnly := map[string]bool{
		"volume": true, "deployment": true, "envVars": true,
		"projectEnvVars": true, "environmentEnvVars": true,
		"server": true, "registry": true, "certificate": true,
		"backup": true, "volumeBackup": true, "schedule": true,
		"domain": true, "destination": true, "notification": true,
		"logs": true, "monitoring": true, "auditLog": true,
	}

	// legacy overrides: 基于 member 表的 can* 字段（仅对 role=member 生效）
	isMember := member.Role == "member"
	legacyOverrides := map[string]map[string]bool{}
	if isMember {
		legacyOverrides = map[string]map[string]bool{
			"project": {
				"create": member.CanCreateProjects,
				"delete": member.CanDeleteProjects,
			},
			"service": {
				"create": member.CanCreateServices,
				"delete": member.CanDeleteServices,
			},
			"environment": {
				"create": member.CanCreateEnvironments,
				"delete": member.CanDeleteEnvironments,
			},
			"traefikFiles": {
				"read": member.CanAccessToTraefikFiles,
			},
			"docker": {
				"read": member.CanAccessToDocker,
			},
			"api": {
				"read": member.CanAccessToAPI,
			},
			"sshKeys": {
				"read": member.CanAccessToSSHKeys,
			},
			"gitProviders": {
				"read": member.CanAccessToGitProviders,
			},
		}
	}

	// 针对 admin/owner 的 free 资源默认全允许
	privilegedFreeResources := map[string]bool{
		"project": true, "service": true, "environment": true,
		"docker": true, "sshKeys": true, "gitProviders": true,
		"traefikFiles": true, "api": true,
		"organization": true, "member": true, "invitation": true,
		"team": true, "ac": true,
	}

	result := map[string]map[string]bool{}
	for resource, actions := range statements {
		rperm := map[string]bool{}
		for _, action := range actions {
			allow := false
			if isPrivileged {
				// owner/admin：企业资源 + free 资源均允许
				if enterpriseOnly[resource] || privilegedFreeResources[resource] {
					allow = true
				}
			}
			// legacy override for member role
			if !allow && isMember {
				if rMap, ok := legacyOverrides[resource]; ok {
					if v, ok := rMap[action]; ok && v {
						allow = true
					}
				}
			}
			rperm[action] = allow
		}
		result[resource] = rperm
	}
	return result
}
