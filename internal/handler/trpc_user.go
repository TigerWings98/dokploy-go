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
				updates[fmt.Sprintf("\"%s\"", col)] = v
			}
		}
		if len(updates) > 0 {
			h.DB.Model(&schema.User{}).Where("id = ?", user.ID).Updates(updates)
		}
		return true, nil
	}

	r["user.remove"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			UserID string `json:"userId"`
		}
		json.Unmarshal(input, &in)
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
		var in struct {
			MemberID    string                 `json:"memberId"`
			Permissions map[string]interface{} `json:"permissions"`
		}
		json.Unmarshal(input, &in)

		permJSON, _ := json.Marshal(in.Permissions)
		permStr := string(permJSON)
		h.DB.Model(&schema.Member{}).Where("id = ?", in.MemberID).Update("permissions", permStr)
		return true, nil
	}

	r["user.sendInvitation"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		member, err := h.getDefaultMember(c)
		if err != nil {
			return nil, err
		}
		var in struct {
			Email string `json:"email"`
			Role  string `json:"role"`
		}
		json.Unmarshal(input, &in)

		role := in.Role
		inv := schema.Invitation{
			OrganizationID: member.OrganizationID,
			Email:          in.Email,
			Role:           &role,
			Status:         "pending",
			InviterID:      member.UserID,
		}
		if err := h.DB.Create(&inv).Error; err != nil {
			return nil, err
		}
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
