// Input: procedureRegistry, db (Organization/Member/Invitation 表)
// Output: registerOrganizationTRPC - Organization 领域的 tRPC procedure 注册
// Role: Organization tRPC 路由注册，将 organization.* procedure 绑定到组织管理操作
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package handler

import (
	"encoding/json"

	"github.com/dokploy/dokploy/internal/db/schema"
	mw "github.com/dokploy/dokploy/internal/middleware"
	"github.com/labstack/echo/v4"
)

func (h *Handler) registerOrganizationTRPC(r procedureRegistry) {
	r["organization.all"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		user := mw.GetUser(c)
		if user == nil {
			return nil, &trpcErr{"Unauthorized", "UNAUTHORIZED", 401}
		}
		var orgs []schema.Organization
		h.DB.Where("id IN (SELECT organization_id FROM member WHERE user_id = ?)", user.ID).
			Preload("Members", "user_id = ?", user.ID).
			Find(&orgs)
		return orgs, nil
	}

	r["organization.one"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		user := mw.GetUser(c)
		if user == nil {
			return nil, &trpcErr{"Unauthorized", "UNAUTHORIZED", 401}
		}
		var in struct{ OrganizationID string `json:"organizationId"` }
		json.Unmarshal(input, &in)
		// Verify membership
		var member schema.Member
		if err := h.DB.Where("user_id = ? AND organization_id = ?", user.ID, in.OrganizationID).
			First(&member).Error; err != nil {
			return nil, &trpcErr{"You are not a member of this organization", "FORBIDDEN", 403}
		}
		var org schema.Organization
		if err := h.DB.First(&org, "id = ?", in.OrganizationID).Error; err != nil {
			return nil, &trpcErr{"Organization not found", "NOT_FOUND", 404}
		}
		return org, nil
	}

	r["organization.active"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		session := mw.GetSession(c)
		if session == nil || session.ActiveOrganizationID == nil || *session.ActiveOrganizationID == "" {
			return nil, nil
		}
		var org schema.Organization
		if err := h.DB.First(&org, "id = ?", *session.ActiveOrganizationID).Error; err != nil {
			return nil, nil
		}
		return org, nil
	}

	r["organization.create"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		user := mw.GetUser(c)
		if user == nil {
			return nil, &trpcErr{"Unauthorized", "UNAUTHORIZED", 401}
		}
		var in struct {
			Name string  `json:"name"`
			Logo *string `json:"logo"`
		}
		json.Unmarshal(input, &in)

		org := &schema.Organization{Name: in.Name, Logo: in.Logo, OwnerID: user.ID}
		tx := h.DB.Begin()
		if err := tx.Create(org).Error; err != nil {
			tx.Rollback()
			return nil, err
		}
		member := &schema.Member{OrganizationID: org.ID, UserID: user.ID, Role: schema.MemberRoleOwner}
		if err := tx.Create(member).Error; err != nil {
			tx.Rollback()
			return nil, err
		}
		tx.Commit()
		return org, nil
	}

	r["organization.update"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		user := mw.GetUser(c)
		if user == nil {
			return nil, &trpcErr{"Unauthorized", "UNAUTHORIZED", 401}
		}
		var in struct {
			OrganizationID string  `json:"organizationId"`
			Name           string  `json:"name"`
			Logo           *string `json:"logo"`
		}
		json.Unmarshal(input, &in)

		// Verify user is a member with owner role
		var member schema.Member
		if err := h.DB.Where("user_id = ? AND organization_id = ?", user.ID, in.OrganizationID).
			First(&member).Error; err != nil {
			return nil, &trpcErr{"You are not a member of this organization", "FORBIDDEN", 403}
		}
		if member.Role != "owner" {
			return nil, &trpcErr{"Only the organization owner can update it", "FORBIDDEN", 403}
		}

		var org schema.Organization
		if err := h.DB.First(&org, "id = ?", in.OrganizationID).Error; err != nil {
			return nil, &trpcErr{"Organization not found", "NOT_FOUND", 404}
		}
		updates := map[string]interface{}{"name": in.Name}
		if in.Logo != nil {
			updates["logo"] = *in.Logo
		}
		h.DB.Model(&org).Updates(updates)
		return org, nil
	}

	r["organization.delete"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		user := mw.GetUser(c)
		if user == nil {
			return nil, &trpcErr{"Unauthorized", "UNAUTHORIZED", 401}
		}
		var in struct{ OrganizationID string `json:"organizationId"` }
		json.Unmarshal(input, &in)

		// Verify user is the owner
		var member schema.Member
		if err := h.DB.Where("user_id = ? AND organization_id = ?", user.ID, in.OrganizationID).
			First(&member).Error; err != nil {
			return nil, &trpcErr{"You are not a member of this organization", "FORBIDDEN", 403}
		}
		if member.Role != "owner" {
			return nil, &trpcErr{"Only the organization owner can delete it", "FORBIDDEN", 403}
		}

		// Must maintain at least 1 owned organization
		var ownedCount int64
		h.DB.Model(&schema.Member{}).Where("user_id = ? AND role = ?", user.ID, "owner").Count(&ownedCount)
		if ownedCount <= 1 {
			return nil, &trpcErr{"You must have at least one organization", "FORBIDDEN", 403}
		}

		h.DB.Delete(&schema.Organization{}, "id = ?", in.OrganizationID)
		return true, nil
	}

	r["organization.setDefault"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		user := mw.GetUser(c)
		if user == nil {
			return nil, &trpcErr{"Unauthorized", "UNAUTHORIZED", 401}
		}
		var in struct{ OrganizationID string `json:"organizationId"` }
		json.Unmarshal(input, &in)
		tx := h.DB.Begin()
		tx.Model(&schema.Member{}).Where("user_id = ?", user.ID).Update("is_default", false)
		tx.Model(&schema.Member{}).Where("user_id = ? AND organization_id = ?", user.ID, in.OrganizationID).Update("is_default", true)
		tx.Commit()
		return map[string]bool{"success": true}, nil
	}

	r["organization.allInvitations"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		member, err := h.getDefaultMember(c)
		if err != nil {
			return nil, err
		}
		var invitations []schema.Invitation
		h.DB.Where("organization_id = ?", member.OrganizationID).
			Order("status DESC, expires_at DESC").
			Find(&invitations)
		return invitations, nil
	}

	r["organization.removeInvitation"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ InvitationID string `json:"invitationId"` }
		json.Unmarshal(input, &in)
		h.DB.Delete(&schema.Invitation{}, "id = ?", in.InvitationID)
		return true, nil
	}

	r["organization.updateMemberRole"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		user := mw.GetUser(c)
		if user == nil {
			return nil, &trpcErr{"Unauthorized", "UNAUTHORIZED", 401}
		}
		var in struct {
			MemberID string `json:"memberId"`
			Role     string `json:"role"`
		}
		json.Unmarshal(input, &in)

		// Get the target member
		var targetMember schema.Member
		if err := h.DB.First(&targetMember, "id = ?", in.MemberID).Error; err != nil {
			return nil, &trpcErr{"Member not found", "NOT_FOUND", 404}
		}

		// Verify caller is admin/owner in the same org
		var callerMember schema.Member
		if err := h.DB.Where("user_id = ? AND organization_id = ?", user.ID, targetMember.OrganizationID).
			First(&callerMember).Error; err != nil {
			return nil, &trpcErr{"You are not a member of this organization", "FORBIDDEN", 403}
		}

		if callerMember.Role != "owner" && callerMember.Role != "admin" {
			return nil, &trpcErr{"Only owners or admins can change roles", "FORBIDDEN", 403}
		}

		// Cannot change own role
		if targetMember.UserID == user.ID {
			return nil, &trpcErr{"Cannot change your own role", "FORBIDDEN", 403}
		}

		// Cannot change owner role
		if targetMember.Role == "owner" {
			return nil, &trpcErr{"Cannot change the owner's role", "FORBIDDEN", 403}
		}

		// Admin can only change member roles (not other admins)
		if callerMember.Role == "admin" && targetMember.Role == "admin" {
			return nil, &trpcErr{"Only the owner can change admin roles", "FORBIDDEN", 403}
		}

		h.DB.Model(&schema.Member{}).Where("id = ?", in.MemberID).Update("role", in.Role)
		return true, nil
	}

	r["organization.getById"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			OrganizationID string `json:"organizationId"`
		}
		json.Unmarshal(input, &in)
		var org schema.Organization
		if err := h.DB.First(&org, "id = ?", in.OrganizationID).Error; err != nil {
			return nil, &trpcErr{"Organization not found", "NOT_FOUND", 404}
		}
		return org, nil
	}
}
