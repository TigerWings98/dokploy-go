// Input: echo, encoding/json, schema, docker, process
// Output: Stub procedure 实现 (Stripe/AI/LicenseKey 企业功能) + Cluster/Swarm 实现
// Role: 企业功能 stub 层 + Docker Swarm 集群管理 tRPC procedure
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/dokploy/dokploy/internal/db/schema"
	"github.com/dokploy/dokploy/internal/process"
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

	// ── Cluster（Docker Swarm 集群管理）──

	r["cluster.addWorker"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			ServerID *string `json:"serverId"`
		}
		json.Unmarshal(input, &in)
		return h.getSwarmJoinCommand(in.ServerID, false)
	}

	r["cluster.addManager"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			ServerID *string `json:"serverId"`
		}
		json.Unmarshal(input, &in)
		return h.getSwarmJoinCommand(in.ServerID, true)
	}

	r["cluster.getNodes"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			ServerID *string `json:"serverId"`
		}
		json.Unmarshal(input, &in)

		if in.ServerID != nil && *in.ServerID != "" {
			// 远程服务器：通过 CLI 获取节点列表
			out := h.execRemoteOrLocal(`docker node ls --format '{{json .}}'`, in.ServerID)
			return parseJSONLines(out), nil
		}
		// 本地：使用 Docker SDK
		if h.Docker == nil {
			return []interface{}{}, nil
		}
		nodes, err := h.Docker.DockerClient().NodeList(context.Background(), types.NodeListOptions{})
		if err != nil {
			return []interface{}{}, nil
		}
		return nodes, nil
	}

	r["cluster.removeWorker"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			NodeID   string  `json:"nodeId"`
			ServerID *string `json:"serverId"`
		}
		json.Unmarshal(input, &in)
		if in.NodeID == "" {
			return nil, &trpcErr{"Node ID is required", "BAD_REQUEST", 400}
		}
		// 先排空节点，再强制移除
		drainCmd := fmt.Sprintf("docker node update --availability drain %s", in.NodeID)
		removeCmd := fmt.Sprintf("docker node rm %s --force", in.NodeID)
		if in.ServerID != nil && *in.ServerID != "" {
			h.execRemoteOrLocal(drainCmd, in.ServerID)
			h.execRemoteOrLocal(removeCmd, in.ServerID)
		} else {
			process.ExecAsync(drainCmd)
			process.ExecAsync(removeCmd)
		}
		return true, nil
	}

	// ── Swarm（Swarm 面板，节点/服务查看）──

	r["swarm.getNodes"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			ServerID *string `json:"serverId"`
		}
		json.Unmarshal(input, &in)
		cmd := `docker node ls --format '{{json .}}'`
		var out string
		if in.ServerID != nil && *in.ServerID != "" {
			out = h.execRemoteOrLocal(cmd, in.ServerID)
		} else {
			result, _ := process.ExecAsync(cmd)
			if result != nil {
				out = result.Stdout
			}
		}
		return parseJSONLines(out), nil
	}

	r["swarm.getNodeInfo"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			NodeID   string  `json:"nodeId"`
			ServerID *string `json:"serverId"`
		}
		json.Unmarshal(input, &in)
		if in.NodeID == "" {
			return nil, &trpcErr{"Node ID is required", "BAD_REQUEST", 400}
		}
		cmd := fmt.Sprintf(`docker node inspect %s --format '{{json .}}'`, in.NodeID)
		var out string
		if in.ServerID != nil && *in.ServerID != "" {
			out = h.execRemoteOrLocal(cmd, in.ServerID)
		} else {
			result, _ := process.ExecAsync(cmd)
			if result != nil {
				out = result.Stdout
			}
		}
		out = strings.TrimSpace(out)
		if out == "" {
			return map[string]interface{}{}, nil
		}
		var info interface{}
		if json.Unmarshal([]byte(out), &info) != nil {
			return map[string]interface{}{}, nil
		}
		return info, nil
	}

	r["swarm.getNodeApps"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			ServerID *string `json:"serverId"`
		}
		json.Unmarshal(input, &in)
		cmd := `docker service ls --format '{{json .}}'`
		var out string
		if in.ServerID != nil && *in.ServerID != "" {
			out = h.execRemoteOrLocal(cmd, in.ServerID)
		} else {
			result, _ := process.ExecAsync(cmd)
			if result != nil {
				out = result.Stdout
			}
		}
		// 过滤掉 dokploy-* 内部服务
		all := parseJSONLines(out)
		var filtered []interface{}
		for _, item := range all {
			if m, ok := item.(map[string]interface{}); ok {
				name, _ := m["Name"].(string)
				if name == "" {
					name, _ = m["name"].(string)
				}
				if strings.HasPrefix(name, "dokploy-") {
					continue
				}
			}
			filtered = append(filtered, item)
		}
		if filtered == nil {
			filtered = []interface{}{}
		}
		return filtered, nil
	}

	r["swarm.getAppInfos"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			AppName  []string `json:"appName"`
			ServerID *string  `json:"serverId"`
		}
		json.Unmarshal(input, &in)
		if len(in.AppName) == 0 {
			return []interface{}{}, nil
		}
		names := strings.Join(in.AppName, " ")
		cmd := fmt.Sprintf(`docker service ps %s --format '{{json .}}' --no-trunc`, names)
		var out string
		if in.ServerID != nil && *in.ServerID != "" {
			out = h.execRemoteOrLocal(cmd, in.ServerID)
		} else {
			result, _ := process.ExecAsync(cmd)
			if result != nil {
				out = result.Stdout
			}
		}
		return parseJSONLines(out), nil
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

}

// getSwarmJoinCommand 获取 Docker Swarm 加入命令（manager 或 worker）
func (h *Handler) getSwarmJoinCommand(serverID *string, manager bool) (interface{}, error) {
	if serverID != nil && *serverID != "" {
		// 远程服务器：通过 CLI 获取
		tokenType := "worker"
		if manager {
			tokenType = "manager"
		}
		tokenCmd := fmt.Sprintf("docker swarm join-token %s -q", tokenType)
		token := strings.TrimSpace(h.execRemoteOrLocal(tokenCmd, serverID))

		ipCmd := "hostname -I | awk '{print $1}'"
		ip := strings.TrimSpace(h.execRemoteOrLocal(ipCmd, serverID))

		versionCmd := "docker version --format '{{.Server.Version}}'"
		version := strings.TrimSpace(h.execRemoteOrLocal(versionCmd, serverID))

		if token == "" {
			return nil, &trpcErr{"Failed to get swarm join token. Is this node a swarm manager?", "BAD_REQUEST", 400}
		}
		return map[string]string{
			"command": fmt.Sprintf("docker swarm join --token %s %s:2377", token, ip),
			"version": version,
		}, nil
	}

	// 本地：使用 Docker SDK
	if h.Docker == nil {
		return nil, &trpcErr{"Docker client not available", "INTERNAL_SERVER_ERROR", 500}
	}
	ctx := context.Background()
	info, err := h.Docker.DockerClient().SwarmInspect(ctx)
	if err != nil {
		return nil, &trpcErr{fmt.Sprintf("Failed to inspect swarm: %v", err), "BAD_REQUEST", 400}
	}

	token := info.JoinTokens.Worker
	if manager {
		token = info.JoinTokens.Manager
	}

	// 获取本机 IP
	sysInfo, _ := h.Docker.DockerClient().Info(ctx)
	ip := sysInfo.Swarm.NodeAddr
	if ip == "" {
		// 降级：从命令行获取
		result, _ := process.ExecAsync("hostname -I | awk '{print $1}'")
		if result != nil {
			ip = strings.TrimSpace(result.Stdout)
		}
	}

	version := sysInfo.ServerVersion

	return map[string]string{
		"command": fmt.Sprintf("docker swarm join --token %s %s:2377", token, ip),
		"version": version,
	}, nil
}

// parseJSONLines 解析多行 JSON 输出（每行一个 JSON 对象），返回 []interface{}
func parseJSONLines(output string) []interface{} {
	var results []interface{}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var obj interface{}
		if json.Unmarshal([]byte(line), &obj) == nil {
			results = append(results, obj)
		}
	}
	if results == nil {
		results = []interface{}{}
	}
	return results
}
