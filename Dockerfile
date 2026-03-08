# syntax=docker/dockerfile:1

# ============================================================
# Stage 1: Build Go binaries
# ============================================================
FROM golang:1.25-alpine AS go-builder
ENV GOPROXY=https://goproxy.cn,direct
RUN sed -i 's|dl-cdn.alpinelinux.org|mirrors.aliyun.com|g' /etc/apk/repositories
WORKDIR /build
RUN apk add --no-cache git
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/server ./cmd/server && \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/scheduler ./cmd/scheduler && \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/api ./cmd/api

# ============================================================
# Stage 2: Build frontend (Next.js static export)
# ============================================================
FROM node:22-alpine AS frontend-builder
RUN sed -i 's|dl-cdn.alpinelinux.org|mirrors.aliyun.com|g' /etc/apk/repositories
RUN corepack enable && corepack prepare pnpm@10.22.0 --activate
WORKDIR /build
COPY frontend/package.json frontend/pnpm-lock.yaml .npmrc* ./
RUN pnpm config set registry https://registry.npmmirror.com
RUN --mount=type=cache,id=pnpm,target=/pnpm/store pnpm install --frozen-lockfile
COPY frontend/ .
ENV SKIP_ENV_VALIDATION=1
ENV NODE_ENV=production
RUN pnpm run build

# ============================================================
# Stage 3: Final runtime image
# ============================================================
FROM alpine:3.21

RUN sed -i 's|dl-cdn.alpinelinux.org|mirrors.aliyun.com|g' /etc/apk/repositories
RUN apk add --no-cache \
    docker-cli \
    docker-cli-compose \
    curl \
    git \
    git-lfs \
    openssh-client \
    rsync \
    bash \
    apache2-utils \
    iproute2 \
    unzip \
    rclone

# Install Nixpacks
RUN curl -sSL https://nixpacks.com/install.sh | bash

# Install Railpack
RUN curl -sSL https://railpack.com/install.sh | bash

# Install Pack CLI (buildpacks) - detect architecture
RUN ARCH=$(uname -m) && \
    if [ "$ARCH" = "aarch64" ] || [ "$ARCH" = "arm64" ]; then \
        PACK_ARCH="arm64"; \
    else \
        PACK_ARCH="amd64"; \
    fi && \
    curl -sSL "https://github.com/buildpacks/pack/releases/download/v0.39.1/pack-v0.39.1-linux-${PACK_ARCH}.tgz" | tar -xz -C /usr/local/bin

# Copy Go binaries
COPY --from=go-builder /out/server /app/server
COPY --from=go-builder /out/scheduler /app/scheduler
COPY --from=go-builder /out/api /app/api

# Copy frontend static export
COPY --from=frontend-builder /build/out /app/out

# Copy Drizzle migration SQL files (与 TS 版共享同一套 migration)
COPY dokploy/apps/dokploy/drizzle /app/drizzle

# Create required directories
RUN mkdir -p /etc/dokploy/{traefik/dynamic/certificates,logs,applications,compose,ssh,monitoring,registry,schedules,volume-backups,volume-backup-lock,patch-repos}

WORKDIR /app

EXPOSE 3000

HEALTHCHECK --interval=10s --timeout=3s --retries=10 \
  CMD curl -fs http://localhost:3000/api/trpc/settings.health || exit 1

CMD ["/app/server"]
