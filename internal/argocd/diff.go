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

// ManagedResource represents a resource from the managed-resources API.
type ManagedResource struct {
	Group               string `json:"group,omitempty"`
	Kind                string `json:"kind,omitempty"`
	Namespace           string `json:"namespace,omitempty"`
	Name                string `json:"name,omitempty"`
	LiveState           string `json:"liveState,omitempty"`
	NormalizedLiveState string `json:"normalizedLiveState,omitempty"`
	Hook                bool   `json:"hook,omitempty"`
}

// resourceKey creates a unique key for a resource.
func resourceKey(group, kind, namespace, name string) string {
	if namespace != "" {
		return fmt.Sprintf("%s/%s/%s/%s", group, kind, namespace, name)
	}
	return fmt.Sprintf("%s/%s/%s", group, kind, name)
}

// GetApplicationDiff computes the diff between live cluster state and target revision.
// This mimics `argocd app diff --revision <revision>` behavior.
//
// revision: the git revision to compare against live state. Required.
//
// Returns what changes would be applied to the cluster if synced to this revision.
func (c *Client) GetApplicationDiff(ctx context.Context, name string, revision string) ([]models.ManifestDiff, error) {
	if revision == "" {
		return nil, fmt.Errorf("revision is required")
	}

	// Get live state from cluster via managed-resources API
	liveResources, err := c.getManagedResources(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("getting live resources: %w", err)
	}

	// Get target manifests at the specified revision
	targetManifests, _, err := c.GetManifests(ctx, name, &GetManifestsParams{Revision: revision})
	if err != nil {
		return nil, fmt.Errorf("getting manifests at revision %s: %w", revision, err)
	}

	return computeLiveVsTargetDiff(liveResources, targetManifests), nil
}

// getManagedResources fetches the live state of resources from the cluster.
func (c *Client) getManagedResources(ctx context.Context, name string) ([]ManagedResource, error) {
	var resp struct {
		Items []ManagedResource `json:"items"`
	}

	if err := c.get(ctx, "/api/v1/applications/"+url.PathEscape(name)+"/managed-resources", nil, &resp); err != nil {
		return nil, err
	}

	return resp.Items, nil
}

// computeLiveVsTargetDiff compares live cluster state against target manifests.
func computeLiveVsTargetDiff(liveResources []ManagedResource, targetManifests []models.Manifest) []models.ManifestDiff {
	// Build maps for comparison
	liveMap := make(map[string]ManagedResource)
	for _, res := range liveResources {
		if res.Hook {
			continue
		}
		// Skip secrets
		if res.Kind == "Secret" && (res.Group == "" || res.Group == "v1") {
			continue
		}
		key := resourceKey(res.Group, res.Kind, res.Namespace, res.Name)
		liveMap[key] = res
	}

	targetMap := buildManifestMap(targetManifests)

	var diffs []models.ManifestDiff

	// Check resources in target (creates and updates)
	for key, targetManifest := range targetMap {
		resource := parseResourceKey(key)

		// Skip secrets
		if resource.Kind == "Secret" && (resource.APIVersion == "" || resource.APIVersion == "v1") {
			continue
		}

		liveRes, exists := liveMap[key]

		if !exists {
			// Resource doesn't exist in cluster - will be created
			diffs = append(diffs, models.ManifestDiff{
				Resource:    resource,
				LiveState:   "",
				TargetState: targetManifest.Raw,
				Diff:        formatCreateDiff(targetManifest.Raw),
				Action:      models.DiffActionCreate,
			})
		} else {
			// Resource exists - check for updates
			// Use NormalizedLiveState for cleaner comparison
			liveState := liveRes.NormalizedLiveState
			if liveState == "" {
				liveState = liveRes.LiveState
			}

			diffStr := computeDiff(liveState, targetManifest.Raw)
			if diffStr != "" {
				diffs = append(diffs, models.ManifestDiff{
					Resource:    resource,
					LiveState:   liveState,
					TargetState: targetManifest.Raw,
					Diff:        diffStr,
					Action:      models.DiffActionUpdate,
				})
			}
		}
	}

	// Check for resources that exist live but not in target (deletes)
	// Note: This only shows resources that ArgoCD is tracking (managed resources)
	for key, liveRes := range liveMap {
		if _, exists := targetMap[key]; exists {
			continue
		}

		resource := models.ResourceKey{
			APIVersion: liveRes.Group,
			Kind:       liveRes.Kind,
			Name:       liveRes.Name,
			Namespace:  liveRes.Namespace,
		}

		liveState := liveRes.NormalizedLiveState
		if liveState == "" {
			liveState = liveRes.LiveState
		}

		// Only show delete if there's actual live state
		if liveState == "" || liveState == "null" {
			continue
		}

		diffs = append(diffs, models.ManifestDiff{
			Resource:    resource,
			LiveState:   liveState,
			TargetState: "",
			Diff:        formatDeleteDiff(liveState),
			Action:      models.DiffActionDelete,
		})
	}

	sort.Slice(diffs, func(i, j int) bool {
		return diffs[i].Resource.String() < diffs[j].Resource.String()
	})

	return diffs
}

// computeManifestsDiff compares two sets of manifests (for testing or manifest-only comparison).
func computeManifestsDiff(current, target []models.Manifest) []models.ManifestDiff {
	currentMap := buildManifestMap(current)
	targetMap := buildManifestMap(target)

	var diffs []models.ManifestDiff

	// Check resources in target (creates and updates)
	for key, targetManifest := range targetMap {
		resource := parseResourceKey(key)

		if resource.Kind == "Secret" && (resource.APIVersion == "" || resource.APIVersion == "v1") {
			continue
		}

		currentManifest, exists := currentMap[key]

		if !exists {
			diffs = append(diffs, models.ManifestDiff{
				Resource:    resource,
				LiveState:   "",
				TargetState: targetManifest.Raw,
				Diff:        formatCreateDiff(targetManifest.Raw),
				Action:      models.DiffActionCreate,
			})
		} else {
			diffStr := computeDiff(currentManifest.Raw, targetManifest.Raw)
			if diffStr != "" {
				diffs = append(diffs, models.ManifestDiff{
					Resource:    resource,
					LiveState:   currentManifest.Raw,
					TargetState: targetManifest.Raw,
					Diff:        diffStr,
					Action:      models.DiffActionUpdate,
				})
			}
		}
	}

	// Check for deletes
	for key, currentManifest := range currentMap {
		if _, exists := targetMap[key]; exists {
			continue
		}

		resource := parseResourceKey(key)
		if resource.Kind == "Secret" && (resource.APIVersion == "" || resource.APIVersion == "v1") {
			continue
		}

		diffs = append(diffs, models.ManifestDiff{
			Resource:    resource,
			LiveState:   currentManifest.Raw,
			TargetState: "",
			Diff:        formatDeleteDiff(currentManifest.Raw),
			Action:      models.DiffActionDelete,
		})
	}

	sort.Slice(diffs, func(i, j int) bool {
		return diffs[i].Resource.String() < diffs[j].Resource.String()
	})

	return diffs
}

// buildManifestMap creates a map of manifests keyed by group/kind/namespace/name.
func buildManifestMap(manifests []models.Manifest) map[string]models.Manifest {
	m := make(map[string]models.Manifest)
	for _, manifest := range manifests {
		group := extractGroup(manifest.APIVersion)
		key := resourceKey(group, manifest.Kind, manifest.Namespace, manifest.Name)
		m[key] = manifest
	}
	return m
}

// parseResourceKey parses a resource key back into a ResourceKey struct.
func parseResourceKey(key string) models.ResourceKey {
	parts := strings.SplitN(key, "/", 4)
	var group, kind, namespace, name string
	if len(parts) == 4 {
		group, kind, namespace, name = parts[0], parts[1], parts[2], parts[3]
	} else if len(parts) == 3 {
		group, kind, name = parts[0], parts[1], parts[2]
	}
	return models.ResourceKey{
		APIVersion: group,
		Kind:       kind,
		Name:       name,
		Namespace:  namespace,
	}
}

// extractGroup extracts the API group from an apiVersion.
func extractGroup(apiVersion string) string {
	if idx := strings.Index(apiVersion, "/"); idx != -1 {
		return apiVersion[:idx]
	}
	return ""
}

// computeDiff generates a unified diff between two JSON states.
func computeDiff(currentJSON, targetJSON string) string {
	currentYAML := jsonToYAML(currentJSON)
	targetYAML := jsonToYAML(targetJSON)

	if currentYAML == targetYAML {
		return ""
	}

	return generateUnifiedDiff(currentYAML, targetYAML, "live", "target")
}

// formatCreateDiff formats a diff for a new resource.
func formatCreateDiff(targetJSON string) string {
	targetYAML := jsonToYAML(targetJSON)
	if targetYAML == "" {
		return ""
	}
	return generateUnifiedDiff("", targetYAML, "live", "target")
}

// formatDeleteDiff formats a diff for a deleted resource.
func formatDeleteDiff(currentJSON string) string {
	currentYAML := jsonToYAML(currentJSON)
	if currentYAML == "" {
		return ""
	}
	return generateUnifiedDiff(currentYAML, "", "live", "target")
}

// generateUnifiedDiff creates a unified diff string.
func generateUnifiedDiff(a, b, fromFile, toFile string) string {
	diff := difflib.UnifiedDiff{
		A:        difflib.SplitLines(a),
		B:        difflib.SplitLines(b),
		FromFile: fromFile,
		ToFile:   toFile,
		Context:  3,
	}

	result, err := difflib.GetUnifiedDiffString(diff)
	if err != nil {
		return ""
	}

	return result
}

// jsonToYAML converts a JSON string to YAML for human-readable diffs.
func jsonToYAML(jsonStr string) string {
	jsonStr = strings.TrimSpace(jsonStr)
	if jsonStr == "" || jsonStr == "null" {
		return ""
	}

	var data map[string]any
	if err := json.Unmarshal([]byte(jsonStr), &data); err != nil {
		return jsonStr
	}

	cleanForDiff(data)

	yamlBytes, err := yaml.Marshal(data)
	if err != nil {
		pretty, _ := json.MarshalIndent(data, "", "  ")
		return string(pretty)
	}

	return string(yamlBytes)
}

// cleanForDiff removes fields that cause noisy diffs.
func cleanForDiff(data map[string]any) {
	if metadata, ok := data["metadata"].(map[string]any); ok {
		delete(metadata, "managedFields")
		delete(metadata, "resourceVersion")
		delete(metadata, "uid")
		delete(metadata, "creationTimestamp")
		delete(metadata, "generation")
		if annotations, ok := metadata["annotations"].(map[string]any); ok {
			delete(annotations, "kubectl.kubernetes.io/last-applied-configuration")
			if len(annotations) == 0 {
				delete(metadata, "annotations")
			}
		}
	}
	delete(data, "status")
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
