// Input: procedureRegistry, db (Server 表), setup, process/ssh
// Output: registerServerTRPC - Server 领域的 tRPC procedure 注册
// Role: Server tRPC 路由注册，将 server.* procedure 绑定到远程服务器管理操作
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package handler

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/dokploy/dokploy/internal/db/schema"
	"github.com/labstack/echo/v4"
	"golang.org/x/crypto/ssh"
)

func (h *Handler) registerServerTRPC(r procedureRegistry) {
	r["server.all"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		member, err := h.getDefaultMember(c)
		if err != nil {
			return nil, err
		}
		var servers []schema.Server
		h.DB.Preload("SSHKey").
			Where("\"organizationId\" = ?", member.OrganizationID).
			Find(&servers)
		return servers, nil
	}

	r["server.one"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ ServerID string `json:"serverId"` }
		json.Unmarshal(input, &in)
		var server schema.Server
		err := h.DB.Preload("SSHKey").First(&server, "\"serverId\" = ?", in.ServerID).Error
		if err != nil {
			return nil, &trpcErr{"Server not found", "NOT_FOUND", 404}
		}
		return server, nil
	}

	r["server.create"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		member, err := h.getDefaultMember(c)
		if err != nil {
			return nil, err
		}
		var in struct {
			Name      string  `json:"name"`
			IPAddress string  `json:"ipAddress"`
			Port      int     `json:"port"`
			Username  string  `json:"username"`
			SSHKeyID  *string `json:"sshKeyId"`
		}
		json.Unmarshal(input, &in)
		server := &schema.Server{
			Name:           in.Name,
			IPAddress:      in.IPAddress,
			Port:           in.Port,
			Username:       in.Username,
			SSHKeyID:       in.SSHKeyID,
			OrganizationID: member.OrganizationID,
		}
		if err := h.DB.Create(server).Error; err != nil {
			return nil, err
		}
		return server, nil
	}

	r["server.remove"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ ServerID string `json:"serverId"` }
		json.Unmarshal(input, &in)
		h.DB.Delete(&schema.Server{}, "\"serverId\" = ?", in.ServerID)
		return true, nil
	}

	r["server.update"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in map[string]interface{}
		json.Unmarshal(input, &in)
		serverID, _ := in["serverId"].(string)
		delete(in, "serverId")
		var server schema.Server
		if err := h.DB.First(&server, "\"serverId\" = ?", serverID).Error; err != nil {
			return nil, &trpcErr{"Server not found", "NOT_FOUND", 404}
		}
		h.DB.Model(&server).Updates(in)
		return server, nil
	}

	r["server.count"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		member, err := h.getDefaultMember(c)
		if err != nil {
			return nil, err
		}
		var count int64
		h.DB.Model(&schema.Server{}).Where("\"organizationId\" = ?", member.OrganizationID).Count(&count)
		return count, nil
	}

	r["server.getDefaultCommand"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			ServerID string `json:"serverId"`
		}
		json.Unmarshal(input, &in)

		var server schema.Server
		if err := h.DB.Preload("SSHKey").First(&server, "\"serverId\" = ?", in.ServerID).Error; err != nil {
			return nil, &trpcErr{"Server not found", "NOT_FOUND", 404}
		}

		settings, _ := h.getOrCreateSettings()
		serverIP := "0.0.0.0"
		if settings != nil && settings.ServerIP != nil {
			serverIP = *settings.ServerIP
		}

		cmd := fmt.Sprintf("curl -sSL https://%s/api/setup | bash -s -- %s",
			serverIP, server.ServerID)
		return cmd, nil
	}

	r["server.validate"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			ServerID string `json:"serverId"`
		}
		json.Unmarshal(input, &in)

		var server schema.Server
		if err := h.DB.Preload("SSHKey").First(&server, "\"serverId\" = ?", in.ServerID).Error; err != nil {
			return nil, &trpcErr{"Server not found", "NOT_FOUND", 404}
		}
		if server.SSHKey == nil {
			return nil, &trpcErr{"Server has no SSH key", "BAD_REQUEST", 400}
		}

		signer, err := ssh.ParsePrivateKey([]byte(server.SSHKey.PrivateKey))
		if err != nil {
			return nil, &trpcErr{"Invalid SSH key: " + err.Error(), "BAD_REQUEST", 400}
		}

		config := &ssh.ClientConfig{
			User:            server.Username,
			Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
			HostKeyCallback: ssh.InsecureIgnoreHostKey(),
			Timeout:         10 * time.Second,
		}

		addr := fmt.Sprintf("%s:%d", server.IPAddress, server.Port)
		client, err := ssh.Dial("tcp", addr, config)
		if err != nil {
			return nil, &trpcErr{fmt.Sprintf("SSH connection failed: %s", err.Error()), "BAD_REQUEST", 400}
		}
		client.Close()

		h.DB.Model(&server).Update("\"serverStatus\"", "active")
		return true, nil
	}

	r["server.publicIp"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		// Returns the main server's public IP (from settings), not a specific server
		settings, _ := h.getOrCreateSettings()
		if settings != nil && settings.ServerIP != nil {
			return *settings.ServerIP, nil
		}
		return "", nil
	}

	r["server.security"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			ServerID string `json:"serverId"`
		}
		json.Unmarshal(input, &in)
		var server schema.Server
		if err := h.DB.First(&server, "\"serverId\" = ?", in.ServerID).Error; err != nil {
			return nil, &trpcErr{"Server not found", "NOT_FOUND", 404}
		}
		return map[string]interface{}{
			"serverId":     server.ServerID,
			"serverStatus": server.ServerStatus,
		}, nil
	}

	r["server.setupMonitoring"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return true, nil
	}

	r["server.getServerMetrics"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return map[string]interface{}{
			"cpuUsage":    []interface{}{},
			"memoryUsage": []interface{}{},
			"diskUsage":   []interface{}{},
		}, nil
	}

	r["server.setup"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return true, nil
	}

	r["server.getServerTime"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return map[string]interface{}{
			"serverTime": time.Now().UTC().Format(time.RFC3339),
		}, nil
	}

	r["server.withSSHKey"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		member, err := h.getDefaultMember(c)
		if err != nil {
			return nil, err
		}
		var servers []schema.Server
		h.DB.Preload("SSHKey").
			Where("\"organizationId\" = ? AND \"sshKeyId\" IS NOT NULL", member.OrganizationID).
			Find(&servers)
		return servers, nil
	}

	r["server.buildServers"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		member, err := h.getDefaultMember(c)
		if err != nil {
			return nil, err
		}
		var servers []schema.Server
		h.DB.Preload("SSHKey").
			Where("\"organizationId\" = ? AND \"serverRole\" = ?", member.OrganizationID, "build").
			Find(&servers)
		return servers, nil
	}
}
