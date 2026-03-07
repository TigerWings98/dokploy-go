package handler

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/dokploy/dokploy/internal/db/schema"
	"github.com/labstack/echo/v4"
	gonanoid "github.com/matoous/go-nanoid/v2"
)

func (h *Handler) registerMiscTRPC(r procedureRegistry) {
	// ===== DESTINATION (S3) =====
	r["destination.one"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			DestinationID string `json:"destinationId"`
		}
		json.Unmarshal(input, &in)
		var dest schema.Destination
		if err := h.DB.First(&dest, "\"destinationId\" = ?", in.DestinationID).Error; err != nil {
			return nil, &trpcErr{"Destination not found", "NOT_FOUND", 404}
		}
		return dest, nil
	}

	r["destination.all"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		member, err := h.getDefaultMember(c)
		if err != nil {
			return nil, err
		}
		var dests []schema.Destination
		h.DB.Where("\"organizationId\" = ?", member.OrganizationID).Find(&dests)
		if dests == nil {
			dests = []schema.Destination{}
		}
		return dests, nil
	}

	r["destination.create"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		member, err := h.getDefaultMember(c)
		if err != nil {
			return nil, err
		}
		var dest schema.Destination
		json.Unmarshal(input, &dest)
		dest.OrganizationID = member.OrganizationID
		if err := h.DB.Create(&dest).Error; err != nil {
			return nil, err
		}
		return dest, nil
	}

	r["destination.remove"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			DestinationID string `json:"destinationId"`
		}
		json.Unmarshal(input, &in)
		h.DB.Delete(&schema.Destination{}, "\"destinationId\" = ?", in.DestinationID)
		return true, nil
	}

	r["destination.update"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in map[string]interface{}
		json.Unmarshal(input, &in)
		id, _ := in["destinationId"].(string)
		delete(in, "destinationId")
		var dest schema.Destination
		if err := h.DB.First(&dest, "\"destinationId\" = ?", id).Error; err != nil {
			return nil, &trpcErr{"Destination not found", "NOT_FOUND", 404}
		}
		h.DB.Model(&dest).Updates(in)
		return dest, nil
	}

	r["destination.testConnection"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			AccessKey      string `json:"accessKey"`
			SecretAccessKey string `json:"secretAccessKey"`
			Bucket         string `json:"bucket"`
			Region         string `json:"region"`
			Endpoint       string `json:"endpoint"`
		}
		json.Unmarshal(input, &in)
		endpoint := in.Endpoint
		if !strings.HasPrefix(endpoint, "http") {
			endpoint = "https://" + endpoint
		}
		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Head(endpoint)
		if err != nil {
			return nil, &trpcErr{fmt.Sprintf("Cannot reach endpoint: %s", err.Error()), "BAD_REQUEST", 400}
		}
		resp.Body.Close()
		return true, nil
	}

	// ===== MOUNTS =====
	r["mounts.one"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			MountID string `json:"mountId"`
		}
		json.Unmarshal(input, &in)
		var m schema.Mount
		if err := h.DB.First(&m, "\"mountId\" = ?", in.MountID).Error; err != nil {
			return nil, &trpcErr{"Mount not found", "NOT_FOUND", 404}
		}
		return m, nil
	}

	r["mounts.create"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var m schema.Mount
		json.Unmarshal(input, &m)
		if err := h.DB.Create(&m).Error; err != nil {
			return nil, err
		}
		return m, nil
	}

	r["mounts.remove"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			MountID string `json:"mountId"`
		}
		json.Unmarshal(input, &in)
		h.DB.Delete(&schema.Mount{}, "\"mountId\" = ?", in.MountID)
		return true, nil
	}

	r["mounts.update"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in map[string]interface{}
		json.Unmarshal(input, &in)
		id, _ := in["mountId"].(string)
		delete(in, "mountId")
		h.DB.Model(&schema.Mount{}).Where("\"mountId\" = ?", id).Updates(in)
		return true, nil
	}

	r["mounts.listByServiceId"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			ServiceID   string `json:"serviceId"`
			ServiceType string `json:"serviceType"`
		}
		json.Unmarshal(input, &in)
		colMap := map[string]string{
			"application": "applicationId",
			"postgres":    "postgresId",
			"mysql":       "mysqlId",
			"mariadb":     "mariadbId",
			"mongo":       "mongoId",
			"redis":       "redisId",
			"compose":     "composeId",
		}
		col, ok := colMap[in.ServiceType]
		if !ok {
			return []schema.Mount{}, nil
		}
		var mounts []schema.Mount
		h.DB.Where(fmt.Sprintf("\"%s\" = ?", col), in.ServiceID).Find(&mounts)
		if mounts == nil {
			mounts = []schema.Mount{}
		}
		return mounts, nil
	}

	// ===== PORT =====
	r["port.one"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			PortID string `json:"portId"`
		}
		json.Unmarshal(input, &in)
		var p schema.Port
		if err := h.DB.First(&p, "\"portId\" = ?", in.PortID).Error; err != nil {
			return nil, &trpcErr{"Port not found", "NOT_FOUND", 404}
		}
		return p, nil
	}

	r["port.create"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var p schema.Port
		json.Unmarshal(input, &p)
		if err := h.DB.Create(&p).Error; err != nil {
			return nil, err
		}
		return p, nil
	}

	r["port.delete"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			PortID string `json:"portId"`
		}
		json.Unmarshal(input, &in)
		h.DB.Delete(&schema.Port{}, "\"portId\" = ?", in.PortID)
		return true, nil
	}

	r["port.update"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in map[string]interface{}
		json.Unmarshal(input, &in)
		id, _ := in["portId"].(string)
		delete(in, "portId")
		h.DB.Model(&schema.Port{}).Where("\"portId\" = ?", id).Updates(in)
		return true, nil
	}

	// ===== REDIRECTS =====
	r["redirects.one"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			RedirectID string `json:"redirectId"`
		}
		json.Unmarshal(input, &in)
		var rd schema.Redirect
		if err := h.DB.First(&rd, "\"redirectId\" = ?", in.RedirectID).Error; err != nil {
			return nil, &trpcErr{"Redirect not found", "NOT_FOUND", 404}
		}
		return rd, nil
	}

	r["redirects.create"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var rd schema.Redirect
		json.Unmarshal(input, &rd)
		if err := h.DB.Create(&rd).Error; err != nil {
			return nil, err
		}
		return rd, nil
	}

	r["redirects.delete"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			RedirectID string `json:"redirectId"`
		}
		json.Unmarshal(input, &in)
		h.DB.Delete(&schema.Redirect{}, "\"redirectId\" = ?", in.RedirectID)
		return true, nil
	}

	r["redirects.update"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in map[string]interface{}
		json.Unmarshal(input, &in)
		id, _ := in["redirectId"].(string)
		delete(in, "redirectId")
		h.DB.Model(&schema.Redirect{}).Where("\"redirectId\" = ?", id).Updates(in)
		return true, nil
	}

	// ===== SECURITY =====
	r["security.one"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			SecurityID string `json:"securityId"`
		}
		json.Unmarshal(input, &in)
		var s schema.Security
		if err := h.DB.First(&s, "\"securityId\" = ?", in.SecurityID).Error; err != nil {
			return nil, &trpcErr{"Security not found", "NOT_FOUND", 404}
		}
		return s, nil
	}

	r["security.create"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var s schema.Security
		json.Unmarshal(input, &s)
		if err := h.DB.Create(&s).Error; err != nil {
			return nil, err
		}
		return s, nil
	}

	r["security.delete"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			SecurityID string `json:"securityId"`
		}
		json.Unmarshal(input, &in)
		h.DB.Delete(&schema.Security{}, "\"securityId\" = ?", in.SecurityID)
		return true, nil
	}

	r["security.update"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in map[string]interface{}
		json.Unmarshal(input, &in)
		id, _ := in["securityId"].(string)
		delete(in, "securityId")
		h.DB.Model(&schema.Security{}).Where("\"securityId\" = ?", id).Updates(in)
		return true, nil
	}

	// ===== DOMAIN =====
	r["domain.one"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			DomainID string `json:"domainId"`
		}
		json.Unmarshal(input, &in)
		var d schema.Domain
		if err := h.DB.First(&d, "\"domainId\" = ?", in.DomainID).Error; err != nil {
			return nil, &trpcErr{"Domain not found", "NOT_FOUND", 404}
		}
		return d, nil
	}

	r["domain.create"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var d schema.Domain
		json.Unmarshal(input, &d)
		if err := h.DB.Create(&d).Error; err != nil {
			return nil, err
		}
		return d, nil
	}

	r["domain.delete"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			DomainID string `json:"domainId"`
		}
		json.Unmarshal(input, &in)
		h.DB.Delete(&schema.Domain{}, "\"domainId\" = ?", in.DomainID)
		return true, nil
	}

	r["domain.update"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in map[string]interface{}
		json.Unmarshal(input, &in)
		id, _ := in["domainId"].(string)
		delete(in, "domainId")
		h.DB.Model(&schema.Domain{}).Where("\"domainId\" = ?", id).Updates(in)
		return true, nil
	}

	r["domain.byApplicationId"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			ApplicationID string `json:"applicationId"`
		}
		json.Unmarshal(input, &in)
		var domains []schema.Domain
		h.DB.Where("\"applicationId\" = ?", in.ApplicationID).Find(&domains)
		if domains == nil {
			domains = []schema.Domain{}
		}
		return domains, nil
	}

	r["domain.byComposeId"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			ComposeID string `json:"composeId"`
		}
		json.Unmarshal(input, &in)
		var domains []schema.Domain
		h.DB.Where("\"composeId\" = ?", in.ComposeID).Find(&domains)
		if domains == nil {
			domains = []schema.Domain{}
		}
		return domains, nil
	}

	r["domain.generateDomain"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			ApplicationID *string `json:"applicationId"`
			ComposeID     *string `json:"composeId"`
			ServerID      *string `json:"serverId"`
		}
		json.Unmarshal(input, &in)

		settings, _ := h.getOrCreateSettings()
		serverIP := "0.0.0.0"
		if settings != nil && settings.ServerIP != nil {
			serverIP = *settings.ServerIP
		}

		slug, _ := gonanoid.Generate("abcdefghijklmnopqrstuvwxyz", 8)
		host := fmt.Sprintf("%s.traefik.me", slug)

		domain := schema.Domain{
			Host:          host,
			ApplicationID: in.ApplicationID,
			ComposeID:     in.ComposeID,
		}
		_ = serverIP

		if err := h.DB.Create(&domain).Error; err != nil {
			return nil, err
		}
		return domain, nil
	}

	r["domain.validateDomain"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			Host string `json:"host"`
		}
		json.Unmarshal(input, &in)

		_, err := net.LookupHost(in.Host)
		if err != nil {
			return nil, &trpcErr{fmt.Sprintf("Domain '%s' does not resolve: %s", in.Host, err.Error()), "BAD_REQUEST", 400}
		}
		return true, nil
	}

	r["domain.canGenerateTraefikMeDomains"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		settings, _ := h.getOrCreateSettings()
		if settings != nil && settings.ServerIP != nil && *settings.ServerIP != "" {
			return true, nil
		}
		return false, nil
	}

	// ===== CERTIFICATE =====
	r["certificate.all"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		member, err := h.getDefaultMember(c)
		if err != nil {
			return nil, err
		}
		var certs []schema.Certificate
		h.DB.Where("\"organizationId\" = ?", member.OrganizationID).Find(&certs)
		if certs == nil {
			certs = []schema.Certificate{}
		}
		return certs, nil
	}

	r["certificate.one"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			CertificateID string `json:"certificateId"`
		}
		json.Unmarshal(input, &in)
		var cert schema.Certificate
		if err := h.DB.First(&cert, "\"certificateId\" = ?", in.CertificateID).Error; err != nil {
			return nil, &trpcErr{"Certificate not found", "NOT_FOUND", 404}
		}
		return cert, nil
	}

	r["certificate.create"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		member, err := h.getDefaultMember(c)
		if err != nil {
			return nil, err
		}
		var cert schema.Certificate
		json.Unmarshal(input, &cert)
		cert.OrganizationID = member.OrganizationID
		if err := h.DB.Create(&cert).Error; err != nil {
			return nil, err
		}
		return cert, nil
	}

	r["certificate.remove"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			CertificateID string `json:"certificateId"`
		}
		json.Unmarshal(input, &in)
		h.DB.Delete(&schema.Certificate{}, "\"certificateId\" = ?", in.CertificateID)
		return true, nil
	}

	// ===== REGISTRY =====
	r["registry.all"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		member, err := h.getDefaultMember(c)
		if err != nil {
			return nil, err
		}
		var regs []schema.Registry
		h.DB.Where("\"organizationId\" = ?", member.OrganizationID).Find(&regs)
		if regs == nil {
			regs = []schema.Registry{}
		}
		return regs, nil
	}

	r["registry.one"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			RegistryID string `json:"registryId"`
		}
		json.Unmarshal(input, &in)
		var reg schema.Registry
		if err := h.DB.First(&reg, "\"registryId\" = ?", in.RegistryID).Error; err != nil {
			return nil, &trpcErr{"Registry not found", "NOT_FOUND", 404}
		}
		return reg, nil
	}

	r["registry.create"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		member, err := h.getDefaultMember(c)
		if err != nil {
			return nil, err
		}
		var reg schema.Registry
		json.Unmarshal(input, &reg)
		reg.OrganizationID = member.OrganizationID
		if err := h.DB.Create(&reg).Error; err != nil {
			return nil, err
		}
		return reg, nil
	}

	r["registry.update"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in map[string]interface{}
		json.Unmarshal(input, &in)
		id, _ := in["registryId"].(string)
		delete(in, "registryId")
		h.DB.Model(&schema.Registry{}).Where("\"registryId\" = ?", id).Updates(in)
		return true, nil
	}

	r["registry.remove"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			RegistryID string `json:"registryId"`
		}
		json.Unmarshal(input, &in)
		h.DB.Delete(&schema.Registry{}, "\"registryId\" = ?", in.RegistryID)
		return true, nil
	}

	r["registry.testConnection"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		// TODO: Test Docker registry login
		return true, nil
	}
}
