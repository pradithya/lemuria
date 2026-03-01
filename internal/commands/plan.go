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
	"time"

	v1alpha1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"

	"github.com/org/lemuria/internal/argocd"
	"github.com/org/lemuria/internal/models"
	"github.com/org/lemuria/pkg/diff"
)

// executePlan runs the plan command.
func (e *Executor) executePlan(ctx context.Context, cmd *Command, event *models.PREvent) error {
	slog.Debug("executing plan command",
		"repo", event.Repo.FullName,
		"pr", event.PR.Number,
		"specific_app", cmd.Application,
		"all_apps", cmd.All,
	)

	// Add reaction to show we're working on it
	if event.Comment != nil {
		if err := e.vcs.AddReaction(ctx, event.Repo.Owner, event.Repo.Name, event.Comment.ID, "eyes"); err != nil {
			slog.Warn("failed to add reaction", "error", err)
		}
	}

	// Invalidate old plan comments before generating new ones
	if err := e.vcs.InvalidatePlanComments(ctx, event.Repo.Owner, event.Repo.Name, event.PR.Number); err != nil {
		slog.Warn("failed to invalidate old plan comments", "error", err)
	}

	// Find affected applications
	var apps []models.Application
	var err error

	if cmd.Application != "" {
		// Specific application requested — try as Application first, then ApplicationSet
		slog.Debug("fetching specific application",
			"app", cmd.Application,
		)
		app, appErr := e.argocd.GetApplication(ctx, cmd.Application)
		if appErr != nil {
			slog.Debug("application not found, trying as applicationset",
				"app", cmd.Application,
				"error", appErr,
			)
			expandedApps, appSetErr := e.expandApplicationSet(ctx, cmd.Application)
			if appSetErr != nil {
				slog.Debug("applicationset also not found",
					"app", cmd.Application,
					"error", appSetErr,
				)
				return e.postError(ctx, event, fmt.Errorf(
					"application %s lookup failed (application error: %v, applicationset error: %w)",
					cmd.Application, appErr, appSetErr))
			}
			apps = expandedApps
		} else {
			apps = []models.Application{*app}
		}
	} else if cmd.All {
		// All applications for this repo
		repoURL := event.Repo.HTMLURL
		slog.Debug("finding all applications for repo",
			"repo_url", repoURL,
		)
		apps, err = e.argocd.FindApplicationsByRepo(ctx, repoURL)
		if err != nil {
			slog.Debug("failed to find applications by repo",
				"repo_url", repoURL,
				"error", err,
			)
			return e.postError(ctx, event, fmt.Errorf("listing applications: %w", err))
		}
	} else {
		// Auto-detect affected applications
		slog.Debug("auto-detecting affected applications")
		apps, err = e.findAffectedApplications(ctx, event)
		if err != nil {
			slog.Debug("failed to find affected applications",
				"error", err,
			)
			return e.postError(ctx, event, err)
		}
	}

	slog.Debug("found applications to plan",
		"count", len(apps),
	)

	if len(apps) == 0 {
		slog.Debug("no applications affected by PR")
		return e.postComment(ctx, event, "", "## Lemuria Plan\n\nNo applications affected by this PR.")
	}

	// Batch-fetch Application CR source files before per-app loop
	sourcePathSet := make(map[string]struct{})
	for _, app := range apps {
		if app.SourceFile != "" {
			sourcePathSet[app.SourceFile] = struct{}{}
		}
	}
	sourcePaths := setToSlice(sourcePathSet)

	var headSourceContents, baseSourceContents map[string][]byte
	if len(sourcePaths) > 0 {
		var fetchErr error
		headSourceContents, fetchErr = e.vcs.GetFileContents(ctx, event.Repo.Owner, event.Repo.Name, sourcePaths, event.PR.HeadRef)
		if fetchErr != nil {
			slog.Warn("failed to batch-fetch source files at head ref", "error", fetchErr)
			headSourceContents = map[string][]byte{}
		}
		baseSourceContents, fetchErr = e.vcs.GetFileContents(ctx, event.Repo.Owner, event.Repo.Name, sourcePaths, event.PR.BaseRef)
		if fetchErr != nil {
			slog.Warn("failed to batch-fetch source files at base ref", "error", fetchErr)
			baseSourceContents = map[string][]byte{}
		}
	} else {
		headSourceContents = map[string][]byte{}
		baseSourceContents = map[string][]byte{}
	}

	// Process each application
	var results []appPlanResult
	for _, app := range apps {
		slog.Debug("planning application",
			"app", app.Name,
			"change_type", app.ChangeType,
		)
		result := e.planApplication(ctx, app, event, headSourceContents, baseSourceContents)
		results = append(results, result)
	}

	slog.Debug("completed planning all applications",
		"results_count", len(results),
	)

	// Render and post results (may produce multiple comments)
	parts := e.renderPlanResults(results, event)
	for _, part := range parts {
		if err := e.postPlanComment(ctx, event, part); err != nil {
			return err
		}
	}
	return nil
}

// appPlanResult holds the result of planning a single application.
type appPlanResult struct {
	Application        string
	ApplicationSetName string
	Diffs              []models.ManifestDiff
	Summary            argocd.DiffSummary
	LockStatus         string
	Warning            string
	Error              error
	ChangeType         models.ApplicationChangeType
	SourceFile         string
	IsGeneratedApp     bool
}

// planApplication generates a diff for a single application.
func (e *Executor) planApplication(ctx context.Context, app models.Application, event *models.PREvent, headContents, baseContents map[string][]byte) appPlanResult {
	slog.Debug("starting plan for application",
		"app", app.Name,
		"change_type", app.ChangeType,
		"source_file", app.SourceFile,
		"path", app.Path,
	)

	result := appPlanResult{
		Application:        app.Name,
		ApplicationSetName: app.ApplicationSetName,
		ChangeType:         app.ChangeType,
		SourceFile:         app.SourceFile,
		IsGeneratedApp:     app.IsGeneratedApp,
	}

	// Handle new applications (not yet in ArgoCD)
	if app.IsNew() {
		slog.Debug("application is new (not yet in ArgoCD)",
			"app", app.Name,
		)

		// Generate diff first — only acquire lock if diff succeeds
		if app.SourceFile == "" {
			slog.Warn("new application has no source file, skipping",
				"app", app.Name,
			)
			return result
		}

		headContent, ok := headContents[app.SourceFile]
		if !ok {
			slog.Warn("application CR not found in head contents for new app, skipping diff",
				"app", app.Name,
				"source_file", app.SourceFile,
			)
			return result
		}

		headAppSpec, parseErr := argocd.ParseRawApplicationFromYAML(headContent, app.Name)
		if parseErr != nil {
			slog.Warn("failed to parse application CR from head branch for new app, skipping diff",
				"app", app.Name,
				"error", parseErr,
			)
			return result
		}

		timeout := e.config.ArgoCD.TempAppTimeout
		if timeout == 0 {
			timeout = 2 * time.Minute
		}

		diffs, diffErr := e.argocd.DiffNewApp(ctx, headAppSpec, argocd.DiffOptions{
			TargetBranch: event.PR.HeadRef,
			PRNumber:     event.PR.Number,
			PRRepo:       event.Repo.HTMLURL,
			Timeout:      timeout,
		})

		if diffErr != nil {
			slog.Warn("failed to generate diff for new application",
				"app", app.Name,
				"error", diffErr,
			)
			result.Error = fmt.Errorf("failed to generate diff for new application: %w", diffErr)
			return result
		}

		result.Diffs = diffs
		result.Summary = argocd.SummarizeDiffs(diffs)

		// Diff succeeded — acquire lock so sync can find it later
		lockResult, err := e.lock.Lock(ctx, models.LockRequest{
			Application: app.Name,
			PRNumber:    event.PR.Number,
			Repo:        event.Repo.FullName,
			RepoURL:     event.Repo.HTMLURL,
			Provider:    string(event.Provider),
			User:        event.Sender.Login,
			ChangeType:  models.ApplicationNew,
		})
		if err != nil {
			result.Error = fmt.Errorf("failed to acquire lock: %w", err)
			return result
		}
		if !lockResult.Acquired {
			result.LockStatus = fmt.Sprintf("Locked by PR #%d (%s)", lockResult.HeldBy.PRNumber, lockResult.HeldBy.User)
			return result
		}
		result.LockStatus = "Locked by this PR"

		// Store plan
		var planSummary string
		var planDiffs []models.PlanDiffEntry
		if len(diffs) > 0 {
			planSummary = formatPlanSummary(result.Summary)
			planDiffs = toPlanDiffEntries(diffs)
		}
		if err := e.lock.StorePlan(ctx, app.Name, event.PR.Number, event.PR.HeadSHA, app.SourceFile, planSummary, planDiffs); err != nil {
			slog.Warn("failed to store plan for new app", "app", app.Name, "error", err)
		}

		return result
	}

	// Handle deleted applications
	if app.IsDeleted() {
		slog.Debug("application will be deleted",
			"app", app.Name,
		)
		result.Warning = "This application will be removed after the PR is merged."

		// Generate diff first — only acquire lock if diff succeeds
		diffMode := argocd.DiffMode(e.config.ArgoCD.DiffMode)
		if diffMode == "" {
			diffMode = argocd.DiffModeBranch
		}

		timeout := e.config.ArgoCD.TempAppTimeout
		if timeout == 0 {
			timeout = 2 * time.Minute
		}

		// Optionally read base branch spec for override
		var baseAppSpec *v1alpha1.Application
		if app.SourceFile != "" {
			if baseContent, ok := baseContents[app.SourceFile]; ok {
				parsed, parseErr := argocd.ParseRawApplicationFromYAML(baseContent, app.Name)
				if parseErr != nil {
					slog.Warn("failed to parse application CR from base branch for deleted app, falling back to live spec",
						"app", app.Name,
						"error", parseErr,
					)
				} else {
					baseAppSpec = parsed
				}
			} else {
				slog.Warn("application CR not found in base contents for deleted app, falling back to live spec",
					"app", app.Name,
					"source_file", app.SourceFile,
				)
			}
		}

		diffs, diffErr := e.argocd.DiffDeletedApp(ctx, app.Name, argocd.DiffOptions{
			Mode:        diffMode,
			BaseBranch:  event.PR.BaseRef,
			PRNumber:    event.PR.Number,
			PRRepo:      event.Repo.HTMLURL,
			Timeout:     timeout,
			BaseAppSpec: baseAppSpec,
		})
		if diffErr != nil {
			slog.Warn("failed to generate diff for deleted application",
				"app", app.Name,
				"error", diffErr,
			)
			result.Error = fmt.Errorf("failed to generate diff for deleted application: %w", diffErr)
			return result
		}

		result.Diffs = diffs
		result.Summary = argocd.SummarizeDiffs(diffs)

		// Diff succeeded — acquire lock so sync can find it later
		lockResult, err := e.lock.Lock(ctx, models.LockRequest{
			Application: app.Name,
			PRNumber:    event.PR.Number,
			Repo:        event.Repo.FullName,
			RepoURL:     event.Repo.HTMLURL,
			Provider:    string(event.Provider),
			User:        event.Sender.Login,
			ChangeType:  models.ApplicationDeleted,
		})
		if err != nil {
			result.Error = fmt.Errorf("failed to acquire lock: %w", err)
			return result
		}
		if !lockResult.Acquired {
			result.LockStatus = fmt.Sprintf("Locked by PR #%d (%s)", lockResult.HeldBy.PRNumber, lockResult.HeldBy.User)
			return result
		}
		result.LockStatus = "Locked by this PR"

		// Store plan
		var planSummary string
		var planDiffs []models.PlanDiffEntry
		if len(diffs) > 0 {
			planSummary = formatPlanSummary(result.Summary)
			planDiffs = toPlanDiffEntries(diffs)
		}
		if err := e.lock.StorePlan(ctx, app.Name, event.PR.Number, event.PR.HeadSHA, app.SourceFile, planSummary, planDiffs); err != nil {
			slog.Warn("failed to store plan for deleted app", "app", app.Name, "error", err)
		}

		return result
	}

	// Check if auto-sync is enabled
	if app.HasAutoSync() {
		slog.Debug("application has auto-sync enabled",
			"app", app.Name,
		)
		result.Warning = "Auto-sync is enabled. Disable auto-sync before using Lemuria to prevent conflicts."
	}

	// Generate diff first — only acquire lock if diff succeeds
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
		slog.Debug("reading application spec from git branches",
			"app", app.Name,
			"source_file", app.SourceFile,
		)

		// Read base branch version from pre-fetched content
		if baseContent, ok := baseContents[app.SourceFile]; ok {
			parsed, parseErr := argocd.ParseRawApplicationFromYAML(baseContent, app.Name)
			if parseErr != nil {
				slog.Warn("failed to parse application CR from base branch, falling back to live spec",
					"app", app.Name,
					"error", parseErr,
				)
			} else {
				baseAppSpec = parsed
			}
		} else {
			slog.Warn("application CR not found in base branch, falling back to live spec",
				"app", app.Name,
				"source_file", app.SourceFile,
				"base_ref", event.PR.BaseRef,
			)
		}

		// Read head branch version from pre-fetched content
		if headContent, ok := headContents[app.SourceFile]; ok {
			parsed, parseErr := argocd.ParseRawApplicationFromYAML(headContent, app.Name)
			if parseErr != nil {
				slog.Warn("failed to parse application CR from head branch, falling back to live spec",
					"app", app.Name,
					"error", parseErr,
				)
			} else {
				headAppSpec = parsed
			}
		} else {
			slog.Warn("application CR not found in head branch, falling back to live spec",
				"app", app.Name,
				"source_file", app.SourceFile,
				"head_ref", event.PR.HeadRef,
			)
		}
	}

	slog.Debug("getting application diff",
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
	if err != nil {
		slog.Debug("failed to generate diff",
			"app", app.Name,
			"error", err,
		)
		result.Error = fmt.Errorf("failed to generate diff: %w", err)
		return result
	}

	result.Diffs = diffs
	result.Summary = argocd.SummarizeDiffs(diffs)

	slog.Debug("diff generated successfully",
		"app", app.Name,
		"diffs_count", len(diffs),
		"created", result.Summary.Created,
		"updated", result.Summary.Updated,
		"deleted", result.Summary.Deleted,
	)

	// Diff succeeded — acquire lock
	slog.Debug("attempting to acquire lock",
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
		ChangeType:  models.ApplicationExisting,
	})
	if err != nil {
		slog.Debug("failed to acquire lock",
			"app", app.Name,
			"error", err,
		)
		result.Error = fmt.Errorf("failed to acquire lock: %w", err)
		return result
	}

	if !lockResult.Acquired {
		slog.Debug("lock held by another PR",
			"app", app.Name,
			"held_by_pr", lockResult.HeldBy.PRNumber,
			"held_by_user", lockResult.HeldBy.User,
		)
		result.LockStatus = fmt.Sprintf("Locked by PR #%d (%s)", lockResult.HeldBy.PRNumber, lockResult.HeldBy.User)
		return result
	}

	slog.Debug("lock acquired",
		"app", app.Name,
		"pr", event.PR.Number,
	)
	result.LockStatus = "Locked by this PR"

	// Store plan revision for later sync verification.
	var planSummary string
	var planDiffs []models.PlanDiffEntry
	if len(diffs) > 0 {
		summary := argocd.SummarizeDiffs(diffs)
		planSummary = formatPlanSummary(summary)
		planDiffs = toPlanDiffEntries(diffs)
	}
	if err := e.lock.StorePlan(ctx, app.Name, event.PR.Number, event.PR.HeadSHA, app.SourceFile, planSummary, planDiffs); err != nil {
		slog.Warn("failed to store plan", "app", app.Name, "error", err)
	}

	return result
}

// renderPlanResults formats plan results as markdown comments.
// It may return multiple parts when the result exceeds the VCS comment size limit.
func (e *Executor) renderPlanResults(results []appPlanResult, event *models.PREvent) []string {
	maxSize := e.vcs.MaxCommentSize()
	return e.renderer.RenderPlan(convertToRenderResults(results), event.PR.Number, maxSize)
}

// convertToRenderResults converts internal results to renderer format.
func convertToRenderResults(results []appPlanResult) []diff.PlanResult {
	rendered := make([]diff.PlanResult, len(results))
	for i, r := range results {
		rendered[i] = diff.PlanResult{
			Application:        r.Application,
			ApplicationSetName: r.ApplicationSetName,
			Diffs:              r.Diffs,
			Created:            r.Summary.Created,
			Updated:            r.Summary.Updated,
			Deleted:            r.Summary.Deleted,
			LockStatus:         r.LockStatus,
			Warning:            r.Warning,
			Error:              r.Error,
			ChangeType:         r.ChangeType,
			SourceFile:         r.SourceFile,
			IsGeneratedApp:     r.IsGeneratedApp,
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

// expandApplicationSet fetches an ApplicationSet by name and returns its generated applications.
func (e *Executor) expandApplicationSet(ctx context.Context, name string) ([]models.Application, error) {
	_, err := e.argocd.GetApplicationSet(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("applicationset %s not found: %w", name, err)
	}

	apps, err := e.argocd.GetApplicationsByApplicationSet(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("getting applications for applicationset %s: %w", name, err)
	}

	if len(apps) == 0 {
		return nil, fmt.Errorf("applicationset %s has no generated applications", name)
	}

	return apps, nil
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
