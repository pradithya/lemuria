# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Quick Reference Commands

```bash
# Build
make build            # Full build (frontend + backend) → bin/lemuria
make build-go         # Backend only (faster iteration)

# Test
make test             # Unit tests (./internal/... ./pkg/...) with race detection
make test-e2e         # E2E tests (requires Docker + k3d, see AGENTS.md)

# Run a single test
go test -v -run "TestName" ./internal/commands/...

# Code quality
make lint             # golangci-lint
make fmt              # gofmt + goimports
make fmt-check        # Check formatting without modifying

# Dev server
make run              # Redis + backend + frontend dev server
```

## Architecture Overview

Lemuria is a PR/MR automation tool for Argo CD (like Atlantis for Terraform). It processes VCS webhook events, computes Argo CD manifest diffs, posts results as PR comments, and triggers syncs on user command.

**Core flow:** VCS webhook → validate signature → parse event → async goroutine → command executor → ArgoCD interaction → post PR/MR comment

**Two runtime modes:** `lemuria -config config.yaml` (server) or `lemuria --mode worker -config config.yaml` (async job processor)

### Key Packages

- `internal/commands/` — Command handlers (plan, sync, unlock, rollback). The `Executor` orchestrates; `VCSClient` interface (`vcs_iface.go`) abstracts GitHub/GitLab.
- `internal/argocd/` — REST API client. Manifests are JSON strings. Temp Application CRs used for branch diff rendering.
- `internal/webhook/` — GitHub (HMAC-SHA256) and GitLab (token compare) webhook handlers. Returns 200 immediately, processes async.
- `internal/config/` — koanf-based YAML config with `${ENV_VAR}` substitution. Multiple files merge in order.
- `internal/lock/` — Redis-based distributed locks with 7-day TTL. Plan revision tracking for sync staleness.
- `internal/models/` — Domain types (`Application`, `WebhookEvent`, `LockInfo`, `User`). `models.Application` is the flattened Lemuria model vs `v1alpha1.Application` (ArgoCD CRD).
- `pkg/diff/` — Diff result → markdown rendering for PR comments.

### Domain Model

- `models.Application` (Lemuria) ↔ `v1alpha1.Application` (ArgoCD CRD) — converted via `convertV1alpha1Application()` in `argocd/applications.go`
- ApplicationSet detection uses `ownerReferences`, not labels
- `v1alpha1.Application` embeds `metav1.ObjectMeta` (fields like `.Name`, `.Labels` are direct)

## Important Conventions

- Go 1.25+ required (argo-cd/v3 dependency)
- Use `sigs.k8s.io/yaml` for YAML↔struct (handles json tags), `gopkg.in/yaml.v3` only for multi-doc splitting
- Logging: `log/slog` with structured key-value pairs
- Error wrapping: `fmt.Errorf("context: %w", err)`
- Interfaces for testability: `lock.Manager`, `commands.VCSClient`
- Table-driven tests throughout
- E2E tests require Docker (testcontainers) and k3d cluster — see `AGENTS.md` for setup

## Detailed Reference

See `AGENTS.md` for comprehensive details including: project structure, adding new commands/endpoints/auth providers, environment variables, e2e test setup/troubleshooting, and code style guidelines.
