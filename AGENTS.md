# AGENTS.md - AI Agent Instructions for Lemuria

This document provides context and guidelines for AI agents working on the Lemuria codebase.

## Project Overview

Lemuria is a GitHub PR automation tool for Argo CD, inspired by Atlantis (Terraform PR automation). It processes GitHub webhook events, interacts with Argo CD to generate manifest diffs, and posts results as PR comments. Users can then approve and trigger deployments directly from PRs.

**Core workflow:** GitHub PR event → webhook → detect affected apps → fetch manifests → compute diff → post PR comment → user comments `lemuria sync` → trigger Argo CD sync → release lock.

## Technology Stack

- **Language**: Go 1.25 (module: `github.com/org/lemuria`)
- **Web Framework**: Chi router (`github.com/go-chi/chi/v5`)
- **CLI Framework**: `github.com/urfave/cli/v3`
- **GitHub Client**: `github.com/google/go-github/v60` with GitHub App auth via `ghinstallation/v2`
- **Argo CD**: REST API client for `github.com/argoproj/argo-cd/v3` (v3.3.0)
- **Redis**: `github.com/redis/go-redis/v9` for distributed locks and session storage
- **Diff**: `github.com/pmezard/go-difflib` for manifest comparison
- **Config**: `github.com/knadh/koanf/v2` — YAML parsing with `${ENV_VAR}` substitution
- **Auth**: `github.com/coreos/go-oidc/v3`, `golang.org/x/oauth2`
- **Logging**: Standard library `log/slog` (structured JSON)
- **Frontend**: React 18 + TypeScript + Vite 5 + Tailwind CSS 3 + TanStack React Query 5
- **Testing**: Standard `testing` package + `testcontainers-go` for e2e

## Project Structure

```
lemuria/
├── cmd/lemuria/main.go          # Entry point: CLI flags, logger setup, server bootstrap
├── internal/                     # Private packages (all business logic lives here)
│   ├── argocd/                  # Argo CD REST API client
│   │   ├── client.go            # HTTP client with auth token
│   │   ├── applications.go      # Get/list Application CRs
│   │   ├── applicationsets.go   # Detect ApplicationSet-generated apps via ownerReferences
│   │   ├── manifests.go         # Fetch rendered manifests from Argo CD API
│   │   ├── diff.go              # Compute diffs between source/target manifests
│   │   ├── parser.go            # Parse manifest JSON strings into structured objects
│   │   └── tempapp.go           # Create temporary Application CRs for branch manifest rendering
│   ├── auth/                    # Authentication & authorization
│   │   ├── auth.go              # Provider interface, UserFromRequest() context helper
│   │   ├── github.go            # GitHub OAuth provider (org/team restrictions)
│   │   ├── oidc.go              # Generic OIDC provider (domain restrictions)
│   │   ├── basic.go             # Basic auth provider (dev/testing only)
│   │   ├── rbac.go              # Role resolver: pattern-based role assignments
│   │   ├── session.go           # Redis-backed session store (create, validate, destroy)
│   │   └── middleware.go        # HTTP middleware: Authenticate, RequireAuth, RequireAdmin
│   ├── commands/                # PR command handlers
│   │   ├── parser.go            # Regex-based parser: "lemuria <cmd> [flags]" from comment body
│   │   ├── executor.go          # Orchestrates command execution, delegates to specific handlers
│   │   ├── plan.go              # Plan: detect affected apps → fetch manifests → compute diff → post comment
│   │   ├── sync.go              # Sync: verify lock/approval → trigger Argo CD sync → report → auto-merge
│   │   ├── unlock.go            # Unlock: release locks for a PR
│   │   ├── rollback.go          # Rollback: revert to previous Argo CD deployment history
│   │   ├── help.go              # Help: post available commands as PR comment
│   │   ├── appdetect.go         # Match changed files to configured application paths
│   │   └── github_iface.go      # GitHub client interface (for testability)
│   ├── config/                  # Configuration management
│   │   ├── config.go            # All config structs (Config, ServerConfig, GitHubConfig, etc.)
│   │   │                        # DefaultConfig() returns sensible defaults
│   │   │                        # RepoConfig for per-repo .lemuria.yaml
│   │   └── loader.go            # Load YAML files with koanf, merge multiple files, env var substitution
│   ├── github/                  # GitHub API operations
│   │   ├── client.go            # GitHub App auth via ghinstallation, per-installation client creation
│   │   ├── comments.go          # Create, update, delete, find PR comments
│   │   └── files.go             # List changed files in a PR
│   ├── lock/                    # Distributed application locking
│   │   ├── manager.go           # Interface: Lock, Unlock, ForceUnlock, GetLock, ListAll, ListByPR, Ping
│   │   └── redis.go             # Redis implementation
│   │                            # Lock key: "lemuria:lock:{app-name}"
│   │                            # PR index: "lemuria:pr-locks:{repo}:{pr-number}"
│   │                            # TTL: 7 days (auto-cleanup for abandoned PRs)
│   │                            # Stores: PR number, repo, user, timestamp, plan revision
│   ├── models/                  # Shared data structures
│   │   ├── application.go       # Application, ManifestDiff, DiffResult, ResourceAction
│   │   ├── event.go             # WebhookEvent, PREvent, CommentEvent, ReviewEvent
│   │   ├── lock.go              # LockInfo, LockState
│   │   └── user.go              # User, Session, AuthProvider, Role
│   ├── server/                  # HTTP server
│   │   ├── server.go            # Server struct, New() wires all dependencies, Run() starts listener
│   │   ├── routes.go            # All route definitions + handler implementations
│   │   │                        # Public: /webhook, /health, /healthz, /ready
│   │   │                        # Auth: /auth/{providers,github/*,oidc/*,basic/*,logout,me}
│   │   │                        # API: /api/v1/{status,locks,users} (auth-gated)
│   │   │                        # Admin: DELETE /api/v1/locks/{app}, GET/PUT /api/v1/users
│   │   └── static.go            # Serve embedded frontend assets from static/ directory
│   └── webhook/                 # GitHub webhook processing
│       ├── handler.go           # Validates → parses → processes asynchronously (goroutine)
│       │                        # Returns 200 immediately, processes in background
│       ├── parser.go            # Parse GitHub events: pull_request, issue_comment, pull_request_review
│       └── validator.go         # HMAC-SHA256 signature validation (X-Hub-Signature-256)
├── pkg/diff/                    # Public diff utilities
│   ├── renderer.go              # Convert DiffResult → markdown for PR comments
│   │                            # Handles: create/update/delete actions, new/deleted apps, errors
│   └── formatter.go             # Output formatting helpers (line truncation, etc.)
├── web/                         # React + TypeScript frontend (Vite + Tailwind)
│   └── src/
│       ├── components/
│       │   ├── Auth/            # AuthProvider (context), LoginPage, ProtectedRoute
│       │   ├── Layout/          # Header, Layout wrapper
│       │   ├── Locks/           # LockCard, LockList
│       │   └── Admin/           # UserManagement
│       ├── hooks/               # useAuth (context consumer), useLocks (React Query)
│       ├── pages/               # Dashboard, LocksPage, AdminPage
│       ├── services/api.ts      # HTTP client for backend API
│       └── types/               # TypeScript types (auth, locks)
├── charts/lemuria/              # Helm chart (v0.1.0)
│   ├── Chart.yaml               # Depends on bitnami/redis (optional, condition: redis.enabled)
│   ├── values.yaml              # Defaults: 1 replica, ClusterIP:80, 500m CPU, 256Mi memory
│   ├── templates/               # Deployment, Service, ConfigMap, Secret, Ingress, RBAC
│   └── ci/                      # CI test values
├── e2e/                         # End-to-end tests
│   ├── scripts/setup.sh         # Creates k3d cluster, installs Argo CD, deploys Redis
│   ├── scripts/teardown.sh      # Cleanup
│   ├── manifests/               # Test Application and ApplicationSet manifests
│   ├── e2e_test.go              # Core integration tests
│   ├── commands_test.go         # Command parsing & execution
│   ├── command_workflow_test.go # Full workflow scenarios (plan → sync)
│   ├── webhook_test.go          # Webhook validation & parsing
│   ├── diff_test.go             # Diff rendering
│   ├── appdetect_test.go        # Application detection
│   ├── approval_test.go         # Approval enforcement
│   ├── automerge_test.go        # Auto-merge logic
│   ├── rollback_test.go         # Rollback functionality
│   ├── helpers_test.go          # Test utilities
│   └── mock_github_test.go      # GitHub API mock server
├── config/test.yaml             # Test/dev configuration
├── docs/                        # Jekyll documentation site
├── static/                      # Embedded frontend build output (generated by `make build-frontend`)
├── Makefile                     # Build automation (see Build Commands below)
├── Dockerfile                   # Multi-stage: node:25 → golang:1.25 → alpine:3.19
└── .github/workflows/ci.yaml   # CI/CD: lint, test, build, publish, deploy, release
```

## Key Architectural Patterns

### Request Processing Flow

```
GitHub Webhook POST
  → validator.go: HMAC-SHA256 signature check
  → parser.go: parse event type (pull_request | issue_comment | pull_request_review)
  → handler.go: return 200 immediately, spawn goroutine for async processing
  → executor.go: route to appropriate command handler
  → plan.go / sync.go / unlock.go / rollback.go: execute command
  → github/comments.go: post result as PR comment
```

### Error Handling

- Wrap errors with context: `fmt.Errorf("operation: %w", err)`
- Return errors up the stack; top-level handler logs and responds
- Use structured logging: `slog.Error("message", "key", value, "error", err)`

### Configuration

- YAML files loaded via koanf with `${ENV_VAR}` substitution
- Multiple config files merged in order (`-config a.yaml -config b.yaml`)
- Default values in `config.DefaultConfig()`
- Per-repo overrides via `.lemuria.yaml` in the repository root

### Argo CD Integration

- REST client in `internal/argocd/client.go` (not gRPC)
- Manifests are returned as JSON strings from the Argo CD API, parsed in `parser.go`
- Temporary Application CRs are created for branch manifest rendering (`tempapp.go`)
- ApplicationSet detection uses `ownerReferences` field on Application CRs
- Multi-source apps handled via `sourcePositions` parameter
- Diff modes: `"branch"` (compare PR branch vs target branch) or `"live"` (compare PR branch vs live cluster)

### Locking

- Redis-based distributed locks with 7-day TTL
- Lock key: `lemuria:lock:{app-name}`
- PR index key: `lemuria:pr-locks:{repo}:{pr-number}`
- Lock stores: PR number, repo, user, timestamp, plan revision (commit SHA)
- A lock is acquired during `plan`, verified during `sync`, released on `unlock` or PR close/merge
- Plan revision tracking: sync verifies the lock's plan revision matches the current PR head

### Command Parsing

- Regex-based: `(?i)^\s*lemuria\s+(\w+)(.*)$`
- Case-insensitive, supports multi-line comments (finds first matching line)
- Commands: `plan`, `sync`, `unlock`, `rollback`, `help`
- Flags: `-a`/`--app`/`--application`, `--all`, `--prune`, `--dry-run`, `--id`
- Bare words (non-flag arguments) treated as application name

### Authentication & Authorization

- Three providers: GitHub OAuth, OIDC, Basic Auth
- Redis-backed session store with configurable TTL (default 24h)
- Cookie-based sessions with CSRF protection
- Two roles: `admin` and `user` (configurable default)
- Pattern-based role assignments in config
- Middleware chain: `Authenticate` → `RequireAuth` → `RequireAdmin` (for admin routes)

### Frontend

- React SPA built with Vite, embedded into the Go binary at build time
- Static files served from `/static/` directory via `server/static.go`
- API communication via `services/api.ts`
- TanStack React Query for server state management
- Tailwind CSS for styling

## Testing

### Unit Tests

```bash
make test             # or: go test -v -race -coverprofile=coverage.out ./internal/... ./pkg/...
```

Unit test files:
- `internal/argocd/diff_test.go` — Diff computation
- `internal/argocd/parser_test.go` — Manifest JSON parsing
- `internal/argocd/tempapp_test.go` — Temporary app CR creation
- `internal/auth/oidc_test.go` — OIDC provider
- `internal/commands/sync_test.go` — Sync command logic

### E2E Tests

Require k3d cluster with Argo CD and Redis:

```bash
make e2e-setup        # Create cluster + install dependencies
make test-e2e         # Run e2e tests (10m timeout)
make e2e-teardown     # Cleanup
make test-e2e-short   # Run in short/unit mode (no infrastructure needed)
```

E2E test coverage:
- `e2e_test.go` — Core integration (Argo CD client, Redis locks)
- `commands_test.go` — Command parsing
- `command_workflow_test.go` — Full plan → sync workflows
- `webhook_test.go` — Webhook validation & event parsing
- `diff_test.go` — Diff rendering
- `appdetect_test.go` — Application detection from changed files
- `approval_test.go` — Approval enforcement
- `automerge_test.go` — Auto-merge after sync
- `rollback_test.go` — Rollback functionality
- `mock_github_test.go` — Mock GitHub API server for testing

## Common Tasks

### Adding a New Command

1. Add command type constant in `internal/commands/parser.go` (e.g., `CommandMyCmd`)
2. Add the case to `parseLine()` switch in `parser.go`
3. Create handler file `internal/commands/mycmd.go` with execution logic
4. Add case to `Executor.Execute()` in `executor.go`
5. Update help text in `internal/commands/help.go`
6. Add tests to `e2e/commands_test.go`

### Adding API Endpoints

1. Add handler method to `internal/server/routes.go`
2. Register route in `setupRoutes()` (consider auth requirements: public, authenticated, or admin-only)
3. Add TypeScript types in `web/src/types/` if the frontend consumes the endpoint
4. Add frontend API method in `web/src/services/api.ts`

### Modifying Argo CD Client

1. Add method to appropriate file in `internal/argocd/` (applications, manifests, diff, etc.)
2. Update response structs if needed (Argo CD returns JSON)
3. Use `context.Context` for all API calls
4. Add unit tests and/or e2e tests

### Adding a New Auth Provider

1. Implement the provider interface in `internal/auth/`
2. Add config struct in `internal/config/config.go`
3. Wire up in `internal/server/server.go` (provider initialization)
4. Add routes in `internal/server/routes.go` (login + callback)
5. Update frontend login page in `web/src/components/Auth/LoginPage.tsx`

### Modifying Configuration

1. Update structs in `internal/config/config.go`
2. Update defaults in `DefaultConfig()` if applicable
3. Update `internal/config/loader.go` if new loading behavior is needed
4. Update Helm chart `values.yaml` and `templates/configmap.yaml` if the config is user-facing

## Build Commands

```bash
# Development
make run              # Start Redis + backend + frontend dev server
make run-redis        # Start Redis in Docker
make run-backend      # Build and run backend (uses CONFIG=config/test.yaml)
make run-frontend     # Start Vite dev server
make stop             # Stop all dev services

# Build
make build            # Build frontend + backend → bin/lemuria
make build-go         # Build backend only (skip frontend, faster iteration)
make docker-build     # Build Docker image with version/commit tags

# Test
make test             # Unit tests with race detection + coverage
make test-e2e         # E2E tests (requires e2e-setup)
make test-e2e-short   # E2E tests in unit mode (no infrastructure)
make e2e-setup        # Create k3d cluster + Argo CD + Redis
make e2e-teardown     # Delete k3d cluster

# Code Quality
make lint             # Run golangci-lint
make fmt              # Format with gofmt + goimports
make fmt-check        # Check formatting (CI mode)

# Helm
make helm-lint        # Lint Helm chart
make helm-template    # Template chart (dry-run)
make helm-test        # Run Helm unit tests
make helm-deploy      # Deploy via Helm to current kubectl context
make helm-dep         # Build Helm dependencies

# Tools
make tools            # Install golangci-lint, goimports, helm-unittest
make deps             # Download and tidy Go dependencies
make clean            # Remove build artifacts
```

## Environment Variables

| Variable | Description | Used In |
|----------|-------------|---------|
| `GITHUB_WEBHOOK_SECRET` | GitHub webhook HMAC secret | `github.webhook_secret` |
| `GITHUB_APP_PRIVATE_KEY` | GitHub App private key (PEM) | `github.app_private_key` |
| `ARGOCD_TOKEN` | Argo CD API token | `argocd.token` |
| `REDIS_PASSWORD` | Redis password | `redis.password` |
| `SESSION_SECRET` | Session encryption secret | `auth.session_secret` |
| `GITHUB_OAUTH_CLIENT_ID` | GitHub OAuth client ID | `auth.github.client_id` |
| `GITHUB_OAUTH_CLIENT_SECRET` | GitHub OAuth client secret | `auth.github.client_secret` |
| `OIDC_CLIENT_ID` | OIDC client ID | `auth.oidc.client_id` |
| `OIDC_CLIENT_SECRET` | OIDC client secret | `auth.oidc.client_secret` |

## Important Implementation Details

1. **Manifest format**: Argo CD returns manifests as JSON strings, not YAML. The `argocd/parser.go` handles parsing.
2. **ApplicationSet detection**: Uses `ownerReferences` field on Application CRs, not labels.
3. **Lock TTL**: 7 days — abandoned PRs auto-cleanup. Defined in `config.LockTTL()`.
4. **Async webhook processing**: Events are processed in goroutines after returning 200 to GitHub. This prevents webhook timeouts.
5. **Multi-source apps**: Handled via `sourcePositions` parameter in the Argo CD manifest API.
6. **Plan staleness**: Sync verifies the lock's stored plan revision matches the PR's current HEAD commit. If files changed since the last plan, sync is rejected and a re-plan is required.
7. **Temp app pattern**: For branch diffs, Lemuria creates a temporary Application CR pointing to the PR branch, fetches its rendered manifests, then deletes the temp app. Timeout is configurable (`argocd.temp_app_timeout`, default 2m).
8. **Frontend embedding**: The frontend is built to `static/` by `make build-frontend`, then embedded into the Go binary. In dev mode, the Vite dev server runs separately on port 5173.
9. **Config merging**: Multiple `-config` flags are supported. Files are merged in order (later files override earlier ones). This allows base + environment-specific configs.
10. **Auto-merge**: When enabled, Lemuria merges the PR after a successful sync using the configured `merge_method` (default: `squash`).

## Code Style

- Format with `gofmt` and `goimports` (`make fmt`)
- Lint with `golangci-lint` (`make lint`)
- Prefer explicit error handling; never use `panic` for expected errors
- Use interfaces for testability (see `lock.Manager`, `commands.github_iface.go`)
- Keep HTTP handlers thin; business logic belongs in `commands/` or dedicated packages
- Use `slog` for all logging with structured key-value pairs
- Context propagation: pass `context.Context` through all layers
