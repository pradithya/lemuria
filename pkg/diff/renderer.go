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

package diff

import (
	"fmt"
	"strings"

	"github.com/org/lemuria/internal/models"
)

// PlanResult represents the result of planning a single application.
type PlanResult struct {
	Application        string
	ApplicationSetName string
	Diffs              []models.ManifestDiff
	Created            int
	Updated            int
	Deleted            int
	LockStatus         string
	Warning            string
	Error              error
	ChangeType         models.ApplicationChangeType
	SourceFile         string
	IsGeneratedApp     bool
}

// Renderer formats diffs as markdown for PR comments.
type Renderer struct{}

// NewRenderer creates a new diff renderer.
func NewRenderer() *Renderer {
	return &Renderer{}
}

// RenderPlan formats plan results as a markdown comment.
// If maxSize > 0, the output is truncated at clean structural boundaries
// (application > resource > line) to fit within the limit.
func (r *Renderer) RenderPlan(results []PlanResult, prNumber int, maxSize ...int) string {
	body := r.renderPlanFull(results, prNumber)
	if len(maxSize) > 0 && maxSize[0] > 0 && len(body) > maxSize[0] {
		return r.truncatePlan(results, prNumber, maxSize[0])
	}
	return body
}

// renderPlanFull renders the complete plan without any truncation.
func (r *Renderer) renderPlanFull(results []PlanResult, prNumber int) string {
	var sb strings.Builder

	sb.WriteString("## Lemuria Plan\n\n")

	// Separate standalone apps from ApplicationSet-grouped apps
	var standalone []PlanResult
	appSetGroups := make(map[string][]PlanResult)
	var appSetOrder []string

	for _, result := range results {
		if result.ApplicationSetName == "" {
			standalone = append(standalone, result)
		} else {
			if _, exists := appSetGroups[result.ApplicationSetName]; !exists {
				appSetOrder = append(appSetOrder, result.ApplicationSetName)
			}
			appSetGroups[result.ApplicationSetName] = append(appSetGroups[result.ApplicationSetName], result)
		}
	}

	// Render standalone apps first
	for _, result := range standalone {
		sb.WriteString(r.renderAppPlan(result))
		sb.WriteString("\n")
	}

	// Render ApplicationSet groups
	for _, appSetName := range appSetOrder {
		group := appSetGroups[appSetName]
		count := len(group)
		noun := "applications"
		if count == 1 {
			noun = "application"
		}
		sb.WriteString(fmt.Sprintf("### ApplicationSet: `%s` (%d %s)\n\n", appSetName, count, noun))
		for _, result := range group {
			sb.WriteString(r.renderAppPlan(result))
			sb.WriteString("\n")
		}
	}

	sb.WriteString("---\n")
	sb.WriteString("To apply: comment `lemuria sync`\n")
	sb.WriteString("To unlock: comment `lemuria unlock`\n")

	return sb.String()
}

const truncationNotice = "\n\n> ⚠️ **Output truncated** due to platform comment size limit. Run `lemuria plan -a <app>` for individual application plans.\n"

// truncatePlan renders the plan progressively, respecting structural boundaries.
// Priority: omit entire apps → omit resources within an app → truncate diff content.
func (r *Renderer) truncatePlan(results []PlanResult, prNumber int, maxSize int) string {
	header := "## Lemuria Plan\n\n"
	footer := "---\nTo apply: comment `lemuria sync`\nTo unlock: comment `lemuria unlock`\n"

	// Reserve space for header, footer, and truncation notice
	budget := maxSize - len(header) - len(footer) - len(truncationNotice)
	if budget <= 0 {
		return header + truncationNotice + footer
	}

	// Flatten all results into ordered sections (standalone first, then appset groups)
	type appSection struct {
		prefix string // appset group header (empty for standalone)
		result PlanResult
	}
	var sections []appSection
	var appSetGroups = make(map[string][]PlanResult)
	var appSetOrder []string

	for _, result := range results {
		if result.ApplicationSetName == "" {
			sections = append(sections, appSection{result: result})
		} else {
			if _, exists := appSetGroups[result.ApplicationSetName]; !exists {
				appSetOrder = append(appSetOrder, result.ApplicationSetName)
			}
			appSetGroups[result.ApplicationSetName] = append(appSetGroups[result.ApplicationSetName], result)
		}
	}
	for _, appSetName := range appSetOrder {
		group := appSetGroups[appSetName]
		count := len(group)
		noun := "applications"
		if count == 1 {
			noun = "application"
		}
		groupHeader := fmt.Sprintf("### ApplicationSet: `%s` (%d %s)\n\n", appSetName, count, noun)
		for i, result := range group {
			prefix := ""
			if i == 0 {
				prefix = groupHeader
			}
			sections = append(sections, appSection{prefix: prefix, result: result})
		}
	}

	// Phase 1: Try adding full app sections one by one.
	var sb strings.Builder
	omittedApps := 0
	for i, sec := range sections {
		fullApp := sec.prefix + r.renderAppPlan(sec.result) + "\n"
		if sb.Len()+len(fullApp) <= budget {
			sb.WriteString(fullApp)
			continue
		}

		// This app doesn't fit fully. Try rendering it with truncated diffs.
		remaining := budget - sb.Len()
		truncatedApp := r.renderAppPlanTruncated(sec.result, remaining-len(sec.prefix))
		if truncatedApp != "" && len(sec.prefix)+len(truncatedApp) <= remaining {
			sb.WriteString(sec.prefix)
			sb.WriteString(truncatedApp)
		} else {
			// Can't fit even a truncated version — count remaining as omitted
			omittedApps = len(sections) - i
			break
		}
		// All subsequent apps are omitted
		omittedApps = len(sections) - i - 1
		break
	}

	if omittedApps > 0 {
		noun := "applications"
		if omittedApps == 1 {
			noun = "application"
		}
		sb.WriteString(fmt.Sprintf("\n*... %d more %s omitted*\n", omittedApps, noun))
	}

	return header + sb.String() + truncationNotice + footer
}

// renderAppPlanTruncated renders an app section that fits within maxSize,
// progressively omitting resource diffs.
func (r *Renderer) renderAppPlanTruncated(result PlanResult, maxSize int) string {
	// First, render without diffs to get the minimum size
	noDiffResult := result
	noDiffResult.Diffs = nil
	base := r.renderAppPlan(noDiffResult) + "\n"
	if len(base) > maxSize {
		return "" // Even the app header/summary doesn't fit
	}

	if len(result.Diffs) == 0 {
		return base
	}

	// Build the diff section progressively, resource by resource
	budget := maxSize - len(base)
	detailsOpen := "<details>\n" + fmt.Sprintf("<summary>Diff (%d resources changed)</summary>\n\n", len(result.Diffs))
	detailsClose := "</details>\n\n"
	omittedNotice := func(n int) string {
		noun := "resources"
		if n == 1 {
			noun = "resource"
		}
		return fmt.Sprintf("\n*... %d more %s omitted*\n\n", n, noun)
	}

	// Minimum: just the details wrapper with "all omitted"
	minDiffSection := detailsOpen + omittedNotice(len(result.Diffs)) + detailsClose
	if len(minDiffSection) > budget {
		return base // Can't fit even the collapsed diff section
	}

	var diffSB strings.Builder
	diffSB.WriteString(detailsOpen)
	omittedResources := 0

	for i, d := range result.Diffs {
		resourceBlock := r.renderResourceDiff(d)
		remaining := budget - diffSB.Len() - len(detailsClose)
		omittedCount := len(result.Diffs) - i - 1

		// Check if this resource fits (accounting for potential "omitted" notice after it)
		spaceForOmitted := 0
		if omittedCount > 0 {
			spaceForOmitted = len(omittedNotice(omittedCount))
		}

		if len(resourceBlock)+spaceForOmitted <= remaining {
			diffSB.WriteString(resourceBlock)
			continue
		}

		// Try truncating this resource's diff content at a line boundary
		truncated := r.renderResourceDiffTruncated(d, remaining-spaceForOmitted-len(omittedNotice(omittedCount+1)))
		if truncated != "" {
			diffSB.WriteString(truncated)
			omittedResources = omittedCount
		} else {
			omittedResources = len(result.Diffs) - i
		}
		break
	}

	if omittedResources > 0 {
		diffSB.WriteString(omittedNotice(omittedResources))
	}
	diffSB.WriteString(detailsClose)

	return base + diffSB.String()
}

// renderResourceDiffTruncated renders a resource diff, truncating the diff
// content at a clean line boundary to fit within maxSize.
func (r *Renderer) renderResourceDiffTruncated(d models.ManifestDiff, maxSize int) string {
	// Resource header
	var actionIcon string
	switch d.Action {
	case models.DiffActionCreate:
		actionIcon = "➕"
	case models.DiffActionUpdate:
		actionIcon = "📝"
	case models.DiffActionDelete:
		actionIcon = "➖"
	default:
		actionIcon = "ℹ️"
	}
	header := fmt.Sprintf("#### %s %s\n\n", actionIcon, d.Resource.String())
	diffTruncNotice := "\n... (truncated)\n"
	codeOpen := "```diff\n"
	codeClose := "```\n\n"

	minSize := len(header) + len(codeOpen) + len(diffTruncNotice) + len(codeClose)
	if maxSize < minSize {
		return ""
	}

	if d.Diff == "" {
		if len(header) <= maxSize {
			return header
		}
		return ""
	}

	// Available space for diff lines
	available := maxSize - len(header) - len(codeOpen) - len(diffTruncNotice) - len(codeClose)
	sanitized := sanitizeDiffForMarkdown(d.Diff)
	if len(header)+len(codeOpen)+len(sanitized)+len("\n")+len(codeClose) <= maxSize {
		// Full diff fits
		return r.renderResourceDiff(d)
	}

	// Truncate at line boundary
	lines := strings.SplitAfter(sanitized, "\n")
	var truncSB strings.Builder
	for _, line := range lines {
		if truncSB.Len()+len(line) > available {
			break
		}
		truncSB.WriteString(line)
	}

	if truncSB.Len() == 0 {
		return ""
	}

	return header + codeOpen + truncSB.String() + diffTruncNotice + codeClose
}

// renderAppPlan formats a single application's plan.
func (r *Renderer) renderAppPlan(result PlanResult) string {
	var sb strings.Builder

	// Header with status indicator for new/deleted apps
	header := fmt.Sprintf("### Application: `%s`", result.Application)
	switch result.ChangeType {
	case models.ApplicationNew:
		header += " 🆕"
	case models.ApplicationDeleted:
		header += " 🗑️"
	}
	sb.WriteString(header + "\n\n")

	if result.Error != nil {
		sb.WriteString(fmt.Sprintf("❌ **Error:** %s\n\n", result.Error.Error()))
		return sb.String()
	}

	// Handle new applications
	if result.ChangeType == models.ApplicationNew {
		if result.IsGeneratedApp {
			sb.WriteString(fmt.Sprintf("➕ **New application** - will be generated by ApplicationSet `%s` after merge\n\n", result.ApplicationSetName))
		} else {
			sb.WriteString("➕ **New application** - will be created when the Application CR is applied\n\n")
		}
		if result.SourceFile != "" {
			sb.WriteString(fmt.Sprintf("**Source file:** `%s`\n\n", result.SourceFile))
		}
		sb.WriteString("ℹ️ Lemuria cannot generate a diff for new applications until they exist in Argo CD.\n\n")
		return sb.String()
	}

	// Handle deleted applications
	if result.ChangeType == models.ApplicationDeleted {
		if result.IsGeneratedApp {
			sb.WriteString(fmt.Sprintf("➖ **Application will be removed** - ApplicationSet `%s` generator no longer produces this app\n\n", result.ApplicationSetName))
		} else {
			sb.WriteString("➖ **Application will be deleted** when the Application CR is removed\n\n")
		}
		if result.SourceFile != "" {
			sb.WriteString(fmt.Sprintf("**Source file:** `%s`\n\n", result.SourceFile))
		}
		sb.WriteString("⚠️ All resources managed by this application will be orphaned or pruned depending on the deletion policy.\n\n")
		return sb.String()
	}

	if result.LockStatus != "" && !strings.Contains(result.LockStatus, "this PR") && result.LockStatus != "New application" && result.LockStatus != "Will be deleted" {
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
	if result.LockStatus != "" && result.LockStatus != "New application" && result.LockStatus != "Will be deleted" {
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
	default:
		actionIcon = "ℹ️"
	}

	sb.WriteString(fmt.Sprintf("#### %s %s\n\n", actionIcon, diff.Resource.String()))

	if diff.Diff != "" {
		sb.WriteString("```diff\n")
		sb.WriteString(sanitizeDiffForMarkdown(diff.Diff))
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
			allSucceeded = false
		default:
			sb.WriteString(fmt.Sprintf("❌ **Sync failed:** %s\n\n", result.Message))
			allSucceeded = false
		}

		if result.Message != "" && (result.Phase == "Succeeded" || result.Phase == "Running") {
			sb.WriteString(fmt.Sprintf("**Message:** %s\n\n", result.Message))
		}

		// Health status
		if result.HealthStatus != "" {
			sb.WriteString(fmt.Sprintf("**Health:** %s %s\n\n", healthStatusIcon(result.HealthStatus), result.HealthStatus))
		}

		// Plan summary (what was planned)
		if result.PlanOutput != "" {
			sb.WriteString(fmt.Sprintf("📋 **Planned changes:** %s\n\n", result.PlanOutput))
		}

		// Plan diffs (detailed per-resource diffs from plan)
		if len(result.PlanDiffs) > 0 {
			sb.WriteString(r.renderPlanDiffs(result.PlanDiffs))
		}

		// Per-resource sync results
		if len(result.Resources) > 0 {
			sb.WriteString(r.renderResourceTable(result.Resources))
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
	Application  string
	Phase        string
	Message      string
	Error        error
	PlanOutput   string
	PlanDiffs    []models.PlanDiffEntry
	Resources    []models.ResourceResult
	HealthStatus string
}

// renderResourceTable formats resource sync results as a collapsible markdown table.
func (r *Renderer) renderResourceTable(resources []models.ResourceResult) string {
	var sb strings.Builder

	sb.WriteString("<details>\n")
	sb.WriteString(fmt.Sprintf("<summary>Resource Results (%d resources)</summary>\n\n", len(resources)))
	sb.WriteString("| Resource | Status | Health | Message |\n")
	sb.WriteString("|----------|--------|--------|--------|\n")

	for _, res := range resources {
		icon := resourceStatusIcon(res.Status)

		// Health column
		healthCol := ""
		if res.HealthStatus != "" {
			healthCol = fmt.Sprintf("%s %s", healthStatusIcon(string(res.HealthStatus)), string(res.HealthStatus))
		}

		// For degraded resources, prefer HealthMessage over sync Message
		msg := res.Message
		if res.HealthStatus == models.HealthStatusDegraded && res.HealthMessage != "" {
			msg = res.HealthMessage
		}
		if len(msg) > 80 {
			msg = msg[:77] + "..."
		}
		// Escape pipe characters in messages to avoid breaking the table
		msg = strings.ReplaceAll(msg, "|", "\\|")
		sb.WriteString(fmt.Sprintf("| %s | %s %s | %s | %s |\n",
			res.Resource.String(), icon, res.Status, healthCol, msg))
	}

	sb.WriteString("\n</details>\n\n")

	return sb.String()
}

// renderPlanDiffs formats plan diff entries as a collapsible section.
func (r *Renderer) renderPlanDiffs(diffs []models.PlanDiffEntry) string {
	var sb strings.Builder

	sb.WriteString("<details>\n")
	sb.WriteString(fmt.Sprintf("<summary>Plan Diff (%d resources changed)</summary>\n\n", len(diffs)))

	for _, d := range diffs {
		var actionIcon string
		switch d.Action {
		case models.DiffActionCreate:
			actionIcon = "➕"
		case models.DiffActionUpdate:
			actionIcon = "📝"
		case models.DiffActionDelete:
			actionIcon = "➖"
		default:
			actionIcon = "ℹ️"
		}

		sb.WriteString(fmt.Sprintf("#### %s %s\n\n", actionIcon, d.Resource.String()))

		if d.Diff != "" {
			sb.WriteString("```diff\n")
			sb.WriteString(sanitizeDiffForMarkdown(d.Diff))
			if !strings.HasSuffix(d.Diff, "\n") {
				sb.WriteString("\n")
			}
			sb.WriteString("```\n\n")
		}
	}

	sb.WriteString("</details>\n\n")

	return sb.String()
}

// sanitizeDiffForMarkdown escapes runs of 3 or more backticks in diff content
// to prevent breaking markdown fenced code blocks. A zero-width space is inserted
// after the second backtick to break any fence-like sequence.
func sanitizeDiffForMarkdown(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	count := 0
	for _, r := range s {
		if r == '`' {
			count++
			b.WriteRune(r)
			if count == 2 {
				// Insert zero-width space after the second consecutive backtick
				// so that any run of 3+ backticks is broken.
				b.WriteRune('\u200B')
			}
		} else {
			count = 0
			b.WriteRune(r)
		}
	}
	return b.String()
}

// resourceStatusIcon returns an emoji for the given sync status.
func resourceStatusIcon(status string) string {
	switch status {
	case "Synced":
		return "✅"
	case "SyncFailed":
		return "❌"
	case "Pruned":
		return "🗑️"
	case "PruningRequired":
		return "⚠️"
	default:
		return "ℹ️"
	}
}

// healthStatusIcon returns an emoji for the given health status.
func healthStatusIcon(status string) string {
	switch status {
	case "Healthy":
		return "💚"
	case "Degraded":
		return "❤️"
	case "Progressing":
		return "⏳"
	case "Suspended":
		return "⏸️"
	case "Missing":
		return "❓"
	default:
		return "ℹ️"
	}
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
| ` + "`lemuria plan -a <appset>`" + ` | Plan all applications in an ApplicationSet |
| ` + "`lemuria sync`" + ` | Sync all planned applications |
| ` + "`lemuria sync -a <app>`" + ` | Sync specific application |
| ` + "`lemuria sync -a <appset>`" + ` | Sync all applications in an ApplicationSet |
| ` + "`lemuria rollback -a <app>`" + ` | Show deployment history for an application |
| ` + "`lemuria rollback -a <app> --id <n>`" + ` | Rollback to a specific revision |
| ` + "`lemuria unlock`" + ` | Release all locks for this PR |
| ` + "`lemuria help`" + ` | Show this help message |

### Workflow

1. PR opened → Auto-plan runs
2. Review diff in PR comment
3. Comment ` + "`lemuria sync`" + ` to apply
4. Merge PR when ready

### Application Detection

Lemuria automatically detects:
- 🆕 **New applications** - Application CRs added in the PR
- 📝 **Modified applications** - Existing apps with changed manifests
- 🗑️ **Deleted applications** - Application CRs removed in the PR

### ApplicationSet Support

When targeting an ApplicationSet with ` + "`-a <name>`" + `, Lemuria expands it to all generated
applications and operates on each one. ApplicationSets can also be mapped in ` + "`.lemuria.yaml`" + `
using the ` + "`applicationset`" + ` field for auto-detection.

---
[Documentation](https://github.com/pradithya/lemuria)`
}
