.PHONY: build test test-unit test-e2e e2e-setup e2e-teardown clean docker-build

# Build variables
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
LDFLAGS := -ldflags "-w -s -X main.version=$(VERSION) -X main.commit=$(COMMIT)"

# Go variables
GOBIN ?= $(shell go env GOBIN)
ifeq ($(GOBIN),)
GOBIN := $(shell go env GOPATH)/bin
endif

# Default target
all: build

# Build the binary
build:
	go build $(LDFLAGS) -o bin/lemuria ./cmd/lemuria

# Run all tests
test: test-unit

# Run unit tests
test-unit:
	go test -v -race -coverprofile=coverage.out ./internal/... ./pkg/...

# Run e2e tests (requires setup first)
test-e2e:
	cd e2e && go test -v -timeout 10m ./...

# Setup e2e test infrastructure
e2e-setup:
	./e2e/scripts/setup.sh

# Teardown e2e test infrastructure
e2e-teardown:
	./e2e/scripts/teardown.sh

# Run e2e tests with auto setup/teardown
e2e: e2e-setup test-e2e

# Clean build artifacts
clean:
	rm -rf bin/
	rm -f coverage.out

# Build Docker image
docker-build:
	docker build --build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) -t lemuria:$(VERSION) .

# Install dependencies
deps:
	go mod download
	go mod tidy

# Lint code
lint:
	golangci-lint run ./...

# Format code
fmt:
	go fmt ./...
	goimports -w .

# Generate mocks (if needed in future)
generate:
	go generate ./...

# Show help
help:
	@echo "Lemuria Makefile"
	@echo ""
	@echo "Usage:"
	@echo "  make build        - Build the lemuria binary"
	@echo "  make test         - Run unit tests"
	@echo "  make test-e2e     - Run e2e tests (requires e2e-setup)"
	@echo "  make e2e-setup    - Setup k3d cluster with Argo CD and Redis"
	@echo "  make e2e-teardown - Teardown e2e test infrastructure"
	@echo "  make e2e          - Setup, run e2e tests (no auto-teardown)"
	@echo "  make docker-build - Build Docker image"
	@echo "  make clean        - Clean build artifacts"
	@echo "  make deps         - Download and tidy dependencies"
	@echo "  make lint         - Run linter"
	@echo "  make fmt          - Format code"
