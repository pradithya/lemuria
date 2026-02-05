package commands

import (
	"context"
	"fmt"
	"strings"

	"github.com/org/lemuria/internal/argocd"
	"github.com/org/lemuria/internal/models"
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
	if allSucceeded && !cmd.DryRun && e.config.Defaults.AutoMerge {
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
}

// syncApplication triggers a sync for a single application.
func (e *Executor) syncApplication(ctx context.Context, l models.Lock, cmd *Command, event *models.PREvent) syncResult {
	e.logger.Debug("starting sync for application",
		"app", l.Application,
		"revision", event.PR.HeadSHA,
		"prune", cmd.Prune,
		"dry_run", cmd.DryRun,
	)

	result := syncResult{
		Application: l.Application,
	}

	opts := &argocd.SyncOptions{
		Revision: event.PR.HeadSHA,
		Prune:    cmd.Prune,
		DryRun:   cmd.DryRun,
	}

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

// checkSyncRequirements verifies all requirements are met before sync.
func (e *Executor) checkSyncRequirements(ctx context.Context, event *models.PREvent, locks []models.Lock) error {
	e.logger.Debug("checking sync requirements",
		"require_approval", e.config.Defaults.RequireApproval,
		"locks_count", len(locks),
	)

	// Check if PR is approved (if required)
	if e.config.Defaults.RequireApproval {
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
		return fmt.Errorf("PR has merge conflicts. Please resolve before syncing.")
	}

	e.logger.Debug("all sync requirements met")
	return nil
}

// renderSyncResults formats sync results as a markdown comment.
func (e *Executor) renderSyncResults(results []syncResult) string {
	var sb strings.Builder
	sb.WriteString("## Lemuria Sync\n\n")

	allSucceeded := true
	for _, r := range results {
		sb.WriteString(fmt.Sprintf("### Application: `%s`\n\n", r.Application))

		if r.Error != nil {
			sb.WriteString(fmt.Sprintf("❌ **Error:** %s\n\n", r.Error.Error()))
			allSucceeded = false
			continue
		}

		switch r.Result.Phase {
		case models.SyncPhaseSucceeded:
			sb.WriteString("✅ **Sync successful**\n\n")
		case models.SyncPhaseRunning:
			sb.WriteString("⏳ **Sync in progress**\n\n")
		case models.SyncPhaseFailed, models.SyncPhaseError:
			sb.WriteString(fmt.Sprintf("❌ **Sync failed:** %s\n\n", r.Result.Message))
			allSucceeded = false
		}
	}

	if allSucceeded {
		sb.WriteString("---\n")
		sb.WriteString("🎉 All applications synced successfully!\n")
	}

	return sb.String()
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
