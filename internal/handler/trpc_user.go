package handler

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/dokploy/dokploy/internal/db/schema"
	mw "github.com/dokploy/dokploy/internal/middleware"
	"github.com/labstack/echo/v4"
	gonanoid "github.com/matoous/go-nanoid/v2"
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
		if err := h.DB.Preload("APIKeys").First(&u, "id = ?", user.ID).Error; err != nil {
			return nil, &trpcErr{"User not found", "NOT_FOUND", 404}
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
		allowed := map[string]string{
			"firstName": "firstName",
			"lastName":  "lastName",
			"image":     "image",
		}
		updates := map[string]interface{}{}
		for k, col := range allowed {
			if v, ok := in[k]; ok {
				updates[col] = v
			}
		}
		if len(updates) > 0 {
			// Use Table() to bypass GORM's NamingStrategy (camelCase column names)
			h.DB.Table("\"user\"").Where("id = ?", user.ID).Updates(updates)
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

		apiKey := schema.APIKey{
			UserID:      user.ID,
			Key:         fullKey,
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

		// Look up the notification provider (for email sending)
		var notif schema.Notification
		if err := h.DB.First(&notif, "\"notificationId\" = ?", in.NotificationID).Error; err != nil {
			return nil, &trpcErr{"Notification provider not found", "NOT_FOUND", 404}
		}

		// Generate invitation link
		// TODO: Send actual email via notification provider when email service supports it
		// inviteLink := fmt.Sprintf("/invitation?token=%s", in.InvitationID)

		return map[string]string{
			"inviteLink": fmt.Sprintf("/invitation?token=%s", in.InvitationID),
		}, nil
	}

	r["user.getContainerMetrics"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return map[string]interface{}{
			"containers": []interface{}{},
		}, nil
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
		return map[string]interface{}{}, nil
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
