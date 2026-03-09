.PHONY: build run test clean dev docker-build docker-push

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "v0.0.0-dev")
REGISTRY ?= dokploy-go
LDFLAGS := -X github.com/dokploy/dokploy/internal/updater.Version=$(VERSION)

# Build the main server binary
build:
	go build -ldflags="$(LDFLAGS)" -o bin/server ./cmd/server

# Run the server in development mode
dev:
	GO_ENV=development go run ./cmd/server

# Run the server in production mode
run:
	GO_ENV=production go run ./cmd/server

# Run tests
test:
	go test ./... -v

# Clean build artifacts
clean:
	rm -rf bin/

# Install dependencies
deps:
	go mod tidy

# Build Docker image (local)
docker-build:
	docker build --build-arg VERSION=$(VERSION) -t $(REGISTRY):$(VERSION) .

# Build and push multi-arch Docker image
# Usage: make docker-push VERSION=28.0.5 REGISTRY=crpi-xxx.cn-shanghai.personal.cr.aliyuncs.com/tigerking/dokploy-go
docker-push:
	docker buildx build --platform linux/amd64,linux/arm64 \
		--build-arg VERSION=$(VERSION) \
		-t $(REGISTRY):$(VERSION) \
		-f Dockerfile . --push

# Format code
fmt:
	go fmt ./...

# Lint code
lint:
	golangci-lint run ./...
