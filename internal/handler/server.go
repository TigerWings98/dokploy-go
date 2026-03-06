package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/dokploy/dokploy/internal/db/schema"
	mw "github.com/dokploy/dokploy/internal/middleware"
	"github.com/dokploy/dokploy/internal/process"
	"github.com/dokploy/dokploy/internal/setup"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

func (h *Handler) registerServerRoutes(g *echo.Group) {
	g.POST("", h.CreateServer)
	g.GET("/:serverId", h.GetServer)
	g.GET("", h.ListServers)
	g.PUT("/:serverId", h.UpdateServer)
	g.DELETE("/:serverId", h.DeleteServer)
	g.POST("/:serverId/setup", h.SetupServer)
	g.POST("/:serverId/validate", h.ValidateServer)

	// Swarm management
	g.GET("/swarm/info", h.GetSwarmInfo)
	g.GET("/swarm/tokens", h.GetSwarmTokens)
	g.GET("/swarm/nodes", h.ListSwarmNodes)
	g.DELETE("/swarm/nodes/:nodeId", h.RemoveSwarmNode)
}

type CreateServerRequest struct {
	Name        string     `json:"name" validate:"required,min=1"`
	Description *string    `json:"description"`
	IPAddress   string     `json:"ipAddress" validate:"required"`
	Port        int        `json:"port" validate:"required"`
	Username    string     `json:"username" validate:"required"`
	SSHKeyID    *string    `json:"sshKeyId"`
	ServerType  *string    `json:"serverType"`
}

func (h *Handler) CreateServer(c echo.Context) error {
	var req CreateServerRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	user := mw.GetUser(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized)
	}

	var member schema.Member
	if err := h.DB.Where("user_id = ? AND is_default = ?", user.ID, true).First(&member).Error; err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "No default organization found")
	}

	server := &schema.Server{
		Name:           req.Name,
		Description:    req.Description,
		IPAddress:      req.IPAddress,
		Port:           req.Port,
		Username:       req.Username,
		SSHKeyID:       req.SSHKeyID,
		OrganizationID: member.OrganizationID,
	}
	if req.ServerType != nil {
		server.ServerType = schema.ServerType(*req.ServerType)
	}

	if err := h.DB.Create(server).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusCreated, server)
}

func (h *Handler) GetServer(c echo.Context) error {
	serverID := c.Param("serverId")

	var server schema.Server
	err := h.DB.
		Preload("SSHKey").
		First(&server, "\"serverId\" = ?", serverID).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Server not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, server)
}

func (h *Handler) ListServers(c echo.Context) error {
	user := mw.GetUser(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized)
	}

	var member schema.Member
	if err := h.DB.Where("user_id = ? AND is_default = ?", user.ID, true).First(&member).Error; err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "No default organization found")
	}

	var servers []schema.Server
	err := h.DB.
		Preload("SSHKey").
		Where("\"organizationId\" = ?", member.OrganizationID).
		Find(&servers).Error
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, servers)
}

func (h *Handler) UpdateServer(c echo.Context) error {
	serverID := c.Param("serverId")

	var updates map[string]interface{}
	if err := c.Bind(&updates); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	var server schema.Server
	if err := h.DB.First(&server, "\"serverId\" = ?", serverID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Server not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if err := h.DB.Model(&server).Updates(updates).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, server)
}

func (h *Handler) DeleteServer(c echo.Context) error {
	serverID := c.Param("serverId")

	result := h.DB.Delete(&schema.Server{}, "\"serverId\" = ?", serverID)
	if result.Error != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, result.Error.Error())
	}
	if result.RowsAffected == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "Server not found")
	}

	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) SetupServer(c echo.Context) error {
	serverID := c.Param("serverId")

	var server schema.Server
	if err := h.DB.Preload("SSHKey").First(&server, "\"serverId\" = ?", serverID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Server not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if server.SSHKey == nil {
		return echo.NewHTTPError(http.StatusBadRequest, "SSH key not configured")
	}

	isBuildServer := server.ServerType == schema.ServerTypeBuild
	script := setup.GenerateServerSetupScript(isBuildServer)

	conn := process.SSHConnection{
		Host:       server.IPAddress,
		Port:       server.Port,
		Username:   server.Username,
		PrivateKey: server.SSHKey.PrivateKey,
	}

	result, err := process.ExecAsyncRemote(conn, script, nil)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("Setup failed: %v", err))
	}

	// Update server status
	h.DB.Model(&server).Update("serverStatus", "active")

	return c.JSON(http.StatusOK, map[string]string{
		"message": "Server setup completed",
		"output":  result.Stdout,
	})
}

func (h *Handler) ValidateServer(c echo.Context) error {
	serverID := c.Param("serverId")

	var server schema.Server
	if err := h.DB.Preload("SSHKey").First(&server, "\"serverId\" = ?", serverID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Server not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if server.SSHKey == nil {
		return echo.NewHTTPError(http.StatusBadRequest, "SSH key not configured")
	}

	script := setup.GenerateValidationScript()
	conn := process.SSHConnection{
		Host:       server.IPAddress,
		Port:       server.Port,
		Username:   server.Username,
		PrivateKey: server.SSHKey.PrivateKey,
	}

	result, err := process.ExecAsyncRemote(conn, script, nil)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("Validation failed: %v", err))
	}

	return c.JSON(http.StatusOK, map[string]string{
		"message": "Validation completed",
		"output":  result.Stdout,
	})
}

func (h *Handler) GetSwarmInfo(c echo.Context) error {
	if h.Docker == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "Docker client not available")
	}

	mgr := setup.NewSwarmManager(h.Docker)
	info, err := mgr.GetSwarmInfo(context.Background())
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, info)
}

func (h *Handler) GetSwarmTokens(c echo.Context) error {
	if h.Docker == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "Docker client not available")
	}

	mgr := setup.NewSwarmManager(h.Docker)
	tokens, err := mgr.GetJoinTokens(context.Background())
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, tokens)
}

func (h *Handler) ListSwarmNodes(c echo.Context) error {
	if h.Docker == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "Docker client not available")
	}

	mgr := setup.NewSwarmManager(h.Docker)
	nodes, err := mgr.ListNodes(context.Background())
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, nodes)
}

func (h *Handler) RemoveSwarmNode(c echo.Context) error {
	if h.Docker == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "Docker client not available")
	}

	nodeID := c.Param("nodeId")
	force := c.QueryParam("force") == "true"

	mgr := setup.NewSwarmManager(h.Docker)
	if err := mgr.RemoveNode(context.Background(), nodeID, force); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, map[string]string{"message": "Node removed"})
}
