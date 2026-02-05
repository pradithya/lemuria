package argocd

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/pmezard/go-difflib/difflib"
	"gopkg.in/yaml.v3"

	"github.com/org/lemuria/internal/models"
)

// resourceKey creates a unique key for a resource.
func resourceKey(group, kind, namespace, name string) string {
	if namespace != "" {
		return fmt.Sprintf("%s/%s/%s/%s", group, kind, namespace, name)
	}
	return fmt.Sprintf("%s/%s/%s", group, kind, name)
}

// GetApplicationDiff computes the diff between current target manifests and target manifests at revision.
// If revision is empty, returns empty diff (same revision).
// This compares what would be deployed now vs what would be deployed at the given revision.
func (c *Client) GetApplicationDiff(ctx context.Context, name string, revision string) ([]models.ManifestDiff, error) {
	// Get current target manifests (at app's configured targetRevision)
	currentManifests, _, err := c.GetManifests(ctx, name, nil)
	if err != nil {
		return nil, fmt.Errorf("getting current manifests: %w", err)
	}

	// Get target manifests at the specified revision
	var targetManifests []models.Manifest
	if revision != "" {
		targetManifests, _, err = c.GetManifests(ctx, name, &GetManifestsParams{Revision: revision})
		if err != nil {
			return nil, fmt.Errorf("getting manifests at revision %s: %w", revision, err)
		}
	} else {
		// No revision specified - compare against itself (no diff)
		targetManifests = currentManifests
	}

	// Build maps keyed by group/kind/namespace/name
	currentMap := buildManifestMap(currentManifests)
	targetMap := buildManifestMap(targetManifests)

	var diffs []models.ManifestDiff

	// Check resources in target (creates and updates)
	for key, targetManifest := range targetMap {
		resource := parseResourceKey(key)

		// Skip secrets
		if resource.Kind == "Secret" && (resource.APIVersion == "" || resource.APIVersion == "v1") {
			continue
		}

		currentManifest, exists := currentMap[key]

		if !exists {
			// Resource will be created (exists in target revision but not current)
			diffs = append(diffs, models.ManifestDiff{
				Resource:    resource,
				LiveState:   "",
				TargetState: targetManifest.Raw,
				Diff:        formatCreateDiff(targetManifest.Raw),
				Action:      models.DiffActionCreate,
			})
		} else {
			// Resource exists in both - check for updates
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

	// Check for resources that exist in current but not in target (deletes)
	for key, currentManifest := range currentMap {
		if _, exists := targetMap[key]; exists {
			continue
		}

		resource := parseResourceKey(key)

		// Skip secrets
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

	// Sort for consistent output
	sort.Slice(diffs, func(i, j int) bool {
		return diffs[i].Resource.String() < diffs[j].Resource.String()
	})

	return diffs, nil
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

// extractGroup extracts the API group from an apiVersion (e.g., "apps/v1" -> "apps", "v1" -> "").
func extractGroup(apiVersion string) string {
	if idx := strings.Index(apiVersion, "/"); idx != -1 {
		return apiVersion[:idx]
	}
	return ""
}

// computeDiff generates a unified diff between current and target states.
func computeDiff(currentJSON, targetJSON string) string {
	currentYAML := jsonToYAML(currentJSON)
	targetYAML := jsonToYAML(targetJSON)

	if currentYAML == targetYAML {
		return ""
	}

	return generateUnifiedDiff(currentYAML, targetYAML, "current", "target")
}

// formatCreateDiff formats a diff for a new resource.
func formatCreateDiff(targetJSON string) string {
	targetYAML := jsonToYAML(targetJSON)
	if targetYAML == "" {
		return ""
	}
	return generateUnifiedDiff("", targetYAML, "current", "target")
}

// formatDeleteDiff formats a diff for a deleted resource.
func formatDeleteDiff(currentJSON string) string {
	currentYAML := jsonToYAML(currentJSON)
	if currentYAML == "" {
		return ""
	}
	return generateUnifiedDiff(currentYAML, "", "current", "target")
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

	// Remove fields that cause noisy diffs
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
	// Remove metadata fields that change frequently
	if metadata, ok := data["metadata"].(map[string]any); ok {
		delete(metadata, "managedFields")
		delete(metadata, "resourceVersion")
		delete(metadata, "uid")
		delete(metadata, "creationTimestamp")
		delete(metadata, "generation")
		// Clean up annotations
		if annotations, ok := metadata["annotations"].(map[string]any); ok {
			delete(annotations, "kubectl.kubernetes.io/last-applied-configuration")
			if len(annotations) == 0 {
				delete(metadata, "annotations")
			}
		}
	}
	// Remove status - we only care about spec
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
