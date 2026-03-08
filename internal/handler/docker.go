// Input: docker (Docker SDK 客户端)
// Output: Docker 容器/服务列表/日志/清理/Config 管理的 tRPC procedure 实现
// Role: Docker 操作 handler，暴露容器管理和系统清理功能给前端
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package handler

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v4"
)

func (h *Handler) registerDockerRoutes(g *echo.Group) {
	g.GET("/containers", h.ListContainers)
	g.GET("/services", h.ListDockerServices)
	g.POST("/prune", h.DockerPrune)
	g.POST("/prune/images", h.DockerPruneImages)
	g.POST("/prune/volumes", h.DockerPruneVolumes)
	g.POST("/prune/containers", h.DockerPruneContainers)
	g.GET("/info", h.DockerInfo)
	g.POST("/service/:serviceName/restart", h.RestartDockerService)
	g.POST("/service/:serviceName/scale", h.ScaleDockerService)
	g.DELETE("/service/:serviceName", h.RemoveDockerService)
}

func (h *Handler) ListContainers(c echo.Context) error {
	if h.Docker == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "Docker client not available")
	}

	containers, err := h.Docker.ListContainers(context.Background())
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, containers)
}

func (h *Handler) ListDockerServices(c echo.Context) error {
	if h.Docker == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "Docker client not available")
	}

	services, err := h.Docker.ListServices(context.Background())
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, services)
}

func (h *Handler) DockerPrune(c echo.Context) error {
	if h.Docker == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "Docker client not available")
	}

	if err := h.Docker.PruneSystem(context.Background()); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, map[string]string{"message": "Prune completed"})
}

func (h *Handler) DockerPruneImages(c echo.Context) error {
	if h.Docker == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "Docker client not available")
	}

	if err := h.Docker.CleanupImages(context.Background()); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, map[string]string{"message": "Images pruned"})
}

func (h *Handler) DockerPruneVolumes(c echo.Context) error {
	if h.Docker == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "Docker client not available")
	}

	if err := h.Docker.CleanupVolumes(context.Background()); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, map[string]string{"message": "Volumes pruned"})
}

func (h *Handler) DockerPruneContainers(c echo.Context) error {
	if h.Docker == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "Docker client not available")
	}

	if err := h.Docker.CleanupContainers(context.Background()); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, map[string]string{"message": "Containers pruned"})
}

func (h *Handler) DockerInfo(c echo.Context) error {
	if h.Docker == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "Docker client not available")
	}

	info, err := h.Docker.DockerClient().Info(context.Background())
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"serverVersion":   info.ServerVersion,
		"containers":      info.Containers,
		"images":          info.Images,
		"memTotal":        info.MemTotal,
		"ncpu":            info.NCPU,
		"operatingSystem": info.OperatingSystem,
		"swarm": map[string]interface{}{
			"localNodeState": info.Swarm.LocalNodeState,
			"nodeID":         info.Swarm.NodeID,
			"nodes":          info.Swarm.Nodes,
			"managers":       info.Swarm.Managers,
		},
	})
}

func (h *Handler) RestartDockerService(c echo.Context) error {
	if h.Docker == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "Docker client not available")
	}

	serviceName := c.Param("serviceName")
	if err := h.Docker.RestartService(context.Background(), serviceName); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, map[string]string{"message": "Service restarted"})
}

type ScaleServiceRequest struct {
	Replicas uint64 `json:"replicas"`
}

func (h *Handler) ScaleDockerService(c echo.Context) error {
	if h.Docker == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "Docker client not available")
	}

	serviceName := c.Param("serviceName")
	var req ScaleServiceRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	if err := h.Docker.ScaleService(context.Background(), serviceName, req.Replicas); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, map[string]string{"message": "Service scaled"})
}

func (h *Handler) RemoveDockerService(c echo.Context) error {
	if h.Docker == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "Docker client not available")
	}

	serviceName := c.Param("serviceName")
	if err := h.Docker.RemoveService(context.Background(), serviceName); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, map[string]string{"message": "Service removed"})
}
