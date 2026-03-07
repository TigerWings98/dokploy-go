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
		// In self-hosted mode, all authenticated users have root access.
		// IS_CLOUD mode would restrict this to admin users only.
		if h.Config != nil && h.Config.IsCloud {
			user := mw.GetUser(c)
			if user == nil {
				return false, nil
			}
			return user.Role == "admin", nil
		}
		return true, nil
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
				updates[fmt.Sprintf("\"%s\"", col)] = v
			}
		}
		if len(updates) > 0 {
			h.DB.Model(&schema.User{}).Where("id = ?", user.ID).Updates(updates)
		}
		return true, nil
	}

	r["user.remove"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		member, err := h.getDefaultMember(c)
		if err != nil {
			return nil, err
		}
		// Only admin/owner can remove users
		if member.Role != "admin" && member.Role != "owner" {
			return nil, &trpcErr{"Only admins can remove users", "UNAUTHORIZED", 403}
		}

		var in struct {
			UserID string `json:"userId"`
		}
		json.Unmarshal(input, &in)

		// Cannot delete yourself
		if in.UserID == member.UserID {
			return nil, &trpcErr{"Cannot delete yourself", "BAD_REQUEST", 400}
		}

		// Delete member records first, then user
		h.DB.Where("user_id = ? AND organization_id = ?", in.UserID, member.OrganizationID).
			Delete(&schema.Member{})
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
			MemberID              string      `json:"memberId"`
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
			h.DB.Model(&schema.Member{}).Where("id = ?", in.MemberID).Updates(updates)
		}
		return true, nil
	}

	r["user.sendInvitation"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		member, err := h.getDefaultMember(c)
		if err != nil {
			return nil, err
		}
		// Only admin/owner can send invitations
		if member.Role != "admin" && member.Role != "owner" {
			return nil, &trpcErr{"Only admins can send invitations", "UNAUTHORIZED", 403}
		}

		var in struct {
			Email string `json:"email"`
			Role  string `json:"role"`
		}
		json.Unmarshal(input, &in)

		// Check if user already in this org
		var existingMember schema.Member
		if err := h.DB.Joins("JOIN \"user\" ON \"user\".id = member.user_id").
			Where("\"user\".email = ? AND member.organization_id = ?", in.Email, member.OrganizationID).
			First(&existingMember).Error; err == nil {
			return nil, &trpcErr{"User already in organization", "BAD_REQUEST", 400}
		}

		// Check for existing pending invitation
		var existing schema.Invitation
		if err := h.DB.Where("email = ? AND organization_id = ? AND status = ?",
			in.Email, member.OrganizationID, "pending").First(&existing).Error; err == nil {
			return nil, &trpcErr{"Invitation already pending", "BAD_REQUEST", 400}
		}

		role := in.Role
		inv := schema.Invitation{
			OrganizationID: member.OrganizationID,
			Email:          in.Email,
			Role:           &role,
			Status:         "pending",
			ExpiresAt:      time.Now().Add(48 * time.Hour),
			InviterID:      member.UserID,
		}
		if err := h.DB.Create(&inv).Error; err != nil {
			return nil, err
		}

		// TODO: Send invitation email when email service is available

		return inv, nil
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
