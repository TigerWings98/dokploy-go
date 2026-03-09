// Input: procedureRegistry, db (Server 表), setup, process/ssh
// Output: registerServerTRPC - Server 领域的 tRPC procedure 注册
// Role: Server tRPC 路由注册，将 server.* procedure 绑定到远程服务器管理操作
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package handler

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/dokploy/dokploy/internal/db/schema"
	"github.com/labstack/echo/v4"
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

	// server.validate - 通过 SSH 执行验证脚本，检查 Docker/RClone/Nixpacks/Buildpacks/Railpack/Swarm/Network/目录（与 TS 版完全一致）
	r["server.validate"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			ServerID string `json:"serverId"`
		}
		json.Unmarshal(input, &in)

		// 通过 SSH 执行验证脚本（与 TS 版 server-validate.ts 完全一致）
		bashCommand := `
command_exists() {
  command -v "$@" > /dev/null 2>&1
}

# Docker
if command_exists docker; then
  dockerVersionEnabled="$(docker --version | awk '{print $3}' | sed 's/,//') true"
else
  dockerVersionEnabled="0.0.0 false"
fi

# RClone
if command_exists rclone; then
  rcloneVersionEnabled="$(rclone --version | head -n 1 | awk '{print $2}' | sed 's/^v//') true"
else
  rcloneVersionEnabled="0.0.0 false"
fi

# Nixpacks
if command_exists nixpacks; then
  version=$(nixpacks --version | awk '{print $2}')
  if [ -n "$version" ]; then
    nixpacksVersionEnabled="$version true"
  else
    nixpacksVersionEnabled="0.0.0 false"
  fi
else
  nixpacksVersionEnabled="0.0.0 false"
fi

# Buildpacks
if command_exists pack; then
  version=$(pack --version | awk '{print $1}')
  if [ -n "$version" ]; then
    buildpacksVersionEnabled="$version true"
  else
    buildpacksVersionEnabled="0.0.0 false"
  fi
else
  buildpacksVersionEnabled="0.0.0 false"
fi

# Railpack
if command_exists railpack; then
  version=$(railpack --version | awk '{print $3}')
  if [ -n "$version" ]; then
    railpackVersionEnabled="$version true"
  else
    railpackVersionEnabled="0.0.0 false"
  fi
else
  railpackVersionEnabled="0.0.0 false"
fi

dockerVersion=$(echo $dockerVersionEnabled | awk '{print $1}')
dockerEnabled=$(echo $dockerVersionEnabled | awk '{print $2}')
rcloneVersion=$(echo $rcloneVersionEnabled | awk '{print $1}')
rcloneEnabled=$(echo $rcloneVersionEnabled | awk '{print $2}')
nixpacksVersion=$(echo $nixpacksVersionEnabled | awk '{print $1}')
nixpacksEnabled=$(echo $nixpacksVersionEnabled | awk '{print $2}')
buildpacksVersion=$(echo $buildpacksVersionEnabled | awk '{print $1}')
buildpacksEnabled=$(echo $buildpacksVersionEnabled | awk '{print $2}')
railpackVersion=$(echo $railpackVersionEnabled | awk '{print $1}')
railpackEnabled=$(echo $railpackVersionEnabled | awk '{print $2}')

# Swarm
if docker info --format '{{.Swarm.LocalNodeState}}' | grep -q 'active'; then
  isSwarmInstalled=true
else
  isSwarmInstalled=false
fi

# Network
if docker network ls | grep -q 'dokploy-network'; then
  isDokployNetworkInstalled=true
else
  isDokployNetworkInstalled=false
fi

# Main directory
if [ -d "/etc/dokploy" ]; then
  isMainDirectoryInstalled=true
else
  isMainDirectoryInstalled=false
fi

echo "{\"docker\": {\"version\": \"$dockerVersion\", \"enabled\": $dockerEnabled}, \"rclone\": {\"version\": \"$rcloneVersion\", \"enabled\": $rcloneEnabled}, \"nixpacks\": {\"version\": \"$nixpacksVersion\", \"enabled\": $nixpacksEnabled}, \"buildpacks\": {\"version\": \"$buildpacksVersion\", \"enabled\": $buildpacksEnabled}, \"railpack\": {\"version\": \"$railpackVersion\", \"enabled\": $railpackEnabled}, \"isDokployNetworkInstalled\": $isDokployNetworkInstalled, \"isSwarmInstalled\": $isSwarmInstalled, \"isMainDirectoryInstalled\": $isMainDirectoryInstalled}"
`
		sid := in.ServerID
		stdout, err := h.execDockerCommand(&sid, bashCommand)
		if err != nil {
			return nil, &trpcErr{fmt.Sprintf("Validation failed: %s", err.Error()), "BAD_REQUEST", 400}
		}

		var result map[string]interface{}
		if jsonErr := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &result); jsonErr != nil {
			return nil, &trpcErr{fmt.Sprintf("Failed to parse validation output: %s", jsonErr.Error()), "BAD_REQUEST", 400}
		}
		return result, nil
	}

	r["server.publicIp"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		// Returns the main server's public IP (from settings), not a specific server
		settings, _ := h.getOrCreateSettings()
		if settings != nil && settings.ServerIP != nil {
			return *settings.ServerIP, nil
		}
		return "", nil
	}

	// server.security - 通过 SSH 执行安全审计脚本，检查 UFW/SSH/Fail2Ban（与 TS 版 server-audit.ts 完全一致）
	r["server.security"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			ServerID string `json:"serverId"`
		}
		json.Unmarshal(input, &in)

		bashCommand := `
command_exists() {
  command -v "$@" > /dev/null 2>&1
}

# UFW Validation
if command -v ufw >/dev/null 2>&1; then
  ufwInstalled=true
  ufwActive=$(sudo ufw status | grep -q "Status: active" && echo true || echo false)
  ufwDefaultIncoming=$(sudo ufw status verbose | grep "Default:" | grep "incoming" | awk '{print $2}')
  ufwStatus="{\"installed\": $ufwInstalled, \"active\": $ufwActive, \"defaultIncoming\": \"$ufwDefaultIncoming\"}"
else
  ufwStatus="{\"installed\": false, \"active\": false, \"defaultIncoming\": \"unknown\"}"
fi

# SSH Validation
if systemctl is-active --quiet sshd || systemctl is-active --quiet ssh; then
  sshEnabled=true
  sshd_config=$(sudo sshd -T 2>/dev/null | grep -i "^configfile" | awk '{print $2}')
  if [ -z "$sshd_config" ]; then
    sshd_config="/etc/ssh/sshd_config"
  fi
  pubkey_line=$(sudo grep -i "^PubkeyAuthentication" "$sshd_config" 2>/dev/null | grep -v "#")
  if [ -z "$pubkey_line" ] || echo "$pubkey_line" | grep -q -i "yes"; then
    sshKeyAuth=true
  else
    sshKeyAuth=false
  fi
  sshPermitRootLogin=$(sudo grep -i "^PermitRootLogin" "$sshd_config" 2>/dev/null | grep -v "#" | awk '{print $2}')
  if [ -z "$sshPermitRootLogin" ]; then
    sshPermitRootLogin="prohibit-password"
  fi
  sshPasswordAuth=$(sudo grep -i "^PasswordAuthentication" "$sshd_config" 2>/dev/null | grep -v "#" | awk '{print $2}')
  if [ -z "$sshPasswordAuth" ]; then
    sshPasswordAuth="yes"
  fi
  sshUsePam=$(sudo grep -i "^UsePAM" "$sshd_config" 2>/dev/null | grep -v "#" | awk '{print $2}')
  if [ -z "$sshUsePam" ]; then
    sshUsePam="yes"
  fi
  sshStatus="{\"enabled\": $sshEnabled, \"keyAuth\": $sshKeyAuth, \"permitRootLogin\": \"$sshPermitRootLogin\", \"passwordAuth\": \"$sshPasswordAuth\", \"usePam\": \"$sshUsePam\"}"
else
  sshStatus="{\"enabled\": false, \"keyAuth\": false, \"permitRootLogin\": \"unknown\", \"passwordAuth\": \"unknown\", \"usePam\": \"unknown\"}"
fi

# Fail2Ban Validation
if dpkg -l | grep -q "fail2ban"; then
  f2bInstalled=true
  f2bEnabled=$(systemctl is-enabled --quiet fail2ban.service && echo true || echo false)
  f2bActive=$(systemctl is-active --quiet fail2ban.service && echo true || echo false)
  if [ -f "/etc/fail2ban/jail.local" ]; then
    f2bSshEnabled=$(grep -A10 "^\[sshd\]" /etc/fail2ban/jail.local | grep "enabled" | awk '{print $NF}' | tr -d '[:space:]')
    f2bSshMode=$(grep -A10 "^\[sshd\]" /etc/fail2ban/jail.local | grep "^mode[[:space:]]*=[[:space:]]*aggressive" >/dev/null && echo "aggressive" || echo "normal")
    fail2banStatus="{\"installed\": $f2bInstalled, \"enabled\": $f2bEnabled, \"active\": $f2bActive, \"sshEnabled\": \"$f2bSshEnabled\", \"sshMode\": \"$f2bSshMode\"}"
  else
    fail2banStatus="{\"installed\": $f2bInstalled, \"enabled\": $f2bEnabled, \"active\": $f2bActive, \"sshEnabled\": \"false\", \"sshMode\": \"normal\"}"
  fi
else
  fail2banStatus="{\"installed\": false, \"enabled\": false, \"active\": false, \"sshEnabled\": \"false\", \"sshMode\": \"normal\"}"
fi

echo "{\"ufw\": $ufwStatus, \"ssh\": $sshStatus, \"fail2ban\": $fail2banStatus}"
`
		sid := in.ServerID
		stdout, err := h.execDockerCommand(&sid, bashCommand)
		if err != nil {
			return nil, &trpcErr{fmt.Sprintf("Security audit failed: %s", err.Error()), "BAD_REQUEST", 400}
		}

		var result map[string]interface{}
		if jsonErr := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &result); jsonErr != nil {
			return nil, &trpcErr{fmt.Sprintf("Failed to parse security audit output: %s", jsonErr.Error()), "BAD_REQUEST", 400}
		}
		return result, nil
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
