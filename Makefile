.PHONY: build run test clean dev

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "v0.0.0-dev")
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

# Build Docker image
docker-build:
	docker build --build-arg VERSION=$(VERSION) -t dokploy-go:$(VERSION) .

# Format code
fmt:
	go fmt ./...

# Lint code
lint:
	golangci-lint run ./...
