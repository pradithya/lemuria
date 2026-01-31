package diff

import (
	"fmt"
	"strings"

	"github.com/org/lemuria/internal/models"
)

// PlanResult represents the result of planning a single application.
type PlanResult struct {
	Application string
	Diffs       []models.ManifestDiff
	Created     int
	Updated     int
	Deleted     int
	LockStatus  string
	Warning     string
	Error       error
}

// Renderer formats diffs as markdown for PR comments.
type Renderer struct{}

// NewRenderer creates a new diff renderer.
func NewRenderer() *Renderer {
	return &Renderer{}
}

// RenderPlan formats plan results as a markdown comment.
func (r *Renderer) RenderPlan(results []PlanResult, prNumber int) string {
	var sb strings.Builder

	sb.WriteString("## Lemuria Plan\n\n")

	for _, result := range results {
		sb.WriteString(r.renderAppPlan(result))
		sb.WriteString("\n")
	}

	sb.WriteString("---\n")
	sb.WriteString("To apply: comment `lemuria sync`\n")
	sb.WriteString("To unlock: comment `lemuria unlock`\n")

	return sb.String()
}

// renderAppPlan formats a single application's plan.
func (r *Renderer) renderAppPlan(result PlanResult) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("### Application: `%s`\n\n", result.Application))

	if result.Error != nil {
		sb.WriteString(fmt.Sprintf("❌ **Error:** %s\n\n", result.Error.Error()))
		return sb.String()
	}

	if result.LockStatus != "" && !strings.Contains(result.LockStatus, "this PR") {
		sb.WriteString(fmt.Sprintf("⚠️ **%s**\n\n", result.LockStatus))
		return sb.String()
	}

	// Warning (e.g., auto-sync enabled)
	if result.Warning != "" {
		sb.WriteString(fmt.Sprintf("⚠️ **Warning:** %s\n\n", result.Warning))
	}

	// Summary
	totalChanges := result.Created + result.Updated + result.Deleted
	if totalChanges == 0 {
		sb.WriteString("✅ **No changes detected**\n\n")
	} else {
		sb.WriteString(r.renderSummary(result.Created, result.Updated, result.Deleted))
	}

	// Diff details
	if len(result.Diffs) > 0 {
		sb.WriteString(r.renderDiffs(result.Diffs))
	}

	// Lock status
	if result.LockStatus != "" {
		sb.WriteString(fmt.Sprintf("**Status:** 🔒 %s\n\n", result.LockStatus))
	}

	return sb.String()
}

// renderSummary formats the change summary.
func (r *Renderer) renderSummary(created, updated, deleted int) string {
	var parts []string

	if created > 0 {
		parts = append(parts, fmt.Sprintf("%d to create", created))
	}
	if updated > 0 {
		parts = append(parts, fmt.Sprintf("%d to update", updated))
	}
	if deleted > 0 {
		parts = append(parts, fmt.Sprintf("%d to delete", deleted))
	}

	return fmt.Sprintf("📋 **Changes:** %s\n\n", strings.Join(parts, ", "))
}

// renderDiffs formats the diff details.
func (r *Renderer) renderDiffs(diffs []models.ManifestDiff) string {
	var sb strings.Builder

	sb.WriteString("<details>\n")
	sb.WriteString(fmt.Sprintf("<summary>Diff (%d resources changed)</summary>\n\n", len(diffs)))

	for _, diff := range diffs {
		sb.WriteString(r.renderResourceDiff(diff))
	}

	sb.WriteString("</details>\n\n")

	return sb.String()
}

// renderResourceDiff formats a single resource diff.
func (r *Renderer) renderResourceDiff(diff models.ManifestDiff) string {
	var sb strings.Builder

	// Resource header with action indicator
	var actionIcon string
	switch diff.Action {
	case models.DiffActionCreate:
		actionIcon = "➕"
	case models.DiffActionUpdate:
		actionIcon = "📝"
	case models.DiffActionDelete:
		actionIcon = "➖"
	}

	sb.WriteString(fmt.Sprintf("#### %s %s\n\n", actionIcon, diff.Resource.String()))

	if diff.Diff != "" {
		sb.WriteString("```diff\n")
		sb.WriteString(diff.Diff)
		if !strings.HasSuffix(diff.Diff, "\n") {
			sb.WriteString("\n")
		}
		sb.WriteString("```\n\n")
	}

	return sb.String()
}

// RenderSync formats sync results as a markdown comment.
func (r *Renderer) RenderSync(results []SyncResultEntry) string {
	var sb strings.Builder

	sb.WriteString("## Lemuria Sync\n\n")

	allSucceeded := true
	for _, result := range results {
		sb.WriteString(fmt.Sprintf("### Application: `%s`\n\n", result.Application))

		if result.Error != nil {
			sb.WriteString(fmt.Sprintf("❌ **Error:** %s\n\n", result.Error.Error()))
			allSucceeded = false
			continue
		}

		switch result.Phase {
		case "Succeeded":
			sb.WriteString("✅ **Sync successful**\n\n")
		case "Running":
			sb.WriteString("⏳ **Sync in progress**\n\n")
		default:
			sb.WriteString(fmt.Sprintf("❌ **Sync failed:** %s\n\n", result.Message))
			allSucceeded = false
		}
	}

	if allSucceeded && len(results) > 0 {
		sb.WriteString("---\n")
		sb.WriteString("🎉 All applications synced successfully!\n")
	}

	return sb.String()
}

// SyncResultEntry represents a sync result for rendering.
type SyncResultEntry struct {
	Application string
	Phase       string
	Message     string
	Error       error
}

// RenderError formats an error as a markdown comment.
func (r *Renderer) RenderError(err error) string {
	return fmt.Sprintf("## Lemuria Error\n\n```\n%s\n```\n", err.Error())
}

// RenderHelp formats the help message.
func (r *Renderer) RenderHelp() string {
	return `## Lemuria Help

Lemuria provides Argo CD pull request automation.

### Commands

| Command | Description |
|---------|-------------|
| ` + "`lemuria plan`" + ` | Generate diff of changes for affected applications |
| ` + "`lemuria plan -a <app>`" + ` | Plan specific application |
| ` + "`lemuria sync`" + ` | Sync all planned applications |
| ` + "`lemuria sync -a <app>`" + ` | Sync specific application |
| ` + "`lemuria unlock`" + ` | Release all locks for this PR |
| ` + "`lemuria help`" + ` | Show this help message |

### Workflow

1. PR opened → Auto-plan runs
2. Review diff in PR comment
3. Comment ` + "`lemuria sync`" + ` to apply
4. Merge PR when ready

---
[Documentation](https://github.com/org/lemuria)`
}
