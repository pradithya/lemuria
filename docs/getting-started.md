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

- **Argo CD** 2.0+ running in your cluster
- **Redis** instance for distributed locking
- **GitHub App** credentials (see [Creating a GitHub App](#creating-a-github-app))
- **kubectl** access to your Kubernetes cluster

---

## Installation

### Option 1: Helm Chart (Recommended)

```bash
# Add the Lemuria Helm repository
helm repo add lemuria https://pradithya.github.io/lemuria/charts
helm repo update

# Install Lemuria
helm install lemuria lemuria/lemuria \
  --namespace lemuria \
  --create-namespace \
  --values values.yaml
```

### Option 2: Kubernetes Manifests

```bash
# Clone the repository
git clone https://github.com/pradithya/lemuria.git
cd lemuria

# Apply the manifests
kubectl apply -k deploy/
```

### Option 3: Docker

```bash
docker run -d \
  -p 4141:4141 \
  -v /path/to/config:/app/config \
  -e GITHUB_WEBHOOK_SECRET=your-secret \
  -e ARGOCD_TOKEN=your-token \
  ghcr.io/pradithya/lemuria:latest
```

---

## Creating a GitHub App

Lemuria uses a GitHub App for authentication and webhook integration.

### Step 1: Create the App

1. Go to **GitHub Settings** → **Developer settings** → **GitHub Apps**
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

## Basic Configuration

Create a configuration file `lemuria.yaml`:

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
  auto_merge: false
  merge_method: "squash"
```

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

  - name: "my-apps-*"  # Wildcard matching
    paths:
      - "apps/**"

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
'
```

---

## Verify Installation

### 1. Check Lemuria is Running

```bash
kubectl get pods -n lemuria
kubectl logs -n lemuria deployment/lemuria
```

### 2. Test Webhook Connectivity

```bash
curl -X POST https://lemuria.example.com/health
```

### 3. Open a Test PR

1. Create a branch with a manifest change
2. Open a PR
3. Verify Lemuria posts a plan comment

---

## Environment Variables

Lemuria supports environment variable substitution in configuration:

| Variable | Description |
|----------|-------------|
| `GITHUB_WEBHOOK_SECRET` | GitHub webhook secret |
| `GITHUB_APP_PRIVATE_KEY` | Path to or content of private key |
| `ARGOCD_TOKEN` | Argo CD API token |
| `REDIS_PASSWORD` | Redis password |
| `SESSION_SECRET` | Session signing secret (for web UI) |

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
2. Verify webhook URL is accessible
3. Check Lemuria logs for incoming requests

### Plan Not Generated

1. Verify `.lemuria.yaml` exists in the repository
2. Check path patterns match changed files
3. Ensure Argo CD application exists

### Sync Blocked

1. Check if PR requires approval
2. Verify plan is not stale (re-run `lemuria plan`)
3. Check if auto-sync is enabled (must be disabled)

See [Troubleshooting](troubleshooting) for more details.
