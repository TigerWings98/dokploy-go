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
ARG VERSION=v0.0.0-dev
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    LDFLAGS="-s -w -X github.com/dokploy/dokploy/internal/updater.Version=${VERSION}" && \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="$LDFLAGS" -o /out/server ./cmd/server && \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="$LDFLAGS" -o /out/scheduler ./cmd/scheduler && \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="$LDFLAGS" -o /out/api ./cmd/api

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
# Stage 3: Final runtime image (Debian slim，与 TS 版对齐)
# ============================================================
FROM debian:bookworm-slim

RUN sed -i 's|deb.debian.org|mirrors.aliyun.com|g' /etc/apt/sources.list.d/debian.sources && \
    apt-get update && apt-get install -y --no-install-recommends \
    curl \
    git \
    git-lfs \
    openssh-client \
    rsync \
    bash \
    apache2-utils \
    iproute2 \
    zip \
    unzip \
    ca-certificates \
    && git lfs install \
    && rm -rf /var/lib/apt/lists/*

# Install Docker CLI (与 TS 版一致，使用官方脚本)
RUN curl -fsSL https://get.docker.com -o get-docker.sh && \
    sh get-docker.sh --version 28.5.2 && \
    rm get-docker.sh

# Install rclone
RUN curl https://rclone.org/install.sh | bash

# Install Nixpacks
ARG TARGETARCH
ARG NIXPACKS_VERSION=v1.41.0
RUN if [ "$TARGETARCH" = "arm64" ]; then NARCH="aarch64"; else NARCH="x86_64"; fi && \
    curl -sSL "https://github.com/railwayapp/nixpacks/releases/download/${NIXPACKS_VERSION}/nixpacks-${NIXPACKS_VERSION}-${NARCH}-unknown-linux-musl.tar.gz" \
    | tar -xz -C /usr/local/bin

# Install Railpack
ARG RAILPACK_VERSION=v0.17.2
RUN if [ "$TARGETARCH" = "arm64" ]; then RARCH="arm64"; else RARCH="x86_64"; fi && \
    curl -sSL "https://github.com/railwayapp/railpack/releases/download/${RAILPACK_VERSION}/railpack-${RAILPACK_VERSION}-${RARCH}-unknown-linux-musl.tar.gz" \
    | tar -xz -C /usr/local/bin

# Install Pack CLI (multi-arch: COPY from official image, same as upstream Dokploy)
COPY --from=buildpacksio/pack:0.39.1 /usr/local/bin/pack /usr/local/bin/pack

# Copy Go binaries
COPY --from=go-builder /out/server /app/server
COPY --from=go-builder /out/scheduler /app/scheduler
COPY --from=go-builder /out/api /app/api

# Copy frontend static export
COPY --from=frontend-builder /build/out /app/out

# Copy Drizzle migration SQL files (与 TS 版共享同一套 migration)
COPY drizzle /app/drizzle

# Create required directories
RUN mkdir -p /etc/dokploy/{traefik/dynamic/certificates,logs,applications,compose,ssh,monitoring,registry,schedules,volume-backups,volume-backup-lock,patch-repos}

WORKDIR /app

EXPOSE 3000

HEALTHCHECK --interval=10s --timeout=3s --retries=10 \
  CMD curl -fs http://localhost:3000/api/trpc/settings.health || exit 1

CMD ["/app/server"]
