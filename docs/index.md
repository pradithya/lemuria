---
layout: default
title: Home
nav_order: 1
---

# Lemuria

**GitOps Pull Request Automation for Argo CD**

Lemuria enables a pull request-based workflow for Argo CD, allowing teams to review and approve infrastructure changes before they are deployed. Similar to Atlantis for Terraform, Lemuria provides plan/sync operations triggered by PR comments.

---

## Key Features

- **Plan on PR** - Automatically generate diffs when PRs are opened or updated
- **Sync via Comments** - Deploy changes by commenting `lemuria sync` on PRs
- **Rollback Support** - Quickly revert to previous deployments
- **Auto-Merge** - Optionally merge PRs after successful sync
- **Distributed Locking** - Prevent concurrent modifications to applications
- **Approval Enforcement** - Require PR approval before deployment
- **Multi-App Support** - Handle multiple applications in a single PR
- **Web UI** - View locks, history, and application status

---

## How It Works

```
1. Developer opens PR with manifest changes
         ↓
2. Lemuria automatically runs `plan` and posts diff as comment
         ↓
3. Team reviews the diff and approves the PR
         ↓
4. Developer comments `lemuria sync` to deploy
         ↓
5. Lemuria syncs the application and releases the lock
         ↓
6. PR is merged (optionally auto-merged)
```

---

## Quick Example

**1. Open a PR with Kubernetes manifest changes**

**2. Lemuria posts a plan comment:**

```markdown
## Lemuria Plan

### Application: `my-app`

📋 **Changes:** 1 to create, 2 to update

<details>
<summary>Diff (3 resources changed)</summary>

#### ➕ ConfigMap/my-config

```diff
+ apiVersion: v1
+ kind: ConfigMap
+ metadata:
+   name: my-config
```

</details>

**Status:** 🔒 Locked by this PR

---
To apply: comment `lemuria sync`
To unlock: comment `lemuria unlock`
```

**3. Comment to deploy:**

```
lemuria sync
```

**4. Lemuria syncs and confirms:**

```markdown
## Lemuria Sync

### Application: `my-app`

✅ **Sync successful**

---
🎉 All applications synced successfully!
```

---

## Requirements

- **Argo CD** 2.0+ with API access
- **GitHub App** for webhook integration
- **Redis** for distributed locking
- **Kubernetes** cluster (for running Lemuria)

---

## Next Steps

- [Getting Started](getting-started) - Install and configure Lemuria
- [Configuration](configuration) - Full configuration reference
- [Commands](commands) - Available commands and options
- [Workflow](workflow) - Detailed workflow documentation

---

## License

Lemuria is open source software licensed under the [Apache 2.0 License](https://github.com/org/lemuria/blob/main/LICENSE).
