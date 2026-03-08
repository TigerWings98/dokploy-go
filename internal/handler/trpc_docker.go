// Input: procedureRegistry, docker (Docker SDK 客户端)
// Output: registerDockerTRPC - Docker 领域的 tRPC procedure 注册
// Role: Docker tRPC 路由注册，将 docker.* procedure 绑定到容器/服务管理和系统清理操作
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package handler

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/docker/docker/api/types"
	containertypes "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/labstack/echo/v4"
)

// containerInfo matches the original TypeScript format returned by getContainers.
type containerInfo struct {
	ContainerID string  `json:"containerId"`
	Name        string  `json:"name"`
	Image       string  `json:"image"`
	Ports       string  `json:"ports"`
	State       string  `json:"state"`
	Status      string  `json:"status"`
	ServerID    *string `json:"serverId"`
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

func (h *Handler) registerDockerTRPC(r procedureRegistry) {
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

		// Transform to the format expected by the frontend
		result := make([]containerInfo, 0, len(containers))
		for _, ct := range containers {
			name := ""
			if len(ct.Names) > 0 {
				name = strings.TrimPrefix(ct.Names[0], "/")
			}
			// Filter out dokploy containers (except monitoring)
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

	r["docker.getContainersByAppLabel"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			AppName  string  `json:"appName"`
			ServerID *string `json:"serverId"`
			Type     string  `json:"type"`
		}
		json.Unmarshal(input, &in)
		if h.Docker == nil {
			return []interface{}{}, nil
		}

		var filters []string
		if in.Type == "swarm" {
			filters = append(filters, "label=com.docker.swarm.service.name="+in.AppName)
		} else {
			filters = append(filters, "name="+in.AppName)
		}

		containers, err := h.Docker.DockerClient().ContainerList(c.Request().Context(), containertypes.ListOptions{
			All: true,
			Filters: filtersFromStrings(filters),
		})
		if err != nil {
			return []interface{}{}, nil
		}

		result := make([]containerInfo, 0, len(containers))
		for _, ct := range containers {
			name := ""
			if len(ct.Names) > 0 {
				name = strings.TrimPrefix(ct.Names[0], "/")
			}
			result = append(result, containerInfo{
				ContainerID: ct.ID,
				Name:        name,
				State:       ct.State,
				Status:      ct.Status,
				Image:       ct.Image,
				Ports:       formatPorts(ct.Ports),
				ServerID:    in.ServerID,
			})
		}
		return result, nil
	}

	r["docker.getContainersByAppNameMatch"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			AppName  string  `json:"appName"`
			AppType  *string `json:"appType"`
			ServerID *string `json:"serverId"`
		}
		json.Unmarshal(input, &in)
		if h.Docker == nil {
			return []interface{}{}, nil
		}

		var opts containertypes.ListOptions
		opts.All = true
		if in.AppType != nil && *in.AppType == "docker-compose" {
			opts.Filters = filtersFromStrings([]string{"label=com.docker.compose.project=" + in.AppName})
		}

		containers, err := h.Docker.DockerClient().ContainerList(c.Request().Context(), opts)
		if err != nil {
			return []interface{}{}, nil
		}

		result := make([]containerInfo, 0)
		for _, ct := range containers {
			name := ""
			if len(ct.Names) > 0 {
				name = strings.TrimPrefix(ct.Names[0], "/")
			}
			// For non-compose, filter by name prefix
			if in.AppType == nil || *in.AppType != "docker-compose" {
				if !strings.HasPrefix(name, in.AppName) {
					continue
				}
			}
			result = append(result, containerInfo{
				ContainerID: ct.ID,
				Name:        name,
				State:       ct.State,
				Status:      ct.Status,
				Image:       ct.Image,
				Ports:       formatPorts(ct.Ports),
				ServerID:    in.ServerID,
			})
		}
		return result, nil
	}

	r["docker.getServiceContainersByAppName"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			AppName  string  `json:"appName"`
			ServerID *string `json:"serverId"`
		}
		json.Unmarshal(input, &in)
		if h.Docker == nil {
			return []interface{}{}, nil
		}

		containers, err := h.Docker.DockerClient().ContainerList(c.Request().Context(), containertypes.ListOptions{
			Filters: filtersFromStrings([]string{"label=com.docker.swarm.service.name=" + in.AppName}),
		})
		if err != nil {
			return []interface{}{}, nil
		}

		result := make([]containerInfo, 0, len(containers))
		for _, ct := range containers {
			name := ""
			if len(ct.Names) > 0 {
				name = strings.TrimPrefix(ct.Names[0], "/")
			}
			result = append(result, containerInfo{
				ContainerID: ct.ID,
				Name:        name,
				State:       ct.State,
				Status:      ct.Status,
				Image:       ct.Image,
				Ports:       formatPorts(ct.Ports),
				ServerID:    in.ServerID,
			})
		}
		return result, nil
	}

	r["docker.getStackContainersByAppName"] = r["docker.getServiceContainersByAppName"]

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
