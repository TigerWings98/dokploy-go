// Input: echo, encoding/json, schema
// Output: Stub procedure 实现 (Stripe/AI/LicenseKey/Cluster/Swarm 企业功能)
// Role: 企业功能 stub 层，为自托管模式提供无操作的 tRPC procedure 响应，避免前端报错
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package handler

import (
	"encoding/json"
	"net/http"

	"github.com/dokploy/dokploy/internal/db/schema"
	"github.com/labstack/echo/v4"
)

func (h *Handler) registerStubsTRPC(r procedureRegistry) {
	// Stripe (self-hosted mode stubs)
	r["stripe.canCreateMoreServers"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return true, nil
	}
	r["stripe.createCheckoutSession"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return nil, &trpcErr{"Not available in self-hosted mode", "BAD_REQUEST", 400}
	}
	r["stripe.createCustomerPortalSession"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return nil, &trpcErr{"Not available in self-hosted mode", "BAD_REQUEST", 400}
	}
	r["stripe.getInvoices"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return []interface{}{}, nil
	}
	r["stripe.getProducts"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return []interface{}{}, nil
	}
	r["stripe.upgradeSubscription"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return nil, &trpcErr{"Not available in self-hosted mode", "BAD_REQUEST", 400}
	}
	r["stripe.getCurrentPlan"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return nil, nil // self-hosted: no subscription plan
	}

	// Auth
	r["auth.logout"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		token := getSessionToken(c)
		if token != "" {
			h.DB.Where("token = ?", token).Delete(&schema.Session{})
		}
		cookie := &http.Cookie{
			Name:     "better-auth.session_token",
			Value:    "",
			Path:     "/",
			MaxAge:   -1,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		}
		c.SetCookie(cookie)
		return true, nil
	}

	// Cluster stubs (requires Docker Swarm)
	r["cluster.addManager"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return "", nil
	}
	r["cluster.addWorker"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return "", nil
	}
	r["cluster.getNodes"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return []interface{}{}, nil
	}
	r["cluster.removeWorker"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return true, nil
	}

	// Swarm stubs
	r["swarm.getNodes"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return []interface{}{}, nil
	}
	r["swarm.getNodeInfo"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return map[string]interface{}{}, nil
	}
	r["swarm.getNodeApps"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return []interface{}{}, nil
	}
	r["swarm.getAppInfos"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return []interface{}{}, nil
	}

	// AI stubs
	r["ai.getAll"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return []interface{}{}, nil
	}
	r["ai.one"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return nil, &trpcErr{"Not found", "NOT_FOUND", 404}
	}
	r["ai.create"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return true, nil
	}
	r["ai.update"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return true, nil
	}
	r["ai.delete"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return true, nil
	}
	r["ai.deploy"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return true, nil
	}
	r["ai.getModels"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return []interface{}{}, nil
	}
	r["ai.suggest"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return "", nil
	}

	// LicenseKey stubs
	r["licenseKey.activate"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return true, nil
	}
	r["licenseKey.deactivate"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return true, nil
	}
	r["licenseKey.validate"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return true, nil
	}
	r["licenseKey.haveValidLicenseKey"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		// 从数据库读取 owner 用户的 enableEnterpriseFeatures 字段
		// 用户需在设置页面手动开启
		var owner schema.Member
		if err := h.DB.Preload("User").Where("role = ?", "owner").Order("created_at ASC").First(&owner).Error; err != nil {
			return false, nil
		}
		if owner.User == nil {
			return false, nil
		}
		return owner.User.EnableEnterpriseFeatures, nil
	}
	r["licenseKey.getEnterpriseSettings"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var owner schema.Member
		if err := h.DB.Preload("User").Where("role = ?", "owner").Order("created_at ASC").First(&owner).Error; err != nil {
			return map[string]interface{}{"enabled": false}, nil
		}
		if owner.User == nil {
			return map[string]interface{}{"enabled": false}, nil
		}
		return map[string]interface{}{
			"enabled":                  owner.User.EnableEnterpriseFeatures,
			"licenseKey":               "",
			"isValidEnterpriseLicense": owner.User.EnableEnterpriseFeatures,
		}, nil
	}
	r["licenseKey.updateEnterpriseSettings"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			Enabled bool `json:"enabled"`
		}
		json.Unmarshal(input, &in)
		// 找到 owner 用户，更新企业功能开关
		var owner schema.Member
		if err := h.DB.Preload("User").Where("role = ?", "owner").Order("created_at ASC").First(&owner).Error; err != nil {
			return nil, &trpcErr{"Owner not found", "NOT_FOUND", 404}
		}
		h.DB.Model(owner.User).Updates(map[string]interface{}{
			"enableEnterpriseFeatures": in.Enabled,
			"isValidEnterpriseLicense": in.Enabled,
		})
		return true, nil
	}

	// Admin
	r["admin.setupMonitoring"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return true, nil
	}
}
