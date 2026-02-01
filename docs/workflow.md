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
3. GitHub sends webhook to Lemuria

### Phase 2: Auto-Plan

When `autoplan: true` (default):

1. Lemuria receives the PR webhook
2. Identifies affected applications:
   - Matches changed files against `.lemuria.yaml` path patterns
   - Or checks Argo CD applications that reference the repo
   - Detects new Application CRs added in the PR
   - Detects Application CRs being deleted in the PR
3. For each affected application:
   - Attempts to acquire lock
   - Generates diff (target state vs live state)
   - Stores plan revision for later verification
4. Posts plan results as PR comment

### Phase 3: Review

Team reviews the plan:

1. Verify expected changes in the diff
2. Look for unintended modifications
3. Approve the PR (if `require_approval: true`)

### Phase 4: Sync

Developer comments `lemuria sync`:

1. Lemuria verifies requirements:
   - PR is approved (if required)
   - Plan is not stale
   - PR is mergeable (no conflicts)
   - Auto-sync is disabled on applications
2. Syncs each application to PR commit SHA
3. Releases locks on successful sync
4. Posts sync results

### Phase 5: Merge

After successful sync:

- **Manual merge**: Developer merges the PR
- **Auto-merge**: Lemuria merges if `auto_merge: true`

---

## Locking

Lemuria uses distributed locking to prevent concurrent modifications.

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
| `lemuria unlock` | Release lock |
| PR merged | Auto-release all locks |
| PR closed | Auto-release all locks |

### Lock Conflict Resolution

When a lock is held by another PR:

1. Plan shows warning: "Locked by PR #42"
2. Wait for other PR to sync/unlock/close
3. Or use `lemuria unlock` on the other PR

---

## Application Detection

Lemuria automatically detects three types of application changes in a PR:

### Existing Applications

Applications that already exist in Argo CD and are affected by manifest changes:

```markdown
### Application: `my-app`

📋 **Changes:** 1 to create, 2 to update

<details>
<summary>Diff (3 resources changed)</summary>
...
</details>

**Status:** 🔒 Locked by this PR
```

### New Applications

Application CRs being added in the PR (the Application doesn't exist in Argo CD yet):

```markdown
### Application: `new-app` 🆕

➕ **New application** - will be created when the Application CR is applied

**Source file:** `apps/new-app.yaml`

ℹ️ Lemuria cannot generate a diff for new applications until they exist in Argo CD.
```

### Deleted Applications

Application CRs being removed in the PR:

```markdown
### Application: `old-app` 🗑️

➖ **Application will be deleted** when the Application CR is removed

**Source file:** `apps/old-app.yaml`

⚠️ All resources managed by this application will be orphaned or pruned depending on the deletion policy.
```

### Detection Logic

Lemuria parses changed YAML files to find Argo CD Application CRs:

1. **Added files**: Parse for new Application CRs
2. **Removed files**: Parse from base branch to find deleted Applications
3. **Modified files**: Compare base and head to detect added/removed Applications within the file

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
📋 **Changes:** 1 to update

### Application: `backend`
📋 **Changes:** 1 to update

---
To apply: comment `lemuria sync`
```

`lemuria sync` deploys both applications.

---

## Revision Tracking

Lemuria tracks revisions to ensure consistency.

### Plan Revision

- Stored when plan is generated
- Must match current PR head for sync
- Prevents deploying outdated plans

### Stale Plan Detection

```
1. Plan generated at commit abc123
2. New commit pushed (def456)
3. Sync attempted
4. ❌ "Plan is stale. Please run lemuria plan again."
```

### Sync Revision

- Sync deploys the specific PR commit SHA
- Not the app's configured targetRevision
- This allows "deploy before merge"

---

## Deploy Before Merge

Lemuria's key feature is deploying changes before merging.

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

Argo CD's sync API accepts a `revision` parameter that overrides the app's `targetRevision` for that specific sync operation.

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

---

## Auto-Merge Workflow

When `auto_merge: true`:

```
┌─────────────────────────────────────────────────────────────┐
│  1. lemuria sync completes successfully                     │
│     - All applications synced                               │
│     - No errors                                             │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│  2. Lemuria auto-merges the PR                              │
│     - Uses configured merge_method                          │
│     - Optionally deletes source branch                      │
└─────────────────────────────────────────────────────────────┘
```

### Auto-Merge Conditions

Auto-merge only triggers when:
- All syncs succeed
- Not a dry-run
- `auto_merge: true` in config

### Protected Branches

These branches are never auto-deleted:
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
| `pull_request` | `closed` | Release all locks |
| `issue_comment` | `created` | Parse and execute command |
| `pull_request_review` | `submitted` | (used for approval check) |

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

---

## Troubleshooting Workflow Issues

### Plan Not Triggered

1. Check `autoplan: true` in config
2. Verify `.lemuria.yaml` exists
3. Check path patterns match files

### Sync Blocked

1. Check approval status
2. Re-run plan if stale
3. Resolve merge conflicts
4. Disable auto-sync on app

### Lock Not Released

1. PR might still be open
2. Use `lemuria unlock`
3. Check Redis connectivity

---

## Next Steps

- [Commands](commands) - Command reference
- [Configuration](configuration) - Customize workflow
- [Troubleshooting](troubleshooting) - Common issues
