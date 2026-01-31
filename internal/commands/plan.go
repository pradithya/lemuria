package commands

import (
	"context"
	"fmt"

	"github.com/org/lemuria/internal/argocd"
	"github.com/org/lemuria/internal/models"
	"github.com/org/lemuria/pkg/diff"
)

// executePlan runs the plan command.
func (e *Executor) executePlan(ctx context.Context, cmd *Command, event *models.PREvent) error {
	// Add reaction to show we're working on it
	if event.Comment != nil {
		e.github.AddReaction(ctx, event.Repo.Owner, event.Repo.Name, event.Comment.ID, "eyes")
	}

	// Find affected applications
	var apps []models.Application
	var err error

	if cmd.Application != "" {
		// Specific application requested
		app, err := e.argocd.GetApplication(ctx, cmd.Application)
		if err != nil {
			return e.postError(ctx, event, fmt.Errorf("application %s not found: %w", cmd.Application, err))
		}
		apps = []models.Application{*app}
	} else if cmd.All {
		// All applications for this repo
		repoURL := fmt.Sprintf("https://github.com/%s", event.Repo.FullName)
		apps, err = e.argocd.FindApplicationsByRepo(ctx, repoURL)
		if err != nil {
			return e.postError(ctx, event, fmt.Errorf("listing applications: %w", err))
		}
	} else {
		// Auto-detect affected applications
		apps, err = e.findAffectedApplications(ctx, event)
		if err != nil {
			return e.postError(ctx, event, err)
		}
	}

	if len(apps) == 0 {
		return e.postComment(ctx, event, "", "## Lemuria Plan\n\nNo applications affected by this PR.")
	}

	// Process each application
	var results []appPlanResult
	for _, app := range apps {
		result := e.planApplication(ctx, app, event)
		results = append(results, result)
	}

	// Render and post results
	output := e.renderPlanResults(results, event)
	return e.postComment(ctx, event, "", output)
}

// appPlanResult holds the result of planning a single application.
type appPlanResult struct {
	Application string
	Diffs       []models.ManifestDiff
	Summary     argocd.DiffSummary
	LockStatus  string
	Warning     string
	Error       error
}

// planApplication generates a diff for a single application.
func (e *Executor) planApplication(ctx context.Context, app models.Application, event *models.PREvent) appPlanResult {
	result := appPlanResult{
		Application: app.Name,
	}

	// Check if auto-sync is enabled
	if app.HasAutoSync() {
		result.Warning = "Auto-sync is enabled. Disable auto-sync before using Lemuria to prevent conflicts."
	}

	// Try to acquire lock
	lockResult, err := e.lock.Lock(ctx, models.LockRequest{
		Application: app.Name,
		PRNumber:    event.PR.Number,
		Repo:        event.Repo.FullName,
		User:        event.Sender.Login,
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

	// Get diff
	diffs, err := e.argocd.GetApplicationDiff(ctx, app.Name, event.PR.HeadSHA)
	if err != nil {
		result.Error = fmt.Errorf("failed to generate diff: %w", err)
		return result
	}

	result.Diffs = diffs
	result.Summary = argocd.SummarizeDiffs(diffs)

	// Store plan output for later sync verification
	if err := e.lock.StorePlan(ctx, app.Name, event.PR.Number, event.PR.HeadSHA); err != nil {
		e.logger.Warn("failed to store plan", "app", app.Name, "error", err)
	}

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
		}
	}
	return rendered
}

// postComment creates or updates a Lemuria comment on the PR.
func (e *Executor) postComment(ctx context.Context, event *models.PREvent, appName, body string) error {
	_, err := e.github.UpsertComment(ctx, event.Repo.Owner, event.Repo.Name, event.PR.Number, appName, body)
	return err
}

// postError posts an error comment.
func (e *Executor) postError(ctx context.Context, event *models.PREvent, err error) error {
	body := fmt.Sprintf("## Lemuria Error\n\n```\n%s\n```", err.Error())
	return e.postComment(ctx, event, "", body)
}
