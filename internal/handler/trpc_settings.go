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
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/dokploy/dokploy/internal/db/schema"
	"github.com/dokploy/dokploy/internal/process"
	"github.com/labstack/echo/v4"
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
		return false, nil
	}

	r["settings.getReleaseTag"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return map[string]string{"releaseTag": "v0.28.5"}, nil
	}

	r["settings.getDokployVersion"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return "v0.28.5", nil
	}

	r["settings.health"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return map[string]string{"status": "ok"}, nil
	}

	r["settings.getUpdateData"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		defaultResult := map[string]interface{}{
			"updateAvailable": false,
			"latestVersion":   nil,
		}

		type dockerTag struct {
			Digest string `json:"digest"`
			Name   string `json:"name"`
		}
		type dockerResp struct {
			Next    *string     `json:"next"`
			Results []dockerTag `json:"results"`
		}

		var allTags []dockerTag
		apiURL := "https://hub.docker.com/v2/repositories/dokploy/dokploy/tags?page_size=100"
		client := &http.Client{Timeout: 10 * time.Second}

		for apiURL != "" {
			resp, err := client.Get(apiURL)
			if err != nil {
				return defaultResult, nil
			}
			var data dockerResp
			json.NewDecoder(resp.Body).Decode(&data)
			resp.Body.Close()
			allTags = append(allTags, data.Results...)
			if data.Next != nil {
				apiURL = *data.Next
			} else {
				apiURL = ""
			}
		}

		var latestDigest string
		for _, t := range allTags {
			if t.Name == "latest" {
				latestDigest = t.Digest
				break
			}
		}
		if latestDigest == "" {
			return defaultResult, nil
		}

		var latestVersion string
		for _, t := range allTags {
			if t.Digest == latestDigest && len(t.Name) > 0 && t.Name[0] == 'v' {
				latestVersion = t.Name
				break
			}
		}
		if latestVersion == "" {
			return defaultResult, nil
		}

		return map[string]interface{}{
			"updateAvailable": true,
			"latestVersion":   latestVersion,
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
		return true, nil
	}

	r["settings.reloadServer"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
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
		return true, nil
	}

	r["settings.cleanRedis"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return true, nil
	}

	r["settings.reloadRedis"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return true, nil
	}

	r["settings.toggleDashboard"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
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
		return map[string]interface{}{}, nil
	}

	r["settings.readStatsLogs"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return map[string]interface{}{}, nil
	}

	r["settings.haveActivateRequests"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return false, nil
	}

	r["settings.toggleRequests"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return true, nil
	}

	r["settings.isUserSubscribed"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return false, nil
	}

	r["settings.setupGPU"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return true, nil
	}

	r["settings.checkGPUStatus"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return map[string]interface{}{"available": false}, nil
	}

	r["settings.updateTraefikPorts"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return true, nil
	}

	r["settings.getTraefikPorts"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return map[string]int{
			"httpPort":  80,
			"httpsPort": 443,
		}, nil
	}

	r["settings.updateServer"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return true, nil
	}

	r["settings.updateLogCleanup"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in map[string]interface{}
		json.Unmarshal(input, &in)
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
		return true, nil
	}

	r["auth.isAdminPresent"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return h.DB.IsAdminPresent(), nil
	}
}

// execDockerCleanup runs a docker cleanup command locally or on a remote server via SSH.
func (h *Handler) execDockerCleanup(serverID *string, command string) {
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
		_, err := process.ExecAsyncRemote(conn, command, nil)
		if err != nil {
			log.Printf("[Docker Cleanup] Remote exec failed on %s: %v", *serverID, err)
		}
	} else {
		cmd := exec.Command("bash", "-c", command)
		if output, err := cmd.CombinedOutput(); err != nil {
			log.Printf("[Docker Cleanup] Local exec failed: %v: %s", err, string(output))
		}
	}
}
