# Gocyclo Reduction Plan

## Target: Reduce all production functions below cyclomatic complexity 15

### Functions to address (ordered by complexity):

| # | Function | File | Complexity | Target |
|---|----------|------|-----------|--------|
| 1 | `findAffectedApplications` | executor.go | 39 | <15 |
| 2 | `computeThreeWayDiff` | diff.go | 33 | <15 |
| 3 | `executeSync` | sync.go | 31 | <15 |
| 4 | `detectAppSetModifiedFile` | appdetect.go | 29 | <15 |
| 5 | `waitForSyncComplete` | applications.go | 23 | <15 |
| 6 | `renderAppPlan` | renderer.go | 20 | <15 |
| 7 | `computeLiveVsTargetDiff` | diff.go | 18 | <15 |
| 8 | `detectApplicationChanges` | appdetect.go | 18 | <15 |
| 9 | `planApplication` | plan.go | 17 | <15 |
| 10 | `validate` | loader.go | 17 | <15 |

Test functions are excluded (complexity in tests is acceptable).

---

## Plan Details

### 1. `findAffectedApplications` (39 → <15)

Extract the repeated "add-or-update in affected list" pattern into helpers:

- **`addNewAppsToAffected(affected, parsed.New, alreadyDetected)`** — Lines handling new app deduplication (check alreadyDetected, append or update ChangeType)
- **`addModifiedAppsToAffected(affected, parsed.Modified, changedFileSet, alreadyDetected)`** — Lines handling modified app processing (check if source file is in changed files, update or add)
- **`addDeletedAppsToAffected(affected, parsed.Deleted, alreadyDetected)`** — Lines handling deleted app deduplication
- **`processAppSetChanges(affected, appSetChanges, parsed, alreadyDetected)`** — Lines handling ApplicationSet CR change integration

Each helper follows the same signature pattern: takes `affected *[]models.Application`, mutates in place, returns nothing.

### 2. `computeThreeWayDiff` (33 → <15)

The branch diff and live diff blocks use identical conditional logic. Extract:

- **`computeStateDiff(stateA, stateB string, existsA, existsB bool) (string, DiffAction)`** — Shared helper for both branch and live diff computation. Handles the 4-way conditional: create (A missing), delete (B missing), update (both exist, content differs), no-op (both exist, same content).
- **`collectAllResourceKeys(baseMaps, targetMaps, liveMaps)`** — Consolidate the key collection + deduplication into one function.
- **`filterAndSortDiffs(diffs []ManifestDiff) []ManifestDiff`** — Extract the final filtering (skip empty diffs) and sorting.

### 3. `executeSync` (31 → <15)

Extract two self-contained blocks:

- **`filterSyncLocks(locks, cmd) ([]models.Lock, error)`** — Lines 73-109: lock filtering with direct match + ApplicationSet expansion fallback. Returns filtered locks or error.
- **`handlePostSync(ctx, event, cmd, locks, results, allSucceeded)`** — Lines 279-318: auto-merge, targetRevision revert, auto-sync restore, lock release. This is a cleanup routine that runs after all syncs complete.
- **`fetchAndPrepareSync(ctx, event, locks) (map[string][]byte, error)`** — Lines 156-175: batch fetch head source contents for all locked apps.

### 4. `detectAppSetModifiedFile` (29 → <15)

Extract:

- **`expandAndDiffAppSet(ctx, headPAS, basePAS) (*AppSetModification, error)`** — Lines handling the per-appset comparison: generate head apps, generate base apps, compute new/removed. This is the inner loop body that's ~50 lines.
- **`classifyAppSetApps(headApps, baseApps, appSetName, sourceFile) AppSetModification`** — The set-difference computation between head and base generated apps.

### 5. `waitForSyncComplete` (23 → <15)

Extract:

- **`shouldSkipStaleEvent(phase, healthStatus, skipFlag, syncStartedAt) bool`** — The stale event detection logic (~20 lines of conditionals).
- **`handleSyncEvent(event, timers) (result, done)`** — Extract the main event processing from the select loop into a method that returns whether to continue or exit, and the final result.

### 6. `renderAppPlan` (20 → <15)

Extract the type-specific rendering blocks:

- **`renderNewAppPlan(sb, result)`** — Lines for new app rendering (source file, summary, diffs, lock).
- **`renderDeletedAppPlan(sb, result)`** — Lines for deleted app rendering.

The existing app path stays inline since it's the shortest branch.

### 7. `computeLiveVsTargetDiff` (18 → <15)

Extract:

- **`buildResourceMaps(targetManifests, liveResources) (targetMap, liveMap)`** — Map building with hook/secret filtering consolidated.
- **`processResourcePair(targetState, liveState string, exists bool) ManifestDiff`** — Per-resource diff computation extracted from the loop body.

### 8. `detectApplicationChanges` (18 → <15)

Extract:

- **`buildExistingAppMap(existingApps) map[string]bool`** — Fetch + map building extracted.
- **`verifyAndFilterChanges(parsed, existingByName)`** — Consolidate `verifyNewAppsExist` + `verifyDeletedAppsExist` calls.

### 9. `planApplication` (17 → <15)

Extract:

- **`lockAndStorePlan(ctx, app, event, revision, sourceFile, planOutput, diffs)`** — The repeated lock acquisition + plan storage pattern used for new, deleted, and existing apps.

### 10. `validate` (17 → <15)

Extract:

- **`validateGitHubConfig(cfg) []string`** — GitHub-specific validation returning errors.
- **`validateGitLabConfig(cfg) []string`** — GitLab-specific validation.
- **`validateArgoCDConfig(cfg) []string`** — ArgoCD validation.

---

## Principles

1. **Pure extractions only** — No behavioral changes. Every extraction must produce identical output.
2. **Preserve test coverage** — All existing tests must pass without modification (unless they directly tested removed helper functions).
3. **No new types unless necessary** — Prefer free functions or methods on existing types.
4. **Keep extracted functions in the same file** — Don't create new files; keep locality.
5. **Run `make test && make lint` after each function is refactored.**
