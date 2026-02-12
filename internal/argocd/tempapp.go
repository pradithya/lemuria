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

package argocd

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	v1alpha1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"

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
	OriginalAppName string                // Name of the original application
	TargetBranch    string                // Branch to point to (e.g., "main" or "feature/xyz")
	PRNumber        int                   // For naming and labeling
	PRRepo          string                // Full repository URL (e.g., "https://github.com/owner/repo") to identify which sources to update
	Suffix          string                // "base" or "head"
	AppSpecOverride *v1alpha1.Application // If set, use this spec instead of fetching from live ArgoCD
}

// CreateTempApp creates a temporary application configured for diff rendering.
// The temp app:
// - Has a unique name: {original-name}-lemuria-pr{number}-{suffix}
// - Points git sources matching PRRepo to the specified branch
// - Has syncPolicy.automated removed (no auto-sync)
// - Is labeled for cleanup identification
func (m *TempAppManager) CreateTempApp(ctx context.Context, cfg TempAppConfig) (string, error) {
	var originalApp *v1alpha1.Application

	if cfg.AppSpecOverride != nil {
		// Use the git-sourced spec instead of fetching from live ArgoCD
		originalApp = cfg.AppSpecOverride
	} else {
		// Get the original application spec from live ArgoCD
		var err error
		originalApp, err = m.client.GetApplicationRaw(ctx, cfg.OriginalAppName)
		if err != nil {
			return "", fmt.Errorf("getting original application: %w", err)
		}
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

	slog.Debug("waiting for manifests", "application", appName, "timeout", timeout)

	for time.Now().Before(deadline) {
		// Check if context is cancelled
		if ctx.Err() != nil {
			slog.Debug("context cancelled while waiting for manifests", "application", appName, "error", ctx.Err())
			return nil, ctx.Err()
		}

		// Try to get manifests.
		// If ArgoCD hasn't finished rendering, the API returns a non-200 error.
		// A successful response (even with 0 manifests) means rendering is complete.
		manifests, _, err := m.client.GetManifests(ctx, appName, nil)
		if err != nil {
			slog.Debug("failed to get manifests, will retry", "application", appName, "error", err)
			time.Sleep(pollInterval)
			continue
		}

		slog.Debug("manifests retrieved successfully", "application", appName, "count", len(manifests))
		return manifests, nil
	}

	slog.Debug("timeout waiting for manifests", "application", appName, "timeout", timeout)
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
func buildTempAppSpec(original *v1alpha1.Application, tempName string, cfg TempAppConfig) *v1alpha1.Application {
	// Deep copy the original
	tempApp := original.DeepCopy()

	// Set new name
	tempApp.Name = tempName

	// Remove resourceVersion and other server-set metadata (required for creation)
	tempApp.ResourceVersion = ""
	tempApp.UID = ""
	tempApp.CreationTimestamp.Reset()
	tempApp.Generation = 0
	tempApp.ManagedFields = nil

	// Add identifying labels
	if tempApp.Labels == nil {
		tempApp.Labels = make(map[string]string)
	}
	tempApp.Labels[labelTempApp] = "true"
	tempApp.Labels[labelOriginalApp] = cfg.OriginalAppName
	tempApp.Labels[labelPRNumber] = strconv.Itoa(cfg.PRNumber)
	tempApp.Labels[labelPRRepo] = sanitizeLabelValue(cfg.PRRepo)

	// Remove automated sync policy to prevent actual syncing
	if tempApp.Spec.SyncPolicy != nil {
		tempApp.Spec.SyncPolicy.Automated = nil
	}

	// Update source(s) to point to target branch
	prRepoNormalized := NormalizeRepoURL(cfg.PRRepo)

	// Handle single source
	if tempApp.Spec.Source != nil {
		if NormalizeRepoURL(tempApp.Spec.Source.RepoURL) == prRepoNormalized {
			tempApp.Spec.Source.TargetRevision = cfg.TargetBranch
		}
	}

	// Handle multi-source
	for i := range tempApp.Spec.Sources {
		if NormalizeRepoURL(tempApp.Spec.Sources[i].RepoURL) == prRepoNormalized {
			tempApp.Spec.Sources[i].TargetRevision = cfg.TargetBranch
		}
	}

	// Remove status (not needed for creation)
	tempApp.Status = v1alpha1.ApplicationStatus{}

	return tempApp
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
