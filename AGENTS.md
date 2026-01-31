# AGENTS.md - AI Agent Instructions for Lemuria

This document provides context and guidelines for AI agents working on the Lemuria codebase.

## Project Overview

Lemuria is a GitHub PR automation tool for Argo CD, inspired by Atlantis (Terraform PR automation). It processes GitHub webhooks, interacts with Argo CD to generate diffs, and posts results as PR comments.

## Technology Stack

- **Language**: Go 1.22+
- **Web Framework**: Chi router (`github.com/go-chi/chi/v5`)
- **GitHub Client**: `github.com/google/go-github/v60` with GitHub App auth via `ghinstallation`
- **Redis Client**: `github.com/redis/go-redis/v9`
- **Diff Library**: `github.com/sergi/go-diff`
- **Config**: YAML with environment variable substitution

## Project Structure

```
lemuria/
├── cmd/lemuria/main.go      # Entry point, CLI flags
├── internal/                 # Private packages
│   ├── argocd/              # Argo CD REST API client
│   │   ├── client.go        # HTTP client setup
│   │   ├── applications.go  # App CRUD operations
│   │   ├── applicationsets.go
│   │   ├── manifests.go     # Manifest fetching
│   │   └── diff.go          # Diff computation
│   ├── commands/            # PR command handling
│   │   ├── parser.go        # Parse "lemuria <cmd>" from comments
│   │   ├── executor.go      # Command orchestration
│   │   ├── plan.go          # Plan command
│   │   ├── sync.go          # Sync command
│   │   ├── unlock.go        # Unlock command
│   │   └── help.go          # Help command
│   ├── config/              # Configuration management
│   │   ├── config.go        # Structs and defaults
│   │   └── loader.go        # YAML loading, env substitution
│   ├── github/              # GitHub API operations
│   │   ├── client.go        # GitHub App authentication
│   │   ├── comments.go      # PR comment CRUD
│   │   └── files.go         # Changed files detection
│   ├── lock/                # Application locking
│   │   ├── manager.go       # Interface definition
│   │   └── redis.go         # Redis implementation
│   ├── models/              # Shared data structures
│   │   ├── application.go   # App, manifest, diff models
│   │   ├── event.go         # Webhook event models
│   │   └── lock.go          # Lock models
│   ├── server/              # HTTP server
│   │   ├── server.go        # Server setup, middleware
│   │   └── routes.go        # Route definitions
│   └── webhook/             # GitHub webhook processing
│       ├── handler.go       # Main webhook handler
│       ├── parser.go        # Event parsing
│       └── validator.go     # HMAC signature validation
├── pkg/diff/                # Public diff utilities
│   ├── renderer.go          # Markdown rendering
│   └── formatter.go         # Output formatting helpers
└── e2e/                     # End-to-end tests
    ├── scripts/             # Setup/teardown scripts
    ├── manifests/           # Test Argo CD apps
    └── *_test.go            # Test files
```

## Key Patterns

### Error Handling

- Wrap errors with context using `fmt.Errorf("operation: %w", err)`
- Return errors up the stack; let the top-level handler log and respond
- Use structured logging with `slog`

### Configuration

- All config supports `${ENV_VAR}` substitution
- Default values in `config.DefaultConfig()`
- Validation in `config.validate()`

### Argo CD API

- REST client in `internal/argocd/client.go`
- All API calls use context for cancellation
- Manifests are returned as JSON strings from the API

### Locking

- Redis-based locks with 7-day TTL
- Lock key format: `lemuria:lock:{app-name}`
- PR index key: `lemuria:pr-locks:{repo}:{pr-number}`
- Locks track: PR number, repo, user, timestamp, plan revision

### Command Parsing

- Commands start with `lemuria ` (case-insensitive)
- Supported: `plan`, `sync`, `unlock`, `help`
- Flags: `-a/--app`, `--all`, `--prune`, `--dry-run`

## Testing

### Unit Tests

```bash
go test ./internal/... ./pkg/...
```

### E2E Tests

Require k3d cluster with Argo CD:

```bash
./e2e/scripts/setup.sh   # Creates cluster
go test -v ./e2e/...     # Run tests
./e2e/scripts/teardown.sh
```

### Test Coverage

- `e2e/e2e_test.go` - Argo CD client, Redis locks, integration workflows
- `e2e/commands_test.go` - Command parsing
- `e2e/webhook_test.go` - Webhook validation, event parsing
- `e2e/diff_test.go` - Diff rendering

## Common Tasks

### Adding a New Command

1. Add command type to `internal/commands/parser.go`
2. Create handler in `internal/commands/<cmd>.go`
3. Add case to `Executor.Execute()` in `executor.go`
4. Add tests to `e2e/commands_test.go`

### Adding API Endpoints

1. Add handler method to `internal/server/routes.go`
2. Register route in `setupRoutes()`
3. Add tests

### Modifying Argo CD Client

1. Check Argo CD API docs for endpoint
2. Add method to appropriate file in `internal/argocd/`
3. Update response structs if needed
4. Add e2e test

## Build Commands

```bash
make build        # Build binary to bin/lemuria
make test         # Run unit tests
make test-e2e     # Run e2e tests (requires setup)
make lint         # Run golangci-lint
make docker-build # Build Docker image
```

## Environment Variables

| Variable | Description |
|----------|-------------|
| `GITHUB_WEBHOOK_SECRET` | GitHub webhook HMAC secret |
| `GITHUB_APP_PRIVATE_KEY` | GitHub App private key (PEM) |
| `ARGOCD_TOKEN` | Argo CD API token |
| `REDIS_PASSWORD` | Redis password (optional) |

## Important Notes

1. **Manifest Format**: Argo CD returns manifests as JSON strings, not YAML
2. **ApplicationSet Detection**: Uses `ownerReferences` field, not just labels
3. **Lock TTL**: 7 days - abandoned PRs auto-cleanup
4. **Webhook Async**: Events are processed asynchronously after 200 response
5. **Multi-source Apps**: Handled via `sourcePositions` parameter in manifest API

## Code Style

- Use `gofmt` and `goimports`
- Prefer explicit error handling over panics
- Use interfaces for testability (see `lock.Manager`)
- Keep handlers thin; business logic in dedicated packages
