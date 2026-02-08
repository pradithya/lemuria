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
| `.lemuria.yaml` | Repository root | Per-repository settings |
| `lemuria.yaml` | Server | Main server configuration |

Multiple server configuration files can be merged by passing `-config` multiple times. Later files override earlier ones.

```bash
./bin/lemuria -config base.yaml -config production.yaml
```

---


## Repository Configuration

Create `.lemuria.yaml` in your repository root to customize behavior per-repo. These settings override the server defaults.

### Full Example

```yaml
version: 1

# Override server defaults
autoplan: true
require_approval: true
auto_merge: true

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

Maps Argo CD applications to repository paths. Lemuria uses this to determine which applications are affected when files change in a PR.

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

**Application Detection Fallback:**

If no `.lemuria.yaml` exists or an app has no explicit path mapping, Lemuria falls back to checking if the app's configured `source.path` in Argo CD contains any of the changed files.

### Sync Requirements Section

Override approval requirements per application. Supports exact names and wildcard patterns.

```yaml
sync_requirements:
  - name: production          # Application name (supports wildcards)
    require_approval: true    # Require PR approval
    allowed_users:            # Users allowed to sync
      - "admin"
      - "@myorg/platform"     # GitHub team
```

---

## Configuration Precedence

Settings are applied in this order (later overrides earlier):

1. **Built-in defaults** (`config.DefaultConfig()`)
2. **Server configuration** (`lemuria.yaml` - can be multiple files merged in order)
3. **Repository configuration** (`.lemuria.yaml` - `autoplan`, `require_approval`, `auto_merge`)
4. **Sync requirements** (per-application `require_approval`, `allowed_users`)

For approval requirements specifically, the resolution order is:
1. `sync_requirements` per-app match (exact match first, then wildcard)
2. Repository `.lemuria.yaml` top-level `require_approval`
3. Server `defaults.require_approval`

For auto-merge, the resolution order is:
1. Repository `.lemuria.yaml` top-level `auto_merge`
2. Server `defaults.auto_merge`

---

## Server Configuration

### Full Example

```yaml
server:
  port: 4141
  host: "0.0.0.0"
  base_url: "https://lemuria.example.com"
  log_level: "info"

github:
  webhook_secret: "${GITHUB_WEBHOOK_SECRET}"
  app_id: 123456
  app_private_key: "/app/secrets/github-app.pem"

argocd:
  server_url: "https://argocd.example.com"
  token: "${ARGOCD_TOKEN}"
  insecure: false
  diff_mode: "branch"
  temp_app_timeout: 2m

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
  cookie_domain: "example.com"
  cookie_secure: true
  default_role: "user"
  github:
    client_id: "${GITHUB_OAUTH_CLIENT_ID}"
    client_secret: "${GITHUB_OAUTH_CLIENT_SECRET}"
    allowed_orgs:
      - "myorg"
  oidc:
    name: "Company SSO"
    issuer_url: "https://sso.example.com"
    client_id: "${OIDC_CLIENT_ID}"
    client_secret: "${OIDC_CLIENT_SECRET}"
    scopes: ["openid", "profile", "email"]
    allowed_domains: ["example.com"]
  basic:
    users:
      - username: admin
        password: admin
        role: admin
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
  log_level: "info"        # Log verbosity
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `port` | int | `4141` | HTTP server port |
| `host` | string | `0.0.0.0` | Bind address |
| `base_url` | string | - | Public URL (required for OAuth callbacks) |
| `log_level` | string | `info` | Log level: `debug`, `info`, `warn`, `error` |

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
| `webhook_secret` | string | Yes | Webhook HMAC-SHA256 secret |
| `app_id` | int | Yes | GitHub App ID |
| `app_private_key` | string | Yes | Path to private key file or PEM content |

---

## Argo CD Section

```yaml
argocd:
  server_url: "https://argocd.example.com"
  token: "${ARGOCD_TOKEN}"
  insecure: false
  diff_mode: "branch"
  temp_app_timeout: 2m
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `server_url` | string | Required | Argo CD API URL |
| `token` | string | Required | API token |
| `insecure` | bool | `false` | Skip TLS verification |
| `diff_mode` | string | `branch` | Diff mode (see below) |
| `temp_app_timeout` | duration | `2m` | Timeout for temporary app manifest rendering |

### Diff Modes

| Mode | Description |
|------|-------------|
| `branch` | Compare PR branch manifests vs target branch manifests. Creates a temporary Application CR pointing to the PR branch, fetches its rendered manifests, then compares with the target branch manifests. |
| `live` | Compare PR branch manifests vs the live cluster state. Useful for detecting drift. |

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

Redis is used for two purposes:
1. **Distributed locks** - Application locks with 7-day TTL
2. **Session storage** - Web UI sessions (when auth is enabled)

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
| `delete_source_branch` | bool | `false` | Delete branch after auto-merge |
| `auto_merge` | bool | `false` | Auto-merge PR after successful sync |
| `merge_method` | string | `squash` | Merge method: `squash`, `merge`, `rebase` |
| `allowed_repos` | []string | `[]` | Repository allowlist (empty = all repos allowed) |

### Repository Allowlist Patterns

```yaml
allowed_repos:
  - "myorg/specific-repo"     # Exact match
  - "myorg/infra-*"           # Prefix wildcard
  - "myorg/*"                 # All repos in org
```

When the list is empty, all repositories where the GitHub App is installed are allowed.

---

## Auth Section

See [Authentication](authentication) for detailed provider setup.

```yaml
auth:
  enabled: true
  session_secret: "${SESSION_SECRET}"
  session_ttl: 24h
  cookie_domain: "example.com"
  cookie_secure: true
  default_role: "user"
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | `false` | Enable authentication for the web UI |
| `session_secret` | string | Required (if auth enabled) | Secret for signing session cookies |
| `session_ttl` | duration | `24h` | Session duration |
| `cookie_domain` | string | auto | Cookie domain |
| `cookie_secure` | bool | `true` | HTTPS-only cookies |
| `default_role` | string | `user` | Default role for new users (`user` or `admin`) |

### Auth Providers

Multiple providers can be configured simultaneously. Users see login buttons for each.

#### GitHub OAuth

```yaml
auth:
  github:
    client_id: "${GITHUB_OAUTH_CLIENT_ID}"
    client_secret: "${GITHUB_OAUTH_CLIENT_SECRET}"
    allowed_orgs: ["myorg"]
    allowed_teams: ["myorg/platform-team"]
```

#### OIDC

```yaml
auth:
  oidc:
    name: "Company SSO"
    issuer_url: "https://sso.example.com"
    client_id: "${OIDC_CLIENT_ID}"
    client_secret: "${OIDC_CLIENT_SECRET}"
    scopes: ["openid", "profile", "email"]
    username_claim: "preferred_username"
    email_claim: "email"
    groups_claim: "groups"
    allowed_domains: ["example.com"]
```

#### Basic Auth (dev only)

```yaml
auth:
  basic:
    users:
      - username: admin
        password: admin
        role: admin
```

### Role Assignments

```yaml
auth:
  role_assignments:
    - pattern: "admin@example.com"
      role: "admin"
    - pattern: "*@platform.example.com"
      role: "admin"
    - pattern: "@myorg/platform-admins"
      provider: "github"
      role: "admin"
```

---

## Auto-Merge Configuration

When `auto_merge: true`:

1. After all syncs succeed, Lemuria merges the PR
2. Uses the specified `merge_method`
3. Optionally deletes the source branch (if `delete_source_branch: true`)
4. Protected branches (`main`, `master`, `develop`, `development`) are never deleted

**Server default:**

```yaml
defaults:
  auto_merge: true
  merge_method: "squash"        # squash, merge, or rebase
  delete_source_branch: true    # Delete branch after merge
```

**Per-repository override** (`.lemuria.yaml`):

```yaml
auto_merge: true    # Override server default for this repo
```

The repo-level `auto_merge` setting takes precedence over the server default, allowing individual repositories to opt in or out of auto-merge regardless of the server configuration.

---


## Environment Variable Substitution

Use `${VAR_NAME}` syntax for environment variables in any configuration value:

```yaml
github:
  webhook_secret: "${GITHUB_WEBHOOK_SECRET}"

argocd:
  token: "${ARGOCD_TOKEN}"

redis:
  password: "${REDIS_PASSWORD}"
```

If an environment variable is not set, the `${VAR_NAME}` string is left as-is.

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
  log_level: "info"

github:
  webhook_secret: "${GITHUB_WEBHOOK_SECRET}"
  app_id: 123456
  app_private_key: "${GITHUB_APP_PRIVATE_KEY}"

argocd:
  server_url: "https://argocd.example.com"
  token: "${ARGOCD_TOKEN}"
  diff_mode: "branch"
  temp_app_timeout: "2m"

redis:
  address: "redis-master.redis:6379"
  password: "${REDIS_PASSWORD}"

defaults:
  autoplan: true
  require_approval: true
  auto_merge: true
  merge_method: "squash"
  delete_source_branch: true
  allowed_repos:
    - "myorg/*"

auth:
  enabled: true
  session_secret: "${SESSION_SECRET}"
  session_ttl: "24h"
  cookie_secure: true
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

## Next Steps

- [Authentication](authentication) - Configure SSO providers
- [Commands](commands) - Available commands
- [Workflow](workflow) - PR workflow details
