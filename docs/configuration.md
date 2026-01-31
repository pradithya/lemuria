---
layout: default
title: Configuration
nav_order: 3
---

# Configuration

Lemuria uses YAML configuration files with support for environment variable substitution.

---

## Configuration Files

| File | Location | Purpose |
|------|----------|---------|
| `lemuria.yaml` | Server | Main server configuration |
| `.lemuria.yaml` | Repository root | Per-repository settings |

---

## Server Configuration

### Full Example

```yaml
server:
  port: 4141
  host: "0.0.0.0"
  base_url: "https://lemuria.example.com"

github:
  webhook_secret: "${GITHUB_WEBHOOK_SECRET}"
  app_id: 123456
  app_private_key: "/app/secrets/github-app.pem"

argocd:
  server_url: "https://argocd.example.com"
  token: "${ARGOCD_TOKEN}"
  insecure: false

redis:
  address: "redis:6379"
  password: "${REDIS_PASSWORD}"
  db: 0

defaults:
  autoplan: true
  require_approval: false
  delete_source_branch: false
  auto_merge: false
  merge_method: "squash"
  allowed_repos:
    - "myorg/*"

auth:
  enabled: true
  session_secret: "${SESSION_SECRET}"
  session_ttl: 24h
  cookie_secure: true
  default_role: "user"
  github:
    client_id: "${GITHUB_OAUTH_CLIENT_ID}"
    client_secret: "${GITHUB_OAUTH_CLIENT_SECRET}"
    allowed_orgs:
      - "myorg"
  role_assignments:
    - pattern: "*@platform.example.com"
      role: "admin"
```

---

## Server Section

```yaml
server:
  port: 4141              # HTTP port (default: 4141)
  host: "0.0.0.0"         # Bind address (default: 0.0.0.0)
  base_url: "https://..."  # Public URL for OAuth callbacks
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `port` | int | `4141` | HTTP server port |
| `host` | string | `0.0.0.0` | Bind address |
| `base_url` | string | - | Public URL (required for auth) |

---

## GitHub Section

```yaml
github:
  webhook_secret: "${GITHUB_WEBHOOK_SECRET}"
  app_id: 123456
  app_private_key: "/app/secrets/github-app.pem"
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `webhook_secret` | string | Yes | Webhook signature secret |
| `app_id` | int | Yes | GitHub App ID |
| `app_private_key` | string | Yes | Path to private key file or key content |

---

## Argo CD Section

```yaml
argocd:
  server_url: "https://argocd.example.com"
  token: "${ARGOCD_TOKEN}"
  insecure: false
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `server_url` | string | Required | Argo CD API URL |
| `token` | string | Required | API token |
| `insecure` | bool | `false` | Skip TLS verification |

---

## Redis Section

```yaml
redis:
  address: "redis:6379"
  password: "${REDIS_PASSWORD}"
  db: 0
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `address` | string | `localhost:6379` | Redis server address |
| `password` | string | - | Redis password |
| `db` | int | `0` | Redis database number |

---

## Defaults Section

```yaml
defaults:
  autoplan: true
  require_approval: false
  delete_source_branch: false
  auto_merge: false
  merge_method: "squash"
  allowed_repos:
    - "myorg/repo1"
    - "myorg/infra-*"
    - "myorg/*"
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `autoplan` | bool | `true` | Auto-run plan on PR open/update |
| `require_approval` | bool | `false` | Require PR approval before sync |
| `delete_source_branch` | bool | `false` | Delete branch after merge |
| `auto_merge` | bool | `false` | Auto-merge PR after successful sync |
| `merge_method` | string | `squash` | Merge method: `squash`, `merge`, `rebase` |
| `allowed_repos` | []string | `[]` | Repository allowlist (empty = all) |

### Repository Allowlist Patterns

```yaml
allowed_repos:
  - "myorg/specific-repo"     # Exact match
  - "myorg/infra-*"           # Prefix wildcard
  - "myorg/*"                 # All repos in org
```

---

## Auto-Merge Configuration

When `auto_merge: true`:

1. After all syncs succeed, Lemuria merges the PR
2. Uses the specified `merge_method`
3. Optionally deletes the source branch (if `delete_source_branch: true`)
4. Protected branches (`main`, `master`, `develop`) are never deleted

```yaml
defaults:
  auto_merge: true
  merge_method: "squash"        # squash, merge, or rebase
  delete_source_branch: true    # Delete branch after merge
```

---

## Repository Configuration

Create `.lemuria.yaml` in your repository root to customize behavior per-repo.

### Full Example

```yaml
version: 1

# Override server defaults
autoplan: true
require_approval: true

# Application to path mappings
applications:
  # Exact application name
  - name: frontend
    paths:
      - "apps/frontend/**"
      - "base/frontend/**"

  # Wildcard application name (matches frontend-dev, frontend-prod, etc.)
  - name: "frontend-*"
    paths:
      - "apps/frontend/**"
      - "envs/**/frontend/**"

  # ApplicationSet-generated apps
  - name: "cluster-*"
    applicationset: cluster-apps
    paths:
      - "clusters/**"

# Per-application sync requirements
sync_requirements:
  - name: production
    require_approval: true
    allowed_users:
      - "senior-dev"
      - "platform-team"

  - name: staging
    require_approval: false
```

### Applications Section

Maps Argo CD applications to repository paths.

```yaml
applications:
  - name: my-app              # Argo CD application name (supports wildcards)
    paths:                    # Paths that affect this app
      - "apps/my-app/**"
      - "base/**"
    applicationset: my-set    # Optional: ApplicationSet name
```

**Path Pattern Syntax:**

| Pattern | Matches |
|---------|---------|
| `apps/my-app/**` | All files under `apps/my-app/` |
| `*.yaml` | All YAML files in root |
| `envs/*/values.yaml` | values.yaml in any env subdirectory |
| `**/*.yaml` | All YAML files recursively |

### Sync Requirements Section

Override approval requirements per application.

```yaml
sync_requirements:
  - name: production          # Application name
    require_approval: true    # Require PR approval
    allowed_users:            # Users allowed to sync
      - "admin"
      - "@myorg/platform"     # GitHub team
```

---

## Environment Variable Substitution

Use `${VAR_NAME}` syntax for environment variables:

```yaml
github:
  webhook_secret: "${GITHUB_WEBHOOK_SECRET}"

argocd:
  token: "${ARGOCD_TOKEN}"

redis:
  password: "${REDIS_PASSWORD}"
```

---

## Configuration Precedence

Settings are applied in this order (later overrides earlier):

1. **Built-in defaults**
2. **Server configuration** (`lemuria.yaml`)
3. **Repository configuration** (`.lemuria.yaml`)
4. **Sync requirements** (per-application)

---

## Validation

Lemuria validates configuration on startup:

```bash
# Check configuration
./lemuria --config lemuria.yaml --validate

# Common validation errors:
# - Missing required fields (github.app_id, argocd.server_url)
# - Invalid YAML syntax
# - Unreachable Redis/Argo CD endpoints
```

---

## Examples

### Minimal Configuration

```yaml
github:
  webhook_secret: "secret"
  app_id: 123456
  app_private_key: "/app/key.pem"

argocd:
  server_url: "https://argocd.example.com"
  token: "token"

redis:
  address: "redis:6379"
```

### Production Configuration

```yaml
server:
  port: 4141
  base_url: "https://lemuria.example.com"

github:
  webhook_secret: "${GITHUB_WEBHOOK_SECRET}"
  app_id: 123456
  app_private_key: "${GITHUB_APP_PRIVATE_KEY}"

argocd:
  server_url: "https://argocd.example.com"
  token: "${ARGOCD_TOKEN}"

redis:
  address: "redis-master.redis:6379"
  password: "${REDIS_PASSWORD}"

defaults:
  autoplan: true
  require_approval: true
  auto_merge: true
  merge_method: "squash"
  allowed_repos:
    - "myorg/*"

auth:
  enabled: true
  session_secret: "${SESSION_SECRET}"
  github:
    client_id: "${GITHUB_OAUTH_CLIENT_ID}"
    client_secret: "${GITHUB_OAUTH_CLIENT_SECRET}"
    allowed_orgs:
      - "myorg"
```

---

## Next Steps

- [Authentication](authentication) - Configure SSO
- [Commands](commands) - Available commands
- [Workflow](workflow) - PR workflow details
