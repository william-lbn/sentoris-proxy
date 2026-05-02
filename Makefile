PROJECT_NAME := sentoris-proxy
VERSION := $(shell git describe --tags --always --dirty)
BUILD_TIME := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
COMMIT_HASH := $(shell git rev-parse HEAD)

GOFLAGS := -ldflags "-X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME) -X main.CommitHash=$(COMMIT_HASH)"

.PHONY: all build test lint clean run docker-build docker-run release help

all: build

build:
	go build $(GOFLAGS) -o $(PROJECT_NAME) ./cmd/sentoris-proxy

build-linux-amd64:
	GOOS=linux GOARCH=amd64 go build $(GOFLAGS) -o $(PROJECT_NAME)-linux-amd64 ./cmd/sentoris-proxy

build-linux-arm64:
	GOOS=linux GOARCH=arm64 go build $(GOFLAGS) -o $(PROJECT_NAME)-linux-arm64 ./cmd/sentoris-proxy

build-darwin-amd64:
	GOOS=darwin GOARCH=amd64 go build $(GOFLAGS) -o $(PROJECT_NAME)-darwin-amd64 ./cmd/sentoris-proxy

build-darwin-arm64:
	GOOS=darwin GOARCH=arm64 go build $(GOFLAGS) -o $(PROJECT_NAME)-darwin-arm64 ./cmd/sentoris-proxy

build-all: build-linux-amd64 build-linux-arm64 build-darwin-amd64 build-darwin-arm64

test:
	go test -v ./...

test-coverage:
	go test -v -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out

lint:
	golangci-lint run

clean:
	rm -f $(PROJECT_NAME) $(PROJECT_NAME)-* coverage.out

run: build
	./$(PROJECT_NAME)

docker-build:
	docker build -t $(PROJECT_NAME):$(VERSION) .
	docker tag $(PROJECT_NAME):$(VERSION) $(PROJECT_NAME):latest

docker-run: docker-build
	docker-compose up -d

docker-stop:
	docker-compose down

migrate-up:
	go run ./cmd/sentoris-proxy migrate up

migrate-down:
	go run ./cmd/sentoris-proxy migrate down

release: build-all
	@echo "Release binaries created:"
	@ls -la $(PROJECT_NAME)-*

help:
	@echo "Available targets:"
	@echo "  build              - Build the project"
	@echo "  build-linux-amd64  - Build for Linux amd64"
	@echo "  build-linux-arm64  - Build for Linux arm64"
	@echo "  build-darwin-amd64 - Build for macOS amd64"
	@echo "  build-darwin-arm64 - Build for macOS arm64"
	@echo "  build-all          - Build all targets"
	@echo "  test               - Run tests"
	@echo "  test-coverage      - Run tests with coverage"
	@echo "  lint               - Run linter"
	@echo "  clean              - Clean build artifacts"
	@echo "  run                - Build and run the project"
	@echo "  docker-build       - Build Docker image"
	@echo "  docker-run         - Run with Docker Compose"
	@echo "  docker-stop        - Stop Docker Compose"
	@echo "  migrate-up         - Run database migrations"
	@echo "  migrate-down       - Rollback database migrations"
	@echo "  release            - Build all release binaries"
	@echo "  help               - Show this help message"