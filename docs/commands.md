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
| `lemuria rollback` | Revert to the application's configured targetRevision |
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

1. Identifies applications affected by changed files (using `.lemuria.yaml` path patterns or Argo CD app source paths)
2. Acquires locks for each application
3. For branch diff mode: creates a temporary Application CR pointing to the PR branch, fetches rendered manifests, compares with target branch
4. Posts results as PR comment with resource-level diffs
5. Stores the plan revision (PR HEAD commit SHA) for later verification during sync

### Example Output

```markdown
## Lemuria Plan

### Application: `frontend`

**Changes:** 1 to create, 2 to update

<details>
<summary>Diff (3 resources changed)</summary>

#### ConfigMap/frontend-config

+ apiVersion: v1
+ kind: ConfigMap
+ metadata:
+   name: frontend-config

#### Deployment/frontend

  spec:
    replicas: 3
-   image: frontend:v1.0.0
+   image: frontend:v1.1.0

</details>

**Status:** Locked by this PR

---
To apply: comment `lemuria sync`
To unlock: comment `lemuria unlock`
```

### Auto-Plan

When `autoplan: true` (default), Lemuria automatically runs `plan` when:
- A PR is opened
- New commits are pushed to a PR branch

When a new plan runs, previous plan comments are marked as stale.

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

Sync requires all of the following:

1. **Valid plan** - Run `lemuria plan` first (locks must exist for the PR)
2. **Non-stale plan** - Plan revision must match current PR HEAD SHA
3. **PR approval** - If `require_approval: true` (checked at server, repo, or per-app level)
4. **No merge conflicts** - PR must be mergeable
5. **Auto-sync disabled** - Application must not have auto-sync enabled in Argo CD

### Sync Behavior

All locked applications are synced **in parallel**, significantly reducing total sync time for PRs affecting multiple applications.

If `skip_no_changes: true` (server default or per-repo override), applications where the plan detected no changes are skipped automatically. Skipped applications appear as succeeded with "Skipped — no changes detected".

For applications sourcing from the PR repository:
- Syncs to the PR's HEAD commit SHA (not the app's configured targetRevision)
- This enables "deploy before merge" workflow

For applications with external sources (e.g., Helm chart repos):
- If the Application CR was modified in the PR, Lemuria updates the live app's spec from the PR branch before syncing
- Syncs using the app's configured revision (not the PR SHA)

On successful sync:
- Locks are released
- If `auto_merge: true` and all syncs succeeded, the PR is merged

### Example Output

```markdown
## Lemuria Sync

### Application: `frontend`

Sync successful

### Application: `backend`

Sync successful

---
All applications synced successfully!
```

### Auto-Merge

When `auto_merge: true` and all syncs succeed (not dry-run):

1. PR is automatically merged
2. Uses configured `merge_method` (squash, merge, rebase)
3. Optionally deletes source branch (if `delete_source_branch: true`)
4. Protected branches (`main`, `master`, `develop`, `development`) are never deleted

---

## Rollback

Revert applications to their configured targetRevision (typically `main` or `HEAD`), effectively undoing any PR-deployed changes.

### Usage

```
lemuria rollback                # Rollback all locked applications
lemuria rollback -a <app>       # Rollback specific application
lemuria rollback --dry-run      # Preview rollback
lemuria rollback --prune        # Rollback with resource pruning
```

### Options

| Flag | Description |
|------|-------------|
| `-a`, `--app`, `--application` | Specific application name |
| `--dry-run` | Preview only, don't apply |
| `--prune` | Enable resource pruning |

### Requirements

- **PR approval** - If `require_approval: true` (same as sync)
- **Auto-sync disabled** - Application must not have auto-sync enabled

### Behavior

Rollback syncs the application using an empty revision, which causes Argo CD to use the application's configured `targetRevision` (typically `main` or `HEAD`). This effectively reverts any changes deployed from the PR branch.

On successful rollback, locks are released (unless dry-run).

### Example Output

```markdown
## Lemuria Rollback - `frontend`

**Target:** `main`

Rollback successful. Application synced to configured targetRevision.
```

### When to Use Rollback

- After syncing, if issues are discovered in the deployed changes
- To revert PR changes before merging
- To restore an application to its main branch state

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
3. Allows other PRs to plan/sync the unlocked applications

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

Locks also expire automatically after 7 days (TTL) for abandoned PRs.

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

Lemuria finds the first matching command line in multi-line comments:

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

### Bare Arguments

A bare word (non-flag) is treated as the application name:

```
lemuria plan my-app       # Same as: lemuria plan -a my-app
```

---

## Error Messages

### Application Not Found

```markdown
## Lemuria Sync

Application `unknown-app` is not locked by this PR.
```

### Stale Plan

```markdown
## Lemuria Sync

Plan for `frontend` is stale. Please run `lemuria plan` again.
```

This happens when new commits are pushed after the plan was generated. The stored plan revision no longer matches the PR's current HEAD SHA.

### Approval Required

```markdown
## Lemuria Sync

PR must be approved before sync
```

### Auto-Sync Enabled

```markdown
## Lemuria Sync

Application `frontend` has auto-sync enabled.

Disable auto-sync before using Lemuria to prevent conflicts.
```

### Lock Conflict

```markdown
## Lemuria Plan

### Application: `frontend`

**Locked by PR #42 (otheruser)**
```

### Merge Conflicts

```markdown
## Lemuria Sync

PR has merge conflicts, please resolve before syncing
```

---

## Reactions

Lemuria adds emoji reactions to PR comments to show command status:

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
