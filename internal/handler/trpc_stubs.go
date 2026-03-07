package handler

import (
	"encoding/json"

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

	// Auth
	r["auth.logout"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
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

	// Patch stubs
	r["patch.byEntityId"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return []interface{}{}, nil
	}
	r["patch.one"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return nil, &trpcErr{"Patch not found", "NOT_FOUND", 404}
	}
	r["patch.cleanPatchRepos"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return true, nil
	}
	r["patch.delete"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return true, nil
	}
	r["patch.ensureRepo"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return true, nil
	}
	r["patch.markFileForDeletion"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return true, nil
	}
	r["patch.readRepoDirectories"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return []interface{}{}, nil
	}
	r["patch.readRepoFile"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return "", nil
	}
	r["patch.saveFileAsPatch"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return true, nil
	}
	r["patch.toggleEnabled"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return true, nil
	}
	r["patch.update"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return true, nil
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
		return false, nil
	}
	r["licenseKey.getEnterpriseSettings"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return map[string]interface{}{"enabled": false}, nil
	}
	r["licenseKey.updateEnterpriseSettings"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return true, nil
	}

	// SSO stubs
	r["sso.listProviders"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return []interface{}{}, nil
	}
	r["sso.one"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return nil, &trpcErr{"SSO provider not found", "NOT_FOUND", 404}
	}
	r["sso.register"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return true, nil
	}
	r["sso.update"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return true, nil
	}
	r["sso.deleteProvider"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return true, nil
	}
	r["sso.getTrustedOrigins"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return []interface{}{}, nil
	}
	r["sso.addTrustedOrigin"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return true, nil
	}
	r["sso.removeTrustedOrigin"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return true, nil
	}
	r["sso.updateTrustedOrigin"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return true, nil
	}

	// Admin
	r["admin.setupMonitoring"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return true, nil
	}
}
