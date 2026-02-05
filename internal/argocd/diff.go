package argocd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/argoproj/gitops-engine/pkg/diff"
	"github.com/pmezard/go-difflib/difflib"
	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/org/lemuria/internal/models"
)

// GetApplicationDiff computes the diff between live and target manifests
// using the same diff approach as ArgoCD's gitops-engine.
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
		// Skip secrets - ArgoCD doesn't have access to k8s secret data
		if item.Kind == "Secret" && (item.Group == "" || item.Group == "v1") {
			continue
		}

		diffStr, err := computeDiffWithGitopsEngine(item.LiveState, item.TargetState)
		if err != nil {
			// Fallback to simple diff on error
			diffStr = computeSimpleDiff(item.LiveState, item.TargetState)
		}

		resource := models.ResourceKey{
			APIVersion: item.Group,
			Kind:       item.Kind,
			Name:       item.Name,
			Namespace:  item.Namespace,
		}

		action := determineDiffAction(item.LiveState, item.TargetState, diffStr)

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

// computeDiffWithGitopsEngine uses ArgoCD's gitops-engine to compute diffs.
// This provides the same diff logic as ArgoCD's `argocd app diff` command.
func computeDiffWithGitopsEngine(liveJSON, targetJSON string) (string, error) {
	// Parse live state
	var live *unstructured.Unstructured
	if liveJSON != "" && liveJSON != "null" {
		live = &unstructured.Unstructured{}
		if err := json.Unmarshal([]byte(liveJSON), &live.Object); err != nil {
			return "", fmt.Errorf("parsing live state: %w", err)
		}
	}

	// Parse target state
	var target *unstructured.Unstructured
	if targetJSON != "" && targetJSON != "null" {
		target = &unstructured.Unstructured{}
		if err := json.Unmarshal([]byte(targetJSON), &target.Object); err != nil {
			return "", fmt.Errorf("parsing target state: %w", err)
		}
	}

	// Handle creation/deletion cases
	if live == nil && target == nil {
		return "", nil
	}

	// Use gitops-engine's diff function
	// This performs a three-way diff if kubectl.kubernetes.io/last-applied-configuration exists,
	// otherwise a two-way diff between live and target
	diffResult, err := diff.Diff(target, live)
	if err != nil {
		return "", fmt.Errorf("computing diff: %w", err)
	}

	// If no modifications, return empty diff
	if !diffResult.Modified {
		return "", nil
	}

	// Generate human-readable unified diff from the normalized states
	return generateUnifiedDiff(diffResult, live, target)
}

// generateUnifiedDiff creates a unified diff string from the gitops-engine diff result.
// It uses the NormalizedLive and PredictedLive from the diff result when available,
// falling back to the original objects if needed.
func generateUnifiedDiff(diffResult *diff.DiffResult, live, target *unstructured.Unstructured) (string, error) {
	var liveYAML, targetYAML string

	// Use normalized states from diff result if available
	if len(diffResult.NormalizedLive) > 0 {
		liveYAML = convertToYAML(diffResult.NormalizedLive)
	} else if live != nil {
		liveYAML = objectToYAML(live)
	}

	if len(diffResult.PredictedLive) > 0 {
		targetYAML = convertToYAML(diffResult.PredictedLive)
	} else if target != nil {
		targetYAML = objectToYAML(target)
	}

	// Generate unified diff
	unifiedDiff := difflib.UnifiedDiff{
		A:        difflib.SplitLines(liveYAML),
		B:        difflib.SplitLines(targetYAML),
		FromFile: "live",
		ToFile:   "target",
		Context:  3,
	}

	return difflib.GetUnifiedDiffString(unifiedDiff)
}

// convertToYAML converts JSON bytes to YAML string for readable diffs.
func convertToYAML(jsonBytes []byte) string {
	if len(jsonBytes) == 0 {
		return ""
	}

	var obj map[string]any
	if err := json.Unmarshal(jsonBytes, &obj); err != nil {
		return string(jsonBytes)
	}

	yamlBytes, err := yaml.Marshal(obj)
	if err != nil {
		return string(jsonBytes)
	}

	return string(yamlBytes)
}

// objectToYAML converts an unstructured object to YAML.
func objectToYAML(obj *unstructured.Unstructured) string {
	if obj == nil {
		return ""
	}

	yamlBytes, err := yaml.Marshal(obj.Object)
	if err != nil {
		jsonBytes, _ := json.MarshalIndent(obj.Object, "", "  ")
		return string(jsonBytes)
	}

	return string(yamlBytes)
}

// computeSimpleDiff is a fallback when gitops-engine diff fails.
// It normalizes and compares JSON states directly.
func computeSimpleDiff(live, target string) string {
	if live == "" || live == "null" {
		live = ""
	}
	if target == "" || target == "null" {
		target = ""
	}

	liveNormalized := normalizeAndConvertToYAML(live)
	targetNormalized := normalizeAndConvertToYAML(target)

	if liveNormalized == targetNormalized {
		return ""
	}

	unifiedDiff := difflib.UnifiedDiff{
		A:        difflib.SplitLines(liveNormalized),
		B:        difflib.SplitLines(targetNormalized),
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

// normalizeAndConvertToYAML normalizes a Kubernetes manifest JSON by removing
// server-side fields and converts it to YAML for better readability.
func normalizeAndConvertToYAML(jsonStr string) string {
	jsonStr = strings.TrimSpace(jsonStr)
	if jsonStr == "" {
		return ""
	}

	var data map[string]any
	if err := json.Unmarshal([]byte(jsonStr), &data); err != nil {
		return jsonStr
	}

	normalizeManifest(data)

	yamlBytes, err := yaml.Marshal(data)
	if err != nil {
		pretty, _ := json.MarshalIndent(data, "", "  ")
		return string(pretty)
	}

	return string(yamlBytes)
}

// normalizeManifest removes server-side fields from a Kubernetes manifest.
// This follows the same normalization as gitops-engine.
func normalizeManifest(data map[string]any) {
	delete(data, "status")

	if metadata, ok := data["metadata"].(map[string]any); ok {
		delete(metadata, "managedFields")
		delete(metadata, "resourceVersion")
		delete(metadata, "uid")
		delete(metadata, "generation")
		delete(metadata, "creationTimestamp")
		delete(metadata, "selfLink")
		delete(metadata, "deletionTimestamp")
		delete(metadata, "deletionGracePeriodSeconds")
		delete(metadata, "ownerReferences")

		if annotations, ok := metadata["annotations"].(map[string]any); ok {
			delete(annotations, "kubectl.kubernetes.io/last-applied-configuration")
			delete(annotations, "argocd.argoproj.io/tracking-id")
			delete(annotations, "deployment.kubernetes.io/revision")
			delete(annotations, "meta.helm.sh/release-name")
			delete(annotations, "meta.helm.sh/release-namespace")
			if len(annotations) == 0 {
				delete(metadata, "annotations")
			}
		}

		if labels, ok := metadata["labels"].(map[string]any); ok && len(labels) == 0 {
			delete(metadata, "labels")
		}

		if finalizers, ok := metadata["finalizers"].([]any); ok && len(finalizers) == 0 {
			delete(metadata, "finalizers")
		}
	}
}

// determineDiffAction determines what will happen to the resource during sync.
func determineDiffAction(live, target, diffStr string) models.DiffAction {
	liveEmpty := live == "" || live == "null"
	targetEmpty := target == "" || target == "null"

	if liveEmpty && !targetEmpty {
		return models.DiffActionCreate
	}
	if !liveEmpty && targetEmpty {
		return models.DiffActionDelete
	}
	if diffStr != "" {
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
