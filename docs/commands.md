---
layout: default
title: Commands
nav_order: 5
---

# Commands

Lemuria commands are triggered by commenting on pull requests. All commands start with `lemuria`.

---

## Command Summary

| Command | Description |
|---------|-------------|
| `lemuria plan` | Generate diff for affected applications |
| `lemuria sync` | Deploy planned changes |
| `lemuria rollback` | Revert to previous deployment |
| `lemuria unlock` | Release application locks |
| `lemuria help` | Show help message |

---

## Plan

Generate a diff showing what would change when syncing.

### Usage

```
lemuria plan                    # Plan all affected applications
lemuria plan -a <app>           # Plan specific application
lemuria plan --all              # Plan all apps in repo (ignore path filtering)
```

### Options

| Flag | Description |
|------|-------------|
| `-a`, `--app`, `--application` | Specific application name |
| `--all` | Plan all applications for this repo |

### Behavior

1. Identifies applications affected by changed files
2. Acquires locks for each application
3. Generates diff between live state and PR state
4. Posts results as PR comment

### Example Output

```markdown
## Lemuria Plan

### Application: `frontend`

📋 **Changes:** 1 to create, 2 to update

<details>
<summary>Diff (3 resources changed)</summary>

#### ➕ ConfigMap/frontend-config

```diff
+ apiVersion: v1
+ kind: ConfigMap
+ metadata:
+   name: frontend-config
```

#### 📝 Deployment/frontend

```diff
  spec:
    replicas: 3
-   image: frontend:v1.0.0
+   image: frontend:v1.1.0
```

</details>

**Status:** 🔒 Locked by this PR

---
To apply: comment `lemuria sync`
To unlock: comment `lemuria unlock`
```

### Auto-Plan

When `autoplan: true` (default), Lemuria automatically runs `plan` when:
- PR is opened
- New commits are pushed to PR

---

## Sync

Deploy the planned changes to Argo CD.

### Usage

```
lemuria sync                    # Sync all locked applications
lemuria sync -a <app>           # Sync specific application
lemuria sync --prune            # Sync with resource pruning enabled
lemuria sync --dry-run          # Preview sync without applying
```

### Options

| Flag | Description |
|------|-------------|
| `-a`, `--app`, `--application` | Specific application name |
| `--prune` | Enable resource pruning |
| `--dry-run` | Preview only, don't apply changes |

### Requirements

Sync requires:

1. **Valid plan** - Run `lemuria plan` first
2. **Non-stale plan** - Plan must match current PR head
3. **PR approval** - If `require_approval: true`
4. **No merge conflicts** - PR must be mergeable
5. **Auto-sync disabled** - Application must not have auto-sync enabled

### Example Output

```markdown
## Lemuria Sync

### Application: `frontend`

✅ **Sync successful**

### Application: `backend`

✅ **Sync successful**

---
🎉 All applications synced successfully!
```

### Auto-Merge

When `auto_merge: true` and all syncs succeed:

1. PR is automatically merged
2. Uses configured `merge_method` (squash, merge, rebase)
3. Optionally deletes source branch

---

## Rollback

Revert applications to their configured targetRevision (main/master).

### Usage

```
lemuria rollback                # Rollback all locked applications
lemuria rollback -a <app>       # Rollback specific application
lemuria rollback --dry-run      # Preview rollback
```

### Options

| Flag | Description |
|------|-------------|
| `-a`, `--app`, `--application` | Specific application name |
| `--dry-run` | Preview only, don't apply |
| `--prune` | Enable resource pruning |

### Behavior

Rollback syncs the application to its configured `targetRevision` (typically `main` or `HEAD`), effectively reverting any changes deployed from the PR.

### Example Output

```markdown
## Lemuria Rollback - `frontend`

**Target:** `main`

✅ Rollback successful. Application synced to configured targetRevision.
```

### When to Use Rollback

- After syncing, if issues are discovered
- To revert PR changes before merging
- To restore application to main branch state

---

## Unlock

Release locks held by this PR without syncing.

### Usage

```
lemuria unlock                  # Unlock all applications
lemuria unlock -a <app>         # Unlock specific application
```

### Options

| Flag | Description |
|------|-------------|
| `-a`, `--app`, `--application` | Specific application name |

### Behavior

1. Releases locks for specified applications
2. Discards stored plan
3. Allows other PRs to plan/sync

### Example Output

```markdown
## Lemuria Unlock

Unlocked 2 applications:
- `frontend`
- `backend`
```

### Automatic Unlock

Locks are automatically released when:
- PR is merged
- PR is closed

---

## Help

Display help information.

### Usage

```
lemuria help
```

### Example Output

```markdown
## Lemuria Help

Lemuria provides Argo CD pull request automation.

### Commands

| Command | Description |
|---------|-------------|
| `lemuria plan` | Generate diff of changes for affected applications |
| `lemuria plan -a <app>` | Plan specific application |
| `lemuria sync` | Sync all planned applications |
| `lemuria rollback` | Rollback all locked apps to their targetRevision |
| `lemuria unlock` | Release all locks for this PR |
| `lemuria help` | Show this help message |
```

---

## Command Parsing

### Case Insensitivity

Commands are case-insensitive:

```
lemuria plan     ✓
Lemuria Plan     ✓
LEMURIA PLAN     ✓
```

### Multi-line Comments

Lemuria finds the command in multi-line comments:

```
I think we should deploy this.

lemuria sync

Thanks!
```

### Quoted Arguments

Application names with spaces can be quoted:

```
lemuria plan -a "my app name"
lemuria sync --app 'another-app'
```

---

## Error Messages

### Application Not Found

```markdown
## Lemuria Sync

⚠️ Application `unknown-app` is not locked by this PR.
```

### Stale Plan

```markdown
## Lemuria Sync

⚠️ Plan for `frontend` is stale. Please run `lemuria plan` again.
```

### Approval Required

```markdown
## Lemuria Sync

❌ PR must be approved before sync
```

### Auto-Sync Enabled

```markdown
## Lemuria Sync

❌ Application `frontend` has auto-sync enabled.

Disable auto-sync before using Lemuria to prevent conflicts.
```

### Lock Conflict

```markdown
## Lemuria Plan

### Application: `frontend`

⚠️ **Locked by PR #42 (otheruser)**
```

---

## Reactions

Lemuria adds emoji reactions to show command status:

| Reaction | Meaning |
|----------|---------|
| 👀 | Command received, processing |
| ✅ | Command completed successfully |
| ❌ | Command failed |

---

## Next Steps

- [Workflow](workflow) - Detailed workflow documentation
- [Configuration](configuration) - Customize command behavior
- [Troubleshooting](troubleshooting) - Common issues
