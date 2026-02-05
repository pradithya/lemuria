package argocd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/pmezard/go-difflib/difflib"

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

// computeDiff generates a context diff between two JSON strings.
// It prettifies JSON for better readability in the diff output.
func computeDiff(live, target string) string {
	if live == "" || live == "null" {
		live = ""
	}
	if target == "" || target == "null" {
		target = ""
	}

	// Prettify JSON for better readability
	livePretty := prettifyJSON(live)
	targetPretty := prettifyJSON(target)

	if livePretty == targetPretty {
		return ""
	}

	// Generate unified diff using go-difflib
	diff := difflib.UnifiedDiff{
		A:        difflib.SplitLines(livePretty),
		B:        difflib.SplitLines(targetPretty),
		FromFile: "live",
		ToFile:   "target",
		Context:  3,
	}

	result, err := difflib.GetUnifiedDiffString(diff)
	if err != nil {
		return ""
	}

	return result
}

// prettifyJSON formats JSON with indentation for readable diffs.
// If the input is not valid JSON, it returns the original string trimmed.
func prettifyJSON(jsonStr string) string {
	jsonStr = strings.TrimSpace(jsonStr)
	if jsonStr == "" {
		return ""
	}

	// Parse and re-marshal with indentation
	var data any
	if err := json.Unmarshal([]byte(jsonStr), &data); err != nil {
		return jsonStr
	}

	pretty, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return jsonStr
	}

	return string(pretty)
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
