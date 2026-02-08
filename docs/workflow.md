---
layout: default
title: Workflow
nav_order: 6
---

# Workflow

This page describes the complete Lemuria workflow, from PR creation to deployment.

---

## Standard Workflow

```
┌─────────────────────────────────────────────────────────────┐
│  1. Developer creates PR with manifest changes              │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│  2. Lemuria auto-plans and posts diff as comment            │
│     - Acquires locks for affected applications              │
│     - Shows what would change                               │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│  3. Team reviews diff and approves PR                       │
│     - Code review                                           │
│     - Verify expected changes                               │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│  4. Developer comments: lemuria sync                        │
│     - Deploys changes to Argo CD                            │
│     - Releases locks on success                             │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│  5. PR is merged (manually or auto-merged)                  │
│     - Application now tracks main branch                    │
└─────────────────────────────────────────────────────────────┘
```

---

## Detailed Flow

### Phase 1: PR Creation

1. Developer creates a branch with Kubernetes manifest changes
2. Opens a pull request against the main branch
3. GitHub sends a `pull_request` webhook event to Lemuria

### Phase 2: Auto-Plan

When `autoplan: true` (default):

1. Lemuria receives the PR webhook and validates the HMAC signature
2. Returns 200 immediately, then processes the event asynchronously in a goroutine
3. Identifies affected applications:
   - Matches changed files against `.lemuria.yaml` path patterns
   - Falls back to checking Argo CD application source paths
   - Detects new Application CRs added in the PR
   - Detects Application CRs being deleted in the PR
4. For each affected application:
   - Attempts to acquire lock (if locked by another PR, shows warning)
   - Creates a temporary Application CR pointing to the PR branch (for branch diff mode)
   - Fetches rendered manifests from Argo CD
   - Computes diff against target branch or live cluster state
   - Stores plan revision (PR HEAD commit SHA) for later verification
5. Posts plan results as PR comment
6. Cleans up temporary Application CRs

### Phase 3: Review

Team reviews the plan:

1. Verify expected changes in the diff
2. Look for unintended modifications
3. Approve the PR (if `require_approval: true`)

### Phase 4: Sync

Developer comments `lemuria sync`:

1. Lemuria receives the `issue_comment` webhook event
2. Since issue_comment events lack branch info, Lemuria fetches full PR details from GitHub API
3. Parses the command from the comment body
4. Verifies requirements:
   - PR is approved (if required, checked at server > repo > per-app level)
   - Plan is not stale (stored revision matches current PR HEAD SHA)
   - PR is mergeable (no conflicts)
   - Auto-sync is disabled on each application
5. For each locked application:
   - If the Application CR was modified in the PR, updates the live app's spec first
   - Syncs to PR HEAD commit SHA (for apps sourcing from PR repo)
   - Or syncs using the app's configured revision (for external source apps)
6. Releases locks on successful sync
7. Posts sync results as PR comment

### Phase 5: Merge

After successful sync:

- **Manual merge**: Developer merges the PR
- **Auto-merge**: Lemuria merges if `auto_merge: true`
  - Uses configured `merge_method` (squash, merge, or rebase)
  - Optionally deletes source branch

---

## Locking

Lemuria uses Redis-based distributed locking to prevent concurrent modifications to the same application.

### Lock Acquisition

```
PR #1 opens → lemuria plan
  └── Acquires lock for app-frontend
  └── Acquires lock for app-backend

PR #2 opens → lemuria plan
  └── ❌ Cannot lock app-frontend (held by PR #1)
  └── ✅ Acquires lock for app-api
```

### Lock Lifecycle

| Event | Lock Action |
|-------|-------------|
| `lemuria plan` | Acquire or refresh lock |
| `lemuria sync` (success) | Release lock |
| `lemuria rollback` (success) | Release lock |
| `lemuria unlock` | Release lock |
| PR merged | Auto-release all locks |
| PR closed | Auto-release all locks |
| 7-day TTL expiry | Auto-release (abandoned PRs) |

### Lock Data

Each lock stores:
- **Application name** - The locked Argo CD application
- **PR number** - Which PR holds the lock
- **Repository** - Full repo name (e.g., `org/repo`)
- **User** - Who triggered the plan
- **Timestamp** - When the lock was acquired
- **Plan revision** - Commit SHA at time of plan (for staleness check)
- **Source file** - Path to Application CR file (if the CR was modified)

### Lock Conflict Resolution

When a lock is held by another PR:

1. Plan shows warning: "Locked by PR #42"
2. Wait for other PR to sync/unlock/close
3. Or use `lemuria unlock` on the other PR
4. Or force unlock via the web UI (admin only)

---

## Application Detection

Lemuria automatically detects three types of application changes in a PR:

### Existing Applications

Applications that already exist in Argo CD and are affected by manifest changes:

```markdown
### Application: `my-app`

**Changes:** 1 to create, 2 to update

<details>
<summary>Diff (3 resources changed)</summary>
...
</details>

**Status:** Locked by this PR
```

### New Applications

Application CRs being added in the PR (the Application doesn't exist in Argo CD yet):

```markdown
### Application: `new-app` 🆕

**New application** - will be created when the Application CR is applied

**Source file:** `apps/new-app.yaml`
```

### Deleted Applications

Application CRs being removed in the PR:

```markdown
### Application: `old-app` 🗑️

**Application will be deleted** when the Application CR is removed

**Source file:** `apps/old-app.yaml`
```

### Detection Logic

Lemuria uses two complementary strategies:

1. **Path-based detection** (`.lemuria.yaml`): Matches changed files against configured path patterns. This is the primary method when a repo config exists.

2. **Source path detection** (fallback): Checks if changed files fall within an Argo CD application's configured `source.path`.

For Application CR changes specifically:
1. **Added files**: Parse for new Application CRs
2. **Removed files**: Parse from base branch to find deleted Applications
3. **Modified files**: Compare base and head to detect added/removed/modified Applications within the file

---

## Multiple Applications

A single PR can affect multiple applications.

### Example

```yaml
# .lemuria.yaml
applications:
  - name: frontend
    paths:
      - "apps/frontend/**"
      - "base/**"

  - name: backend
    paths:
      - "apps/backend/**"
      - "base/**"
```

PR changes `base/configmap.yaml`:

```markdown
## Lemuria Plan

### Application: `frontend`
**Changes:** 1 to update

### Application: `backend`
**Changes:** 1 to update

---
To apply: comment `lemuria sync`
```

`lemuria sync` deploys both applications.

---

## Revision Tracking

Lemuria tracks revisions to ensure consistency between plan and sync.

### Plan Revision

- Stored in the Redis lock when plan is generated (PR HEAD commit SHA)
- Must match current PR HEAD for sync to proceed
- Prevents deploying outdated plans

### Stale Plan Detection

```
1. Plan generated at commit abc123
2. New commit pushed (def456)
3. Sync attempted
4. ❌ "Plan is stale. Please run lemuria plan again."
```

When autoplan is enabled and new commits are pushed, a new plan automatically runs and previous plan comments are marked as stale.

### Sync Revision

- For apps sourcing from the PR repo: sync deploys the specific PR commit SHA
- For apps with external sources: sync uses the app's configured revision
- This allows "deploy before merge"

---

## Deploy Before Merge

Lemuria's key feature is deploying changes before merging the PR.

### How It Works

```
App targetRevision: main (abc123)
PR head: feature-branch (def456)

lemuria sync → Deploys def456 (not main)
                    │
                    ▼
App now running code from feature-branch
                    │
                    ▼
Verify deployment works
                    │
                    ▼
Merge PR → main now includes feature-branch
                    │
                    ▼
App targetRevision still main (now def456)
```

### Why This Works

Argo CD's sync API accepts a `revision` parameter that overrides the app's `targetRevision` for that specific sync operation. Lemuria uses this to sync to the PR's HEAD commit SHA.

---

## Rollback Flow

If issues are discovered after sync:

```
lemuria sync → Deploys PR changes
       │
       ▼
Issues discovered
       │
       ▼
lemuria rollback → Syncs back to targetRevision (main)
       │
       ▼
App restored to pre-PR state
       │
       ▼
Fix issues, push new commits
       │
       ▼
lemuria plan → New plan generated
       │
       ▼
lemuria sync → Deploy fixes
```

---

## Approval Workflow

When `require_approval: true`:

```
┌─────────────────────────────────────────────────────────────┐
│  1. PR opened → Auto-plan runs                              │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│  2. lemuria sync attempted                                  │
│     ❌ "PR must be approved before sync"                    │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│  3. Reviewer approves PR                                    │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│  4. lemuria sync succeeds                                   │
└─────────────────────────────────────────────────────────────┘
```

### Approval Precedence

Approval requirements are resolved in this order (highest priority first):

1. **Per-app sync requirements** (`.lemuria.yaml` `sync_requirements` section, exact match first, then wildcard)
2. **Repository-level** (`.lemuria.yaml` top-level `require_approval`)
3. **Server defaults** (`defaults.require_approval` in `lemuria.yaml`)

This means you can set a global default of `require_approval: false` but override it for specific production apps.

---

## Auto-Merge Workflow

When `auto_merge: true`:

```
┌─────────────────────────────────────────────────────────────┐
│  1. lemuria sync completes successfully                     │
│     - All applications synced                               │
│     - No errors                                             │
│     - Not a dry-run                                         │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│  2. Lemuria auto-merges the PR                              │
│     - Uses configured merge_method (squash/merge/rebase)    │
│     - Optionally deletes source branch                      │
└─────────────────────────────────────────────────────────────┘
```

### Auto-Merge Conditions

Auto-merge only triggers when:
- All syncs succeed (no errors, all phases are `Succeeded`)
- Not a dry-run
- `auto_merge: true` is enabled

### Auto-Merge Precedence

Auto-merge is resolved in this order (highest priority first):

1. **Repository-level** (`.lemuria.yaml` top-level `auto_merge`)
2. **Server defaults** (`defaults.auto_merge` in `lemuria.yaml`)

This means you can keep auto-merge disabled globally but enable it for specific repositories, or vice versa.

### Protected Branches

These branches are never auto-deleted (even when `delete_source_branch: true`):
- `main`
- `master`
- `develop`
- `development`

---

## Webhook Events

Lemuria responds to these GitHub webhook events:

| Event | Action | Lemuria Behavior |
|-------|--------|------------------|
| `pull_request` | `opened` | Auto-plan (if enabled) |
| `pull_request` | `synchronize` | Auto-plan (if enabled) |
| `pull_request` | `closed` | Release all locks for the PR |
| `issue_comment` | `created` | Parse and execute command |
| `pull_request_review` | `submitted` | (used for approval checks) |

### Async Processing

All webhook events are processed **asynchronously**. Lemuria returns `200 OK` immediately after validating the signature and parsing the event, then spawns a goroutine for actual processing. This prevents GitHub webhook timeouts.

### Repository Allowlist

Before processing, Lemuria checks if the repository is in the configured `allowed_repos` list. If the list is empty, all repositories are allowed.

---

## Best Practices

### 1. Use Path Patterns

Define specific path patterns in `.lemuria.yaml`:

```yaml
applications:
  - name: frontend
    paths:
      - "apps/frontend/**"    # Only frontend files
```

### 2. Require Approval for Production

```yaml
sync_requirements:
  - name: "prod-*"
    require_approval: true
```

### 3. Review Before Sync

Always review the plan diff before syncing, especially for:
- Resource deletions
- Replica changes
- Image updates

### 4. Use Dry-Run for Verification

```
lemuria sync --dry-run
```

### 5. Keep PRs Focused

Smaller PRs are easier to review and safer to deploy.

### 6. Monitor Auto-Merge

If using `auto_merge`, ensure your CI pipeline and branch protection rules are properly configured.

---

## Troubleshooting Workflow Issues

### Plan Not Triggered

1. Check `autoplan: true` in server config
2. Verify `.lemuria.yaml` exists in the repo root
3. Check path patterns match the changed files
4. Verify the repo is in `allowed_repos` (or the list is empty)

### Sync Blocked

1. Check approval status (`require_approval` at server, repo, or per-app level)
2. Re-run plan if stale (new commits since last plan)
3. Resolve merge conflicts
4. Disable auto-sync on the Argo CD application

### Lock Not Released

1. PR might still be open
2. Use `lemuria unlock`
3. Check Redis connectivity
4. Force unlock via web UI (admin only)
5. Wait for 7-day TTL expiry

---

## Next Steps

- [Commands](commands) - Command reference
- [Configuration](configuration) - Customize workflow
- [Troubleshooting](troubleshooting) - Common issues
