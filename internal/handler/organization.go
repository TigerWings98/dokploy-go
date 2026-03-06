package handler

import (
	"errors"
	"net/http"

	"github.com/dokploy/dokploy/internal/db/schema"
	mw "github.com/dokploy/dokploy/internal/middleware"
	gonanoid "github.com/matoous/go-nanoid/v2"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

func (h *Handler) registerOrganizationRoutes(g *echo.Group) {
	g.POST("", h.CreateOrganization)
	g.GET("", h.ListOrganizations)
	g.GET("/active", h.GetActiveOrganization)
	g.GET("/:organizationId", h.GetOrganization)
	g.PUT("/:organizationId", h.UpdateOrganization)
	g.DELETE("/:organizationId", h.DeleteOrganization)
	g.POST("/set-default", h.SetDefaultOrganization)

	// Member management
	g.GET("/:organizationId/members", h.ListMembers)
	g.PUT("/members/:memberId/role", h.UpdateMemberRole)
	g.DELETE("/members/:memberId", h.RemoveMember)
	g.PUT("/members/:memberId/permissions", h.UpdateMemberPermissions)

	// Invitation management
	g.GET("/invitations", h.ListInvitations)
	g.DELETE("/invitations/:invitationId", h.RemoveInvitation)
}

type CreateOrganizationRequest struct {
	Name string  `json:"name" validate:"required,min=1"`
	Logo *string `json:"logo"`
}

func (h *Handler) CreateOrganization(c echo.Context) error {
	var req CreateOrganizationRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	user := mw.GetUser(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized)
	}

	slug, _ := gonanoid.New()

	org := &schema.Organization{
		Name:    req.Name,
		Logo:    req.Logo,
		Slug:    &slug,
		OwnerID: user.ID,
	}

	tx := h.DB.Begin()

	if err := tx.Create(org).Error; err != nil {
		tx.Rollback()
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	member := &schema.Member{
		OrganizationID: org.ID,
		UserID:         user.ID,
		Role:           schema.MemberRoleOwner,
		IsDefault:      false,
	}

	if err := tx.Create(member).Error; err != nil {
		tx.Rollback()
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if err := tx.Commit().Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusCreated, org)
}

func (h *Handler) ListOrganizations(c echo.Context) error {
	user := mw.GetUser(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized)
	}

	var orgs []schema.Organization
	err := h.DB.
		Where("id IN (SELECT organization_id FROM member WHERE user_id = ?)", user.ID).
		Preload("Members", "user_id = ?", user.ID).
		Find(&orgs).Error
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, orgs)
}

func (h *Handler) GetOrganization(c echo.Context) error {
	orgID := c.Param("organizationId")
	user := mw.GetUser(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized)
	}

	// Verify membership
	var member schema.Member
	if err := h.DB.Where("organization_id = ? AND user_id = ?", orgID, user.ID).First(&member).Error; err != nil {
		return echo.NewHTTPError(http.StatusForbidden, "You are not a member of this organization")
	}

	var org schema.Organization
	if err := h.DB.First(&org, "id = ?", orgID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Organization not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, org)
}

func (h *Handler) GetActiveOrganization(c echo.Context) error {
	user := mw.GetUser(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized)
	}

	// Find the user's default member record
	var member schema.Member
	if err := h.DB.Where("user_id = ? AND is_default = ?", user.ID, true).First(&member).Error; err != nil {
		return c.JSON(http.StatusOK, nil)
	}

	var org schema.Organization
	if err := h.DB.First(&org, "id = ?", member.OrganizationID).Error; err != nil {
		return c.JSON(http.StatusOK, nil)
	}

	return c.JSON(http.StatusOK, org)
}

type UpdateOrganizationRequest struct {
	OrganizationID string  `json:"organizationId"`
	Name           string  `json:"name" validate:"required"`
	Logo           *string `json:"logo"`
}

func (h *Handler) UpdateOrganization(c echo.Context) error {
	orgID := c.Param("organizationId")
	user := mw.GetUser(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized)
	}

	var req UpdateOrganizationRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	var org schema.Organization
	if err := h.DB.First(&org, "id = ?", orgID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Organization not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	// Verify ownership
	if err := h.verifyOrgOwnership(orgID, user.ID); err != nil {
		return err
	}

	updates := map[string]interface{}{"name": req.Name}
	if req.Logo != nil {
		updates["logo"] = *req.Logo
	}

	if err := h.DB.Model(&org).Updates(updates).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, org)
}

func (h *Handler) DeleteOrganization(c echo.Context) error {
	orgID := c.Param("organizationId")
	user := mw.GetUser(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized)
	}

	var org schema.Organization
	if err := h.DB.First(&org, "id = ?", orgID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Organization not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	// Verify ownership
	if err := h.verifyOrgOwnership(orgID, user.ID); err != nil {
		return err
	}

	// Must keep at least one owned organization
	var ownedCount int64
	h.DB.Model(&schema.Organization{}).Where("owner_id = ?", user.ID).Count(&ownedCount)
	if ownedCount <= 1 {
		return echo.NewHTTPError(http.StatusForbidden, "You must maintain at least one organization where you are the owner")
	}

	if err := h.DB.Delete(&org).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.NoContent(http.StatusNoContent)
}

type SetDefaultRequest struct {
	OrganizationID string `json:"organizationId" validate:"required,min=1"`
}

func (h *Handler) SetDefaultOrganization(c echo.Context) error {
	var req SetDefaultRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	user := mw.GetUser(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized)
	}

	// Verify membership
	var member schema.Member
	if err := h.DB.Where("organization_id = ? AND user_id = ?", req.OrganizationID, user.ID).First(&member).Error; err != nil {
		return echo.NewHTTPError(http.StatusForbidden, "You are not a member of this organization")
	}

	tx := h.DB.Begin()
	// Unset all defaults for the user
	if err := tx.Model(&schema.Member{}).
		Where("user_id = ?", user.ID).
		Update("is_default", false).Error; err != nil {
		tx.Rollback()
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	// Set the requested one as default
	if err := tx.Model(&member).Update("is_default", true).Error; err != nil {
		tx.Rollback()
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	if err := tx.Commit().Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, map[string]bool{"success": true})
}

func (h *Handler) ListMembers(c echo.Context) error {
	orgID := c.Param("organizationId")

	var members []schema.Member
	if err := h.DB.
		Preload("User").
		Where("organization_id = ?", orgID).
		Find(&members).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, members)
}

type UpdateMemberRoleRequest struct {
	MemberID string `json:"memberId"`
	Role     string `json:"role" validate:"required"`
}

func (h *Handler) UpdateMemberRole(c echo.Context) error {
	memberID := c.Param("memberId")
	user := mw.GetUser(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized)
	}

	var req UpdateMemberRoleRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	if req.Role != "admin" && req.Role != "member" {
		return echo.NewHTTPError(http.StatusBadRequest, "Role must be 'admin' or 'member'")
	}

	var targetMember schema.Member
	if err := h.DB.Preload("User").First(&targetMember, "id = ?", memberID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Member not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if targetMember.UserID == user.ID {
		return echo.NewHTTPError(http.StatusForbidden, "You cannot change your own role")
	}

	if targetMember.Role == schema.MemberRoleOwner {
		return echo.NewHTTPError(http.StatusForbidden, "The owner role is intransferible")
	}

	// Check the acting user's role
	var actingMember schema.Member
	if err := h.DB.Where("organization_id = ? AND user_id = ?", targetMember.OrganizationID, user.ID).First(&actingMember).Error; err != nil {
		return echo.NewHTTPError(http.StatusForbidden, "You are not allowed to update this member's role")
	}

	// Only owner can change admin roles
	if targetMember.Role == schema.MemberRoleAdmin && actingMember.Role != schema.MemberRoleOwner {
		return echo.NewHTTPError(http.StatusForbidden, "Only the organization owner can change admin roles. Admins can only modify member roles.")
	}

	if err := h.DB.Model(&targetMember).Update("role", req.Role).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, true)
}

func (h *Handler) RemoveMember(c echo.Context) error {
	memberID := c.Param("memberId")

	result := h.DB.Delete(&schema.Member{}, "id = ?", memberID)
	if result.Error != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, result.Error.Error())
	}
	if result.RowsAffected == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "Member not found")
	}

	return c.NoContent(http.StatusNoContent)
}

type UpdateMemberPermissionsRequest struct {
	CanCreateProjects       *bool        `json:"canCreateProjects"`
	CanAccessToSSHKeys      *bool        `json:"canAccessToSSHKeys"`
	CanCreateServices       *bool        `json:"canCreateServices"`
	CanDeleteProjects       *bool        `json:"canDeleteProjects"`
	CanDeleteServices       *bool        `json:"canDeleteServices"`
	CanAccessToDocker       *bool        `json:"canAccessToDocker"`
	CanAccessToAPI          *bool        `json:"canAccessToAPI"`
	CanAccessToGitProviders *bool        `json:"canAccessToGitProviders"`
	CanAccessToTraefikFiles *bool        `json:"canAccessToTraefikFiles"`
	CanDeleteEnvironments   *bool        `json:"canDeleteEnvironments"`
	CanCreateEnvironments   *bool        `json:"canCreateEnvironments"`
	AccessedProjects        *[]string    `json:"accessedProjects"`
	AccessedEnvironments    *[]string    `json:"accessedEnvironments"`
	AccessedServices        *[]string    `json:"accessedServices"`
}

func (h *Handler) UpdateMemberPermissions(c echo.Context) error {
	memberID := c.Param("memberId")

	var req UpdateMemberPermissionsRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	var member schema.Member
	if err := h.DB.First(&member, "id = ?", memberID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Member not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	updates := make(map[string]interface{})
	if req.CanCreateProjects != nil {
		updates["canCreateProjects"] = *req.CanCreateProjects
	}
	if req.CanAccessToSSHKeys != nil {
		updates["canAccessToSSHKeys"] = *req.CanAccessToSSHKeys
	}
	if req.CanCreateServices != nil {
		updates["canCreateServices"] = *req.CanCreateServices
	}
	if req.CanDeleteProjects != nil {
		updates["canDeleteProjects"] = *req.CanDeleteProjects
	}
	if req.CanDeleteServices != nil {
		updates["canDeleteServices"] = *req.CanDeleteServices
	}
	if req.CanAccessToDocker != nil {
		updates["canAccessToDocker"] = *req.CanAccessToDocker
	}
	if req.CanAccessToAPI != nil {
		updates["canAccessToAPI"] = *req.CanAccessToAPI
	}
	if req.CanAccessToGitProviders != nil {
		updates["canAccessToGitProviders"] = *req.CanAccessToGitProviders
	}
	if req.CanAccessToTraefikFiles != nil {
		updates["canAccessToTraefikFiles"] = *req.CanAccessToTraefikFiles
	}
	if req.CanDeleteEnvironments != nil {
		updates["canDeleteEnvironments"] = *req.CanDeleteEnvironments
	}
	if req.CanCreateEnvironments != nil {
		updates["canCreateEnvironments"] = *req.CanCreateEnvironments
	}
	if req.AccessedProjects != nil {
		updates["accesedProjects"] = schema.StringArray(*req.AccessedProjects)
	}
	if req.AccessedEnvironments != nil {
		updates["accessedEnvironments"] = schema.StringArray(*req.AccessedEnvironments)
	}
	if req.AccessedServices != nil {
		updates["accesedServices"] = schema.StringArray(*req.AccessedServices)
	}

	if len(updates) > 0 {
		if err := h.DB.Model(&member).Updates(updates).Error; err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}
	}

	return c.JSON(http.StatusOK, member)
}

func (h *Handler) ListInvitations(c echo.Context) error {
	user := mw.GetUser(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized)
	}

	// Get user's default org
	var member schema.Member
	if err := h.DB.Where("user_id = ? AND is_default = ?", user.ID, true).First(&member).Error; err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "No default organization found")
	}

	var invitations []schema.Invitation
	if err := h.DB.
		Where("organization_id = ?", member.OrganizationID).
		Order("status DESC, expires_at DESC").
		Find(&invitations).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, invitations)
}

func (h *Handler) RemoveInvitation(c echo.Context) error {
	invitationID := c.Param("invitationId")
	user := mw.GetUser(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized)
	}

	var member schema.Member
	if err := h.DB.Where("user_id = ? AND is_default = ?", user.ID, true).First(&member).Error; err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "No default organization found")
	}

	var invitation schema.Invitation
	if err := h.DB.First(&invitation, "id = ?", invitationID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Invitation not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if invitation.OrganizationID != member.OrganizationID {
		return echo.NewHTTPError(http.StatusForbidden, "You are not allowed to remove this invitation")
	}

	if err := h.DB.Delete(&invitation).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.NoContent(http.StatusNoContent)
}

// verifyOrgOwnership checks if user is org owner.
func (h *Handler) verifyOrgOwnership(orgID, userID string) error {
	var org schema.Organization
	if err := h.DB.First(&org, "id = ?", orgID).Error; err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "Organization not found")
	}

	if org.OwnerID == userID {
		return nil
	}

	// Check member role
	var member schema.Member
	if err := h.DB.Where("organization_id = ? AND user_id = ?", orgID, userID).First(&member).Error; err != nil {
		return echo.NewHTTPError(http.StatusForbidden, "You are not a member of this organization")
	}

	if member.Role != schema.MemberRoleOwner {
		return echo.NewHTTPError(http.StatusForbidden, "Only the organization owner can perform this action")
	}

	return nil
}
