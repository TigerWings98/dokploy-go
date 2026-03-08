// Input: isBuildServer bool 参数
// Output: GenerateServerSetupScript (12 步远程服务器初始化脚本), GenerateValidationScript (组件验证脚本)
// Role: 远程服务器设置脚本生成器，生成安装 Docker/Swarm/Traefik/Nixpacks/Buildpacks 的 bash 脚本
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package setup

import (
	"fmt"
	"strings"
)

// GenerateServerSetupScript generates the bash script for remote server setup.
func GenerateServerSetupScript(isBuildServer bool) string {
	var steps []string

	// Step 1: Install base packages
	steps = append(steps, `
echo "=== Step 1: Installing base packages ==="
if command -v apt-get &>/dev/null; then
    apt-get update -qq
    apt-get install -y -qq curl wget git jq openssl
elif command -v yum &>/dev/null; then
    yum install -y -q curl wget git jq openssl
elif command -v dnf &>/dev/null; then
    dnf install -y -q curl wget git jq openssl
elif command -v apk &>/dev/null; then
    apk add --no-cache curl wget git jq openssl
fi
echo "Base packages installed"
`)

	if !isBuildServer {
		// Step 2: Verify ports
		steps = append(steps, `
echo "=== Step 2: Verifying ports ==="
for port in 80 443; do
    if command -v ss &>/dev/null; then
        if ss -tlnp | grep -q ":${port} "; then
            echo "WARNING: Port ${port} is already in use"
        fi
    fi
done
echo "Port check complete"
`)

		// Step 3: Install rclone
		steps = append(steps, `
echo "=== Step 3: Installing RClone ==="
if ! command -v rclone &>/dev/null; then
    curl -s https://rclone.org/install.sh | bash
    echo "RClone installed"
else
    echo "RClone already installed"
fi
`)
	}

	// Step 4: Install Docker
	steps = append(steps, `
echo "=== Step 4: Installing Docker ==="
if ! command -v docker &>/dev/null; then
    curl -fsSL https://get.docker.com | sh
    systemctl enable docker 2>/dev/null || true
    systemctl start docker 2>/dev/null || true
    echo "Docker installed"
else
    echo "Docker already installed: $(docker --version)"
fi
`)

	if !isBuildServer {
		// Step 5: Setup Docker Swarm
		steps = append(steps, `
echo "=== Step 5: Setting up Docker Swarm ==="
SWARM_STATUS=$(docker info --format '{{.Swarm.LocalNodeState}}' 2>/dev/null)
if [ "$SWARM_STATUS" != "active" ]; then
    PUBLIC_IP=$(curl -4s https://ifconfig.io 2>/dev/null || echo "127.0.0.1")
    docker swarm init --advertise-addr "$PUBLIC_IP" || docker swarm init --advertise-addr "127.0.0.1"
    echo "Docker Swarm initialized"
else
    echo "Docker Swarm already active"
fi
`)

		// Step 6: Create network
		steps = append(steps, `
echo "=== Step 6: Creating dokploy-network ==="
if ! docker network ls | grep -q "dokploy-network"; then
    docker network create --driver overlay --attachable dokploy-network
    echo "Network created"
else
    echo "Network already exists"
fi
`)

		// Step 7: Create directories
		steps = append(steps, `
echo "=== Step 7: Creating directories ==="
mkdir -p /etc/dokploy/traefik/dynamic \
         /etc/dokploy/logs \
         /etc/dokploy/applications \
         /etc/dokploy/compose \
         /etc/dokploy/monitoring \
         /etc/dokploy/schedules \
         /etc/dokploy/volume-backups
mkdir -p /etc/dokploy/ssh && chmod 700 /etc/dokploy/ssh
echo "Directories created"
`)

		// Step 8: Generate Traefik config
		steps = append(steps, `
echo "=== Step 8: Generating Traefik config ==="
if [ ! -f /etc/dokploy/traefik/traefik.yml ]; then
    cat > /etc/dokploy/traefik/traefik.yml << 'TRAEFIKEOF'
api:
  insecure: true
entryPoints:
  web:
    address: ":80"
  websecure:
    address: ":443"
    http:
      tls: {}
providers:
  docker:
    exposedByDefault: false
    network: dokploy-network
  file:
    directory: /etc/dokploy/traefik/dynamic
    watch: true
log:
  level: ERROR
TRAEFIKEOF
    echo "Traefik config created"
else
    echo "Traefik config already exists"
fi
`)

		// Step 9: Create default middlewares
		steps = append(steps, `
echo "=== Step 9: Creating default middlewares ==="
if [ ! -f /etc/dokploy/traefik/dynamic/middlewares.yml ]; then
    cat > /etc/dokploy/traefik/dynamic/middlewares.yml << 'MWEOF'
http:
  middlewares:
    redirect-to-https:
      redirectScheme:
        scheme: https
        permanent: true
MWEOF
    echo "Middlewares created"
else
    echo "Middlewares already exist"
fi
`)

		// Step 10: Create Traefik container
		steps = append(steps, `
echo "=== Step 10: Creating Traefik container ==="
if ! docker ps -a --format '{{.Names}}' | grep -q "^dokploy-traefik$"; then
    docker run -d \
        --name dokploy-traefik \
        --restart always \
        --network dokploy-network \
        -p 80:80 \
        -p 443:443 \
        -v /etc/dokploy/traefik/traefik.yml:/etc/traefik/traefik.yml:ro \
        -v /etc/dokploy/traefik/dynamic:/etc/traefik/dynamic:ro \
        -v /var/run/docker.sock:/var/run/docker.sock:ro \
        traefik:v3.3
    echo "Traefik container created"
else
    echo "Traefik container already exists"
fi
`)
	}

	// Step 11: Install Nixpacks
	steps = append(steps, `
echo "=== Step 11: Installing Nixpacks ==="
if ! command -v nixpacks &>/dev/null; then
    curl -sSL https://nixpacks.com/install.sh | bash
    echo "Nixpacks installed"
else
    echo "Nixpacks already installed"
fi
`)

	// Step 12: Install Buildpacks
	steps = append(steps, `
echo "=== Step 12: Installing Buildpacks (pack) ==="
if ! command -v pack &>/dev/null; then
    (curl -sSL "https://github.com/buildpacks/pack/releases/latest/download/pack-$(uname -s | tr '[:upper:]' '[:lower:]')-$(uname -m).tgz" | tar -C /usr/local/bin/ --no-same-owner -xzf - pack) 2>/dev/null || true
    echo "Buildpacks installed"
else
    echo "Buildpacks already installed"
fi
`)

	steps = append(steps, `
echo "=== Server setup complete ==="
`)

	return strings.Join(steps, "\n")
}

// GenerateValidationScript generates a script to validate server components.
func GenerateValidationScript() string {
	return fmt.Sprintf(`#!/bin/bash
set -e

# Docker
DOCKER_VERSION=$(docker --version 2>/dev/null | awk '{print $3}' | tr -d ',') || DOCKER_VERSION=""
DOCKER_ENABLED=$(systemctl is-active docker 2>/dev/null) || DOCKER_ENABLED="unknown"

# Swarm
SWARM_STATUS=$(docker info --format '{{.Swarm.LocalNodeState}}' 2>/dev/null) || SWARM_STATUS="inactive"

# RClone
RCLONE_VERSION=$(rclone version 2>/dev/null | head -1 | awk '{print $2}') || RCLONE_VERSION=""

# Nixpacks
NIXPACKS_VERSION=$(nixpacks --version 2>/dev/null | awk '{print $2}') || NIXPACKS_VERSION=""

# Buildpacks
PACK_VERSION=$(pack version 2>/dev/null) || PACK_VERSION=""

echo "{
  \"docker\": {\"version\": \"$DOCKER_VERSION\", \"enabled\": \"$DOCKER_ENABLED\"},
  \"swarm\": {\"status\": \"$SWARM_STATUS\"},
  \"rclone\": {\"version\": \"$RCLONE_VERSION\"},
  \"nixpacks\": {\"version\": \"$NIXPACKS_VERSION\"},
  \"buildpacks\": {\"version\": \"$PACK_VERSION\"}
}"
`)
}
