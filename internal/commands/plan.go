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
	"strings"
	"time"

	v1alpha1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"

	"github.com/org/lemuria/internal/argocd"
	"github.com/org/lemuria/internal/models"
	"github.com/org/lemuria/pkg/diff"
)

// executePlan runs the plan command.
func (e *Executor) executePlan(ctx context.Context, cmd *Command, event *models.PREvent) error {
	e.logger.Debug("executing plan command",
		"repo", event.Repo.FullName,
		"pr", event.PR.Number,
		"specific_app", cmd.Application,
		"all_apps", cmd.All,
	)

	// Add reaction to show we're working on it
	if event.Comment != nil {
		if err := e.vcs.AddReaction(ctx, event.Repo.Owner, event.Repo.Name, event.Comment.ID, "eyes"); err != nil {
			e.logger.Warn("failed to add reaction", "error", err)
		}
	}

	// Invalidate old plan comments before generating new ones
	if err := e.vcs.InvalidatePlanComments(ctx, event.Repo.Owner, event.Repo.Name, event.PR.Number); err != nil {
		e.logger.Warn("failed to invalidate old plan comments", "error", err)
	}

	// Find affected applications
	var apps []models.Application
	var err error

	if cmd.Application != "" {
		// Specific application requested
		e.logger.Debug("fetching specific application",
			"app", cmd.Application,
		)
		app, err := e.argocd.GetApplication(ctx, cmd.Application)
		if err != nil {
			e.logger.Debug("application not found",
				"app", cmd.Application,
				"error", err,
			)
			return e.postError(ctx, event, fmt.Errorf("application %s not found: %w", cmd.Application, err))
		}
		apps = []models.Application{*app}
	} else if cmd.All {
		// All applications for this repo
		repoURL := event.Repo.HTMLURL
		e.logger.Debug("finding all applications for repo",
			"repo_url", repoURL,
		)
		apps, err = e.argocd.FindApplicationsByRepo(ctx, repoURL)
		if err != nil {
			e.logger.Debug("failed to find applications by repo",
				"repo_url", repoURL,
				"error", err,
			)
			return e.postError(ctx, event, fmt.Errorf("listing applications: %w", err))
		}
	} else {
		// Auto-detect affected applications
		e.logger.Debug("auto-detecting affected applications")
		apps, err = e.findAffectedApplications(ctx, event)
		if err != nil {
			e.logger.Debug("failed to find affected applications",
				"error", err,
			)
			return e.postError(ctx, event, err)
		}
	}

	e.logger.Debug("found applications to plan",
		"count", len(apps),
	)

	if len(apps) == 0 {
		e.logger.Debug("no applications affected by PR")
		return e.postComment(ctx, event, "", "## Lemuria Plan\n\nNo applications affected by this PR.")
	}

	// Process each application
	var results []appPlanResult
	for _, app := range apps {
		e.logger.Debug("planning application",
			"app", app.Name,
			"change_type", app.ChangeType,
		)
		result := e.planApplication(ctx, app, event)
		results = append(results, result)
	}

	e.logger.Debug("completed planning all applications",
		"results_count", len(results),
	)

	// Render and post results
	output := e.renderPlanResults(results, event)
	return e.postPlanComment(ctx, event, output)
}

// appPlanResult holds the result of planning a single application.
type appPlanResult struct {
	Application string
	Diffs       []models.ManifestDiff
	Summary     argocd.DiffSummary
	LockStatus  string
	Warning     string
	Error       error
	ChangeType  models.ApplicationChangeType
	SourceFile  string
}

// planApplication generates a diff for a single application.
func (e *Executor) planApplication(ctx context.Context, app models.Application, event *models.PREvent) appPlanResult {
	e.logger.Debug("starting plan for application",
		"app", app.Name,
		"change_type", app.ChangeType,
		"source_file", app.SourceFile,
		"path", app.Path,
	)

	result := appPlanResult{
		Application: app.Name,
		ChangeType:  app.ChangeType,
		SourceFile:  app.SourceFile,
	}

	// Handle new applications (not yet in ArgoCD)
	if app.IsNew() {
		e.logger.Debug("application is new (not yet in ArgoCD)",
			"app", app.Name,
		)
		result.LockStatus = "New application"
		return result
	}

	// Handle deleted applications
	if app.IsDeleted() {
		e.logger.Debug("application will be deleted",
			"app", app.Name,
		)
		result.LockStatus = "Will be deleted"
		result.Warning = "This application will be removed after the PR is merged."
		return result
	}

	// Check if auto-sync is enabled
	if app.HasAutoSync() {
		e.logger.Debug("application has auto-sync enabled",
			"app", app.Name,
		)
		result.Warning = "Auto-sync is enabled. Disable auto-sync before using Lemuria to prevent conflicts."
	}

	// Try to acquire lock
	e.logger.Debug("attempting to acquire lock",
		"app", app.Name,
		"pr", event.PR.Number,
		"repo", event.Repo.FullName,
		"user", event.Sender.Login,
	)
	lockResult, err := e.lock.Lock(ctx, models.LockRequest{
		Application: app.Name,
		PRNumber:    event.PR.Number,
		Repo:        event.Repo.FullName,
		RepoURL:     event.Repo.HTMLURL,
		Provider:    string(event.Provider),
		User:        event.Sender.Login,
	})
	if err != nil {
		e.logger.Debug("failed to acquire lock",
			"app", app.Name,
			"error", err,
		)
		result.Error = fmt.Errorf("failed to acquire lock: %w", err)
		return result
	}

	if !lockResult.Acquired {
		e.logger.Debug("lock held by another PR",
			"app", app.Name,
			"held_by_pr", lockResult.HeldBy.PRNumber,
			"held_by_user", lockResult.HeldBy.User,
		)
		result.LockStatus = fmt.Sprintf("Locked by PR #%d (%s)", lockResult.HeldBy.PRNumber, lockResult.HeldBy.User)
		return result
	}

	e.logger.Debug("lock acquired",
		"app", app.Name,
		"pr", event.PR.Number,
	)
	result.LockStatus = "Locked by this PR"

	// Get diff using temporary applications
	// This properly handles multi-source apps with external Helm charts
	diffMode := argocd.DiffMode(e.config.ArgoCD.DiffMode)
	if diffMode == "" {
		diffMode = argocd.DiffModeBranch // Default to branch mode
	}

	timeout := e.config.ArgoCD.TempAppTimeout
	if timeout == 0 {
		timeout = 2 * time.Minute
	}

	// If the Application CR file was modified, read specs from git branches
	// so the diff reflects inline changes (e.g., Helm values in the Application CR)
	var baseAppSpec, headAppSpec *v1alpha1.Application
	if app.SourceFile != "" {
		e.logger.Debug("reading application spec from git branches",
			"app", app.Name,
			"source_file", app.SourceFile,
		)

		// Read base branch version
		baseContent, err := e.vcs.GetFileContent(ctx, event.Repo.Owner, event.Repo.Name, app.SourceFile, event.PR.BaseRef)
		if err != nil {
			e.logger.Warn("failed to read application CR from base branch, falling back to live spec",
				"app", app.Name,
				"source_file", app.SourceFile,
				"base_ref", event.PR.BaseRef,
				"error", err,
			)
		} else {
			parsed, parseErr := argocd.ParseRawApplicationFromYAML(baseContent, app.Name)
			if parseErr != nil {
				e.logger.Warn("failed to parse application CR from base branch, falling back to live spec",
					"app", app.Name,
					"error", parseErr,
				)
			} else {
				baseAppSpec = parsed
			}
		}

		// Read head branch version
		headContent, err := e.vcs.GetFileContent(ctx, event.Repo.Owner, event.Repo.Name, app.SourceFile, event.PR.HeadRef)
		if err != nil {
			e.logger.Warn("failed to read application CR from head branch, falling back to live spec",
				"app", app.Name,
				"source_file", app.SourceFile,
				"head_ref", event.PR.HeadRef,
				"error", err,
			)
		} else {
			parsed, parseErr := argocd.ParseRawApplicationFromYAML(headContent, app.Name)
			if parseErr != nil {
				e.logger.Warn("failed to parse application CR from head branch, falling back to live spec",
					"app", app.Name,
					"error", parseErr,
				)
			} else {
				headAppSpec = parsed
			}
		}
	}

	e.logger.Debug("getting application diff",
		"app", app.Name,
		"mode", diffMode,
		"base_branch", event.PR.BaseRef,
		"target_branch", event.PR.HeadRef,
		"has_base_spec_override", baseAppSpec != nil,
		"has_head_spec_override", headAppSpec != nil,
	)

	diffs, err := e.argocd.GetApplicationDiff(ctx, app.Name, argocd.DiffOptions{
		Mode:         diffMode,
		BaseBranch:   event.PR.BaseRef,
		TargetBranch: event.PR.HeadRef,
		PRNumber:     event.PR.Number,
		PRRepo:       event.Repo.HTMLURL,
		Timeout:      timeout,
		BaseAppSpec:  baseAppSpec,
		HeadAppSpec:  headAppSpec,
	})
	// Compute a concise plan summary from diffs (empty if diff failed).
	var planSummary string
	var planDiffs []models.PlanDiffEntry
	if err == nil && len(diffs) > 0 {
		summary := argocd.SummarizeDiffs(diffs)
		planSummary = formatPlanSummary(summary)
		planDiffs = toPlanDiffEntries(diffs)
	}

	// Store plan revision for later sync verification.
	// This is stored regardless of diff outcome so that sync can proceed
	// even if the diff fails (e.g., temp app timeout for external Helm charts).
	if err := e.lock.StorePlan(ctx, app.Name, event.PR.Number, event.PR.HeadSHA, app.SourceFile, planSummary, planDiffs); err != nil {
		e.logger.Warn("failed to store plan", "app", app.Name, "error", err)
	}

	if err != nil {
		e.logger.Debug("failed to generate diff",
			"app", app.Name,
			"error", err,
		)
		result.Error = fmt.Errorf("failed to generate diff: %w", err)
		return result
	}

	result.Diffs = diffs
	result.Summary = argocd.SummarizeDiffs(diffs)

	e.logger.Debug("diff generated successfully",
		"app", app.Name,
		"diffs_count", len(diffs),
		"created", result.Summary.Created,
		"updated", result.Summary.Updated,
		"deleted", result.Summary.Deleted,
	)

	return result
}

// renderPlanResults formats plan results as a markdown comment.
func (e *Executor) renderPlanResults(results []appPlanResult, event *models.PREvent) string {
	return e.renderer.RenderPlan(convertToRenderResults(results), event.PR.Number)
}

// convertToRenderResults converts internal results to renderer format.
func convertToRenderResults(results []appPlanResult) []diff.PlanResult {
	rendered := make([]diff.PlanResult, len(results))
	for i, r := range results {
		rendered[i] = diff.PlanResult{
			Application: r.Application,
			Diffs:       r.Diffs,
			Created:     r.Summary.Created,
			Updated:     r.Summary.Updated,
			Deleted:     r.Summary.Deleted,
			LockStatus:  r.LockStatus,
			Warning:     r.Warning,
			Error:       r.Error,
			ChangeType:  r.ChangeType,
			SourceFile:  r.SourceFile,
		}
	}
	return rendered
}

// formatPlanSummary builds a concise summary string from diff counts.
// e.g., "3 to create, 1 to update, 2 to delete"
func formatPlanSummary(summary argocd.DiffSummary) string {
	var parts []string
	if summary.Created > 0 {
		parts = append(parts, fmt.Sprintf("%d to create", summary.Created))
	}
	if summary.Updated > 0 {
		parts = append(parts, fmt.Sprintf("%d to update", summary.Updated))
	}
	if summary.Deleted > 0 {
		parts = append(parts, fmt.Sprintf("%d to delete", summary.Deleted))
	}
	if len(parts) == 0 {
		return "No changes detected"
	}
	return strings.Join(parts, ", ")
}

// toPlanDiffEntries converts ManifestDiff entries to lightweight PlanDiffEntry,
// stripping full YAML states to reduce storage size.
// Only diffs with non-empty content are included, so plan_diffs contains
// actionable per-resource changes that can be rendered consistently.
func toPlanDiffEntries(diffs []models.ManifestDiff) []models.PlanDiffEntry {
	entries := make([]models.PlanDiffEntry, 0, len(diffs))
	for _, d := range diffs {
		if d.Diff == "" {
			continue
		}
		entries = append(entries, models.PlanDiffEntry{
			Resource: d.Resource,
			Action:   d.Action,
			Diff:     d.Diff,
		})
	}
	return entries
}

// postComment creates a new Lemuria comment on the PR.
func (e *Executor) postComment(ctx context.Context, event *models.PREvent, appName, body string) error {
	_, err := e.vcs.PostComment(ctx, event.Repo.Owner, event.Repo.Name, event.PR.Number, body, false)
	return err
}

// postPlanComment creates a new plan comment on the PR (can be invalidated on new changes).
func (e *Executor) postPlanComment(ctx context.Context, event *models.PREvent, body string) error {
	_, err := e.vcs.PostComment(ctx, event.Repo.Owner, event.Repo.Name, event.PR.Number, body, true)
	return err
}

// postError posts an error comment.
func (e *Executor) postError(ctx context.Context, event *models.PREvent, err error) error {
	body := fmt.Sprintf("## Lemuria Error\n\n```\n%s\n```", err.Error())
	return e.postComment(ctx, event, "", body)
}
