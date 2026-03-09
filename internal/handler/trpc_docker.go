// Input: procedureRegistry, docker (Docker SDK 客户端), db (Server/SSHKey 表)
// Output: registerDockerTRPC - Docker 领域的 tRPC procedure 注册
// Role: Docker tRPC 路由注册，使用 CLI 命令（与 TS 版一致）列出容器/服务/Stack，支持远程服务器
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package handler

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/docker/docker/api/types"
	containertypes "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/dokploy/dokploy/internal/db/schema"
	"github.com/dokploy/dokploy/internal/process"
	"github.com/labstack/echo/v4"
)

// containerInfo 用于 getContainers（Docker 管理页面）的返回格式
type containerInfo struct {
	ContainerID string  `json:"containerId"`
	Name        string  `json:"name"`
	Image       string  `json:"image"`
	Ports       string  `json:"ports"`
	State       string  `json:"state"`
	Status      string  `json:"status"`
	ServerID    *string `json:"serverId"`
}

// appContainerInfo 用于 getContainersByAppNameMatch（应用日志页面）的返回格式
type appContainerInfo struct {
	ContainerID string `json:"containerId"`
	Name        string `json:"name"`
	State       string `json:"state"`
	Status      string `json:"status"`
}

// serviceContainerInfo 用于 getServiceContainersByAppName / getStackContainersByAppName（Swarm 模式）的返回格式
type serviceContainerInfo struct {
	ContainerID  string `json:"containerId"`
	Name         string `json:"name"`
	State        string `json:"state"`
	Node         string `json:"node"`
	CurrentState string `json:"currentState"`
	Error        string `json:"error"`
}

// formatPorts converts Docker port bindings to a human-readable string.
func formatPorts(ports []types.Port) string {
	var parts []string
	for _, p := range ports {
		if p.IP != "" && p.PublicPort != 0 {
			parts = append(parts, fmt.Sprintf("%s:%d->%d/%s", p.IP, p.PublicPort, p.PrivatePort, p.Type))
		} else if p.PrivatePort != 0 {
			parts = append(parts, fmt.Sprintf("%d/%s", p.PrivatePort, p.Type))
		}
	}
	return strings.Join(parts, ", ")
}

// execDockerCommand 执行 Docker CLI 命令，支持本地和远程（与 TS 版 execAsync/execAsyncRemote 一致）
func (h *Handler) execDockerCommand(serverID *string, command string) (string, error) {
	if serverID != nil && *serverID != "" {
		// 远程执行：通过 SSH 连接到远程服务器
		var srv schema.Server
		if err := h.DB.Preload("SSHKey").First(&srv, "\"serverId\" = ?", *serverID).Error; err != nil {
			return "", fmt.Errorf("server not found: %w", err)
		}
		if srv.SSHKey == nil {
			return "", fmt.Errorf("server has no SSH key configured")
		}
		conn := process.SSHConnection{
			Host:       srv.IPAddress,
			Port:       srv.Port,
			Username:   srv.Username,
			PrivateKey: srv.SSHKey.PrivateKey,
		}
		result, err := process.ExecAsyncRemote(conn, command, nil)
		if err != nil {
			return "", err
		}
		return result.Stdout, nil
	}
	// 本地执行
	result, err := process.ExecAsync(command)
	if err != nil {
		return "", err
	}
	return result.Stdout, nil
}

// parseContainerLine 解析 docker ps 输出行（格式与 TS 版完全一致）
// 格式: "CONTAINER ID : xxx | Name: xxx | State: xxx | Status: xxx"
func parseContainerLine(line string) appContainerInfo {
	parts := strings.Split(line, " | ")
	info := appContainerInfo{
		ContainerID: "No container id",
		Name:        "No container name",
		State:       "No state",
	}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "CONTAINER ID : ") {
			info.ContainerID = strings.TrimSpace(strings.TrimPrefix(part, "CONTAINER ID : "))
		} else if strings.HasPrefix(part, "Name: ") {
			info.Name = strings.TrimSpace(strings.TrimPrefix(part, "Name: "))
		} else if strings.HasPrefix(part, "State: ") {
			info.State = strings.TrimSpace(strings.TrimPrefix(part, "State: "))
		} else if strings.HasPrefix(part, "Status: ") {
			info.Status = strings.TrimSpace(strings.TrimPrefix(part, "Status: "))
		}
	}
	return info
}

// parseServiceLine 解析 docker service ps / docker stack ps 输出行
// 格式: "CONTAINER ID : xxx | Name: xxx | State: xxx | Node: xxx | CurrentState: xxx | Error: xxx"
func parseServiceLine(line string) serviceContainerInfo {
	parts := strings.Split(line, " | ")
	info := serviceContainerInfo{
		ContainerID: "No container id",
		Name:        "No container name",
		State:       "No state",
		Node:        "No specific node",
	}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "CONTAINER ID : ") {
			info.ContainerID = strings.TrimSpace(strings.TrimPrefix(part, "CONTAINER ID : "))
		} else if strings.HasPrefix(part, "Name: ") {
			info.Name = strings.TrimSpace(strings.TrimPrefix(part, "Name: "))
		} else if strings.HasPrefix(part, "State: ") {
			info.State = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(part, "State: ")))
		} else if strings.HasPrefix(part, "Node: ") {
			info.Node = strings.TrimSpace(strings.TrimPrefix(part, "Node: "))
		} else if strings.HasPrefix(part, "CurrentState: ") {
			info.CurrentState = strings.TrimSpace(strings.TrimPrefix(part, "CurrentState: "))
		} else if strings.HasPrefix(part, "Error: ") {
			info.Error = strings.TrimSpace(strings.TrimPrefix(part, "Error: "))
		}
	}
	return info
}

func (h *Handler) registerDockerTRPC(r procedureRegistry) {
	// getContainers - Docker 管理页面，列出所有容器（使用 SDK，仅本地）
	r["docker.getContainers"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			ServerID *string `json:"serverId"`
		}
		json.Unmarshal(input, &in)

		if h.Docker == nil {
			return []interface{}{}, nil
		}
		containers, err := h.Docker.DockerClient().ContainerList(c.Request().Context(), containertypes.ListOptions{All: true})
		if err != nil {
			return nil, &trpcErr{err.Error(), "BAD_REQUEST", 400}
		}

		result := make([]containerInfo, 0, len(containers))
		for _, ct := range containers {
			name := ""
			if len(ct.Names) > 0 {
				name = strings.TrimPrefix(ct.Names[0], "/")
			}
			if strings.Contains(name, "dokploy") && !strings.Contains(name, "dokploy-monitoring") {
				continue
			}
			result = append(result, containerInfo{
				ContainerID: ct.ID,
				Name:        name,
				Image:       ct.Image,
				Ports:       formatPorts(ct.Ports),
				State:       ct.State,
				Status:      ct.Status,
				ServerID:    in.ServerID,
			})
		}
		return result, nil
	}

	// getConfig - 获取容器详细配置（用于下载日志文件名等）
	r["docker.getConfig"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			ContainerID string  `json:"containerId"`
			ServerID    *string `json:"serverId"`
		}
		json.Unmarshal(input, &in)
		if h.Docker == nil {
			return nil, &trpcErr{"Docker not available", "BAD_REQUEST", 400}
		}
		container, err := h.Docker.DockerClient().ContainerInspect(c.Request().Context(), in.ContainerID)
		if err != nil {
			return nil, &trpcErr{err.Error(), "BAD_REQUEST", 400}
		}
		return container, nil
	}

	// getContainersByAppLabel - 按标签过滤容器（与 TS 版一致，使用 CLI）
	r["docker.getContainersByAppLabel"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			AppName  string  `json:"appName"`
			ServerID *string `json:"serverId"`
			Type     string  `json:"type"`
		}
		json.Unmarshal(input, &in)

		format := "'CONTAINER ID : {{.ID}} | Name: {{.Names}} | State: {{.State}} | Status: {{.Status}}'"
		var command string
		if in.Type == "swarm" {
			command = fmt.Sprintf("docker ps -a --no-trunc --format %s --filter='label=com.docker.swarm.service.name=%s'", format, in.AppName)
		} else {
			command = fmt.Sprintf("docker ps -a --no-trunc --format %s --filter='name=%s'", format, in.AppName)
		}

		stdout, err := h.execDockerCommand(in.ServerID, command)
		if err != nil || strings.TrimSpace(stdout) == "" {
			return []appContainerInfo{}, nil
		}

		var result []appContainerInfo
		for _, line := range strings.Split(strings.TrimSpace(stdout), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			result = append(result, parseContainerLine(line))
		}
		if result == nil {
			result = []appContainerInfo{}
		}
		return result, nil
	}

	// getContainersByAppNameMatch - 按应用名匹配容器（与 TS 版完全一致）
	// 用于应用日志页面 native 模式
	r["docker.getContainersByAppNameMatch"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			AppName  string  `json:"appName"`
			AppType  *string `json:"appType"`
			ServerID *string `json:"serverId"`
		}
		json.Unmarshal(input, &in)

		format := "'CONTAINER ID : {{.ID}} | Name: {{.Names}} | State: {{.State}} | Status: {{.Status}}'"
		var command string
		if in.AppType != nil && *in.AppType == "docker-compose" {
			// docker-compose 类型：按标签过滤
			command = fmt.Sprintf("docker ps -a --no-trunc --format %s --filter='label=com.docker.compose.project=%s'", format, in.AppName)
		} else {
			// 普通应用：用 grep 按名字匹配（与 TS 版一致）
			command = fmt.Sprintf("docker ps -a --no-trunc --format %s | grep '^.*Name: %s'", format, in.AppName)
		}

		stdout, err := h.execDockerCommand(in.ServerID, command)
		if err != nil || strings.TrimSpace(stdout) == "" {
			return []appContainerInfo{}, nil
		}

		var result []appContainerInfo
		for _, line := range strings.Split(strings.TrimSpace(stdout), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			result = append(result, parseContainerLine(line))
		}
		if result == nil {
			result = []appContainerInfo{}
		}
		return result, nil
	}

	// getServiceContainersByAppName - 列出 Swarm 服务的任务（与 TS 版一致，使用 docker service ps）
	r["docker.getServiceContainersByAppName"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			AppName  string  `json:"appName"`
			ServerID *string `json:"serverId"`
		}
		json.Unmarshal(input, &in)

		// 与 TS 版完全一致的命令
		command := fmt.Sprintf("docker service ps %s --no-trunc --format 'CONTAINER ID : {{.ID}} | Name: {{.Name}} | State: {{.DesiredState}} | Node: {{.Node}} | CurrentState: {{.CurrentState}} | Error: {{.Error}}'", in.AppName)

		stdout, err := h.execDockerCommand(in.ServerID, command)
		if err != nil || strings.TrimSpace(stdout) == "" {
			return []serviceContainerInfo{}, nil
		}

		var result []serviceContainerInfo
		for _, line := range strings.Split(strings.TrimSpace(stdout), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			result = append(result, parseServiceLine(line))
		}
		if result == nil {
			result = []serviceContainerInfo{}
		}
		return result, nil
	}

	// getStackContainersByAppName - 列出 Stack 的任务（与 TS 版一致，使用 docker stack ps）
	r["docker.getStackContainersByAppName"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			AppName  string  `json:"appName"`
			ServerID *string `json:"serverId"`
		}
		json.Unmarshal(input, &in)

		// 与 TS 版完全一致的命令
		command := fmt.Sprintf("docker stack ps %s --no-trunc --format 'CONTAINER ID : {{.ID}} | Name: {{.Name}} | State: {{.DesiredState}} | Node: {{.Node}} | CurrentState: {{.CurrentState}} | Error: {{.Error}}'", in.AppName)

		stdout, err := h.execDockerCommand(in.ServerID, command)
		if err != nil || strings.TrimSpace(stdout) == "" {
			return []serviceContainerInfo{}, nil
		}

		var result []serviceContainerInfo
		for _, line := range strings.Split(strings.TrimSpace(stdout), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			result = append(result, parseServiceLine(line))
		}
		if result == nil {
			result = []serviceContainerInfo{}
		}
		return result, nil
	}

	r["docker.restartContainer"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			ContainerID string  `json:"containerId"`
			ServerID    *string `json:"serverId"`
		}
		json.Unmarshal(input, &in)
		if h.Docker == nil {
			return nil, &trpcErr{"Docker not available", "BAD_REQUEST", 400}
		}
		timeout := 10
		if err := h.Docker.DockerClient().ContainerRestart(c.Request().Context(), in.ContainerID, containertypes.StopOptions{Timeout: &timeout}); err != nil {
			return nil, &trpcErr{err.Error(), "BAD_REQUEST", 400}
		}
		return true, nil
	}
}

// filtersFromStrings creates Docker API filters from key=value strings.
func filtersFromStrings(filterStrs []string) filters.Args {
	f := filters.NewArgs()
	for _, s := range filterStrs {
		parts := strings.SplitN(s, "=", 2)
		if len(parts) == 2 {
			f.Add(parts[0], parts[1])
		}
	}
	return f
}
