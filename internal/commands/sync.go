// Copyright 2026 Lemuria Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package commands

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/org/lemuria/internal/argocd"
	"github.com/org/lemuria/internal/config"
	"github.com/org/lemuria/internal/models"
	"github.com/org/lemuria/internal/vcs"
	"github.com/org/lemuria/pkg/diff"
)

// executeSync runs the sync command.
func (e *Executor) executeSync(ctx context.Context, cmd *Command, event *models.PREvent) error {
	slog.Debug("executing sync command",
		"repo", event.Repo.FullName,
		"pr", event.PR.Number,
		"specific_app", cmd.Application,
		"dry_run", cmd.DryRun,
		"prune", cmd.Prune,
	)

	// Add reaction to show we're working on it
	if event.Comment != nil {
		if err := e.vcs.AddReaction(ctx, event.Repo.Owner, event.Repo.Name, event.Comment.ID, "eyes"); err != nil {
			slog.Warn("failed to add reaction", "error", err)
		}
	}

	// Get locks held by this PR
	locks, err := e.lock.ListByPR(ctx, event.Repo.FullName, event.PR.Number)
	if err != nil {
		slog.Debug("failed to list locks",
			"error", err,
		)
		return e.postError(ctx, event, fmt.Errorf("listing locks: %w", err))
	}

	slog.Debug("found locks for PR",
		"count", len(locks),
		"pr", event.PR.Number,
	)

	if len(locks) == 0 {
		slog.Debug("no locks found for PR")
		return e.postComment(ctx, event, "", "## Lemuria Sync\n\n⚠️ No applications are locked by this PR. Run `lemuria plan` first.")
	}

	// Filter to specific application if requested
	if cmd.Application != "" {
		slog.Debug("filtering locks to specific application",
			"app", cmd.Application,
		)
		var filtered []models.Lock
		for _, l := range locks {
			if l.Application == cmd.Application {
				filtered = append(filtered, l)
			}
		}
		// If no direct match, try matching by ApplicationSetName
		if len(filtered) == 0 {
			slog.Debug("no direct lock match, trying applicationset name",
				"app", cmd.Application,
			)
			apps, err := e.argocd.GetApplicationsByApplicationSet(ctx, cmd.Application)
			if err != nil {
				return e.postError(ctx, event, fmt.Errorf("listing applications for applicationset %q: %w", cmd.Application, err))
			}
			appNames := make(map[string]struct{}, len(apps))
			for _, app := range apps {
				appNames[app.Name] = struct{}{}
			}
			for _, l := range locks {
				if _, ok := appNames[l.Application]; ok {
					filtered = append(filtered, l)
				}
			}
		}
		if len(filtered) == 0 {
			slog.Debug("application not locked by this PR",
				"app", cmd.Application,
			)
			return e.postComment(ctx, event, "", fmt.Sprintf("## Lemuria Sync\n\n⚠️ Application `%s` is not locked by this PR.", cmd.Application))
		}
		locks = filtered
	}

	// Check requirements before sync
	slog.Debug("checking sync requirements")
	if err := e.checkSyncRequirements(ctx, event, locks); err != nil {
		slog.Debug("sync requirements not met",
			"error", err,
		)
		return e.postComment(ctx, event, "", fmt.Sprintf("## Lemuria Sync\n\n❌ %s", err.Error()))
	}

	// Verify plans are not stale and check for auto-sync
	for _, l := range locks {
		slog.Debug("verifying plan freshness",
			"app", l.Application,
			"plan_revision", l.PlanRevision,
			"current_head_sha", event.PR.HeadSHA,
		)
		if l.PlanRevision != event.PR.HeadSHA {
			slog.Debug("plan is stale",
				"app", l.Application,
				"plan_revision", l.PlanRevision,
				"current_head_sha", event.PR.HeadSHA,
			)
			return e.postComment(ctx, event, "", fmt.Sprintf("## Lemuria Sync\n\n⚠️ Plan for `%s` is stale. Please run `lemuria plan` again.", l.Application))
		}

		// Check if auto-sync is enabled
		app, err := e.argocd.GetApplication(ctx, l.Application)
		if err != nil {
			slog.Debug("failed to get application",
				"app", l.Application,
				"error", err,
			)
			return e.postError(ctx, event, fmt.Errorf("getting application %s: %w", l.Application, err))
		}
		if app.HasAutoSync() {
			slog.Debug("application has auto-sync enabled",
				"app", l.Application,
			)
			return e.postComment(ctx, event, "", fmt.Sprintf("## Lemuria Sync\n\n❌ Application `%s` has auto-sync enabled.\n\nDisable auto-sync before using Lemuria to prevent conflicts.", l.Application))
		}
	}

	// Build app names for tracker
	appNames := make([]string, len(locks))
	for i, l := range locks {
		appNames[i] = l.Application
	}

	// Create progressive comment tracker
	tracker := newSyncCommentTracker(e.vcs, event, appNames)

	// Post initial progress comment
	tracker.postInitial(ctx)

	// Sync each application
	slog.Debug("starting sync for applications",
		"count", len(locks),
	)
	results := make([]syncResult, len(locks))
	for i, l := range locks {
		slog.Debug("syncing application",
			"app", l.Application,
		)
		results[i] = e.syncApplication(ctx, l, cmd, event)
		tracker.updateResult(ctx, i, results[i])
	}

	// Check if all syncs succeeded
	allSucceeded := true
	for _, r := range results {
		if r.Error != nil || (r.Result != nil && r.Result.Phase != models.SyncPhaseSucceeded) {
			allSucceeded = false
			break
		}
	}

	slog.Debug("sync completed",
		"all_succeeded", allSucceeded,
		"results_count", len(results),
	)

	// Auto-merge if enabled and all syncs succeeded (not dry-run)
	// Resolution order: repo config (.lemuria.yaml) > server defaults
	autoMerge := e.config.Defaults.AutoMerge
	repoConfig := e.getRepoConfig(ctx, event)
	if repoConfig != nil && repoConfig.AutoMerge != nil {
		autoMerge = *repoConfig.AutoMerge
	}
	slog.Debug("auto-merge resolved",
		"auto_merge", autoMerge,
		"all_succeeded", allSucceeded,
	)

	if allSucceeded && !cmd.DryRun && autoMerge {
		slog.Debug("attempting auto-merge",
			"repo", event.Repo.FullName,
			"pr", event.PR.Number,
		)
		if err := e.autoMergePR(ctx, event); err != nil {
			slog.Warn("auto-merge failed", "error", err)
		}
	}

	// Post final results (update existing comment or fall back to new comment)
	output := e.renderSyncResults(results)
	return tracker.postFinal(ctx, output)
}

// syncResult holds the result of syncing a single application.
type syncResult struct {
	Application string
	Result      *models.SyncResult
	Error       error
	PlanOutput  string
	PlanDiffs   []models.PlanDiffEntry
}

// syncApplication triggers a sync for a single application.
func (e *Executor) syncApplication(ctx context.Context, l models.Lock, cmd *Command, event *models.PREvent) syncResult {
	slog.Debug("starting sync for application",
		"app", l.Application,
		"revision", event.PR.HeadSHA,
		"source_file", l.SourceFile,
		"prune", cmd.Prune,
		"dry_run", cmd.DryRun,
	)

	result := syncResult{
		Application: l.Application,
		PlanOutput:  l.PlanOutput,
		PlanDiffs:   l.PlanDiffs,
	}

	// Determine if the app sources from the PR repo
	app, err := e.argocd.GetApplication(ctx, l.Application)
	if err != nil {
		result.Error = fmt.Errorf("getting application %s: %w", l.Application, err)
		return result
	}

	repoURL := event.Repo.HTMLURL
	fromPRRepo := appSourcesFromRepo(*app, repoURL)

	// If the Application CR was modified in the PR, update the live app's spec
	// before syncing. This handles cases where the PR modifies Helm values,
	// chart versions, or other spec fields in the Application CR file.
	if l.SourceFile != "" {
		slog.Debug("updating application spec from PR head branch",
			"app", l.Application,
			"source_file", l.SourceFile,
		)

		headContent, err := e.vcs.GetFileContent(ctx, event.Repo.Owner, event.Repo.Name, l.SourceFile, event.PR.HeadRef)
		if err != nil {
			result.Error = fmt.Errorf("reading application CR from head branch: %w", err)
			return result
		}

		parsed, err := argocd.ParseRawApplicationFromYAML(headContent, l.Application)
		if err != nil {
			result.Error = fmt.Errorf("parsing application CR from head branch: %w", err)
			return result
		}

		if err := e.argocd.UpdateApplicationSpec(ctx, l.Application, parsed.Spec); err != nil {
			result.Error = fmt.Errorf("updating application spec: %w", err)
			return result
		}

		slog.Debug("application spec updated from PR",
			"app", l.Application,
		)
	}

	opts := &argocd.SyncOptions{
		Prune:   cmd.Prune,
		DryRun:  cmd.DryRun,
		Timeout: e.config.ArgoCD.SyncTimeout,
	}

	// Only set revision for apps sourcing from the PR repo.
	// External sources (e.g., Helm chart repos) should use the revision
	// already configured in the app spec.
	if fromPRRepo {
		opts.Revision = event.PR.HeadSHA
	}

	slog.Debug("triggering sync",
		"app", l.Application,
		"revision", opts.Revision,
		"from_pr_repo", fromPRRepo,
	)

	syncResult, err := e.argocd.SyncApplication(ctx, l.Application, opts)
	if err != nil {
		slog.Debug("sync failed",
			"app", l.Application,
			"error", err,
		)
		result.Error = err
		return result
	}

	result.Result = syncResult

	slog.Debug("sync completed",
		"app", l.Application,
		"phase", syncResult.Phase,
		"message", syncResult.Message,
	)

	// Fetch per-resource health and merge into sync result
	healthInfo, err := e.argocd.GetResourceHealth(ctx, l.Application)
	if err != nil {
		slog.Warn("failed to fetch resource health", "app", l.Application, "error", err)
	} else {
		mergeResourceHealth(syncResult, healthInfo)
	}

	// Release lock on successful sync (unless dry-run)
	if !cmd.DryRun && syncResult.Phase == models.SyncPhaseSucceeded {
		slog.Debug("releasing lock after successful sync",
			"app", l.Application,
		)
		if err := e.lock.Unlock(ctx, l.Application, event.Repo.FullName, event.PR.Number); err != nil {
			slog.Warn("failed to release lock after sync", "app", l.Application, "error", err)
		}
	}

	return result
}

// appSourcesFromRepo returns true if any of the app's sources reference the given repository URL.
func appSourcesFromRepo(app models.Application, repoURL string) bool {
	normalized := argocd.NormalizeRepoURL(repoURL)
	for _, u := range app.GetRepoURLs() {
		if argocd.NormalizeRepoURL(u) == normalized {
			return true
		}
	}
	return false
}

// checkSyncRequirements verifies all requirements are met before sync.
func (e *Executor) checkSyncRequirements(ctx context.Context, event *models.PREvent, locks []models.Lock) error {
	slog.Debug("checking sync requirements",
		"require_approval", e.config.Defaults.RequireApproval,
		"locks_count", len(locks),
	)

	// Load repo config for per-app sync requirements
	repoConfig := e.getRepoConfig(ctx, event)

	// Check if PR approval is required for any locked application
	requireApproval := e.isApprovalRequired(repoConfig, locks)
	slog.Debug("approval requirement resolved",
		"require_approval", requireApproval,
	)

	if requireApproval {
		slog.Debug("checking PR approval status")
		approved, err := e.vcs.IsPRApproved(ctx, event.Repo.Owner, event.Repo.Name, event.PR.Number)
		if err != nil {
			slog.Debug("failed to check PR approval",
				"error", err,
			)
			return fmt.Errorf("checking PR approval: %w", err)
		}
		slog.Debug("PR approval status",
			"approved", approved,
		)
		if !approved {
			return fmt.Errorf("PR must be approved before sync")
		}
	}

	// Check if PR is mergeable
	slog.Debug("checking PR mergeable status")
	pr, err := e.vcs.GetPR(ctx, event.Repo.Owner, event.Repo.Name, event.PR.Number)
	if err != nil {
		slog.Debug("failed to get PR status",
			"error", err,
		)
		return fmt.Errorf("getting PR status: %w", err)
	}

	mergeable := pr.Mergeable
	slog.Debug("PR mergeable status",
		"mergeable", mergeable,
		"state", pr.State,
	)

	if !mergeable {
		return fmt.Errorf("PR has merge conflicts, please resolve before syncing")
	}

	slog.Debug("all sync requirements met")
	return nil
}

// isApprovalRequired checks if any of the locked applications require PR approval.
// Resolution order (highest priority first): sync_requirements per-app > repo config top-level > server defaults.
func (e *Executor) isApprovalRequired(repoConfig *config.RepoConfig, locks []models.Lock) bool {
	for _, l := range locks {
		if e.appRequiresApproval(repoConfig, l.Application) {
			return true
		}
	}
	return false
}

// appRequiresApproval determines if a specific application requires PR approval for sync.
func (e *Executor) appRequiresApproval(repoConfig *config.RepoConfig, appName string) bool {
	// Start with server default
	requireApproval := e.config.Defaults.RequireApproval

	if repoConfig != nil {
		// Override with repo config top-level setting
		if repoConfig.RequireApproval != nil {
			requireApproval = *repoConfig.RequireApproval
		}

		// Check sync_requirements for per-app override (exact match first, then wildcard)
		for _, req := range repoConfig.SyncRequirements {
			if req.Name == appName {
				slog.Debug("sync requirement exact match",
					"app", appName,
					"require_approval", req.RequireApproval,
				)
				return req.RequireApproval
			}
		}
		for _, req := range repoConfig.SyncRequirements {
			if req.Name != appName && matchAppName(req.Name, appName) {
				slog.Debug("sync requirement wildcard match",
					"app", appName,
					"pattern", req.Name,
					"require_approval", req.RequireApproval,
				)
				return req.RequireApproval
			}
		}
	}

	return requireApproval
}

// renderSyncResults formats sync results as a markdown comment.
func (e *Executor) renderSyncResults(results []syncResult) string {
	entries := make([]diff.SyncResultEntry, len(results))
	for i, r := range results {
		entry := diff.SyncResultEntry{
			Application: r.Application,
			PlanOutput:  r.PlanOutput,
			PlanDiffs:   r.PlanDiffs,
			Error:       r.Error,
		}
		if r.Result != nil {
			entry.Phase = string(r.Result.Phase)
			entry.Message = r.Result.Message
			entry.Resources = r.Result.Resources
			entry.HealthStatus = string(r.Result.HealthStatus)
		}
		entries[i] = entry
	}
	return e.renderer.RenderSync(entries)
}

// autoMergePR merges the PR and optionally deletes the source branch.
func (e *Executor) autoMergePR(ctx context.Context, event *models.PREvent) error {
	// Determine merge method
	method := e.config.Defaults.MergeMethod
	if method == "" {
		method = "squash"
	}

	// Merge the PR
	if err := e.vcs.MergePullRequest(
		ctx,
		event.Repo.Owner,
		event.Repo.Name,
		event.PR.Number,
		event.PR.Title,
		"",
		method,
	); err != nil {
		return fmt.Errorf("merging PR: %w", err)
	}

	slog.Info("auto-merged PR",
		"repo", event.Repo.FullName,
		"pr", event.PR.Number,
		"method", method,
	)

	// Delete source branch if configured and not a protected branch
	if e.config.Defaults.DeleteSourceBranch {
		branch := event.PR.HeadRef
		if !IsProtectedBranch(branch) {
			if err := e.vcs.DeleteBranch(ctx, event.Repo.Owner, event.Repo.Name, branch); err != nil {
				slog.Warn("failed to delete source branch",
					"branch", branch,
					"error", err,
				)
			} else {
				slog.Info("deleted source branch", "branch", branch)
			}
		}
	}

	return nil
}

// IsProtectedBranch returns true if the branch should never be deleted.
func IsProtectedBranch(branch string) bool {
	protected := []string{"main", "master", "develop", "development"}
	for _, p := range protected {
		if branch == p {
			return true
		}
	}
	return false
}

// mergeResourceHealth merges per-resource health info into sync result resources.
func mergeResourceHealth(result *models.SyncResult, healthInfo []models.ResourceHealthInfo) {
	healthMap := make(map[string]models.ResourceHealthInfo, len(healthInfo))
	for _, h := range healthInfo {
		healthMap[h.Resource.String()] = h
	}
	for i := range result.Resources {
		key := result.Resources[i].Resource.String()
		if h, ok := healthMap[key]; ok {
			result.Resources[i].HealthStatus = h.HealthStatus
			result.Resources[i].HealthMessage = h.HealthMessage
		}
	}
}

// syncCommentTracker manages the lifecycle of a progressive sync comment.
type syncCommentTracker struct {
	mu         sync.Mutex
	vcs        vcs.Client
	event      *models.PREvent
	appNames   []string
	results    []syncResult
	completed  []bool
	commentID  int64
	lastUpdate time.Time
}

// minUpdateInterval is the minimum time between intermediate comment updates.
const minUpdateInterval = 1 * time.Minute

func newSyncCommentTracker(vcs vcs.Client, event *models.PREvent, appNames []string) *syncCommentTracker {
	return &syncCommentTracker{
		vcs:       vcs,
		event:     event,
		appNames:  appNames,
		results:   make([]syncResult, len(appNames)),
		completed: make([]bool, len(appNames)),
	}
}

// postInitial posts the initial progress comment showing all apps as "Waiting...".
func (t *syncCommentTracker) postInitial(ctx context.Context) {
	body := t.renderProgress()
	result, err := t.vcs.PostComment(ctx, t.event.Repo.Owner, t.event.Repo.Name, t.event.PR.Number, body, false)
	if err != nil {
		slog.Warn("failed to post initial sync comment", "error", err)
		return
	}
	t.mu.Lock()
	t.commentID = result.ID
	t.lastUpdate = time.Now()
	t.mu.Unlock()
}

// updateResult records a result and throttled-updates the comment.
func (t *syncCommentTracker) updateResult(ctx context.Context, i int, result syncResult) {
	t.mu.Lock()
	t.results[i] = result
	t.completed[i] = true
	commentID := t.commentID
	shouldUpdate := commentID != 0 && time.Since(t.lastUpdate) >= minUpdateInterval
	t.mu.Unlock()

	if !shouldUpdate {
		return
	}

	body := t.renderProgress()
	if err := t.vcs.UpdateComment(ctx, t.event.Repo.Owner, t.event.Repo.Name, t.event.PR.Number, commentID, body); err != nil {
		slog.Warn("failed to update sync comment", "error", err)
		return
	}
	t.mu.Lock()
	t.lastUpdate = time.Now()
	t.mu.Unlock()
}

// postFinal updates the comment with the final rendered output, or falls back to PostComment.
func (t *syncCommentTracker) postFinal(ctx context.Context, body string) error {
	t.mu.Lock()
	commentID := t.commentID
	t.mu.Unlock()

	if commentID != 0 {
		if err := t.vcs.UpdateComment(ctx, t.event.Repo.Owner, t.event.Repo.Name, t.event.PR.Number, commentID, body); err != nil {
			slog.Warn("failed to update final sync comment, falling back to new comment", "error", err)
			_, err = t.vcs.PostComment(ctx, t.event.Repo.Owner, t.event.Repo.Name, t.event.PR.Number, body, false)
			return err
		}
		return nil
	}

	// Fallback: initial post failed, post a new comment
	_, err := t.vcs.PostComment(ctx, t.event.Repo.Owner, t.event.Repo.Name, t.event.PR.Number, body, false)
	return err
}

// renderProgress renders a progress table showing app statuses.
func (t *syncCommentTracker) renderProgress() string {
	t.mu.Lock()
	defer t.mu.Unlock()

	var sb strings.Builder
	sb.WriteString("## Lemuria Sync\n\n")
	sb.WriteString("⏳ **Sync in progress...**\n\n")
	sb.WriteString("| Application | Status |\n")
	sb.WriteString("|-------------|--------|\n")

	for i, name := range t.appNames {
		if t.completed[i] {
			r := t.results[i]
			if r.Error != nil {
				sb.WriteString(fmt.Sprintf("| `%s` | ❌ Error |\n", name))
			} else if r.Result != nil {
				switch r.Result.Phase {
				case models.SyncPhaseSucceeded:
					sb.WriteString(fmt.Sprintf("| `%s` | ✅ Succeeded |\n", name))
				case models.SyncPhaseFailed, models.SyncPhaseError:
					sb.WriteString(fmt.Sprintf("| `%s` | ❌ %s |\n", name, r.Result.Phase))
				default:
					sb.WriteString(fmt.Sprintf("| `%s` | ⏳ %s |\n", name, r.Result.Phase))
				}
			} else {
				sb.WriteString(fmt.Sprintf("| `%s` | ❌ Error |\n", name))
			}
		} else {
			sb.WriteString(fmt.Sprintf("| `%s` | ⏳ Waiting... |\n", name))
		}
	}

	return sb.String()
}
