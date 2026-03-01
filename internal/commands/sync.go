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
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	v1alpha1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
	"golang.org/x/sync/errgroup"

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

	// Verify plans are not stale
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
	}

	// Disable auto-sync on applications that have it enabled (skip for dry-run)
	if !cmd.DryRun {
		if err := e.disableAutoSyncForLocks(ctx, locks, event); err != nil {
			return e.postError(ctx, event, fmt.Errorf("disabling auto-sync: %w", err))
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

	// Batch-fetch Application CR source files before per-app sync loop
	sourcePathSet := make(map[string]struct{})
	for _, l := range locks {
		if l.SourceFile != "" {
			sourcePathSet[l.SourceFile] = struct{}{}
		}
	}
	sourcePaths := setToSlice(sourcePathSet)

	var headSourceContents map[string][]byte
	if len(sourcePaths) > 0 {
		var fetchErr error
		headSourceContents, fetchErr = e.vcs.GetFileContents(ctx, event.Repo.Owner, event.Repo.Name, sourcePaths, event.PR.HeadRef)
		if fetchErr != nil {
			slog.Warn("failed to batch-fetch source files at head ref for sync", "error", fetchErr)
			headSourceContents = map[string][]byte{}
		}
	} else {
		headSourceContents = map[string][]byte{}
	}

	// Resolve skip_no_changes setting: repo config (.lemuria.yaml) > server defaults
	skipNoChanges := e.config.Defaults.SkipNoChanges
	repoConfigForSkip := e.getRepoConfig(ctx, event)
	if repoConfigForSkip != nil && repoConfigForSkip.SkipNoChanges != nil {
		skipNoChanges = *repoConfigForSkip.SkipNoChanges
	}

	// Determine which apps to sync — only leaf apps are synced during
	// `lemuria sync`. Parent apps that manage other locked apps are skipped
	// because syncing them could reconcile child Application CRs and undo
	// the child's PR-specific targetRevision override. Parents will reconcile
	// naturally when the PR is merged and their auto-sync is restored.
	slog.Debug("starting sync for applications",
		"count", len(locks),
		"skip_no_changes", skipNoChanges,
	)
	results := make([]syncResult, len(locks))
	syncIndices, skippedParents := e.computeSyncTargets(ctx, locks)

	// Mark skipped parent apps in results
	for _, i := range skippedParents {
		l := locks[i]
		results[i] = syncResult{
			Application: l.Application,
			PlanOutput:  l.PlanOutput,
			PlanDiffs:   l.PlanDiffs,
			Result: &models.SyncResult{
				Application:  l.Application,
				Phase:        models.SyncPhaseSucceeded,
				Message:      "Skipped — parent app will reconcile on PR merge",
				HealthStatus: models.HealthStatusUnknown,
			},
		}
		tracker.updateResult(ctx, i, results[i])
	}

	// Sync leaf apps in parallel (bounded concurrency)
	g := new(errgroup.Group)
	g.SetLimit(10)
	for _, i := range syncIndices {
		l := locks[i]
		// Skip applications with no detected changes if configured.
		// A no-op plan stores PlanOutput="" with zero diffs, or
		// PlanOutput="No changes detected" (from formatPlanSummary fallback).
		if skipNoChanges && len(l.PlanDiffs) == 0 &&
			(l.PlanOutput == "No changes detected" || l.PlanOutput == "") {
			slog.Debug("skipping application with no changes",
				"app", l.Application,
			)
			results[i] = syncResult{
				Application: l.Application,
				PlanOutput:  l.PlanOutput,
				PlanDiffs:   l.PlanDiffs,
				Result: &models.SyncResult{
					Application:  l.Application,
					Phase:        models.SyncPhaseSucceeded,
					Message:      "Skipped — no changes detected",
					HealthStatus: models.HealthStatusUnknown,
				},
			}
			tracker.updateResult(ctx, i, results[i])
			continue
		}

		idx, lock := i, l
		g.Go(func() error {
			slog.Debug("syncing application",
				"app", lock.Application,
			)
			results[idx] = e.syncApplication(ctx, lock, cmd, event, headSourceContents)
			tracker.updateResult(ctx, idx, results[idx])
			return nil
		})
	}
	g.Wait() //nolint:errcheck

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
		} else {
			// Auto-merge succeeded. Revert targetRevision back to the base
			// branch for apps where we rewrote it during sync — during sync
			// we set it to the PR SHA, but now that the PR is merged the
			// files exist on the base branch.
			for i, l := range locks {
				if l.ChangeType == models.ApplicationNew || results[i].TargetRevRewritten {
					e.revertTargetRevision(ctx, l, event)
				}
			}

			// Restore auto-sync before releasing locks. Re-fetch locks
			// to get the updated auto-sync state stored during disableAutoSyncForLocks.
			updatedLocks, err := e.lock.ListByPR(ctx, event.Repo.FullName, event.PR.Number)
			if err != nil {
				slog.Warn("failed to list locks for auto-sync restore", "error", err)
			} else {
				restoreAllAutoSync(ctx, e.argocd, e.lock, updatedLocks, event.Repo.FullName, event.PR.Number)
			}

			// Release all locks after successful auto-merge.
			// If auto-merge is disabled or fails, locks persist until
			// the PR is closed/merged (cleaned up by UnlockAll).
			if updatedLocks == nil {
				updatedLocks = locks
			}
			for _, l := range updatedLocks {
				if err := e.lock.Unlock(ctx, l.Application, event.Repo.FullName, event.PR.Number); err != nil {
					slog.Warn("failed to release lock after auto-merge", "app", l.Application, "error", err)
				}
			}
		}
	}

	// Post final results (update existing comment or fall back to new comment)
	output := e.renderSyncResults(results)
	return tracker.postFinal(ctx, output)
}

// syncResult holds the result of syncing a single application.
type syncResult struct {
	Application        string
	Result             *models.SyncResult
	Error              error
	PlanOutput         string
	PlanDiffs          []models.PlanDiffEntry
	TargetRevRewritten bool // true if targetRevision was rewritten in the app spec during sync
}

// syncApplication triggers a sync for a single application.
func (e *Executor) syncApplication(ctx context.Context, l models.Lock, cmd *Command, event *models.PREvent, headContents map[string][]byte) syncResult {
	slog.Debug("starting sync for application",
		"app", l.Application,
		"revision", event.PR.HeadSHA,
		"source_file", l.SourceFile,
		"change_type", l.ChangeType,
		"prune", cmd.Prune,
		"dry_run", cmd.DryRun,
	)

	// Handle new apps — create in ArgoCD then sync
	if l.ChangeType == models.ApplicationNew {
		return e.syncNewApplication(ctx, l, cmd, event, headContents)
	}

	// Handle deleted apps — just release the lock (deletion happens on merge)
	if l.ChangeType == models.ApplicationDeleted {
		return e.syncDeletedApplication(ctx, l, cmd, event)
	}

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

	// If the Application CR was modified in the PR, parse it so we can update
	// the live app's spec before syncing. This handles cases where the PR
	// modifies Helm values, chart versions, or other spec fields.
	var parsed *v1alpha1.Application
	if l.SourceFile != "" {
		slog.Debug("updating application spec from PR head branch",
			"app", l.Application,
			"source_file", l.SourceFile,
		)

		headContent, ok := headContents[l.SourceFile]
		if !ok {
			result.Error = fmt.Errorf("application CR %s not found in pre-fetched head contents", l.SourceFile)
			return result
		}

		parsed, err = argocd.ParseRawApplicationFromYAML(headContent, l.Application)
		if err != nil {
			result.Error = fmt.Errorf("parsing application CR from head branch: %w", err)
			return result
		}
	}

	opts := &argocd.SyncOptions{
		Prune:   cmd.Prune,
		DryRun:  cmd.DryRun,
		Timeout: e.config.ArgoCD.SyncTimeout,
	}

	if fromPRRepo {
		var rawApp *v1alpha1.Application
		if parsed != nil {
			rawApp = parsed
		} else {
			rawApp, err = e.argocd.GetApplicationRaw(ctx, l.Application)
			if err != nil {
				result.Error = fmt.Errorf("getting raw application %s: %w", l.Application, err)
				return result
			}
		}

		if len(rawApp.Spec.Sources) > 0 && !cmd.DryRun {
			// Multi-source: rewrite targetRevisions in the spec and update,
			// then sync without revision overrides to avoid ArgoCD bug
			// with multi-source revision resolution.
			// Skipped in dry-run mode to avoid mutating the live app spec.
			rewriteTargetRevision(rawApp, repoURL, event.PR.HeadSHA)
			result.TargetRevRewritten = true

			if err := e.argocd.UpdateApplicationSpec(ctx, l.Application, rawApp.Spec); err != nil {
				result.Error = fmt.Errorf("updating application spec for multi-source sync: %w", err)
				return result
			}
		} else if len(rawApp.Spec.Sources) == 0 {
			// Single-source: use revision override directly.
			// Update the spec first if we parsed a SourceFile.
			if parsed != nil && !cmd.DryRun {
				if err := e.argocd.UpdateApplicationSpec(ctx, l.Application, parsed.Spec); err != nil {
					result.Error = fmt.Errorf("updating application spec: %w", err)
					return result
				}
			}
			opts.Revision = event.PR.HeadSHA
		}
	} else if parsed != nil && !cmd.DryRun {
		// App doesn't source from PR repo but SourceFile was modified —
		// still update the spec. Skipped in dry-run mode.
		if err := e.argocd.UpdateApplicationSpec(ctx, l.Application, parsed.Spec); err != nil {
			result.Error = fmt.Errorf("updating application spec: %w", err)
			return result
		}
	}

	slog.Debug("triggering sync",
		"app", l.Application,
		"revision", opts.Revision,
		"from_pr_repo", fromPRRepo,
		"target_rev_rewritten", result.TargetRevRewritten,
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

	return result
}

// syncNewApplication creates a new app in ArgoCD and syncs it.
func (e *Executor) syncNewApplication(ctx context.Context, l models.Lock, cmd *Command, event *models.PREvent, headContents map[string][]byte) syncResult {
	result := syncResult{
		Application: l.Application,
		PlanOutput:  l.PlanOutput,
		PlanDiffs:   l.PlanDiffs,
	}

	if l.SourceFile == "" {
		result.Error = fmt.Errorf("new application %s has no source file", l.Application)
		return result
	}

	headContent, ok := headContents[l.SourceFile]
	if !ok {
		result.Error = fmt.Errorf("application CR %s not found in pre-fetched head contents", l.SourceFile)
		return result
	}

	parsed, err := argocd.ParseRawApplicationFromYAML(headContent, l.Application)
	if err != nil {
		result.Error = fmt.Errorf("parsing application CR from head branch: %w", err)
		return result
	}

	// Rewrite targetRevision for sources from the PR repo so ArgoCD
	// validates against the PR branch (files don't exist on main yet)
	rewriteTargetRevision(parsed, event.Repo.HTMLURL, event.PR.HeadSHA)

	// Create the app in ArgoCD; if it already exists (e.g., from a prior root-app sync),
	// update its spec instead
	if err := e.argocd.CreateApplication(ctx, parsed); err != nil {
		slog.Debug("create failed, trying update",
			"app", l.Application,
			"error", err,
		)
		if updateErr := e.argocd.UpdateApplicationSpec(ctx, l.Application, parsed.Spec); updateErr != nil {
			result.Error = fmt.Errorf("creating application %s: %w (update also failed: %v)", l.Application, err, updateErr)
			return result
		}
	}

	// For new apps, rewriteTargetRevision already set the correct
	// targetRevision in the spec for each source (commit hash for git
	// sources, original version for Helm charts). We sync without
	// revision overrides so ArgoCD uses the spec's targetRevisions
	// directly, avoiding issues with multi-source revision resolution.
	opts := &argocd.SyncOptions{
		Prune:   cmd.Prune,
		DryRun:  cmd.DryRun,
		Timeout: e.config.ArgoCD.SyncTimeout,
	}

	slog.Debug("syncing new application",
		"app", l.Application,
	)

	syncRes, err := e.argocd.SyncApplication(ctx, l.Application, opts)
	if err != nil {
		result.Error = err
		return result
	}

	result.Result = syncRes

	// Fetch per-resource health
	healthInfo, err := e.argocd.GetResourceHealth(ctx, l.Application)
	if err != nil {
		slog.Warn("failed to fetch resource health", "app", l.Application, "error", err)
	} else {
		mergeResourceHealth(syncRes, healthInfo)
	}

	return result
}

// rewriteTargetRevision updates targetRevision for sources matching the given
// repo URL. This is needed when creating new apps from a PR branch — the
// Application CR typically has targetRevision: main, but the referenced files
// (values, manifests) only exist on the PR branch until merge.
func rewriteTargetRevision(app *v1alpha1.Application, repoURL, revision string) {
	normalized := argocd.NormalizeRepoURL(repoURL)
	if app.Spec.Source != nil && argocd.NormalizeRepoURL(app.Spec.Source.RepoURL) == normalized {
		app.Spec.Source.TargetRevision = revision
	}
	for i := range app.Spec.Sources {
		if argocd.NormalizeRepoURL(app.Spec.Sources[i].RepoURL) == normalized {
			app.Spec.Sources[i].TargetRevision = revision
		}
	}
}

// syncDeletedApplication releases the lock for a deleted app.
// The actual deletion happens when the PR is merged and the parent app syncs.
func (e *Executor) syncDeletedApplication(ctx context.Context, l models.Lock, cmd *Command, event *models.PREvent) syncResult {
	result := syncResult{
		Application: l.Application,
		PlanOutput:  l.PlanOutput,
		PlanDiffs:   l.PlanDiffs,
	}

	slog.Debug("handling deleted application sync — releasing lock only",
		"app", l.Application,
	)

	// For deleted apps, there's nothing to sync — the app CR removal
	// takes effect when the PR is merged and the parent app syncs to the new branch
	result.Result = &models.SyncResult{
		Application:  l.Application,
		Phase:        models.SyncPhaseSucceeded,
		Message:      "Application will be deleted when the PR is merged.",
		HealthStatus: models.HealthStatusHealthy,
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
	maxSize := e.vcs.MaxCommentSize()
	return e.renderer.RenderSync(entries, maxSize)
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

// computeSyncTargets returns the lock indices of leaf apps (apps that do not
// manage other locked apps). Parent/grandparent apps in the locked set are
// excluded from sync — they will reconcile naturally when the PR is merged
// and their auto-sync is restored. This prevents parent apps from overwriting
// child app targetRevision overrides during sync.
//
// Locks with IsParentApp=true are always excluded (they are locked only for
// auto-sync management, not for syncing).
//
// If managed-resource detection fails for all apps, falls back to returning
// all non-IsParentApp lock indices (same as pre-wave behavior).
func (e *Executor) computeSyncTargets(ctx context.Context, locks []models.Lock) (syncIndices []int, skippedParents []int) {
	// Build set of candidate lock indices — exclude IsParentApp locks
	type appInfo struct {
		lockIdx int
		name    string
	}
	var candidates []appInfo
	nameToIdx := make(map[string]int) // app name → index in candidates slice
	for i, l := range locks {
		if l.IsParentApp {
			continue
		}
		nameToIdx[l.Application] = len(candidates)
		candidates = append(candidates, appInfo{lockIdx: i, name: l.Application})
	}

	if len(candidates) == 0 {
		return nil, nil
	}

	// If only one app, no parent-child detection needed
	if len(candidates) == 1 {
		return []int{candidates[0].lockIdx}, nil
	}

	// Fetch managed resources for all candidate apps concurrently
	type managedResult struct {
		idx       int
		resources []argocd.ManagedResource
		err       error
	}
	results := make([]managedResult, len(candidates))
	g := new(errgroup.Group)
	g.SetLimit(10)
	for i, info := range candidates {
		idx, name := i, info.name
		g.Go(func() error {
			resources, err := e.argocd.GetManagedResources(ctx, name)
			results[idx] = managedResult{idx: idx, resources: resources, err: err}
			return nil
		})
	}
	g.Wait() //nolint:errcheck

	// Identify which candidates are parents of other candidates
	isParent := make(map[int]bool) // candidates index → true if parent
	for i, mr := range results {
		if mr.err != nil {
			slog.Warn("failed to get managed resources for parent detection",
				"app", candidates[i].name, "error", mr.err)
			continue
		}
		for _, r := range mr.resources {
			if r.Kind == "Application" {
				if _, ok := nameToIdx[r.Name]; ok {
					isParent[i] = true
					break
				}
			}
		}
	}

	// If no parent-child relationships detected, return all candidates
	if len(isParent) == 0 {
		all := make([]int, len(candidates))
		for i, info := range candidates {
			all[i] = info.lockIdx
		}
		return all, nil
	}

	// Split into leaf apps (to sync) and parent apps (to skip)
	for i, info := range candidates {
		if isParent[i] {
			skippedParents = append(skippedParents, info.lockIdx)
			slog.Debug("skipping parent app from sync, will reconcile on PR merge",
				"app", info.name)
		} else {
			syncIndices = append(syncIndices, info.lockIdx)
		}
	}

	slog.Debug("computed sync targets",
		"sync_count", len(syncIndices),
		"skipped_parents", len(skippedParents),
	)

	return syncIndices, skippedParents
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

// disableAutoSyncForLocks disables auto-sync on all locked applications that have
// it enabled, including their ApplicationSet templates and parent apps.
func (e *Executor) disableAutoSyncForLocks(ctx context.Context, locks []models.Lock, event *models.PREvent) error {
	// Check if any existing apps need auto-sync handling
	hasExistingApps := false
	for _, l := range locks {
		if l.ChangeType != models.ApplicationNew && l.ChangeType != models.ApplicationDeleted {
			hasExistingApps = true
			break
		}
	}
	if !hasExistingApps {
		return nil
	}

	// Precompute parent map once to avoid repeated ListApplications + GetManagedResources
	// API calls for each locked app. This reduces O(N × M) calls to O(1 + M) where N is
	// locked apps and M is auto-sync enabled apps.
	parentMap, err := e.argocd.BuildParentMap(ctx)
	if err != nil {
		slog.Warn("failed to precompute parent map; falling back to per-app parent detection", "error", err)
	}

	visited := make(map[string]bool)
	appSetDisabled := make(map[string]bool)

	for _, l := range locks {
		// Skip new and deleted apps
		if l.ChangeType == models.ApplicationNew || l.ChangeType == models.ApplicationDeleted {
			continue
		}

		state, err := disableAutoSync(ctx, e.argocd, l.Application)
		if err != nil {
			return fmt.Errorf("disabling auto-sync for %s: %w", l.Application, err)
		}

		// Mark this application as visited for parent traversal
		visited[l.Application] = true

		if state != nil {
			// Store auto-sync state in lock
			policyJSON, err := json.Marshal(state.OriginalPolicy)
			if err != nil {
				return fmt.Errorf("marshaling sync policy for %s: %w", l.Application, err)
			}

			currentLock, err := e.lock.Get(ctx, l.Application)
			if err != nil {
				return fmt.Errorf("getting lock for %s: %w", l.Application, err)
			}
			if currentLock == nil {
				return fmt.Errorf("no lock found for %s while storing auto-sync state", l.Application)
			}

			currentLock.AutoSyncDisabled = true
			currentLock.OriginalSyncPolicy = policyJSON
			currentLock.ApplicationSetName = state.ApplicationSetName
			if err := e.lock.UpdateLock(ctx, currentLock); err != nil {
				return fmt.Errorf("storing auto-sync state in lock for %s: %w", l.Application, err)
			}

			// Disable auto-sync on ApplicationSet template if applicable
			if state.ApplicationSetName != "" && !appSetDisabled[state.ApplicationSetName] {
				originalBytes, err := disableAppSetAutoSync(ctx, e.argocd, state.ApplicationSetName)
				if err != nil {
					slog.Warn("failed to disable auto-sync on applicationset template",
						"applicationset", state.ApplicationSetName, "error", err)
				} else if originalBytes != nil {
					if err := e.lock.StoreAppSetAutoSync(ctx, state.ApplicationSetName, event.Repo.FullName, event.PR.Number, originalBytes); err != nil {
						slog.Warn("failed to store applicationset auto-sync state",
							"applicationset", state.ApplicationSetName, "error", err)
					}
				}
				appSetDisabled[state.ApplicationSetName] = true
			}
		}

		// Disable auto-sync on parent apps (apps-of-apps pattern) — always run
		// regardless of whether this app itself has auto-sync, because a parent
		// app with auto-sync can still interfere.
		if err := disableParentAutoSync(ctx, e.argocd, e.lock, l.Application,
			event.Repo.FullName, event.Repo.HTMLURL, string(event.Provider),
			event.PR.Number, event.Sender.Login, visited, 0, parentMap); err != nil {
			slog.Warn("failed to disable parent auto-sync",
				"app", l.Application, "error", err)
		}
	}

	return nil
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
