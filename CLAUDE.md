# CLAUDE.md

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

## Verification

**IMPORTANT** For every code change ensure that:
1. A new test case is added to verify the behavior, optionally add end to end test case.
2. Run `make test` to run unit test.
3. Run `make lint` to perform static code analysis.
4. Run `make fmt` to fix formatting.
5. Run `make test-e2e` to run complete end to end test. 
6. Run `go test -v -run "TestName" ./internal/commands/...` to run single test.

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
