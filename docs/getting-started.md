---
layout: default
title: Getting Started
nav_order: 2
---

# Getting Started

This guide walks you through installing and configuring Lemuria for your Argo CD environment.

---

## Prerequisites

Before installing Lemuria, ensure you have:

- **Argo CD** v2.0+ running in your cluster
- **Redis** instance for distributed locking and session storage
- **GitHub App** credentials (see [Creating a GitHub App](#creating-a-github-app)) and/or **GitLab access token** (see [Configuring GitLab](#configuring-gitlab))
- **kubectl** access to your Kubernetes cluster
- **Helm** 3.16+ (for Helm-based installation)

---

## Installation

### Option 1: Helm Chart (Recommended)

Lemuria publishes Helm charts to an OCI registry on GitHub Container Registry.

```bash
# Install Lemuria with Helm (OCI registry)
helm upgrade --install lemuria \
  oci://ghcr.io/pradithya/charts/lemuria \
  --namespace lemuria \
  --create-namespace \
  -f values.yaml
```

The Helm chart includes an optional Bitnami Redis dependency. Enable it with `redis.enabled: true` in your values file, or point to an existing Redis instance.

### Option 2: Docker

```bash
docker run -d \
  -p 4141:4141 \
  -v /path/to/config:/app/config \
  -e GITHUB_WEBHOOK_SECRET=your-secret \
  -e ARGOCD_TOKEN=your-token \
  ghcr.io/pradithya/lemuria:latest
```

### Option 3: Build from Source

```bash
# Clone the repository
git clone https://github.com/pradithya/lemuria.git
cd lemuria

# Install dependencies and build (requires Go 1.25+ and Node.js 20+)
make build

# Run
./bin/lemuria -config lemuria.yaml
```

---

## Creating a GitHub App

Lemuria uses a GitHub App for webhook integration and PR interaction.

### Step 1: Create the App

1. Go to **GitHub Settings** > **Developer settings** > **GitHub Apps**
2. Click **New GitHub App**
3. Fill in the details:

| Field | Value |
|-------|-------|
| **GitHub App name** | `Lemuria` (or your preferred name) |
| **Homepage URL** | Your Lemuria instance URL |
| **Webhook URL** | `https://your-lemuria-url/webhook` |
| **Webhook secret** | Generate a secure secret |

### Step 2: Configure Permissions

Set the following **Repository permissions**:

| Permission | Access |
|------------|--------|
| **Contents** | Read |
| **Issues** | Read & Write |
| **Pull requests** | Read & Write |
| **Metadata** | Read |

### Step 3: Subscribe to Events

Subscribe to these webhook events:

- `Issue comment`
- `Pull request`
- `Pull request review`

### Step 4: Generate Private Key

1. After creating the app, click **Generate a private key**
2. Save the downloaded `.pem` file securely
3. Note the **App ID** from the app settings

### Step 5: Install the App

1. Click **Install App** in the sidebar
2. Select your organization or repositories
3. Grant access to repositories you want Lemuria to manage

---

## Configuring GitLab

Lemuria integrates with GitLab using a personal or group access token and webhook secret.

### Step 1: Create an Access Token

1. Go to **GitLab** → **Settings** → **Access Tokens** (user, group, or project level)
2. Create a token with the following scopes:
   - `api` — Full API access (for reading MR details, posting comments, managing branches)
3. Note the generated token

### Step 2: Configure a Webhook

1. Go to your **GitLab project** → **Settings** → **Webhooks**
2. Configure:

| Field | Value |
|-------|-------|
| **URL** | `https://your-lemuria-url/webhook/gitlab` |
| **Secret token** | Generate a secure secret |
| **Trigger** | Merge request events, Comments |

3. Click **Add webhook**

### Step 3: Configure Lemuria

Add the GitLab section to your `lemuria.yaml`:

```yaml
gitlab:
  url: "https://gitlab.com"              # Your GitLab instance URL
  token: "${GITLAB_TOKEN}"               # Access token from Step 1
  webhook_secret: "${GITLAB_WEBHOOK_SECRET}"  # Secret from Step 2
```

For self-managed GitLab instances, set `url` to your instance URL (e.g., `https://gitlab.example.com`).

---

## Basic Configuration

Create a configuration file `lemuria.yaml`:

```yaml
server:
  port: 4141
  host: "0.0.0.0"
  base_url: "https://lemuria.example.com"
  log_level: "info"           # debug, info, warn, error

## For GitHub:
github:
  webhook_secret: "${GITHUB_WEBHOOK_SECRET}"
  app_id: 123456
  app_private_key: "/app/secrets/github-app.pem"

## For GitLab (can be used alongside or instead of GitHub):
gitlab:
  url: "https://gitlab.com"              # GitLab instance URL (default: https://gitlab.com)
  token: "${GITLAB_TOKEN}"               # Personal or Group Access Token
  webhook_secret: "${GITLAB_WEBHOOK_SECRET}"

argocd:
  server_url: "https://argocd.example.com"
  token: "${ARGOCD_TOKEN}"
  insecure: false
  diff_mode: "branch"         # "branch" (PR vs target branch) or "live" (PR vs cluster)
  temp_app_timeout: "2m"      # Timeout for temporary app manifest rendering

redis:
  address: "redis:6379"
  password: "${REDIS_PASSWORD}"
  db: 0

defaults:
  autoplan: true
  require_approval: false
  auto_merge: false
  merge_method: "squash"      # squash, merge, or rebase
```

### Multi-file Configuration

Lemuria supports merging multiple config files. Later files override earlier ones:

```bash
./bin/lemuria -config base.yaml -config production.yaml
```

This is useful for separating base settings from environment-specific overrides.

---

## Repository Configuration

Create `.lemuria.yaml` in your repository root:

```yaml
version: 1

# Override server defaults for this repo
autoplan: true
require_approval: true

# Map Argo CD applications to repository paths
applications:
  - name: my-app
    paths:
      - "apps/my-app/**"
      - "base/**"

  - name: "my-apps-*"           # Wildcard for ApplicationSet-generated apps
    paths:
      - "apps/**"
    applicationset: "my-appset"

# Sync requirements per application
sync_requirements:
  - name: production-app
    require_approval: true
    allowed_users:
      - "senior-dev"
      - "platform-team"
```

---

## Argo CD Configuration

### Important: Disable Auto-Sync

Lemuria requires auto-sync to be **disabled** on managed applications to prevent conflicts.

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: my-app
spec:
  # ... other config ...
  syncPolicy:
    # Do NOT set 'automated' - leave it null or remove it
    # automated:
    #   prune: true
    #   selfHeal: true
    syncOptions:
      - CreateNamespace=true
```

If auto-sync is enabled, Lemuria will:
- Show a **warning** during `plan`
- **Block** `sync` and `rollback` operations

### Generate API Token

Create an Argo CD API token for Lemuria:

```bash
# Using argocd CLI
argocd account generate-token --account lemuria

# Or create a local user in argocd-cm ConfigMap
kubectl patch configmap argocd-cm -n argocd --patch='
data:
  accounts.lemuria: apiKey
  accounts.lemuria.enabled: "true"
'

# Set permissions in argocd-rbac-cm
kubectl patch configmap argocd-rbac-cm -n argocd --patch='
data:
  policy.csv: |
    p, lemuria, applications, get, */*, allow
    p, lemuria, applications, sync, */*, allow
    p, lemuria, applications, action/*, */*, allow
    p, lemuria, applications, create, */*, allow
    p, lemuria, applications, update, */*, allow
    p, lemuria, applications, delete, */*, allow
'
```

The `create`, `update`, and `delete` permissions are needed for the temporary Application CR pattern that Lemuria uses to render branch manifests during `plan`.

---

## Verify Installation

### 1. Check Lemuria is Running

```bash
kubectl get pods -n lemuria
kubectl logs -n lemuria deployment/lemuria
```

### 2. Test Health Endpoint

```bash
# Health check (always returns 200)
curl https://lemuria.example.com/health

# Readiness check (verifies Redis connectivity)
curl https://lemuria.example.com/ready
```

### 3. Open a Test PR

1. Create a branch with a manifest change
2. Open a PR
3. Verify Lemuria posts a plan comment

---

## Environment Variables

Lemuria supports `${VAR_NAME}` substitution in YAML configuration:

| Variable | Description |
|----------|-------------|
| `GITHUB_WEBHOOK_SECRET` | GitHub webhook HMAC secret |
| `GITHUB_APP_PRIVATE_KEY` | Path to or content of GitHub App private key |
| `GITLAB_TOKEN` | GitLab personal/group access token |
| `GITLAB_WEBHOOK_SECRET` | GitLab webhook secret token |
| `ARGOCD_TOKEN` | Argo CD API token |
| `REDIS_PASSWORD` | Redis password |
| `SESSION_SECRET` | Session signing secret (for web UI auth) |
| `GITHUB_OAUTH_CLIENT_ID` | GitHub OAuth client ID (for web UI) |
| `GITHUB_OAUTH_CLIENT_SECRET` | GitHub OAuth client secret (for web UI) |
| `GITLAB_OAUTH_CLIENT_ID` | GitLab OAuth client ID (for web UI) |
| `GITLAB_OAUTH_CLIENT_SECRET` | GitLab OAuth client secret (for web UI) |
| `OIDC_CLIENT_ID` | OIDC client ID (for web UI) |
| `OIDC_CLIENT_SECRET` | OIDC client secret (for web UI) |

---

## Next Steps

- [Configuration Reference](configuration) - Full configuration options
- [Authentication](authentication) - Set up SSO for web UI
- [Commands](commands) - Learn available commands
- [Workflow](workflow) - Understand the PR workflow

---

## Troubleshooting

### Webhook Not Received

1. Check GitHub webhook delivery status in App settings
2. Verify webhook URL is accessible from GitHub
3. Check Lemuria logs for incoming requests

### Plan Not Generated

1. Verify `.lemuria.yaml` exists in the repository root
2. Check path patterns match changed files
3. Ensure Argo CD application exists and references the repository

### Sync Blocked

1. Check if PR requires approval
2. Verify plan is not stale (re-run `lemuria plan`)
3. Check if auto-sync is enabled on the application (must be disabled)
4. Resolve any merge conflicts

See [Troubleshooting](troubleshooting) for more details.
