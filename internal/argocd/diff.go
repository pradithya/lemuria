package argocd

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/sergi/go-diff/diffmatchpatch"

	"github.com/org/lemuria/internal/models"
)

// GetApplicationDiff computes the diff between live and target manifests.
func (c *Client) GetApplicationDiff(ctx context.Context, name string, revision string) ([]models.ManifestDiff, error) {
	// Get managed resources with both live and target state
	var resp struct {
		Items []struct {
			Group       string `json:"group"`
			Kind        string `json:"kind"`
			Name        string `json:"name"`
			Namespace   string `json:"namespace"`
			TargetState string `json:"targetState"`
			LiveState   string `json:"liveState"`
		} `json:"items"`
	}

	query := url.Values{}
	if revision != "" {
		query.Set("revision", revision)
	}

	if err := c.get(ctx, "/api/v1/applications/"+url.PathEscape(name)+"/managed-resources", query, &resp); err != nil {
		return nil, fmt.Errorf("getting managed resources for %s: %w", name, err)
	}

	var diffs []models.ManifestDiff
	for _, item := range resp.Items {
		diff := computeDiff(item.LiveState, item.TargetState)

		resource := models.ResourceKey{
			APIVersion: item.Group,
			Kind:       item.Kind,
			Name:       item.Name,
			Namespace:  item.Namespace,
		}

		action := determineDiffAction(item.LiveState, item.TargetState, diff)

		if action != models.DiffActionNone {
			diffs = append(diffs, models.ManifestDiff{
				Resource:    resource,
				LiveState:   item.LiveState,
				TargetState: item.TargetState,
				Diff:        diff,
				Action:      action,
			})
		}
	}

	// Sort diffs by resource for consistent output
	sort.Slice(diffs, func(i, j int) bool {
		return diffs[i].Resource.String() < diffs[j].Resource.String()
	})

	return diffs, nil
}

// computeDiff generates a unified diff between two strings.
func computeDiff(live, target string) string {
	if live == "" || live == "null" {
		live = ""
	}
	if target == "" || target == "null" {
		target = ""
	}

	// Normalize whitespace
	live = normalizeYAML(live)
	target = normalizeYAML(target)

	if live == target {
		return ""
	}

	dmp := diffmatchpatch.New()
	_ = dmp.DiffMain(live, target, true) // Used for semantic cleanup if needed

	// Convert to unified diff format
	return formatUnifiedDiff(live, target)
}

// normalizeYAML normalizes YAML for comparison.
func normalizeYAML(s string) string {
	// Trim whitespace
	s = strings.TrimSpace(s)

	// Normalize line endings
	s = strings.ReplaceAll(s, "\r\n", "\n")

	return s
}

// formatUnifiedDiff creates a unified diff format output.
func formatUnifiedDiff(a, b string) string {
	linesA := strings.Split(a, "\n")
	linesB := strings.Split(b, "\n")

	var result strings.Builder
	result.WriteString("--- live\n")
	result.WriteString("+++ target\n")

	// Simple line-by-line diff
	maxLines := len(linesA)
	if len(linesB) > maxLines {
		maxLines = len(linesB)
	}

	inHunk := false
	hunkStart := 0
	var hunkLines []string

	flushHunk := func() {
		if len(hunkLines) > 0 {
			result.WriteString(fmt.Sprintf("@@ -%d,%d +%d,%d @@\n", hunkStart+1, len(linesA), hunkStart+1, len(linesB)))
			for _, line := range hunkLines {
				result.WriteString(line)
				result.WriteString("\n")
			}
			hunkLines = nil
		}
	}

	for i := 0; i < maxLines; i++ {
		var lineA, lineB string
		if i < len(linesA) {
			lineA = linesA[i]
		}
		if i < len(linesB) {
			lineB = linesB[i]
		}

		if lineA == lineB {
			if inHunk {
				hunkLines = append(hunkLines, " "+lineA)
			}
		} else {
			if !inHunk {
				inHunk = true
				hunkStart = i
				// Add context before
				for j := max(0, i-3); j < i; j++ {
					if j < len(linesA) {
						hunkLines = append(hunkLines, " "+linesA[j])
					}
				}
			}

			if i < len(linesA) && lineA != "" {
				hunkLines = append(hunkLines, "-"+lineA)
			}
			if i < len(linesB) && lineB != "" {
				hunkLines = append(hunkLines, "+"+lineB)
			}
		}
	}

	flushHunk()
	return result.String()
}

// determineDiffAction determines what will happen to the resource during sync.
func determineDiffAction(live, target, diff string) models.DiffAction {
	liveEmpty := live == "" || live == "null"
	targetEmpty := target == "" || target == "null"

	if liveEmpty && !targetEmpty {
		return models.DiffActionCreate
	}
	if !liveEmpty && targetEmpty {
		return models.DiffActionDelete
	}
	if diff != "" {
		return models.DiffActionUpdate
	}
	return models.DiffActionNone
}

// DiffSummary provides a summary of changes.
type DiffSummary struct {
	TotalResources int
	Created        int
	Updated        int
	Deleted        int
	Unchanged      int
}

// SummarizeDiffs creates a summary from a list of diffs.
func SummarizeDiffs(diffs []models.ManifestDiff) DiffSummary {
	summary := DiffSummary{
		TotalResources: len(diffs),
	}

	for _, d := range diffs {
		switch d.Action {
		case models.DiffActionCreate:
			summary.Created++
		case models.DiffActionUpdate:
			summary.Updated++
		case models.DiffActionDelete:
			summary.Deleted++
		default:
			summary.Unchanged++
		}
	}

	return summary
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
