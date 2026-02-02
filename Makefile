.PHONY: build build-frontend build-backend test test-unit test-e2e e2e-setup e2e-teardown clean docker-build run run-redis run-backend run-frontend stop \
        k8s-deploy k8s-delete k8s-status k8s-logs k8s-port-forward k8s-restart \
        k3d-create k3d-delete k3d-build k3d-deploy k3d-all help

# Build variables
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
LDFLAGS := -ldflags "-w -s -X main.version=$(VERSION) -X main.commit=$(COMMIT)"

# Go variables
GOBIN ?= $(shell go env GOBIN)
ifeq ($(GOBIN),)
GOBIN := $(shell go env GOPATH)/bin
endif

# Node/npm variables
NPM ?= npm

# Default target
all: build

# Build frontend and backend
build: build-frontend build-backend

# Build frontend assets
build-frontend:
	cd web && $(NPM) install && $(NPM) run build

# Build the Go binary (requires frontend to be built first)
build-backend:
	go build $(LDFLAGS) -o bin/lemuria ./cmd/lemuria

# Build backend only (skip frontend, for development)
build-go:
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

# ============================================================================
# Development Commands
# ============================================================================

# Configuration
CONFIG ?= config/test.yaml

# Start Redis in Docker (if not already running)
run-redis:
	@if [ -z "$$(docker ps -q -f name=lemuria-redis)" ]; then \
		echo "Starting Redis..."; \
		docker run -d --name lemuria-redis -p 6379:6379 redis:7-alpine; \
		sleep 2; \
	else \
		echo "Redis is already running"; \
	fi

# Stop Redis container
stop-redis:
	@if [ -n "$$(docker ps -q -f name=lemuria-redis)" ]; then \
		echo "Stopping Redis..."; \
		docker stop lemuria-redis; \
	fi
	@if [ -n "$$(docker ps -aq -f name=lemuria-redis)" ]; then \
		docker rm lemuria-redis; \
	fi

# Run the backend server
run-backend: build-go
	./bin/lemuria -config $(CONFIG)

# Run the frontend dev server
run-frontend:
	cd web && $(NPM) install && $(NPM) run dev

# Run everything (Redis + backend + frontend in parallel)
# Use: make run
# Or with custom config: make run CONFIG=path/to/config.yaml
run: run-redis
	@echo "Starting Lemuria development environment..."
	@echo "Backend will be available at http://localhost:4141"
	@echo "Frontend dev server will be available at http://localhost:5173"
	@echo ""
	@echo "Press Ctrl+C to stop all services"
	@trap 'kill 0' INT; \
		(cd web && $(NPM) install --silent && $(NPM) run dev) & \
		($(MAKE) build-go && ./bin/lemuria -config $(CONFIG)) & \
		wait

# Stop all development services
stop: stop-redis
	@echo "Stopping Lemuria processes..."
	@pkill -f "bin/lemuria" 2>/dev/null || true
	@pkill -f "vite" 2>/dev/null || true
	@echo "All services stopped"

# ============================================================================
# Kubernetes Deployment Commands
# ============================================================================

# Deploy to Kubernetes using Kustomize
k8s-deploy:
	kubectl apply -k deploy/kubernetes

# Delete Kubernetes deployment
k8s-delete:
	kubectl delete -k deploy/kubernetes --ignore-not-found

# Show Kubernetes status
k8s-status:
	@echo "=== Pods ==="
	kubectl get pods -n lemuria
	@echo ""
	@echo "=== Services ==="
	kubectl get svc -n lemuria
	@echo ""
	@echo "=== Deployments ==="
	kubectl get deployments -n lemuria

# View logs
k8s-logs:
	kubectl logs -n lemuria -l app.kubernetes.io/name=lemuria -f

# Port forward for local access
k8s-port-forward:
	kubectl port-forward -n lemuria svc/lemuria 4141:80

# Restart deployment
k8s-restart:
	kubectl rollout restart deployment/lemuria -n lemuria

# ============================================================================
# Local K3d Development Cluster
# ============================================================================

K3D_CLUSTER_NAME ?= lemuria-dev

# Create local k3d cluster
k3d-create:
	k3d cluster create $(K3D_CLUSTER_NAME) \
		--servers 1 \
		--agents 0 \
		--port "4141:80@loadbalancer" \
		--k3s-arg "--disable=traefik@server:0"

# Delete local k3d cluster
k3d-delete:
	k3d cluster delete $(K3D_CLUSTER_NAME)

# Build and import image to k3d
k3d-build:
	docker build -t lemuria:dev .
	k3d image import lemuria:dev -c $(K3D_CLUSTER_NAME)

# Deploy to k3d cluster
k3d-deploy: k3d-build
	cd deploy/kubernetes && kustomize edit set image lemuria=lemuria:dev
	kubectl apply -k deploy/kubernetes
	kubectl rollout status deployment/lemuria -n lemuria --timeout=120s

# Full k3d setup (create + deploy)
k3d-all: k3d-create k3d-deploy k8s-status

# Show help
help:
	@echo "Lemuria Makefile"
	@echo ""
	@echo "Usage:"
	@echo ""
	@echo "Development:"
	@echo "  make run            - Run Redis, backend, and frontend dev server"
	@echo "  make run-redis      - Start Redis in Docker"
	@echo "  make run-backend    - Run backend server only"
	@echo "  make run-frontend   - Run frontend dev server only"
	@echo "  make stop           - Stop all development services"
	@echo ""
	@echo "Build:"
	@echo "  make build          - Build frontend and backend"
	@echo "  make build-frontend - Build frontend assets only"
	@echo "  make build-backend  - Build Go binary (requires frontend)"
	@echo "  make build-go       - Build Go binary only (skip frontend)"
	@echo "  make docker-build   - Build Docker image"
	@echo ""
	@echo "Testing:"
	@echo "  make test           - Run unit tests"
	@echo "  make test-e2e       - Run e2e tests (requires e2e-setup)"
	@echo "  make e2e-setup      - Setup k3d cluster with Argo CD and Redis"
	@echo "  make e2e-teardown   - Teardown e2e test infrastructure"
	@echo "  make e2e            - Setup, run e2e tests (no auto-teardown)"
	@echo ""
	@echo "Kubernetes:"
	@echo "  make k8s-deploy     - Deploy to Kubernetes using Kustomize"
	@echo "  make k8s-delete     - Delete Kubernetes deployment"
	@echo "  make k8s-status     - Show deployment status"
	@echo "  make k8s-logs       - View Lemuria logs"
	@echo "  make k8s-port-forward - Port forward to localhost:4141"
	@echo "  make k8s-restart    - Restart Lemuria deployment"
	@echo ""
	@echo "Local K3d Cluster:"
	@echo "  make k3d-create     - Create local k3d cluster"
	@echo "  make k3d-delete     - Delete local k3d cluster"
	@echo "  make k3d-build      - Build and import image to k3d"
	@echo "  make k3d-deploy     - Deploy to k3d cluster"
	@echo "  make k3d-all        - Full setup (create + deploy)"
	@echo ""
	@echo "Other:"
	@echo "  make clean          - Clean build artifacts"
	@echo "  make deps           - Download and tidy dependencies"
	@echo "  make lint           - Run linter"
	@echo "  make fmt            - Format code"
	@echo ""
	@echo "Options:"
	@echo "  CONFIG=path/to/config.yaml  - Use custom config (default: config/test.yaml)"
