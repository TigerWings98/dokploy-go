// Input: procedureRegistry, db (WebServerSettings/Admin 表), setup (LetsEncrypt)
// Output: registerSettingsTRPC - Settings/Admin 领域的 tRPC procedure 注册
// Role: Settings tRPC 路由注册，将 settings/admin.* procedure 绑定到系统设置和管理员操作
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package handler

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/dokploy/dokploy/internal/db/schema"
	"github.com/dokploy/dokploy/internal/process"
	"github.com/labstack/echo/v4"
	"gopkg.in/yaml.v3"
)

// treeDataItem 与 TS 版 TreeDataItem 一致的目录树结构
type treeDataItem struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Type     string         `json:"type"` // "file" or "directory"
	Children []treeDataItem `json:"children,omitempty"`
}

// readDirectoryTree 递归读取目录树（与 TS 版 readDirectory 本地模式一致）
func readDirectoryTree(dirPath string) []treeDataItem {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return []treeDataItem{}
	}
	var result []treeDataItem
	for _, entry := range entries {
		fullPath := filepath.Join(dirPath, entry.Name())
		if entry.IsDir() {
			item := treeDataItem{
				ID:       fullPath,
				Name:     entry.Name(),
				Type:     "directory",
				Children: readDirectoryTree(fullPath),
			}
			result = append(result, item)
		} else {
			result = append(result, treeDataItem{
				ID:   fullPath,
				Name: entry.Name(),
				Type: "file",
			})
		}
	}
	if result == nil {
		result = []treeDataItem{}
	}
	return result
}

// base64Encode 将字符串进行 base64 编码（用于远程写文件）
func base64Encode(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}

func (h *Handler) registerSettingsTRPC(r procedureRegistry) {
	r["settings.getWebServerSettings"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		settings, err := h.getOrCreateSettings()
		if err != nil {
			return nil, err
		}
		return settings, nil
	}

	r["settings.isCloud"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return false, nil
	}

	r["settings.haveTraefikDashboardPortEnabled"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			ServerID *string `json:"serverId"`
		}
		json.Unmarshal(input, &in)
		ports, err := h.readDockerPorts("dokploy-traefik", in.ServerID)
		if err != nil {
			return false, nil
		}
		for _, p := range ports {
			if p.TargetPort == 8080 {
				return true, nil
			}
		}
		return false, nil
	}

	r["settings.getReleaseTag"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		if h.Updater != nil {
			return map[string]string{"releaseTag": h.Updater.GetReleaseTag()}, nil
		}
		return map[string]string{"releaseTag": "latest"}, nil
	}

	r["settings.getDokployVersion"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		if h.Updater != nil {
			return h.Updater.GetVersion(), nil
		}
		return "v0.0.0-dev", nil
	}

	r["settings.health"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return map[string]string{"status": "ok"}, nil
	}

	r["settings.getUpdateData"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		if h.Updater != nil {
			return h.Updater.CheckUpdate(), nil
		}
		return map[string]interface{}{
			"updateAvailable": false,
			"latestVersion":   nil,
		}, nil
	}

	r["settings.getIp"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		settings, _ := h.getOrCreateSettings()
		if settings != nil && settings.ServerIP != nil {
			return *settings.ServerIP, nil
		}
		return "", nil
	}

	r["settings.assignDomainServer"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			Host             string  `json:"host"`
			CertificateType  string  `json:"certificateType"`
			LetsEncryptEmail *string `json:"letsEncryptEmail"`
			HTTPS            *bool   `json:"https"`
		}
		json.Unmarshal(input, &in)
		settings, err := h.getOrCreateSettings()
		if err != nil {
			return nil, err
		}
		updates := map[string]interface{}{
			"host":             in.Host,
			"certificateType":  in.CertificateType,
			"letsEncryptEmail": in.LetsEncryptEmail,
		}
		if in.HTTPS != nil {
			updates["https"] = *in.HTTPS
		}
		h.DB.Model(settings).Updates(updates)
		return settings, nil
	}

	r["settings.updateServerIp"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ ServerIP string `json:"serverIp"` }
		json.Unmarshal(input, &in)
		settings, _ := h.getOrCreateSettings()
		h.DB.Model(settings).Update("serverIp", in.ServerIP)
		return settings, nil
	}

	r["settings.cleanUnusedImages"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ ServerID *string `json:"serverId"` }
		json.Unmarshal(input, &in)
		h.execDockerCleanup(in.ServerID, "docker image prune --all --force")
		return true, nil
	}

	r["settings.cleanUnusedVolumes"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ ServerID *string `json:"serverId"` }
		json.Unmarshal(input, &in)
		h.execDockerCleanup(in.ServerID, "docker volume prune --all --force")
		return true, nil
	}

	r["settings.cleanStoppedContainers"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ ServerID *string `json:"serverId"` }
		json.Unmarshal(input, &in)
		h.execDockerCleanup(in.ServerID, "docker container prune --force")
		return true, nil
	}

	r["settings.cleanAll"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ ServerID *string `json:"serverId"` }
		json.Unmarshal(input, &in)
		// Volume cleanup excluded from cleanAll to prevent data loss
		// (volumes attached to stopped containers could be deleted)
		// See: https://github.com/Dokploy/dokploy/pull/3267
		cmds := []string{
			"docker container prune --force",
			"docker image prune --all --force",
			"docker builder prune --all --force",
			"docker system prune --all --force",
		}
		go func() {
			for _, cmd := range cmds {
				h.execDockerCleanup(in.ServerID, cmd)
			}
		}()
		return map[string]string{
			"status":  "scheduled",
			"message": "Docker cleanup has been initiated in the background",
		}, nil
	}

	r["settings.cleanDockerBuilder"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ ServerID *string `json:"serverId"` }
		json.Unmarshal(input, &in)
		h.execDockerCleanup(in.ServerID, "docker builder prune --all --force")
		return true, nil
	}

	r["settings.readTraefikConfig"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		if h.Traefik != nil {
			config, _ := h.Traefik.ReadMainConfig()
			return config, nil
		}
		return "", nil
	}

	r["settings.updateTraefikConfig"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ TraefikConfig string `json:"traefikConfig"` }
		json.Unmarshal(input, &in)
		if h.Traefik != nil {
			h.Traefik.WriteMainConfig(in.TraefikConfig)
		}
		return true, nil
	}

	r["settings.reloadTraefik"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			ServerID *string `json:"serverId"`
		}
		json.Unmarshal(input, &in)
		go h.reloadDockerResource("dokploy-traefik", in.ServerID)
		return true, nil
	}

	r["settings.reloadServer"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		if h.Updater != nil {
			if err := h.Updater.ReloadService(); err != nil {
				return nil, &trpcErr{err.Error(), "INTERNAL_SERVER_ERROR", 500}
			}
		}
		return true, nil
	}

	r["settings.updateDockerCleanup"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			EnableDockerCleanup bool    `json:"enableDockerCleanup"`
			ServerID            *string `json:"serverId"`
		}
		json.Unmarshal(input, &in)

		// Daily cleanup at 23:50
		const cleanupCron = "50 23 * * *"

		if in.ServerID != nil {
			h.DB.Model(&schema.Server{}).Where("\"serverId\" = ?", *in.ServerID).
				Update("enableDockerCleanup", in.EnableDockerCleanup)

			jobName := "docker-cleanup-" + *in.ServerID
			if in.EnableDockerCleanup {
				// Verify server is active
				var srv schema.Server
				if err := h.DB.Preload("SSHKey").First(&srv, "\"serverId\" = ?", *in.ServerID).Error; err != nil {
					return nil, &trpcErr{"Server not found", "NOT_FOUND", 404}
				}
				if srv.ServerStatus == "inactive" {
					return nil, &trpcErr{"Server is inactive", "BAD_REQUEST", 400}
				}
				serverID := *in.ServerID
				if h.Scheduler != nil {
					h.Scheduler.AddFunc(jobName, cleanupCron, func() {
						log.Printf("[Docker Cleanup] Running for server %s", serverID)
						cmds := []string{
							"docker container prune --force",
							"docker image prune --all --force",
							"docker builder prune --all --force",
							"docker system prune --all --force",
						}
						for _, cmd := range cmds {
							h.execDockerCleanup(&serverID, cmd)
						}
					})
				}
			} else {
				if h.Scheduler != nil {
					h.Scheduler.RemoveJob(jobName)
				}
			}
		} else {
			settings, _ := h.getOrCreateSettings()
			h.DB.Model(settings).Update("enableDockerCleanup", in.EnableDockerCleanup)

			jobName := "docker-cleanup"
			if in.EnableDockerCleanup {
				if h.Scheduler != nil {
					h.Scheduler.AddFunc(jobName, cleanupCron, func() {
						log.Printf("[Docker Cleanup] Running for local server")
						cmds := []string{
							"docker container prune --force",
							"docker image prune --all --force",
							"docker builder prune --all --force",
							"docker system prune --all --force",
						}
						for _, cmd := range cmds {
							h.execDockerCleanup(nil, cmd)
						}
					})
				}
			} else {
				if h.Scheduler != nil {
					h.Scheduler.RemoveJob(jobName)
				}
			}
		}
		return true, nil
	}

	r["settings.saveSSHPrivateKey"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ SSHPrivateKey string `json:"sshPrivateKey"` }
		json.Unmarshal(input, &in)
		settings, _ := h.getOrCreateSettings()
		h.DB.Model(settings).Update("sshPrivateKey", in.SSHPrivateKey)
		return true, nil
	}

	r["settings.cleanSSHPrivateKey"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		settings, _ := h.getOrCreateSettings()
		h.DB.Model(settings).Update("sshPrivateKey", nil)
		return true, nil
	}

	// readDirectories - 读取 Traefik 配置目录树（与 TS 版一致，始终从 MAIN_TRAEFIK_PATH 开始）
	// 返回递归树形结构 TreeDataItem[]：{id, name, type, children?}
	r["settings.readDirectories"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			ServerID *string `json:"serverId"`
		}
		json.Unmarshal(input, &in)

		// 与 TS 版一致：始终从 MAIN_TRAEFIK_PATH 开始
		traefikPath := "/etc/dokploy/traefik"
		if h.Config != nil {
			traefikPath = h.Config.Paths.MainTraefikPath
		}

		if in.ServerID != nil && *in.ServerID != "" {
			// 远程服务器：通过 SSH 执行 shell 脚本生成 JSON 树（与 TS 版完全一致）
			stdout, err := h.execDockerCommand(in.ServerID, fmt.Sprintf(`
process_items() {
    local parent_dir="$1"
    local __resultvar=$2
    local items_json=""
    local first=true
    for item in "$parent_dir"/*; do
        [ -e "$item" ] || continue
        process_item "$item" item_json
        if [ "$first" = true ]; then
            first=false
            items_json="$item_json"
        else
            items_json="$items_json,$item_json"
        fi
    done
    eval $__resultvar="'[$items_json]'"
}
process_item() {
    local item_path="$1"
    local __resultvar=$2
    local item_name=$(basename "$item_path")
    local escaped_name=$(echo "$item_name" | sed 's/"/\\"/g')
    local escaped_path=$(echo "$item_path" | sed 's/"/\\"/g')
    if [ -d "$item_path" ]; then
        process_items "$item_path" children_json
        local json='{"id":"'"$escaped_path"'","name":"'"$escaped_name"'","type":"directory","children":'"$children_json"'}'
    else
        local json='{"id":"'"$escaped_path"'","name":"'"$escaped_name"'","type":"file"}'
    fi
    eval $__resultvar="'$json'"
}
process_items "%s" json_output
echo "$json_output"
`, traefikPath))
			if err != nil {
				return []interface{}{}, nil
			}
			var result []interface{}
			if jsonErr := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &result); jsonErr != nil {
				return []interface{}{}, nil
			}
			return result, nil
		}

		// 本地：递归读取目录树
		result := readDirectoryTree(traefikPath)
		return result, nil
	}

	// readTraefikFile - 读取 Traefik 配置文件内容（支持远程服务器）
	r["settings.readTraefikFile"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			Path     string  `json:"path"`
			ServerID *string `json:"serverId"`
		}
		json.Unmarshal(input, &in)

		if in.ServerID != nil && *in.ServerID != "" {
			stdout, err := h.execDockerCommand(in.ServerID, fmt.Sprintf("cat %q 2>/dev/null", in.Path))
			if err != nil {
				return "", nil
			}
			return stdout, nil
		}

		data, err := os.ReadFile(in.Path)
		if err != nil {
			return "", nil
		}
		return string(data), nil
	}

	// updateTraefikFile - 更新 Traefik 配置文件（支持远程服务器，使用 base64 编码）
	r["settings.updateTraefikFile"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			Path          string  `json:"path"`
			TraefikConfig string  `json:"traefikConfig"`
			ServerID      *string `json:"serverId"`
		}
		json.Unmarshal(input, &in)

		if in.ServerID != nil && *in.ServerID != "" {
			// 远程服务器：base64 编码后写入（与 TS 版一致）
			encoded := base64Encode(in.TraefikConfig)
			cmd := fmt.Sprintf("echo '%s' | base64 -d > %q", encoded, in.Path)
			_, err := h.execDockerCommand(in.ServerID, cmd)
			if err != nil {
				return nil, &trpcErr{err.Error(), "BAD_REQUEST", 400}
			}
			return true, nil
		}

		if err := os.WriteFile(in.Path, []byte(in.TraefikConfig), 0644); err != nil {
			return nil, &trpcErr{err.Error(), "BAD_REQUEST", 400}
		}
		return true, nil
	}

	r["settings.readWebServerTraefikConfig"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		data, err := os.ReadFile("/etc/dokploy/traefik/dynamic/dokploy.yml")
		if err != nil {
			return "", nil
		}
		return string(data), nil
	}

	r["settings.updateWebServerTraefikConfig"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ Content string `json:"traefikConfig"` }
		json.Unmarshal(input, &in)
		os.WriteFile("/etc/dokploy/traefik/dynamic/dokploy.yml", []byte(in.Content), 0644)
		return true, nil
	}

	r["settings.readMiddlewareTraefikConfig"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		data, err := os.ReadFile("/etc/dokploy/traefik/dynamic/middlewares.yml")
		if err != nil {
			return "", nil
		}
		return string(data), nil
	}

	r["settings.updateMiddlewareTraefikConfig"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ Content string `json:"traefikConfig"` }
		json.Unmarshal(input, &in)
		os.WriteFile("/etc/dokploy/traefik/dynamic/middlewares.yml", []byte(in.Content), 0644)
		return true, nil
	}

	r["settings.getOpenApiDocument"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return h.GenerateOpenAPIDocument(), nil
	}

	r["settings.cleanDockerPrune"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ ServerID *string `json:"serverId"` }
		json.Unmarshal(input, &in)
		h.execDockerCleanup(in.ServerID, "docker system prune --all --force")
		h.execDockerCleanup(in.ServerID, "docker builder prune --all --force")
		return true, nil
	}

	r["settings.cleanMonitoring"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		monitoringPath := "/etc/dokploy/monitoring"
		os.RemoveAll(monitoringPath)
		os.MkdirAll(monitoringPath, 0755)
		return true, nil
	}

	r["settings.cleanAllDeploymentQueue"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		if h.Queue != nil {
			h.Queue.CancelAllJobs()
		}
		return true, nil
	}

	r["settings.cleanRedis"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		// 查找 dokploy-redis 容器并执行 FLUSHALL
		result, err := process.ExecAsync(`docker ps --filter "name=dokploy-redis" --filter "status=running" -q | head -n 1`)
		if err != nil || result == nil || strings.TrimSpace(result.Stdout) == "" {
			return nil, &trpcErr{"Redis container not found", "INTERNAL_SERVER_ERROR", 500}
		}
		containerID := strings.TrimSpace(result.Stdout)
		if _, err := process.ExecAsync(fmt.Sprintf("docker exec -i %s redis-cli flushall", containerID)); err != nil {
			return nil, &trpcErr{err.Error(), "INTERNAL_SERVER_ERROR", 500}
		}
		return true, nil
	}

	r["settings.reloadRedis"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		go h.reloadDockerResource("dokploy-redis", nil)
		return true, nil
	}

	r["settings.toggleDashboard"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			EnableDashboard bool    `json:"enableDashboard"`
			ServerID        *string `json:"serverId"`
		}
		json.Unmarshal(input, &in)

		if in.EnableDashboard {
			// 检查端口 8080 是否被占用（双层检测：Docker 容器 + 主机级服务）
			conflictInfo, err := h.checkPortInUse(8080, in.ServerID)
			if err == nil && conflictInfo != "" {
				return nil, &trpcErr{
					message: fmt.Sprintf("Port 8080 is already in use by %s. Please stop the conflicting service or use a different port for the Traefik dashboard.", conflictInfo),
					code:    "CONFLICT",
					status:  409,
				}
			}
		}

		// 读取 Traefik 服务当前端口配置
		serviceName := "dokploy-traefik"
		portsCmd := fmt.Sprintf(`docker service inspect %s --format '{{range .Endpoint.Ports}}{{.TargetPort}}:{{.PublishedPort}}/{{.Protocol}} {{end}}'`, serviceName)
		var portsOut string
		if in.ServerID != nil {
			conn := h.getSSHConn(*in.ServerID)
			if conn != nil {
				result, _ := process.ExecAsyncRemote(*conn, portsCmd, nil)
				if result != nil {
					portsOut = result.Stdout
				}
			}
		} else {
			result, _ := process.ExecAsync(portsCmd)
			if result != nil {
				portsOut = result.Stdout
			}
		}

		// 解析当前端口列表，添加或移除 8080
		var portSpecs []string
		for _, p := range strings.Fields(strings.TrimSpace(portsOut)) {
			if p == "" {
				continue
			}
			// 移除已有的 8080
			if strings.HasPrefix(p, "8080:") {
				continue
			}
			portSpecs = append(portSpecs, p)
		}

		if in.EnableDashboard {
			portSpecs = append(portSpecs, "8080:8080/tcp")
		}

		// 构建 --publish-add 参数更新服务
		var publishArgs []string
		// 先移除所有端口再重新添加
		publishArgs = append(publishArgs, "--publish-rm 8080")
		if in.EnableDashboard {
			publishArgs = append(publishArgs, "--publish-add 8080:8080")
		}

		updateCmd := fmt.Sprintf("docker service update %s %s",
			strings.Join(publishArgs, " "), serviceName)

		// 异步执行以避免代理超时
		go func() {
			if in.ServerID != nil {
				conn := h.getSSHConn(*in.ServerID)
				if conn != nil {
					process.ExecAsyncRemote(*conn, updateCmd, nil)
				}
			} else {
				process.ExecAsync(updateCmd)
			}
		}()

		return true, nil
	}

	r["settings.readTraefikEnv"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		data, err := os.ReadFile("/etc/dokploy/traefik/.env")
		if err != nil {
			return "", nil
		}
		return string(data), nil
	}

	r["settings.writeTraefikEnv"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ Env string `json:"env"` }
		json.Unmarshal(input, &in)
		os.WriteFile("/etc/dokploy/traefik/.env", []byte(in.Env), 0644)
		return true, nil
	}

	r["settings.readStats"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		// 读取 Traefik access log 并按小时分组统计请求数
		logPath := "/etc/dokploy/traefik/dynamic/access.log"
		entries := readAccessLogEntries(logPath, false)
		hourCounts := map[string]int{}
		for _, e := range entries {
			if t, ok := e["StartUTC"].(string); ok && len(t) >= 13 {
				hour := t[:13] // "2024-01-15T14" 级别
				hourCounts[hour]++
			}
		}
		var stats []map[string]interface{}
		for hour, count := range hourCounts {
			stats = append(stats, map[string]interface{}{"hour": hour, "count": count})
		}
		if stats == nil {
			stats = []map[string]interface{}{}
		}
		return stats, nil
	}

	r["settings.readStatsLogs"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			Page     int    `json:"page"`
			PageSize int    `json:"pageSize"`
			Status   string `json:"status"`
			Search   string `json:"search"`
			Sort     string `json:"sort"`
		}
		json.Unmarshal(input, &in)
		if in.Page <= 0 {
			in.Page = 1
		}
		if in.PageSize <= 0 {
			in.PageSize = 50
		}
		logPath := "/etc/dokploy/traefik/dynamic/access.log"
		entries := readAccessLogEntries(logPath, true)

		// 过滤
		var filtered []map[string]interface{}
		for _, e := range entries {
			if in.Status != "" {
				if s, ok := e["DownstreamStatus"].(float64); ok {
					statusStr := fmt.Sprintf("%d", int(s))
					if !strings.HasPrefix(statusStr, in.Status[:1]) {
						continue
					}
				}
			}
			if in.Search != "" {
				reqPath, _ := e["RequestPath"].(string)
				if !strings.Contains(strings.ToLower(reqPath), strings.ToLower(in.Search)) {
					continue
				}
			}
			filtered = append(filtered, e)
		}
		totalCount := len(filtered)

		// 分页
		start := (in.Page - 1) * in.PageSize
		end := start + in.PageSize
		if start > len(filtered) {
			start = len(filtered)
		}
		if end > len(filtered) {
			end = len(filtered)
		}
		page := filtered[start:end]
		if page == nil {
			page = []map[string]interface{}{}
		}
		return map[string]interface{}{
			"data":       page,
			"totalCount": totalCount,
		}, nil
	}

	r["settings.haveActivateRequests"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		// 检查 Traefik 主配置是否启用了 accessLog
		traefikPath := "/etc/dokploy/traefik/traefik.yml"
		if h.Config != nil {
			traefikPath = filepath.Join(h.Config.Paths.MainTraefikPath, "traefik.yml")
		}
		data, err := os.ReadFile(traefikPath)
		if err != nil {
			return false, nil
		}
		var cfg map[string]interface{}
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return false, nil
		}
		if al, ok := cfg["accessLog"]; ok && al != nil {
			if alMap, ok := al.(map[string]interface{}); ok {
				if _, ok := alMap["filePath"]; ok {
					return true, nil
				}
			}
		}
		return false, nil
	}

	r["settings.toggleRequests"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			Enable bool `json:"enable"`
		}
		json.Unmarshal(input, &in)

		traefikPath := "/etc/dokploy/traefik/traefik.yml"
		if h.Config != nil {
			traefikPath = filepath.Join(h.Config.Paths.MainTraefikPath, "traefik.yml")
		}
		data, err := os.ReadFile(traefikPath)
		if err != nil {
			return false, nil
		}
		var cfg map[string]interface{}
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return false, nil
		}

		if in.Enable {
			accessLogPath := "/etc/dokploy/traefik/dynamic/access.log"
			if h.Config != nil {
				accessLogPath = filepath.Join(h.Config.Paths.DynamicTraefikPath, "access.log")
			}
			cfg["accessLog"] = map[string]interface{}{
				"filePath":      accessLogPath,
				"format":        "json",
				"bufferingSize": 100,
			}
		} else {
			delete(cfg, "accessLog")
		}

		out, err := yaml.Marshal(cfg)
		if err != nil {
			return false, nil
		}
		os.WriteFile(traefikPath, out, 0644)
		return true, nil
	}

	r["settings.isUserSubscribed"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		member, err := h.getDefaultMember(c)
		if err != nil {
			return false, nil
		}
		var serverCount int64
		h.DB.Model(&schema.Server{}).Where("\"organizationId\" = ?", member.OrganizationID).Count(&serverCount)
		var projectCount int64
		h.DB.Model(&schema.Project{}).Where("\"organizationId\" = ?", member.OrganizationID).Count(&projectCount)
		return serverCount > 0 || projectCount > 0, nil
	}

	r["settings.setupGPU"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			ServerID *string `json:"serverId"`
		}
		json.Unmarshal(input, &in)

		// 检查 nvidia-smi 是否存在
		checkCmd := "nvidia-smi --query-gpu=count --format=csv,noheader 2>/dev/null || echo ''"
		var gpuCountStr string
		if in.ServerID != nil && *in.ServerID != "" {
			gpuCountStr = h.execRemoteOrLocal(checkCmd, in.ServerID)
		} else {
			result, _ := process.ExecAsync(checkCmd)
			if result != nil {
				gpuCountStr = strings.TrimSpace(result.Stdout)
			}
		}
		if gpuCountStr == "" || gpuCountStr == "0" {
			return nil, &trpcErr{"NVIDIA driver not detected. Please install NVIDIA drivers first.", "BAD_REQUEST", 400}
		}

		// 配置 Docker daemon + Swarm GPU 资源
		setupCmds := []string{
			// 设置 nvidia-container-runtime
			`mkdir -p /etc/nvidia-container-runtime && echo -e '[nvidia-container-cli]\nno-cgroups = true' > /etc/nvidia-container-runtime/config.toml`,
			// 更新 daemon.json
			fmt.Sprintf(`python3 -c "
import json
with open('/etc/docker/daemon.json','r') as f: d=json.load(f)
d.setdefault('runtimes',{})['nvidia']={'path':'nvidia-container-runtime','runtimeArgs':[]}
d['node-generic-resources']=['GPU=%s']
with open('/etc/docker/daemon.json','w') as f: json.dump(d,f,indent=2)
" 2>/dev/null || echo '{"runtimes":{"nvidia":{"path":"nvidia-container-runtime","runtimeArgs":[]}},"node-generic-resources":["GPU=%s"]}' > /etc/docker/daemon.json`, gpuCountStr, gpuCountStr),
			"systemctl restart docker",
		}
		for _, cmd := range setupCmds {
			if in.ServerID != nil && *in.ServerID != "" {
				h.execRemoteOrLocal(cmd, in.ServerID)
			} else {
				process.ExecAsync(cmd)
			}
		}
		return true, nil
	}

	r["settings.checkGPUStatus"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			ServerID *string `json:"serverId"`
		}
		json.Unmarshal(input, &in)

		result := map[string]interface{}{"available": false}
		checkCmd := "nvidia-smi --query-gpu=driver_version,count --format=csv,noheader 2>/dev/null || echo ''"
		var out string
		if in.ServerID != nil && *in.ServerID != "" {
			out = h.execRemoteOrLocal(checkCmd, in.ServerID)
		} else {
			r, _ := process.ExecAsync(checkCmd)
			if r != nil {
				out = strings.TrimSpace(r.Stdout)
			}
		}
		if out != "" && !strings.Contains(out, "command not found") {
			parts := strings.Split(out, ", ")
			if len(parts) >= 2 {
				result["available"] = true
				result["driverVersion"] = strings.TrimSpace(parts[0])
				result["gpuCount"] = strings.TrimSpace(parts[1])
			}
		}
		return result, nil
	}

	r["settings.updateTraefikPorts"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			AdditionalPorts []map[string]interface{} `json:"additionalPorts"`
			ServerID        *string                  `json:"serverId"`
		}
		json.Unmarshal(input, &in)
		// 检查端口冲突
		for _, port := range in.AdditionalPorts {
			if p, ok := port["targetPort"].(float64); ok {
				conflict, _ := h.checkPortInUse(int(p), in.ServerID)
				if conflict != "" {
					return nil, &trpcErr{fmt.Sprintf("Port %d is in use by %s", int(p), conflict), "BAD_REQUEST", 400}
				}
			}
		}
		// 后台重建 Traefik 服务（添加端口）
		go func() {
			// 构建端口发布参数：默认 80:80 + 443:443 + 自定义端口
			portArgs := "--publish-add published=80,target=80,protocol=tcp,mode=host " +
				"--publish-add published=443,target=443,protocol=tcp,mode=host"
			for _, port := range in.AdditionalPorts {
				tp, _ := port["targetPort"].(float64)
				pp, _ := port["publishedPort"].(float64)
				proto := "tcp"
				if p, ok := port["protocol"].(string); ok && p != "" {
					proto = p
				}
				portArgs += fmt.Sprintf(" --publish-add published=%d,target=%d,protocol=%s,mode=host", int(pp), int(tp), proto)
			}
			cmd := fmt.Sprintf("docker service update --force %s dokploy-traefik", portArgs)
			if in.ServerID != nil && *in.ServerID != "" {
				h.execRemoteOrLocal(cmd, in.ServerID)
			} else {
				process.ExecAsync(cmd)
			}
		}()
		return true, nil
	}

	r["settings.getTraefikPorts"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			ServerID *string `json:"serverId"`
		}
		json.Unmarshal(input, &in)
		ports, err := h.readDockerPorts("dokploy-traefik", in.ServerID)
		if err != nil {
			return []interface{}{}, nil
		}
		// 过滤掉默认端口 80 和 443
		var filtered []map[string]interface{}
		for _, p := range ports {
			if p.TargetPort != 80 && p.TargetPort != 443 {
				filtered = append(filtered, map[string]interface{}{
					"targetPort":    p.TargetPort,
					"publishedPort": p.PublishedPort,
					"protocol":      p.Protocol,
				})
			}
		}
		if filtered == nil {
			filtered = []map[string]interface{}{}
		}
		return filtered, nil
	}

	r["settings.updateServer"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		if h.Updater == nil {
			return nil, &trpcErr{"Updater not available", "INTERNAL_SERVER_ERROR", 500}
		}
		// 获取最新版本并执行更新（异步，不阻塞）
		data := h.Updater.CheckUpdate()
		if !data.UpdateAvailable || data.LatestVersion == nil {
			return nil, &trpcErr{"No update available", "BAD_REQUEST", 400}
		}
		if err := h.Updater.ApplyUpdate(*data.LatestVersion); err != nil {
			return nil, &trpcErr{err.Error(), "INTERNAL_SERVER_ERROR", 500}
		}
		return true, nil
	}

	// Go 版专属：更新源配置（读取/修改 go_config 表，Registry 通过 FK 关联）
	r["settings.getRegistryConfig"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		if h.Updater == nil {
			return nil, &trpcErr{"Updater not available", "INTERNAL_SERVER_ERROR", 500}
		}
		cfg := h.Updater.GetConfig()
		return cfg, nil
	}

	r["settings.updateRegistryConfig"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		if h.Updater == nil {
			return nil, &trpcErr{"Updater not available", "INTERNAL_SERVER_ERROR", 500}
		}
		var in struct {
			RegistryImage string  `json:"registryImage"`
			RegistryID    *string `json:"registryId"` // 关联已有的 Registry 记录（nil 表示清除）
			ServiceName   string  `json:"serviceName"`
		}
		json.Unmarshal(input, &in)

		cfg := h.Updater.GetConfig()
		cfg.RegistryImage = in.RegistryImage
		cfg.RegistryID = in.RegistryID
		if in.ServiceName != "" {
			cfg.ServiceName = in.ServiceName
		}

		if err := h.Updater.UpdateConfig(cfg); err != nil {
			return nil, &trpcErr{err.Error(), "INTERNAL_SERVER_ERROR", 500}
		}
		return true, nil
	}

	r["settings.testRegistryConnection"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		if h.Updater == nil {
			return nil, &trpcErr{"Updater not available", "INTERNAL_SERVER_ERROR", 500}
		}
		success, updateAvailable, latestVersion, err := h.Updater.TestConnection()
		if err != nil {
			return nil, &trpcErr{err.Error(), "BAD_REQUEST", 400}
		}
		return map[string]interface{}{
			"success":         success,
			"updateAvailable": updateAvailable,
			"latestVersion":   latestVersion,
		}, nil
	}

	r["settings.updateLogCleanup"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in map[string]interface{}
		json.Unmarshal(input, &in)
		in = h.filterColumns(&schema.WebServerSettings{}, in)
		settings, _ := h.getOrCreateSettings()
		if settings != nil {
			h.DB.Model(settings).Updates(in)
		}
		return true, nil
	}

	r["settings.getLogCleanupStatus"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		settings, _ := h.getOrCreateSettings()
		return settings, nil
	}

	r["settings.getDokployCloudIps"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return []string{}, nil
	}

	// Admin
	r["admin.one"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		settings, err := h.getOrCreateSettings()
		if err != nil {
			return nil, err
		}
		return settings, nil
	}

	r["admin.setupMonitoring"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		// 解析 metricsConfig 并写入 WebServerSettings
		var in struct {
			MetricsConfig json.RawMessage `json:"metricsConfig"`
		}
		json.Unmarshal(input, &in)

		settings, err := h.getOrCreateSettings()
		if err != nil {
			return nil, err
		}

		// 将 metricsConfig.server.type 强制设为 "Dokploy"（TS 版逻辑）
		var mc map[string]interface{}
		json.Unmarshal(in.MetricsConfig, &mc)
		if srv, ok := mc["server"].(map[string]interface{}); ok {
			srv["type"] = "Dokploy"
		}
		mcJSON, _ := json.Marshal(mc)

		// 更新数据库
		h.DB.Model(settings).Update("metricsConfig", string(mcJSON))

		// 提取端口号（默认 3001）
		port := 3001
		if srv, ok := mc["server"].(map[string]interface{}); ok {
			if p, ok := srv["port"].(float64); ok && p > 0 {
				port = int(p)
			}
		}

		// 后台重新部署 dokploy-monitoring 容器（本地主服务器）
		go func() {
			configStr := strings.ReplaceAll(string(mcJSON), "'", "'\\''")
			cmds := []string{
				"mkdir -p /etc/dokploy/monitoring && touch /etc/dokploy/monitoring/monitoring.db",
				"docker pull dokploy/monitoring:latest 2>/dev/null || true",
				"docker rm -f dokploy-monitoring 2>/dev/null || true",
				fmt.Sprintf(
					`docker run -d --name dokploy-monitoring --restart always `+
						`-e 'METRICS_CONFIG=%s' `+
						`-p %d:%d `+
						`-v /var/run/docker.sock:/var/run/docker.sock:ro `+
						`-v /sys:/host/sys:ro `+
						`-v /etc/os-release:/etc/os-release:ro `+
						`-v /proc:/host/proc:ro `+
						`-v /etc/dokploy/monitoring/monitoring.db:/app/monitoring.db `+
						`dokploy/monitoring:latest`,
					configStr, port, port,
				),
			}
			for _, cmd := range cmds {
				process.ExecAsync(cmd)
			}
		}()

		// 返回更新后的 settings
		h.DB.First(settings, "id = ?", settings.ID)
		return settings, nil
	}

	r["auth.isAdminPresent"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return h.DB.IsAdminPresent(), nil
	}
}

// execDockerCleanup runs a docker cleanup command locally or on a remote server via SSH.
// dockerSafeExec 包装 Docker 命令，等待 Docker 空闲后再执行
// 与 TS 版 dockerSafeExec 完全对齐：轮询 ps aux 检测 docker 进程，空闲后执行
func dockerSafeExec(cmd string) string {
	return fmt.Sprintf(`
CHECK_INTERVAL=10

echo "Preparing for execution..."

while true; do
    PROCESSES=$(ps aux | grep -E "^.*docker [A-Za-z]" | grep -v grep)

    if [ -z "$PROCESSES" ]; then
        echo "Docker is idle. Starting execution..."
        break
    else
        echo "Docker is busy. Will check again in $CHECK_INTERVAL seconds..."
        sleep $CHECK_INTERVAL
    fi
done

%s

echo "Execution completed."
`, cmd)
}

func (h *Handler) execDockerCleanup(serverID *string, command string) {
	// 与 TS 版一致：所有清理命令包裹在 dockerSafeExec 中，等待 Docker 空闲后执行
	safeCmd := dockerSafeExec(command)
	if serverID != nil && *serverID != "" {
		var srv schema.Server
		if err := h.DB.Preload("SSHKey").First(&srv, "\"serverId\" = ?", *serverID).Error; err != nil {
			log.Printf("[Docker Cleanup] Server %s not found: %v", *serverID, err)
			return
		}
		if srv.SSHKey == nil {
			log.Printf("[Docker Cleanup] Server %s has no SSH key", *serverID)
			return
		}
		conn := process.SSHConnection{
			Host:       srv.IPAddress,
			Port:       srv.Port,
			Username:   srv.Username,
			PrivateKey: srv.SSHKey.PrivateKey,
		}
		_, err := process.ExecAsyncRemote(conn, safeCmd, nil)
		if err != nil {
			log.Printf("[Docker Cleanup] Remote exec failed on %s: %v", *serverID, err)
		}
	} else {
		cmd := exec.Command("bash", "-c", safeCmd)
		if output, err := cmd.CombinedOutput(); err != nil {
			log.Printf("[Docker Cleanup] Local exec failed: %v: %s", err, string(output))
		}
	}
}

// checkPortInUse 双层端口冲突检测（与 TS v0.28.5 checkPortInUse 一致）
// 返回冲突描述（空字符串表示未占用）
func (h *Handler) checkPortInUse(port int, serverID *string) (string, error) {
	// 层1：检查 Docker 容器是否占用端口
	dockerCmd := fmt.Sprintf(
		`docker ps -a --format '{{.Names}}' | grep -v '^dokploy-traefik$' | while read name; do docker port "$name" 2>/dev/null | grep -q ':%d' && echo "$name" && break; done || true`,
		port,
	)

	var dockerOut string
	if serverID != nil && *serverID != "" {
		dockerOut = h.execRemoteOrLocal(dockerCmd, serverID)
	} else {
		result, err := process.ExecAsync(dockerCmd)
		if err != nil {
			return "", err
		}
		if result != nil {
			dockerOut = result.Stdout
		}
	}

	container := strings.TrimSpace(dockerOut)
	if container != "" {
		return fmt.Sprintf(`container "%s"`, container), nil
	}

	// 层2：检查主机级服务是否占用端口（通过 --net=host 容器检测）
	hostCmd := fmt.Sprintf(
		`docker run --rm --net=host busybox sh -c 'nc -z 0.0.0.0 %d 2>/dev/null && echo in_use || echo free'`,
		port,
	)

	var hostOut string
	if serverID != nil && *serverID != "" {
		hostOut = h.execRemoteOrLocal(hostCmd, serverID)
	} else {
		result, _ := process.ExecAsync(hostCmd)
		if result != nil {
			hostOut = result.Stdout
		}
	}

	if strings.Contains(hostOut, "in_use") {
		return "a host-level service", nil
	}

	return "", nil
}

// reloadDockerResource 重启 Docker 服务或容器（与 TS reloadDockerResource 一致）
func (h *Handler) reloadDockerResource(name string, serverID *string) {
	// 尝试 service update --force（Swarm 模式），失败则 docker restart（standalone 模式）
	cmd := fmt.Sprintf(`docker service update --force %s 2>/dev/null || docker restart %s 2>/dev/null || true`, name, name)
	if serverID != nil && *serverID != "" {
		h.execRemoteOrLocal(cmd, serverID)
	} else {
		process.ExecAsync(cmd)
	}
}

// execRemoteOrLocal 在远程服务器或本地执行命令
func (h *Handler) execRemoteOrLocal(cmd string, serverID *string) string {
	if serverID != nil && *serverID != "" {
		conn := h.getSSHConn(*serverID)
		if conn != nil {
			result, _ := process.ExecAsyncRemote(*conn, cmd, nil)
			if result != nil {
				return strings.TrimSpace(result.Stdout)
			}
			return ""
		}
		// SSH 连接获取失败，降级到本地执行
	}
	result, _ := process.ExecAsync(cmd)
	if result != nil {
		return strings.TrimSpace(result.Stdout)
	}
	return ""
}

// dockerPort 表示 Docker 端口映射
type dockerPort struct {
	TargetPort    int
	PublishedPort int
	Protocol      string
}

// readDockerPorts 读取 Docker 服务/容器的端口映射（与 TS readPorts 一致）
func (h *Handler) readDockerPorts(name string, serverID *string) ([]dockerPort, error) {
	// 先尝试 service inspect（Swarm 模式）
	cmd := fmt.Sprintf(`docker service inspect %s --format '{{json .Endpoint.Ports}}' 2>/dev/null || docker inspect %s --format '{{json .NetworkSettings.Ports}}' 2>/dev/null || echo '[]'`, name, name)
	var out string
	if serverID != nil && *serverID != "" {
		out = h.execRemoteOrLocal(cmd, serverID)
	} else {
		result, _ := process.ExecAsync(cmd)
		if result != nil {
			out = strings.TrimSpace(result.Stdout)
		}
	}
	if out == "" || out == "[]" || out == "null" {
		return nil, nil
	}

	// 尝试解析 Swarm 格式 [{TargetPort, PublishedPort, Protocol}]
	var swarmPorts []struct {
		TargetPort    int    `json:"TargetPort"`
		PublishedPort int    `json:"PublishedPort"`
		Protocol      string `json:"Protocol"`
	}
	if err := json.Unmarshal([]byte(out), &swarmPorts); err == nil && len(swarmPorts) > 0 {
		var result []dockerPort
		for _, p := range swarmPorts {
			result = append(result, dockerPort{
				TargetPort:    p.TargetPort,
				PublishedPort: p.PublishedPort,
				Protocol:      p.Protocol,
			})
		}
		return result, nil
	}

	// 尝试解析 standalone 格式 {"port/proto": [{"HostPort": "..."}]}
	var standalonePorts map[string][]struct {
		HostPort string `json:"HostPort"`
	}
	if err := json.Unmarshal([]byte(out), &standalonePorts); err == nil {
		var result []dockerPort
		for key, bindings := range standalonePorts {
			parts := strings.Split(key, "/")
			targetPort := 0
			proto := "tcp"
			if len(parts) >= 1 {
				fmt.Sscanf(parts[0], "%d", &targetPort)
			}
			if len(parts) >= 2 {
				proto = parts[1]
			}
			for _, b := range bindings {
				pubPort := 0
				fmt.Sscanf(b.HostPort, "%d", &pubPort)
				result = append(result, dockerPort{
					TargetPort:    targetPort,
					PublishedPort: pubPort,
					Protocol:      proto,
				})
			}
		}
		return result, nil
	}

	return nil, nil
}

// readAccessLogEntries 读取 Traefik access log JSON 条目
func readAccessLogEntries(logPath string, readAll bool) []map[string]interface{} {
	data, err := os.ReadFile(logPath)
	if err != nil {
		return nil
	}
	lines := strings.Split(string(data), "\n")
	var entries []map[string]interface{}
	maxLines := 500
	if readAll {
		maxLines = len(lines)
	}
	for i, line := range lines {
		if !readAll && i >= maxLines {
			break
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var entry map[string]interface{}
		if err := json.Unmarshal([]byte(line), &entry); err == nil {
			// 跳过 dokploy 内部服务的请求
			if svc, ok := entry["ServiceName"].(string); ok && strings.Contains(svc, "dokploy-service-app") {
				continue
			}
			entries = append(entries, entry)
		}
	}
	return entries
}

// getSSHConn 从 serverID 获取 SSH 连接参数
func (h *Handler) getSSHConn(serverID string) *process.SSHConnection {
	var srv schema.Server
	if err := h.DB.Preload("SSHKey").First(&srv, "\"serverId\" = ?", serverID).Error; err != nil {
		return nil
	}
	if srv.SSHKey == nil {
		return nil
	}
	return &process.SSHConnection{
		Host:       srv.IPAddress,
		Port:       srv.Port,
		Username:   srv.Username,
		PrivateKey: srv.SSHKey.PrivateKey,
	}
}
