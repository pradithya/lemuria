package commands

import (
	"context"
	"fmt"

	"github.com/org/lemuria/internal/argocd"
	"github.com/org/lemuria/internal/config"
	"github.com/org/lemuria/internal/models"
	"github.com/org/lemuria/pkg/diff"
)

// executeSync runs the sync command.
func (e *Executor) executeSync(ctx context.Context, cmd *Command, event *models.PREvent) error {
	e.logger.Debug("executing sync command",
		"repo", event.Repo.FullName,
		"pr", event.PR.Number,
		"specific_app", cmd.Application,
		"dry_run", cmd.DryRun,
		"prune", cmd.Prune,
	)

	// Add reaction to show we're working on it
	if event.Comment != nil {
		if err := e.github.AddReaction(ctx, event.Repo.Owner, event.Repo.Name, event.Comment.ID, "eyes"); err != nil {
			e.logger.Warn("failed to add reaction", "error", err)
		}
	}

	// Get locks held by this PR
	locks, err := e.lock.ListByPR(ctx, event.Repo.FullName, event.PR.Number)
	if err != nil {
		e.logger.Debug("failed to list locks",
			"error", err,
		)
		return e.postError(ctx, event, fmt.Errorf("listing locks: %w", err))
	}

	e.logger.Debug("found locks for PR",
		"count", len(locks),
		"pr", event.PR.Number,
	)

	if len(locks) == 0 {
		e.logger.Debug("no locks found for PR")
		return e.postComment(ctx, event, "", "## Lemuria Sync\n\n⚠️ No applications are locked by this PR. Run `lemuria plan` first.")
	}

	// Filter to specific application if requested
	if cmd.Application != "" {
		e.logger.Debug("filtering locks to specific application",
			"app", cmd.Application,
		)
		var filtered []models.Lock
		for _, l := range locks {
			if l.Application == cmd.Application {
				filtered = append(filtered, l)
			}
		}
		if len(filtered) == 0 {
			e.logger.Debug("application not locked by this PR",
				"app", cmd.Application,
			)
			return e.postComment(ctx, event, "", fmt.Sprintf("## Lemuria Sync\n\n⚠️ Application `%s` is not locked by this PR.", cmd.Application))
		}
		locks = filtered
	}

	// Check requirements before sync
	e.logger.Debug("checking sync requirements")
	if err := e.checkSyncRequirements(ctx, event, locks); err != nil {
		e.logger.Debug("sync requirements not met",
			"error", err,
		)
		return e.postComment(ctx, event, "", fmt.Sprintf("## Lemuria Sync\n\n❌ %s", err.Error()))
	}

	// Verify plans are not stale and check for auto-sync
	for _, l := range locks {
		e.logger.Debug("verifying plan freshness",
			"app", l.Application,
			"plan_revision", l.PlanRevision,
			"current_head_sha", event.PR.HeadSHA,
		)
		if l.PlanRevision != event.PR.HeadSHA {
			e.logger.Debug("plan is stale",
				"app", l.Application,
				"plan_revision", l.PlanRevision,
				"current_head_sha", event.PR.HeadSHA,
			)
			return e.postComment(ctx, event, "", fmt.Sprintf("## Lemuria Sync\n\n⚠️ Plan for `%s` is stale. Please run `lemuria plan` again.", l.Application))
		}

		// Check if auto-sync is enabled
		app, err := e.argocd.GetApplication(ctx, l.Application)
		if err != nil {
			e.logger.Debug("failed to get application",
				"app", l.Application,
				"error", err,
			)
			return e.postError(ctx, event, fmt.Errorf("getting application %s: %w", l.Application, err))
		}
		if app.HasAutoSync() {
			e.logger.Debug("application has auto-sync enabled",
				"app", l.Application,
			)
			return e.postComment(ctx, event, "", fmt.Sprintf("## Lemuria Sync\n\n❌ Application `%s` has auto-sync enabled.\n\nDisable auto-sync before using Lemuria to prevent conflicts.", l.Application))
		}
	}

	// Sync each application
	e.logger.Debug("starting sync for applications",
		"count", len(locks),
	)
	var results []syncResult
	for _, l := range locks {
		e.logger.Debug("syncing application",
			"app", l.Application,
		)
		result := e.syncApplication(ctx, l, cmd, event)
		results = append(results, result)
	}

	// Check if all syncs succeeded
	allSucceeded := true
	for _, r := range results {
		if r.Error != nil || (r.Result != nil && r.Result.Phase != models.SyncPhaseSucceeded) {
			allSucceeded = false
			break
		}
	}

	e.logger.Debug("sync completed",
		"all_succeeded", allSucceeded,
		"results_count", len(results),
	)

	// Auto-merge if enabled and all syncs succeeded (not dry-run)
	// Resolution order: repo config (.lemuria.yaml) > server defaults
	autoMerge := e.config.Defaults.AutoMerge
	repoConfig := e.loadRepoConfig(ctx, event)
	if repoConfig != nil && repoConfig.AutoMerge != nil {
		autoMerge = *repoConfig.AutoMerge
	}
	e.logger.Debug("auto-merge resolved",
		"auto_merge", autoMerge,
		"all_succeeded", allSucceeded,
	)

	if allSucceeded && !cmd.DryRun && autoMerge {
		e.logger.Debug("attempting auto-merge",
			"repo", event.Repo.FullName,
			"pr", event.PR.Number,
		)
		if err := e.autoMergePR(ctx, event); err != nil {
			e.logger.Warn("auto-merge failed", "error", err)
		}
	}

	// Render and post results
	output := e.renderSyncResults(results)
	return e.postComment(ctx, event, "", output)
}

// syncResult holds the result of syncing a single application.
type syncResult struct {
	Application string
	Result      *models.SyncResult
	Error       error
	PlanOutput  string
}

// syncApplication triggers a sync for a single application.
func (e *Executor) syncApplication(ctx context.Context, l models.Lock, cmd *Command, event *models.PREvent) syncResult {
	e.logger.Debug("starting sync for application",
		"app", l.Application,
		"revision", event.PR.HeadSHA,
		"source_file", l.SourceFile,
		"prune", cmd.Prune,
		"dry_run", cmd.DryRun,
	)

	result := syncResult{
		Application: l.Application,
		PlanOutput:  l.PlanOutput,
	}

	// Determine if the app sources from the PR repo
	app, err := e.argocd.GetApplication(ctx, l.Application)
	if err != nil {
		result.Error = fmt.Errorf("getting application %s: %w", l.Application, err)
		return result
	}

	repoURL := fmt.Sprintf("https://github.com/%s", event.Repo.FullName)
	fromPRRepo := appSourcesFromRepo(*app, repoURL)

	// If the Application CR was modified in the PR, update the live app's spec
	// before syncing. This handles cases where the PR modifies Helm values,
	// chart versions, or other spec fields in the Application CR file.
	if l.SourceFile != "" {
		e.logger.Debug("updating application spec from PR head branch",
			"app", l.Application,
			"source_file", l.SourceFile,
		)

		headContent, err := e.github.GetFileContent(ctx, event.Repo.Owner, event.Repo.Name, l.SourceFile, event.PR.HeadRef)
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

		e.logger.Debug("application spec updated from PR",
			"app", l.Application,
		)
	}

	opts := &argocd.SyncOptions{
		Prune:  cmd.Prune,
		DryRun: cmd.DryRun,
	}

	// Only set revision for apps sourcing from the PR repo.
	// External sources (e.g., Helm chart repos) should use the revision
	// already configured in the app spec.
	if fromPRRepo {
		opts.Revision = event.PR.HeadSHA
	}

	e.logger.Debug("triggering sync",
		"app", l.Application,
		"revision", opts.Revision,
		"from_pr_repo", fromPRRepo,
	)

	syncResult, err := e.argocd.SyncApplication(ctx, l.Application, opts)
	if err != nil {
		e.logger.Debug("sync failed",
			"app", l.Application,
			"error", err,
		)
		result.Error = err
		return result
	}

	result.Result = syncResult

	e.logger.Debug("sync completed",
		"app", l.Application,
		"phase", syncResult.Phase,
		"message", syncResult.Message,
	)

	// Release lock on successful sync (unless dry-run)
	if !cmd.DryRun && syncResult.Phase == models.SyncPhaseSucceeded {
		e.logger.Debug("releasing lock after successful sync",
			"app", l.Application,
		)
		if err := e.lock.Unlock(ctx, l.Application, event.Repo.FullName, event.PR.Number); err != nil {
			e.logger.Warn("failed to release lock after sync", "app", l.Application, "error", err)
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
	e.logger.Debug("checking sync requirements",
		"require_approval", e.config.Defaults.RequireApproval,
		"locks_count", len(locks),
	)

	// Load repo config for per-app sync requirements
	repoConfig := e.loadRepoConfig(ctx, event)

	// Check if PR approval is required for any locked application
	requireApproval := e.isApprovalRequired(repoConfig, locks)
	e.logger.Debug("approval requirement resolved",
		"require_approval", requireApproval,
	)

	if requireApproval {
		e.logger.Debug("checking PR approval status")
		approved, err := e.github.IsPRApproved(ctx, event.Repo.Owner, event.Repo.Name, event.PR.Number)
		if err != nil {
			e.logger.Debug("failed to check PR approval",
				"error", err,
			)
			return fmt.Errorf("checking PR approval: %w", err)
		}
		e.logger.Debug("PR approval status",
			"approved", approved,
		)
		if !approved {
			return fmt.Errorf("PR must be approved before sync")
		}
	}

	// Check if PR is mergeable
	e.logger.Debug("checking PR mergeable status")
	pr, err := e.github.GetPR(ctx, event.Repo.Owner, event.Repo.Name, event.PR.Number)
	if err != nil {
		e.logger.Debug("failed to get PR status",
			"error", err,
		)
		return fmt.Errorf("getting PR status: %w", err)
	}

	mergeable := pr.GetMergeable()
	e.logger.Debug("PR mergeable status",
		"mergeable", mergeable,
		"state", pr.GetState(),
	)

	if !mergeable {
		return fmt.Errorf("PR has merge conflicts, please resolve before syncing")
	}

	e.logger.Debug("all sync requirements met")
	return nil
}

// loadRepoConfig fetches and parses the repo's .lemuria.yaml from the PR head branch.
func (e *Executor) loadRepoConfig(ctx context.Context, event *models.PREvent) *config.RepoConfig {
	configData, err := e.github.GetRepoConfig(ctx, event.Repo.Owner, event.Repo.Name, event.PR.HeadRef)
	if err != nil {
		e.logger.Debug("failed to load .lemuria.yaml for sync requirements", "error", err)
		return nil
	}

	repoConfig, err := config.LoadRepoConfig(configData)
	if err != nil {
		e.logger.Debug("failed to parse .lemuria.yaml for sync requirements", "error", err)
		return nil
	}

	return repoConfig
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
				e.logger.Debug("sync requirement exact match",
					"app", appName,
					"require_approval", req.RequireApproval,
				)
				return req.RequireApproval
			}
		}
		for _, req := range repoConfig.SyncRequirements {
			if req.Name != appName && matchAppName(req.Name, appName) {
				e.logger.Debug("sync requirement wildcard match",
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
	if err := e.github.MergePullRequest(
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

	e.logger.Info("auto-merged PR",
		"repo", event.Repo.FullName,
		"pr", event.PR.Number,
		"method", method,
	)

	// Delete source branch if configured and not a protected branch
	if e.config.Defaults.DeleteSourceBranch {
		branch := event.PR.HeadRef
		if !IsProtectedBranch(branch) {
			if err := e.github.DeleteBranch(ctx, event.Repo.Owner, event.Repo.Name, branch); err != nil {
				e.logger.Warn("failed to delete source branch",
					"branch", branch,
					"error", err,
				)
			} else {
				e.logger.Info("deleted source branch", "branch", branch)
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
