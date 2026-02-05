package argocd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/pmezard/go-difflib/difflib"
	"gopkg.in/yaml.v3"

	"github.com/org/lemuria/internal/models"
)

// ResourceDiff matches ArgoCD's ResourceDiff structure from the managed-resources API.
// This contains the server-side computed diff information.
type ResourceDiff struct {
	Group               string `json:"group,omitempty"`
	Kind                string `json:"kind,omitempty"`
	Namespace           string `json:"namespace,omitempty"`
	Name                string `json:"name,omitempty"`
	TargetState         string `json:"targetState,omitempty"`
	LiveState           string `json:"liveState,omitempty"`
	Diff                string `json:"diff,omitempty"` // Deprecated by ArgoCD, but may still be present
	Hook                bool   `json:"hook,omitempty"`
	NormalizedLiveState string `json:"normalizedLiveState,omitempty"`
	PredictedLiveState  string `json:"predictedLiveState,omitempty"`
	ResourceVersion     string `json:"resourceVersion,omitempty"`
	Modified            bool   `json:"modified,omitempty"`
}

// GetApplicationDiff computes the diff between live and target manifests.
// It uses ArgoCD's server-side diff computation via the managed-resources API,
// which returns NormalizedLiveState and PredictedLiveState - the same diff
// logic used by `argocd app diff`.
func (c *Client) GetApplicationDiff(ctx context.Context, name string, revision string) ([]models.ManifestDiff, error) {
	var resp struct {
		Items []ResourceDiff `json:"items"`
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
		// Skip hooks (same as argocd app diff)
		if item.Hook {
			continue
		}

		// Skip secrets - ArgoCD doesn't have access to k8s secret data
		if item.Kind == "Secret" && (item.Group == "" || item.Group == "v1") {
			continue
		}

		// Use ArgoCD's server-side computed diff when available
		diffStr := computeDiffFromArgoCD(item)

		resource := models.ResourceKey{
			APIVersion: item.Group,
			Kind:       item.Kind,
			Name:       item.Name,
			Namespace:  item.Namespace,
		}

		action := determineDiffAction(item)

		if action != models.DiffActionNone {
			diffs = append(diffs, models.ManifestDiff{
				Resource:    resource,
				LiveState:   item.LiveState,
				TargetState: item.TargetState,
				Diff:        diffStr,
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

// computeDiffFromArgoCD generates a unified diff using ArgoCD's server-side
// normalized states. This uses NormalizedLiveState and PredictedLiveState
// which are computed by ArgoCD using the same logic as `argocd app diff`.
func computeDiffFromArgoCD(item ResourceDiff) string {
	// If ArgoCD says it's not modified and both states exist, no diff needed
	if !item.Modified && item.NormalizedLiveState != "" && item.PredictedLiveState != "" {
		return ""
	}

	// Use ArgoCD's normalized states if available (preferred - this is what argocd diff uses)
	liveState := item.NormalizedLiveState
	targetState := item.PredictedLiveState

	// Fallback to raw states if normalized not available
	if liveState == "" {
		liveState = item.LiveState
	}
	if targetState == "" {
		targetState = item.TargetState
	}

	// Handle empty states
	if (liveState == "" || liveState == "null") && (targetState == "" || targetState == "null") {
		return ""
	}

	// Convert to YAML for human-readable diff output
	liveYAML := jsonToYAML(liveState)
	targetYAML := jsonToYAML(targetState)

	if liveYAML == targetYAML {
		return ""
	}

	// Generate unified diff
	unifiedDiff := difflib.UnifiedDiff{
		A:        difflib.SplitLines(liveYAML),
		B:        difflib.SplitLines(targetYAML),
		FromFile: "live",
		ToFile:   "target",
		Context:  3,
	}

	result, err := difflib.GetUnifiedDiffString(unifiedDiff)
	if err != nil {
		return ""
	}

	return result
}

// jsonToYAML converts a JSON string to YAML for more readable diffs.
// This matches ArgoCD's diff output format.
func jsonToYAML(jsonStr string) string {
	jsonStr = strings.TrimSpace(jsonStr)
	if jsonStr == "" || jsonStr == "null" {
		return ""
	}

	var data map[string]any
	if err := json.Unmarshal([]byte(jsonStr), &data); err != nil {
		// If it's not valid JSON, return as-is
		return jsonStr
	}

	yamlBytes, err := yaml.Marshal(data)
	if err != nil {
		// Fallback to prettified JSON
		pretty, _ := json.MarshalIndent(data, "", "  ")
		return string(pretty)
	}

	return string(yamlBytes)
}

// determineDiffAction determines what will happen to the resource during sync,
// using ArgoCD's server-side computed state.
func determineDiffAction(item ResourceDiff) models.DiffAction {
	liveEmpty := item.LiveState == "" || item.LiveState == "null"
	targetEmpty := item.TargetState == "" || item.TargetState == "null"

	if liveEmpty && !targetEmpty {
		return models.DiffActionCreate
	}
	if !liveEmpty && targetEmpty {
		return models.DiffActionDelete
	}
	// Use ArgoCD's Modified flag when available
	if item.Modified {
		return models.DiffActionUpdate
	}
	// Fallback: check if normalized states differ
	if item.NormalizedLiveState != "" && item.PredictedLiveState != "" {
		if item.NormalizedLiveState != item.PredictedLiveState {
			return models.DiffActionUpdate
		}
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
