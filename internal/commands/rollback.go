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

	"github.com/org/lemuria/internal/argocd"
	"github.com/org/lemuria/internal/models"
)

// executeRollback runs the rollback command.
// Rollback syncs the application(s) to their configured targetRevision (main/master),
// effectively reverting any PR-deployed changes.
func (e *Executor) executeRollback(ctx context.Context, cmd *Command, event *models.PREvent) error {
	// Add reaction to show we're working on it
	if event.Comment != nil {
		if err := e.vcs.AddReaction(ctx, event.Repo.Owner, event.Repo.Name, event.Comment.ID, "eyes"); err != nil {
			slog.Warn("failed to add reaction", "error", err)
		}
	}

	// Check requirements before rollback
	if err := e.checkRollbackRequirements(ctx, event); err != nil {
		return e.postComment(ctx, event, "", fmt.Sprintf("## Lemuria Rollback\n\n"+
			"Rollback blocked.\n\n%s", err.Error()))
	}

	// If specific app requested, rollback just that app
	if cmd.Application != "" {
		app, err := e.argocd.GetApplication(ctx, cmd.Application)
		if err != nil {
			return e.postComment(ctx, event, "", fmt.Sprintf("## Lemuria Rollback\n\n"+
				"Application `%s` not found: %s", cmd.Application, err.Error()))
		}
		// Disable auto-sync if enabled (will be restored on PR merge/close)
		if app.HasAutoSync() && !cmd.DryRun {
			if err := e.disableAutoSyncForRollback(ctx, cmd.Application, event); err != nil {
				return e.postComment(ctx, event, "", fmt.Sprintf("## Lemuria Rollback\n\n"+
					"Failed to disable auto-sync for `%s`: %s", cmd.Application, err.Error()))
			}
		}
		return e.performRollback(ctx, cmd, event, app)
	}

	// No specific app - rollback all apps locked by this PR
	locks, err := e.lock.ListByPR(ctx, event.Repo.FullName, event.PR.Number)
	if err != nil {
		return e.postError(ctx, event, fmt.Errorf("listing locks: %w", err))
	}

	if len(locks) == 0 {
		return e.postComment(ctx, event, "", "## Lemuria Rollback\n\n"+
			"No applications are locked by this PR. Nothing to rollback.")
	}

	// Disable auto-sync on locked apps before rollback (skip for dry-run)
	if !cmd.DryRun {
		if err := e.disableAutoSyncForLocks(ctx, locks, event); err != nil {
			return e.postError(ctx, event, fmt.Errorf("disabling auto-sync for rollback: %w", err))
		}
	}

	// Rollback each application
	var results []rollbackResult
	for _, l := range locks {
		app, err := e.argocd.GetApplication(ctx, l.Application)
		if err != nil {
			results = append(results, rollbackResult{
				Application: l.Application,
				Error:       err,
			})
			continue
		}
		result := e.rollbackApplication(ctx, cmd, event, app)
		results = append(results, result)
	}

	// Render and post results
	output := e.renderRollbackResults(results, cmd.DryRun)
	return e.postComment(ctx, event, "", output)
}

// disableAutoSyncForRollback disables auto-sync on a single application and its
// parent apps for rollback. It stores the original policy in the lock if one exists.
func (e *Executor) disableAutoSyncForRollback(ctx context.Context, appName string, event *models.PREvent) error {
	state, err := disableAutoSync(ctx, e.argocd, appName)
	if err != nil {
		return err
	}
	if state == nil {
		return nil
	}

	policyJSON, err := json.Marshal(state.OriginalPolicy)
	if err != nil {
		return fmt.Errorf("marshaling sync policy: %w", err)
	}

	// Update lock if one exists
	currentLock, _ := e.lock.Get(ctx, appName)
	if currentLock != nil {
		currentLock.AutoSyncDisabled = true
		currentLock.OriginalSyncPolicy = policyJSON
		currentLock.ApplicationSetName = state.ApplicationSetName
		if err := e.lock.UpdateLock(ctx, currentLock); err != nil {
			slog.Warn("failed to store auto-sync state in lock",
				"app", appName, "error", err)
		}
	}

	// Disable auto-sync on parent apps
	visited := map[string]bool{appName: true}
	if err := disableParentAutoSync(ctx, e.argocd, e.lock, appName,
		event.Repo.FullName, event.Repo.HTMLURL, string(event.Provider),
		event.PR.Number, event.Sender.Login, visited, 0); err != nil {
		slog.Warn("failed to disable parent auto-sync for rollback",
			"app", appName, "error", err)
	}

	return nil
}

// rollbackResult holds the result of rolling back a single application.
type rollbackResult struct {
	Application    string
	TargetRevision string
	Result         *models.SyncResult
	Error          error
}

// checkRollbackRequirements verifies all requirements are met before rollback.
func (e *Executor) checkRollbackRequirements(ctx context.Context, event *models.PREvent) error {
	// Check if PR is approved (if required)
	if e.config.Defaults.RequireApproval {
		approved, err := e.vcs.IsPRApproved(ctx, event.Repo.Owner, event.Repo.Name, event.PR.Number)
		if err != nil {
			return fmt.Errorf("checking PR approval: %w", err)
		}
		if !approved {
			return fmt.Errorf("PR must be approved before rollback")
		}
	}

	return nil
}

// rollbackApplication syncs a single application to its configured targetRevision.
func (e *Executor) rollbackApplication(ctx context.Context, cmd *Command, event *models.PREvent, app *models.Application) rollbackResult {
	result := rollbackResult{
		Application: app.Name,
	}

	// Get the app's configured targetRevision
	targetRevision := app.TargetRevision
	if targetRevision == "" && len(app.Sources) > 0 {
		targetRevision = app.Sources[0].TargetRevision
	}
	if targetRevision == "" {
		targetRevision = "HEAD"
	}
	result.TargetRevision = targetRevision

	opts := &argocd.SyncOptions{
		Revision: "", // Empty revision = use app's configured targetRevision
		Prune:    cmd.Prune,
		DryRun:   cmd.DryRun,
	}

	syncResult, err := e.argocd.SyncApplication(ctx, app.Name, opts)
	if err != nil {
		result.Error = err
		return result
	}

	result.Result = syncResult

	return result
}

// performRollback handles rollback of a single specified application.
func (e *Executor) performRollback(ctx context.Context, cmd *Command, event *models.PREvent, app *models.Application) error {
	result := e.rollbackApplication(ctx, cmd, event, app)

	if result.Error != nil {
		return e.postComment(ctx, event, "", fmt.Sprintf("## Lemuria Rollback\n\n"+
			"Rollback of `%s` to `%s` failed.\n\n"+
			"Error: %s", app.Name, result.TargetRevision, result.Error.Error()))
	}

	return e.postComment(ctx, event, "", e.renderSingleRollbackResult(result, cmd.DryRun))
}

// renderSingleRollbackResult formats a single rollback result as a markdown comment.
func (e *Executor) renderSingleRollbackResult(r rollbackResult, dryRun bool) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Lemuria Rollback - `%s`\n\n", r.Application))

	if dryRun {
		sb.WriteString("**Mode:** Dry-run\n\n")
	}

	sb.WriteString(fmt.Sprintf("**Target:** `%s`\n\n", r.TargetRevision))

	if r.Error != nil {
		sb.WriteString(fmt.Sprintf("Rollback failed: %s\n", r.Error.Error()))
		return sb.String()
	}

	switch r.Result.Phase {
	case models.SyncPhaseSucceeded:
		sb.WriteString("Rollback successful. Application synced to configured targetRevision.\n")
	case models.SyncPhaseRunning:
		sb.WriteString("Rollback in progress...\n")
	case models.SyncPhaseFailed, models.SyncPhaseError:
		sb.WriteString(fmt.Sprintf("Rollback failed: %s\n", r.Result.Message))
	default:
		sb.WriteString(fmt.Sprintf("Rollback status: %s\n", r.Result.Phase))
	}

	return sb.String()
}

// renderRollbackResults formats multiple rollback results as a markdown comment.
func (e *Executor) renderRollbackResults(results []rollbackResult, dryRun bool) string {
	var sb strings.Builder
	sb.WriteString("## Lemuria Rollback\n\n")

	if dryRun {
		sb.WriteString("**Mode:** Dry-run\n\n")
	}

	allSucceeded := true
	for _, r := range results {
		sb.WriteString(fmt.Sprintf("### Application: `%s`\n\n", r.Application))
		sb.WriteString(fmt.Sprintf("**Target:** `%s`\n\n", r.TargetRevision))

		if r.Error != nil {
			sb.WriteString(fmt.Sprintf("Rollback failed: %s\n\n", r.Error.Error()))
			allSucceeded = false
			continue
		}

		switch r.Result.Phase {
		case models.SyncPhaseSucceeded:
			sb.WriteString("Rollback successful.\n\n")
		case models.SyncPhaseRunning:
			sb.WriteString("Rollback in progress...\n\n")
		case models.SyncPhaseFailed, models.SyncPhaseError:
			sb.WriteString(fmt.Sprintf("Rollback failed: %s\n\n", r.Result.Message))
			allSucceeded = false
		default:
			sb.WriteString(fmt.Sprintf("Rollback status: %s\n\n", r.Result.Phase))
		}
	}

	if allSucceeded && len(results) > 0 {
		sb.WriteString("---\n")
		sb.WriteString("All applications rolled back successfully.\n")
	}

	return sb.String()
}
