package argocd

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/org/lemuria/internal/models"
)

const (
	// Label keys for temporary applications
	labelTempApp     = "lemuria.io/temp-app"
	labelOriginalApp = "lemuria.io/original-app"
	labelPRNumber    = "lemuria.io/pr-number"
	labelPRRepo      = "lemuria.io/pr-repo"
	labelCreatedAt   = "lemuria.io/created-at"
)

// TempAppManager handles temporary application lifecycle for diff generation.
type TempAppManager struct {
	client *Client
}

// NewTempAppManager creates a new TempAppManager.
func NewTempAppManager(client *Client) *TempAppManager {
	return &TempAppManager{client: client}
}

// TempAppConfig configures a temporary application.
type TempAppConfig struct {
	OriginalAppName string // Name of the original application
	TargetBranch    string // Branch to point to (e.g., "main" or "feature/xyz")
	PRNumber        int    // For naming and labeling
	PRRepo          string // Repository (e.g., "owner/repo") to identify which sources to update
	Suffix          string // "base" or "head"
}

// CreateTempApp creates a temporary application configured for diff rendering.
// The temp app:
// - Has a unique name: {original-name}-lemuria-pr{number}-{suffix}
// - Points git sources matching PRRepo to the specified branch
// - Has syncPolicy.automated removed (no auto-sync)
// - Is labeled for cleanup identification
func (m *TempAppManager) CreateTempApp(ctx context.Context, cfg TempAppConfig) (string, error) {
	// Get the original application spec
	originalApp, err := m.client.GetApplicationRaw(ctx, cfg.OriginalAppName)
	if err != nil {
		return "", fmt.Errorf("getting original application: %w", err)
	}

	tempName := generateTempAppName(cfg.OriginalAppName, cfg.PRNumber, cfg.Suffix)

	// Build the temp app spec
	tempApp := buildTempAppSpec(originalApp, tempName, cfg)

	// Create the application
	if err := m.client.CreateApplication(ctx, tempApp); err != nil {
		return "", fmt.Errorf("creating temp application: %w", err)
	}

	return tempName, nil
}

// WaitForManifests waits for ArgoCD to render manifests for the temp app.
// It polls until manifests are available or timeout is reached.
func (m *TempAppManager) WaitForManifests(ctx context.Context, appName string, timeout time.Duration) ([]models.Manifest, error) {
	deadline := time.Now().Add(timeout)
	pollInterval := 2 * time.Second

	for time.Now().Before(deadline) {
		// Check if context is cancelled
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		// Try to get manifests
		manifests, _, err := m.client.GetManifests(ctx, appName, nil)
		if err == nil && len(manifests) > 0 {
			return manifests, nil
		}

		// Check app status for errors
		app, appErr := m.client.GetApplication(ctx, appName)
		if appErr == nil && app != nil {
			// If app has a sync error, it might still have manifests
			if app.HealthStatus == models.HealthStatusDegraded {
				// Try one more time to get manifests even with degraded health
				manifests, _, err = m.client.GetManifests(ctx, appName, nil)
				if err == nil && len(manifests) > 0 {
					return manifests, nil
				}
			}
		}

		time.Sleep(pollInterval)
	}

	return nil, fmt.Errorf("timeout waiting for manifests for %s", appName)
}

// DeleteTempApp removes a temporary application.
func (m *TempAppManager) DeleteTempApp(ctx context.Context, appName string) error {
	// Use cascade=false since we don't want to delete actual resources
	return m.client.DeleteApplication(ctx, appName, false)
}

// CleanupStaleApps removes temp apps older than maxAge.
func (m *TempAppManager) CleanupStaleApps(ctx context.Context, maxAge time.Duration) (int, error) {
	// List all apps with lemuria temp label
	apps, err := m.client.ListApplicationsWithSelector(ctx, labelTempApp+"=true")
	if err != nil {
		return 0, fmt.Errorf("listing temp apps: %w", err)
	}

	var deleted int
	cutoff := time.Now().Add(-maxAge)

	for _, app := range apps {
		createdAtStr, ok := app.Labels[labelCreatedAt]
		if !ok {
			continue
		}

		createdAt, err := time.Parse(time.RFC3339, createdAtStr)
		if err != nil {
			continue
		}

		if createdAt.Before(cutoff) {
			if err := m.DeleteTempApp(ctx, app.Name); err != nil {
				// Log but continue with other apps
				continue
			}
			deleted++
		}
	}

	return deleted, nil
}

// generateTempAppName creates a unique name for a temp application.
func generateTempAppName(baseName string, prNumber int, suffix string) string {
	// ArgoCD app names have a max length of 63 characters (Kubernetes name limit)
	// Format: {base}-lemuria-pr{num}-{suffix}
	name := fmt.Sprintf("%s-lemuria-pr%d-%s", baseName, prNumber, suffix)

	// Truncate if too long (keep suffix visible)
	if len(name) > 63 {
		// Calculate how much we need to trim from baseName
		overflow := len(name) - 63
		if len(baseName) > overflow {
			baseName = baseName[:len(baseName)-overflow]
			name = fmt.Sprintf("%s-lemuria-pr%d-%s", baseName, prNumber, suffix)
		}
	}

	return name
}

// buildTempAppSpec creates a modified application spec for diff rendering.
func buildTempAppSpec(original map[string]interface{}, tempName string, cfg TempAppConfig) map[string]interface{} {
	// Deep copy the original
	tempApp := deepCopyMap(original)

	// Get or create metadata
	metadata, ok := tempApp["metadata"].(map[string]interface{})
	if !ok {
		metadata = make(map[string]interface{})
		tempApp["metadata"] = metadata
	}

	// Set new name
	metadata["name"] = tempName

	// Remove resourceVersion (required for creation)
	delete(metadata, "resourceVersion")
	delete(metadata, "uid")
	delete(metadata, "creationTimestamp")
	delete(metadata, "generation")
	delete(metadata, "managedFields")

	// Add identifying labels
	labels, ok := metadata["labels"].(map[string]interface{})
	if !ok {
		labels = make(map[string]interface{})
		metadata["labels"] = labels
	}
	labels[labelTempApp] = "true"
	labels[labelOriginalApp] = cfg.OriginalAppName
	labels[labelPRNumber] = strconv.Itoa(cfg.PRNumber)
	labels[labelPRRepo] = sanitizeLabelValue(cfg.PRRepo)
	// Get spec
	spec, ok := tempApp["spec"].(map[string]interface{})
	if !ok {
		return tempApp
	}

	// Remove automated sync policy to prevent actual syncing
	if syncPolicy, ok := spec["syncPolicy"].(map[string]interface{}); ok {
		delete(syncPolicy, "automated")
	}

	// Update source(s) to point to target branch
	prRepoNormalized := NormalizeRepoURL("https://github.com/" + cfg.PRRepo)

	// Handle single source
	if source, ok := spec["source"].(map[string]interface{}); ok {
		updateSourceTargetRevision(source, prRepoNormalized, cfg.TargetBranch)
	}

	// Handle multi-source
	if sources, ok := spec["sources"].([]interface{}); ok {
		for _, src := range sources {
			if source, ok := src.(map[string]interface{}); ok {
				updateSourceTargetRevision(source, prRepoNormalized, cfg.TargetBranch)
			}
		}
	}

	// Remove status (not needed for creation)
	delete(tempApp, "status")

	return tempApp
}

// updateSourceTargetRevision updates the targetRevision for sources matching the PR repo.
func updateSourceTargetRevision(source map[string]interface{}, prRepoNormalized, targetBranch string) {
	repoURL, ok := source["repoURL"].(string)
	if !ok {
		return
	}

	// Only update sources that match the PR repository
	if NormalizeRepoURL(repoURL) == prRepoNormalized {
		source["targetRevision"] = targetBranch
	}
	// External sources (Helm charts, other repos) keep their original targetRevision
}

// deepCopyMap creates a deep copy of a map.
func deepCopyMap(m map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})
	for k, v := range m {
		switch val := v.(type) {
		case map[string]interface{}:
			result[k] = deepCopyMap(val)
		case []interface{}:
			result[k] = deepCopySlice(val)
		default:
			result[k] = v
		}
	}
	return result
}

// deepCopySlice creates a deep copy of a slice.
func deepCopySlice(s []interface{}) []interface{} {
	result := make([]interface{}, len(s))
	for i, v := range s {
		switch val := v.(type) {
		case map[string]interface{}:
			result[i] = deepCopyMap(val)
		case []interface{}:
			result[i] = deepCopySlice(val)
		default:
			result[i] = v
		}
	}
	return result
}

// sanitizeLabelValue ensures a string is valid for use as a Kubernetes label value.
// Label values must be empty or consist of alphanumeric characters, '-', '_', or '.',
// and must start and end with an alphanumeric character.
func sanitizeLabelValue(s string) string {
	// Replace common invalid characters
	s = strings.ReplaceAll(s, "/", "-")
	s = strings.ReplaceAll(s, ":", "-")

	// Ensure it starts and ends with alphanumeric
	s = strings.Trim(s, "-_.")

	// Truncate to max label value length (63 characters)
	if len(s) > 63 {
		s = s[:63]
		s = strings.TrimRight(s, "-_.")
	}

	return s
}
