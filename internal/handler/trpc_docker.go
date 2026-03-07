package handler

import (
	"encoding/json"

	containertypes "github.com/docker/docker/api/types/container"
	"github.com/labstack/echo/v4"
)

func (h *Handler) registerDockerTRPC(r procedureRegistry) {
	r["docker.getContainers"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		if h.Docker == nil {
			return []interface{}{}, nil
		}
		containers, err := h.Docker.DockerClient().ContainerList(c.Request().Context(), containertypes.ListOptions{All: true})
		if err != nil {
			return nil, &trpcErr{err.Error(), "BAD_REQUEST", 400}
		}
		return containers, nil
	}

	r["docker.getConfig"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			ContainerID string `json:"containerId"`
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
		}
		json.Unmarshal(input, &in)
		if h.Docker == nil {
			return []interface{}{}, nil
		}
		container, err := h.Docker.GetContainerByName(c.Request().Context(), in.AppName)
		if err != nil || container == nil {
			return []interface{}{}, nil
		}
		return []interface{}{container}, nil
	}

	r["docker.getContainersByAppNameMatch"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			AppName  string  `json:"appName"`
			ServerID *string `json:"serverId"`
		}
		json.Unmarshal(input, &in)
		if h.Docker == nil {
			return []interface{}{}, nil
		}
		container, err := h.Docker.GetContainerByName(c.Request().Context(), in.AppName)
		if err != nil || container == nil {
			return []interface{}{}, nil
		}
		return []interface{}{container}, nil
	}

	r["docker.getServiceContainersByAppName"] = r["docker.getContainersByAppNameMatch"]
	r["docker.getStackContainersByAppName"] = r["docker.getContainersByAppNameMatch"]

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
