#!/bin/bash

# Dokploy Go - Installation Script
#
# Usage:
#   curl -sSL https://your-domain.com/install.sh | sh
#   curl -sSL https://your-domain.com/install.sh | sh -s update
#
# Environment variables:
#   DOKPLOY_VERSION    - Image tag to install (default: auto-detect latest from registry)
#   DOCKER_IMAGE       - Full image reference (default: dokploy/dokploy:$VERSION)
#   ADVERTISE_ADDR     - Docker Swarm advertise address (default: auto-detect private IP)
#   DOCKER_SWARM_INIT_ARGS - Extra args for docker swarm init (e.g. --default-addr-pool)
#   REGISTRY_URL       - Private registry URL (e.g. registry.example.com)
#   REGISTRY_USERNAME  - Private registry username
#   REGISTRY_PASSWORD  - Private registry password

# ============================================================
# Configuration - Change these to match your registry
# ============================================================

# Default Docker image (override with DOCKER_IMAGE env var)
DEFAULT_REGISTRY="crpi-aslakz6qmbvaprxp.cn-shanghai.personal.cr.aliyuncs.com/tigerking/dokploy-go"

# ============================================================
# Helper Functions
# ============================================================

detect_version() {
    local version="${DOKPLOY_VERSION}"

    if [ -z "$version" ]; then
        version="latest"
        echo "No DOKPLOY_VERSION specified, using '$version'" >&2
    fi

    echo "$version"
}

is_proxmox_lxc() {
    if [ -n "$container" ] && [ "$container" = "lxc" ]; then
        return 0
    fi
    if grep -q "container=lxc" /proc/1/environ 2>/dev/null; then
        return 0
    fi
    return 1
}

generate_random_password() {
    local password=""

    if command -v openssl >/dev/null 2>&1; then
        password=$(openssl rand -base64 32 | tr -d "=+/" | cut -c1-32)
    elif [ -r /dev/urandom ]; then
        password=$(tr -dc 'A-Za-z0-9' < /dev/urandom | head -c 32)
    else
        if command -v sha256sum >/dev/null 2>&1; then
            password=$(date +%s%N | sha256sum | base64 | head -c 32)
        elif command -v shasum >/dev/null 2>&1; then
            password=$(date +%s%N | shasum -a 256 | base64 | head -c 32)
        else
            password=$(echo "$(date +%s%N)-$(hostname)-$$-$RANDOM" | base64 | tr -d "=+/" | head -c 32)
        fi
    fi

    if [ -z "$password" ] || [ ${#password} -lt 20 ]; then
        echo "Error: Failed to generate random password" >&2
        exit 1
    fi

    echo "$password"
}

get_ip() {
    local ip=""

    # Try IPv4
    ip=$(curl -4s --connect-timeout 5 https://ifconfig.io 2>/dev/null)
    [ -z "$ip" ] && ip=$(curl -4s --connect-timeout 5 https://icanhazip.com 2>/dev/null)
    [ -z "$ip" ] && ip=$(curl -4s --connect-timeout 5 https://ipecho.net/plain 2>/dev/null)

    # Fallback to IPv6
    if [ -z "$ip" ]; then
        ip=$(curl -6s --connect-timeout 5 https://ifconfig.io 2>/dev/null)
        [ -z "$ip" ] && ip=$(curl -6s --connect-timeout 5 https://icanhazip.com 2>/dev/null)
        [ -z "$ip" ] && ip=$(curl -6s --connect-timeout 5 https://ipecho.net/plain 2>/dev/null)
    fi

    if [ -z "$ip" ]; then
        echo "Error: Could not determine server IP address (neither IPv4 nor IPv6)." >&2
        echo "Please set the ADVERTISE_ADDR environment variable manually." >&2
        echo "Example: export ADVERTISE_ADDR=<your-server-ip>" >&2
        exit 1
    fi

    echo "$ip"
}

get_private_ip() {
    ip addr show | grep -E "inet (192\.168\.|10\.|172\.1[6-9]\.|172\.2[0-9]\.|172\.3[0-1]\.)" | head -n1 | awk '{print $2}' | cut -d/ -f1
}

format_ip_for_url() {
    local ip="$1"
    if echo "$ip" | grep -q ':'; then
        echo "[${ip}]"
    else
        echo "${ip}"
    fi
}

command_exists() {
    command -v "$@" > /dev/null 2>&1
}

# ============================================================
# Install
# ============================================================

install_dokploy() {
    VERSION_TAG=$(detect_version)
    DOCKER_IMAGE="${DOCKER_IMAGE:-${DEFAULT_REGISTRY}:${VERSION_TAG}}"

    echo "============================================"
    echo "  Dokploy Go - Installing ${DOCKER_IMAGE}"
    echo "============================================"

    # --- Pre-flight checks ---
    if [ "$(id -u)" != "0" ]; then
        echo "Error: This script must be run as root" >&2
        exit 1
    fi

    if [ "$(uname)" = "Darwin" ]; then
        echo "Error: This script must be run on Linux, not macOS" >&2
        exit 1
    fi

    if [ -f /.dockerenv ]; then
        echo "Error: This script should not be run inside a Docker container" >&2
        exit 1
    fi

    for port in 80 443 3000; do
        if ss -tulnp | grep ":${port} " >/dev/null; then
            echo "Error: port ${port} is already in use" >&2
            exit 1
        fi
    done

    # --- Install Docker if needed ---
    if command_exists docker; then
        echo "Docker already installed"
    else
        echo "Installing Docker..."
        curl -sSL https://get.docker.com | sh -s -- --version 28.5.0
    fi

    # --- Login to private registry if credentials provided ---
    if [ -n "$REGISTRY_URL" ]; then
        if [ -n "$REGISTRY_USERNAME" ] && [ -n "$REGISTRY_PASSWORD" ]; then
            echo "Logging in to private registry: $REGISTRY_URL"
            echo "$REGISTRY_PASSWORD" | docker login "$REGISTRY_URL" -u "$REGISTRY_USERNAME" --password-stdin
        else
            echo "Error: REGISTRY_URL is set but REGISTRY_USERNAME or REGISTRY_PASSWORD is missing" >&2
            exit 1
        fi
    fi

    # --- Proxmox LXC detection ---
    endpoint_mode=""
    if is_proxmox_lxc; then
        echo ""
        echo "WARNING: Detected Proxmox LXC container environment!"
        echo "Adding --endpoint-mode dnsrr for LXC compatibility."
        endpoint_mode="--endpoint-mode dnsrr"
        sleep 3
    fi

    # --- Docker Swarm init ---
    docker swarm leave --force 2>/dev/null

    advertise_addr="${ADVERTISE_ADDR:-$(get_private_ip)}"
    if [ -z "$advertise_addr" ]; then
        echo "Error: Could not determine private IP address." >&2
        echo "Please set ADVERTISE_ADDR manually. Example: export ADVERTISE_ADDR=192.168.1.100" >&2
        exit 1
    fi
    echo "Using advertise address: $advertise_addr"

    swarm_init_args="${DOCKER_SWARM_INIT_ARGS:-}"
    if [ -n "$swarm_init_args" ]; then
        echo "Using custom swarm init arguments: $swarm_init_args"
        docker swarm init --advertise-addr "$advertise_addr" $swarm_init_args
    else
        docker swarm init --advertise-addr "$advertise_addr"
    fi

    if [ $? -ne 0 ]; then
        echo "Error: Failed to initialize Docker Swarm" >&2
        exit 1
    fi
    echo "Swarm initialized"

    # --- Network ---
    docker network rm -f dokploy-network 2>/dev/null
    docker network create --driver overlay --attachable dokploy-network
    echo "Network created"

    # --- Directories ---
    mkdir -p /etc/dokploy
    chmod 777 /etc/dokploy

    # --- Database credentials (Docker Secrets) ---
    POSTGRES_PASSWORD=$(generate_random_password)
    echo "$POSTGRES_PASSWORD" | docker secret create dokploy_postgres_password - 2>/dev/null || true
    echo "Generated secure database credentials (stored in Docker Secrets)"

    # --- PostgreSQL ---
    docker service create \
        --name dokploy-postgres \
        --constraint 'node.role==manager' \
        --network dokploy-network \
        --env POSTGRES_USER=dokploy \
        --env POSTGRES_DB=dokploy \
        --secret source=dokploy_postgres_password,target=/run/secrets/postgres_password \
        --env POSTGRES_PASSWORD_FILE=/run/secrets/postgres_password \
        --mount type=volume,source=dokploy-postgres,target=/var/lib/postgresql/data \
        $endpoint_mode \
        postgres:16

    # --- Redis ---
    docker service create \
        --name dokploy-redis \
        --constraint 'node.role==manager' \
        --network dokploy-network \
        --mount type=volume,source=dokploy-redis,target=/data \
        $endpoint_mode \
        redis:7

    # --- Dokploy Go ---
    release_tag_env=""
    if echo "$VERSION_TAG" | grep -qE '^v?[0-9]+\.[0-9]+\.[0-9]+'; then
        release_tag_env="-e RELEASE_TAG=latest"
    elif [ "$VERSION_TAG" != "latest" ]; then
        release_tag_env="-e RELEASE_TAG=$VERSION_TAG"
    fi

    docker service create \
        --name dokploy \
        --replicas 1 \
        --network dokploy-network \
        --mount type=bind,source=/var/run/docker.sock,target=/var/run/docker.sock \
        --mount type=bind,source=/etc/dokploy,target=/etc/dokploy \
        --mount type=volume,source=dokploy,target=/root/.docker \
        --secret source=dokploy_postgres_password,target=/run/secrets/postgres_password \
        --publish published=3000,target=3000,mode=host \
        --update-parallelism 1 \
        --update-order stop-first \
        --constraint 'node.role == manager' \
        $endpoint_mode \
        $release_tag_env \
        -e ADVERTISE_ADDR="$advertise_addr" \
        -e POSTGRES_PASSWORD_FILE=/run/secrets/postgres_password \
        "$DOCKER_IMAGE"

    sleep 4

    # --- Traefik ---
    docker run -d \
        --name dokploy-traefik \
        --restart always \
        -v /etc/dokploy/traefik/traefik.yml:/etc/traefik/traefik.yml \
        -v /etc/dokploy/traefik/dynamic:/etc/dokploy/traefik/dynamic \
        -v /var/run/docker.sock:/var/run/docker.sock:ro \
        -p 80:80/tcp \
        -p 443:443/tcp \
        -p 443:443/udp \
        traefik:v3.6.7

    docker network connect dokploy-network dokploy-traefik

    # --- Done ---
    public_ip="${ADVERTISE_ADDR:-$(get_ip)}"
    formatted_addr=$(format_ip_for_url "$public_ip")

    GREEN="\033[0;32m"
    YELLOW="\033[1;33m"
    BLUE="\033[0;34m"
    NC="\033[0m"

    echo ""
    printf "${GREEN}Congratulations, Dokploy Go is installed!${NC}\n"
    printf "${BLUE}Wait about 15 seconds for the server to start${NC}\n"
    printf "${YELLOW}Please go to http://${formatted_addr}:3000${NC}\n\n"
}

# ============================================================
# Update
# ============================================================

update_dokploy() {
    VERSION_TAG=$(detect_version)
    DOCKER_IMAGE="${DOCKER_IMAGE:-${DEFAULT_REGISTRY}:${VERSION_TAG}}"

    echo "Updating Dokploy Go to: ${DOCKER_IMAGE}"

    # Pull the image first (Swarm nodes may not share docker login credentials)
    docker pull "$DOCKER_IMAGE"

    # Update the service with registry auth forwarding
    docker service update --force --with-registry-auth --image "$DOCKER_IMAGE" dokploy

    echo "Dokploy Go has been updated to: ${DOCKER_IMAGE}"
}

# ============================================================
# Main
# ============================================================

if [ "$1" = "update" ]; then
    update_dokploy
else
    install_dokploy
fi
