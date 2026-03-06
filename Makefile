.PHONY: build run test clean dev

# Build the main server binary
build:
	go build -o bin/server ./cmd/server

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
	docker build -t dokploy-go:latest .

# Format code
fmt:
	go fmt ./...

# Lint code
lint:
	golangci-lint run ./...
