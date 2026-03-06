FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /server ./cmd/server

FROM alpine:3.20
RUN apk add --no-cache \
    docker-cli \
    docker-cli-compose \
    curl \
    git \
    openssh-client \
    rsync \
    bash

# Install Nixpacks
RUN curl -sSL https://nixpacks.com/install.sh | bash

# Install Pack CLI (Heroku/Paketo buildpacks)
RUN curl -sSL "https://github.com/buildpacks/pack/releases/download/v0.35.1/pack-v0.35.1-linux-arm64.tgz" | tar -xz -C /usr/local/bin

COPY --from=builder /server /server

# Create required directories
RUN mkdir -p /etc/dokploy/{traefik/dynamic/certificates,logs,applications,compose,ssh,monitoring,registry,schedules,volume-backups,volume-backup-lock,patch-repos}

EXPOSE 3000

CMD ["/server"]
