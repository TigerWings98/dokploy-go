// Input: procedureRegistry, db (各类表), config
// Output: registerMiscTRPC - 杂项领域 tRPC procedure 注册 (domain/redirect/security/port/mount/environment/certificate/registry/logDrain)
// Role: 杂项 tRPC 路由注册，将非核心领域的 CRUD procedure 集中注册
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package handler

import (
	"encoding/json"
	"fmt"
	"net"
	"os/exec"
	"strings"

	containertypes "github.com/docker/docker/api/types/container"
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
			AccessKey      string  `json:"accessKey"`
			SecretAccessKey string `json:"secretAccessKey"`
			Bucket         string  `json:"bucket"`
			Region         string  `json:"region"`
			Endpoint       string  `json:"endpoint"`
			Provider       *string `json:"provider"`
		}
		json.Unmarshal(input, &in)

		// 使用 rclone ls 实际测试 S3 连接（与 TS 版一致）
		rcloneFlags := []string{
			fmt.Sprintf(`--s3-access-key-id=%s`, in.AccessKey),
			fmt.Sprintf(`--s3-secret-access-key=%s`, in.SecretAccessKey),
			fmt.Sprintf(`--s3-region=%s`, in.Region),
			fmt.Sprintf(`--s3-endpoint=%s`, in.Endpoint),
			"--s3-no-check-bucket",
			"--s3-force-path-style",
			"--retries", "1",
			"--low-level-retries", "1",
			"--timeout", "10s",
			"--contimeout", "5s",
		}
		if in.Provider != nil && *in.Provider != "" {
			rcloneFlags = append([]string{fmt.Sprintf(`--s3-provider=%s`, *in.Provider)}, rcloneFlags...)
		}

		args := append([]string{"ls"}, rcloneFlags...)
		args = append(args, fmt.Sprintf(":s3:%s", in.Bucket))

		cmd := exec.CommandContext(c.Request().Context(), "rclone", args...)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return nil, &trpcErr{fmt.Sprintf("S3 connection failed: %s", strings.TrimSpace(string(output))), "BAD_REQUEST", 400}
		}
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

	// allNamedByApplicationId - returns Docker volume mounts for an application container
	r["mounts.allNamedByApplicationId"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			ApplicationID string `json:"applicationId"`
		}
		json.Unmarshal(input, &in)

		var app schema.Application
		if err := h.DB.First(&app, "\"applicationId\" = ?", in.ApplicationID).Error; err != nil {
			return nil, &trpcErr{"Application not found", "NOT_FOUND", 404}
		}

		if h.Docker == nil {
			return []interface{}{}, nil
		}

		containers, err := h.Docker.DockerClient().ContainerList(c.Request().Context(), containertypes.ListOptions{
			Filters: filtersFromStrings([]string{"name=" + app.AppName}),
		})
		if err != nil || len(containers) == 0 {
			return []interface{}{}, nil
		}

		inspect, err := h.Docker.DockerClient().ContainerInspect(c.Request().Context(), containers[0].ID)
		if err != nil {
			return []interface{}{}, nil
		}

		var result []map[string]interface{}
		for _, m := range inspect.Mounts {
			if m.Type == "volume" && m.Name != "" {
				result = append(result, map[string]interface{}{
					"Type":        string(m.Type),
					"Name":        m.Name,
					"Source":      m.Source,
					"Destination": m.Destination,
					"Driver":      m.Driver,
					"RW":          m.RW,
				})
			}
		}
		if result == nil {
			result = []map[string]interface{}{}
		}
		return result, nil
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
		// 同步 Traefik 配置（与 TS 版 manageDomain 一致）
		h.generateTraefikForDomain(&d)
		return d, nil
	}

	r["domain.delete"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			DomainID string `json:"domainId"`
		}
		json.Unmarshal(input, &in)

		// 先读取 domain 信息用于 Traefik 清理
		var domain schema.Domain
		if err := h.DB.First(&domain, "\"domainId\" = ?", in.DomainID).Error; err != nil {
			return nil, &trpcErr{"Domain not found", "NOT_FOUND", 404}
		}
		appName := h.resolveAppName(&domain)

		h.DB.Delete(&schema.Domain{}, "\"domainId\" = ?", in.DomainID)

		// 删除后同步 Traefik 配置（与 TS 版 removeDomain 一致）
		if h.Traefik != nil && appName != "" {
			var count int64
			if domain.ApplicationID != nil {
				h.DB.Model(&schema.Domain{}).Where("\"applicationId\" = ?", *domain.ApplicationID).Count(&count)
			} else if domain.ComposeID != nil {
				h.DB.Model(&schema.Domain{}).Where("\"composeId\" = ?", *domain.ComposeID).Count(&count)
			}
			if count == 0 {
				h.Traefik.RemoveApplicationConfig(appName)
			} else {
				// 还有其他域名，重新生成配置
				h.regenerateTraefikForApp(appName, domain.ApplicationID, domain.ComposeID)
			}
		}
		return true, nil
	}

	r["domain.update"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in map[string]interface{}
		json.Unmarshal(input, &in)
		id, _ := in["domainId"].(string)
		delete(in, "domainId")
		h.DB.Model(&schema.Domain{}).Where("\"domainId\" = ?", id).Updates(in)

		// 更新后重新生成 Traefik 配置（与 TS 版 manageDomain 一致）
		var domain schema.Domain
		if err := h.DB.First(&domain, "\"domainId\" = ?", id).Error; err == nil {
			h.generateTraefikForDomain(&domain)
		}
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

	testRegistryByID := func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			RegistryID string `json:"registryId"`
		}
		json.Unmarshal(input, &in)
		var reg schema.Registry
		if err := h.DB.First(&reg, "\"registryId\" = ?", in.RegistryID).Error; err != nil {
			return nil, &trpcErr{"Registry not found", "NOT_FOUND", 404}
		}
		if h.Docker != nil {
			if err := h.Docker.TestRegistryLogin(c.Request().Context(), reg.RegistryURL, reg.Username, reg.Password); err != nil {
				return nil, &trpcErr{"Registry connection failed: " + err.Error(), "BAD_REQUEST", 400}
			}
		}
		return true, nil
	}
	r["registry.testConnection"] = testRegistryByID
	r["registry.testRegistryById"] = testRegistryByID
	r["registry.testRegistry"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			RegistryURL string  `json:"registryUrl"`
			Username    string  `json:"username"`
			Password    string  `json:"password"`
			ServerID    *string `json:"serverId"`
		}
		json.Unmarshal(input, &in)
		if h.Docker != nil {
			if err := h.Docker.TestRegistryLogin(c.Request().Context(), in.RegistryURL, in.Username, in.Password); err != nil {
				return nil, &trpcErr{"Registry connection failed: " + err.Error(), "BAD_REQUEST", 400}
			}
		}
		return true, nil
	}

}
