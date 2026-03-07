package handler

import (
	"encoding/json"
	"net/url"
	"strings"

	"github.com/dokploy/dokploy/internal/db/schema"
	mw "github.com/dokploy/dokploy/internal/middleware"
	"github.com/labstack/echo/v4"
	"github.com/lib/pq"
)

func (h *Handler) registerSSOTRPC(r procedureRegistry) {
	r["sso.showSignInWithSSO"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var owner schema.Member
		if err := h.DB.Preload("User").Where("role = ?", "owner").Order("created_at ASC").First(&owner).Error; err != nil {
			return false, nil
		}
		if owner.User == nil {
			return false, nil
		}
		return owner.User.EnableEnterpriseFeatures && owner.User.IsValidEnterpriseLicense, nil
	}

	r["sso.listProviders"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		user := mw.GetUser(c)
		session := mw.GetSession(c)
		if user == nil || session == nil || session.ActiveOrganizationID == nil {
			return []interface{}{}, nil
		}
		var providers []schema.SSOProvider
		h.DB.Where("organization_id = ? AND user_id = ?", *session.ActiveOrganizationID, user.ID).
			Order("created_at ASC").Find(&providers)
		if providers == nil {
			providers = []schema.SSOProvider{}
		}
		return providers, nil
	}

	r["sso.one"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			ProviderID string `json:"providerId"`
		}
		json.Unmarshal(input, &in)
		user := mw.GetUser(c)
		session := mw.GetSession(c)
		if user == nil || session == nil || session.ActiveOrganizationID == nil {
			return nil, &trpcErr{"SSO provider not found", "NOT_FOUND", 404}
		}
		var provider schema.SSOProvider
		if err := h.DB.Where("provider_id = ? AND organization_id = ? AND user_id = ?",
			in.ProviderID, *session.ActiveOrganizationID, user.ID).First(&provider).Error; err != nil {
			return nil, &trpcErr{"SSO provider not found or you do not have permission to access it", "NOT_FOUND", 404}
		}
		return provider, nil
	}

	r["sso.register"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			ProviderID string   `json:"providerId"`
			Issuer     string   `json:"issuer"`
			Domains    []string `json:"domains"`
			OIDCConfig *json.RawMessage `json:"oidcConfig"`
			SAMLConfig *json.RawMessage `json:"samlConfig"`
		}
		json.Unmarshal(input, &in)
		user := mw.GetUser(c)
		session := mw.GetSession(c)
		if user == nil || session == nil || session.ActiveOrganizationID == nil {
			return nil, &trpcErr{"Authentication required", "UNAUTHORIZED", 401}
		}

		domains := normalizeDomains(in.Domains)
		if err := checkDomainConflicts(h, domains, ""); err != nil {
			return nil, err
		}

		domain := strings.Join(domains, ",")
		provider := &schema.SSOProvider{
			ProviderID:     in.ProviderID,
			Issuer:         in.Issuer,
			Domain:         domain,
			UserID:         &user.ID,
			OrganizationID: session.ActiveOrganizationID,
		}
		if in.OIDCConfig != nil {
			s := string(*in.OIDCConfig)
			provider.OIDCConfig = &s
		}
		if in.SAMLConfig != nil {
			s := string(*in.SAMLConfig)
			provider.SAMLConfig = &s
		}
		if err := h.DB.Create(provider).Error; err != nil {
			return nil, &trpcErr{"Failed to create SSO provider: " + err.Error(), "BAD_REQUEST", 400}
		}
		return map[string]bool{"success": true}, nil
	}

	r["sso.update"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			ProviderID string   `json:"providerId"`
			Issuer     string   `json:"issuer"`
			Domains    []string `json:"domains"`
			OIDCConfig *json.RawMessage `json:"oidcConfig"`
			SAMLConfig *json.RawMessage `json:"samlConfig"`
		}
		json.Unmarshal(input, &in)
		user := mw.GetUser(c)
		session := mw.GetSession(c)
		if user == nil || session == nil || session.ActiveOrganizationID == nil {
			return nil, &trpcErr{"Authentication required", "UNAUTHORIZED", 401}
		}

		var existing schema.SSOProvider
		if err := h.DB.Where("provider_id = ? AND organization_id = ? AND user_id = ?",
			in.ProviderID, *session.ActiveOrganizationID, user.ID).First(&existing).Error; err != nil {
			return nil, &trpcErr{"SSO provider not found or you do not have permission to update it", "NOT_FOUND", 404}
		}

		domains := normalizeDomains(in.Domains)
		if err := checkDomainConflicts(h, domains, in.ProviderID); err != nil {
			return nil, err
		}

		// Check trusted origins if issuer changed
		oldOrigin := normalizeTrustedOrigin(existing.Issuer)
		newOrigin := normalizeTrustedOrigin(in.Issuer)
		if oldOrigin != newOrigin {
			ownerID := getOrganizationOwnerID(h, *session.ActiveOrganizationID)
			if ownerID == "" {
				return nil, &trpcErr{"Organization owner not found", "INTERNAL_SERVER_ERROR", 500}
			}
			var owner schema.User
			h.DB.First(&owner, "id = ?", ownerID)
			origins := []string(owner.TrustedOrigins)
			found := false
			for _, o := range origins {
				if strings.EqualFold(o, newOrigin) {
					found = true
					break
				}
			}
			if !found {
				return nil, &trpcErr{"The new Issuer URL is not in the organization's trusted origins list. Please add it in Manage origins before saving.", "BAD_REQUEST", 400}
			}
		}

		updates := map[string]interface{}{
			"issuer":      in.Issuer,
			"domain":      strings.Join(domains, ","),
			"provider_id": in.ProviderID,
		}
		if in.OIDCConfig != nil {
			updates["oidc_config"] = string(*in.OIDCConfig)
		}
		if in.SAMLConfig != nil {
			updates["saml_config"] = string(*in.SAMLConfig)
		}
		h.DB.Model(&existing).Updates(updates)
		return map[string]bool{"success": true}, nil
	}

	r["sso.deleteProvider"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			ProviderID string `json:"providerId"`
		}
		json.Unmarshal(input, &in)
		user := mw.GetUser(c)
		session := mw.GetSession(c)
		if user == nil || session == nil || session.ActiveOrganizationID == nil {
			return nil, &trpcErr{"Authentication required", "UNAUTHORIZED", 401}
		}
		result := h.DB.Where("provider_id = ? AND organization_id = ? AND user_id = ?",
			in.ProviderID, *session.ActiveOrganizationID, user.ID).Delete(&schema.SSOProvider{})
		if result.RowsAffected == 0 {
			return nil, &trpcErr{"SSO provider not found or you do not have permission to delete it", "NOT_FOUND", 404}
		}
		return map[string]bool{"success": true}, nil
	}

	r["sso.getTrustedOrigins"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		session := mw.GetSession(c)
		if session == nil || session.ActiveOrganizationID == nil {
			return []string{}, nil
		}
		ownerID := getOrganizationOwnerID(h, *session.ActiveOrganizationID)
		if ownerID == "" {
			return []string{}, nil
		}
		var owner schema.User
		h.DB.First(&owner, "id = ?", ownerID)
		origins := []string(owner.TrustedOrigins)
		if origins == nil {
			return []string{}, nil
		}
		return origins, nil
	}

	r["sso.addTrustedOrigin"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			Origin string `json:"origin"`
		}
		json.Unmarshal(input, &in)
		session := mw.GetSession(c)
		if session == nil || session.ActiveOrganizationID == nil {
			return nil, &trpcErr{"Authentication required", "UNAUTHORIZED", 401}
		}
		ownerID := getOrganizationOwnerID(h, *session.ActiveOrganizationID)
		if ownerID == "" {
			return nil, &trpcErr{"Organization owner not found", "INTERNAL_SERVER_ERROR", 500}
		}
		normalized := normalizeTrustedOrigin(in.Origin)
		var owner schema.User
		h.DB.First(&owner, "id = ?", ownerID)
		origins := []string(owner.TrustedOrigins)
		for _, o := range origins {
			if strings.EqualFold(o, normalized) {
				return map[string]bool{"success": true}, nil
			}
		}
		origins = append(origins, normalized)
		h.DB.Model(&owner).Update("trustedOrigins", pq.StringArray(origins))
		return map[string]bool{"success": true}, nil
	}

	r["sso.removeTrustedOrigin"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			Origin string `json:"origin"`
		}
		json.Unmarshal(input, &in)
		session := mw.GetSession(c)
		if session == nil || session.ActiveOrganizationID == nil {
			return nil, &trpcErr{"Authentication required", "UNAUTHORIZED", 401}
		}
		ownerID := getOrganizationOwnerID(h, *session.ActiveOrganizationID)
		if ownerID == "" {
			return nil, &trpcErr{"Organization owner not found", "INTERNAL_SERVER_ERROR", 500}
		}
		normalized := normalizeTrustedOrigin(in.Origin)
		var owner schema.User
		h.DB.First(&owner, "id = ?", ownerID)
		var next []string
		for _, o := range owner.TrustedOrigins {
			if !strings.EqualFold(o, normalized) {
				next = append(next, o)
			}
		}
		if next == nil {
			next = []string{}
		}
		h.DB.Model(&owner).Update("trustedOrigins", pq.StringArray(next))
		return map[string]bool{"success": true}, nil
	}

	r["sso.updateTrustedOrigin"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			OldOrigin string `json:"oldOrigin"`
			NewOrigin string `json:"newOrigin"`
		}
		json.Unmarshal(input, &in)
		session := mw.GetSession(c)
		if session == nil || session.ActiveOrganizationID == nil {
			return nil, &trpcErr{"Authentication required", "UNAUTHORIZED", 401}
		}
		ownerID := getOrganizationOwnerID(h, *session.ActiveOrganizationID)
		if ownerID == "" {
			return nil, &trpcErr{"Organization owner not found", "INTERNAL_SERVER_ERROR", 500}
		}
		oldNorm := normalizeTrustedOrigin(in.OldOrigin)
		newNorm := normalizeTrustedOrigin(in.NewOrigin)
		var owner schema.User
		h.DB.First(&owner, "id = ?", ownerID)
		var next []string
		for _, o := range owner.TrustedOrigins {
			if strings.EqualFold(o, oldNorm) {
				next = append(next, newNorm)
			} else {
				next = append(next, o)
			}
		}
		if next == nil {
			next = []string{}
		}
		h.DB.Model(&owner).Update("trustedOrigins", pq.StringArray(next))
		return map[string]bool{"success": true}, nil
	}
}

func normalizeDomains(domains []string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, d := range domains {
		d = strings.TrimSpace(strings.ToLower(d))
		if d != "" && !seen[d] {
			seen[d] = true
			result = append(result, d)
		}
	}
	return result
}

func normalizeTrustedOrigin(origin string) string {
	origin = strings.TrimSpace(origin)
	u, err := url.Parse(origin)
	if err != nil || u.Scheme == "" {
		return strings.ToLower(origin)
	}
	return strings.ToLower(u.Scheme + "://" + u.Host)
}

func getOrganizationOwnerID(h *Handler, orgID string) string {
	var org schema.Organization
	if err := h.DB.First(&org, "id = ?", orgID).Error; err != nil {
		return ""
	}
	return org.OwnerID
}

func checkDomainConflicts(h *Handler, domains []string, excludeProviderID string) error {
	var providers []schema.SSOProvider
	h.DB.Find(&providers)
	for _, p := range providers {
		if p.ProviderID == excludeProviderID {
			continue
		}
		existingDomains := strings.Split(p.Domain, ",")
		for _, ed := range existingDomains {
			ed = strings.TrimSpace(strings.ToLower(ed))
			for _, d := range domains {
				if ed == d {
					return &trpcErr{"Domain " + d + " is already registered for another provider", "BAD_REQUEST", 400}
				}
			}
		}
	}
	return nil
}
