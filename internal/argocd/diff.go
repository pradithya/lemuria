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
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	v1alpha1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
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

// DiffMode specifies how to compute diffs.
type DiffMode string

const (
	// DiffModeBranch compares base branch vs target branch (what the PR changes).
	DiffModeBranch DiffMode = "branch"
	// DiffModeLive compares live cluster vs target branch (what would change if synced).
	DiffModeLive DiffMode = "live"
	// DiffModeBoth performs a 3-way diff: base branch, target branch, and live cluster.
	// Shows both what the PR changes and what would be synced to the cluster.
	DiffModeBoth DiffMode = "both"
)

// DiffOptions configures diff generation.
type DiffOptions struct {
	Mode         DiffMode      // "branch", "live", or "both"
	BaseBranch   string        // Required for DiffModeBranch/DiffModeBoth (e.g., "main")
	TargetBranch string        // Target branch (e.g., "feature/my-change")
	PRNumber     int           // PR number for temp app naming
	PRRepo       string        // Full repository URL (e.g., "https://github.com/owner/repo")
	Timeout      time.Duration // Timeout for manifest rendering

	// BaseAppSpec and HeadAppSpec are typed Application specs read from git.
	// When set, they override the live ArgoCD spec for temp app creation,
	// allowing diffs to reflect inline changes to Application CRs (e.g., Helm values).
	BaseAppSpec *v1alpha1.Application // Application spec from base branch (nil = use live ArgoCD)
	HeadAppSpec *v1alpha1.Application // Application spec from head branch (nil = use live ArgoCD)
}

// DiffNewApp computes a diff for a new application that doesn't yet exist in ArgoCD.
// It diffs empty (nil) current manifests against the head branch manifests, so all
// resources appear as creates.
func (c *Client) DiffNewApp(ctx context.Context, appSpec *v1alpha1.Application, opts DiffOptions) ([]models.ManifestDiff, error) {
	if opts.Timeout == 0 {
		opts.Timeout = 2 * time.Minute
	}

	tempMgr := NewTempAppManager(c)

	// Create temp app using the actual app name (idempotent — handles pre-existing apps)
	targetAppName, err := tempMgr.CreateNewApp(ctx, TempAppConfig{
		OriginalAppName: appSpec.Name,
		TargetBranch:    opts.TargetBranch,
		PRNumber:        opts.PRNumber,
		PRRepo:          opts.PRRepo,
		AppSpecOverride: appSpec,
	})
	if err != nil {
		return nil, fmt.Errorf("creating temp app for new application: %w", err)
	}
	defer func() {
		_ = tempMgr.DeleteTempApp(context.Background(), targetAppName)
	}()

	// Wait for manifests
	targetManifests, err := tempMgr.WaitForManifests(ctx, targetAppName, opts.Timeout)
	if err != nil {
		return nil, fmt.Errorf("waiting for manifests for new application: %w", err)
	}

	// Diff nil (empty) current against target — everything is a create
	return computeManifestsDiff(nil, targetManifests), nil
}

// DiffDeletedApp computes a diff for an application that will be deleted.
// It diffs current manifests against empty (nil) target, so all resources
// appear as deletes. Respects the configured diff mode.
func (c *Client) DiffDeletedApp(ctx context.Context, appName string, opts DiffOptions) ([]models.ManifestDiff, error) {
	if opts.Timeout == 0 {
		opts.Timeout = 2 * time.Minute
	}

	tempMgr := NewTempAppManager(c)

	switch opts.Mode {
	case DiffModeLive:
		return c.diffDeletedLive(ctx, appName)
	case DiffModeBoth:
		return c.diffDeletedBoth(ctx, tempMgr, appName, opts)
	default:
		// Default to branch mode
		return c.diffDeletedBranch(ctx, tempMgr, appName, opts)
	}
}

// diffDeletedBranch diffs base branch manifests against empty target for a deleted app.
func (c *Client) diffDeletedBranch(ctx context.Context, tempMgr *TempAppManager, appName string, opts DiffOptions) ([]models.ManifestDiff, error) {
	if opts.BaseBranch == "" {
		return nil, fmt.Errorf("base branch is required for branch diff mode")
	}

	// Create temp app from base branch
	baseAppName, err := tempMgr.CreateTempApp(ctx, TempAppConfig{
		OriginalAppName: appName,
		TargetBranch:    opts.BaseBranch,
		PRNumber:        opts.PRNumber,
		PRRepo:          opts.PRRepo,
		Suffix:          "base",
		AppSpecOverride: opts.BaseAppSpec,
	})
	if err != nil {
		return nil, fmt.Errorf("creating base temp app for deleted application: %w", err)
	}
	defer func() {
		_ = tempMgr.DeleteTempApp(context.Background(), baseAppName)
	}()

	baseManifests, err := tempMgr.WaitForManifests(ctx, baseAppName, opts.Timeout)
	if err != nil {
		return nil, fmt.Errorf("waiting for base manifests for deleted application: %w", err)
	}

	// Diff base manifests against nil target — everything is a delete
	return computeManifestsDiff(baseManifests, nil), nil
}

// diffDeletedLive diffs live managed resources against empty target for a deleted app.
func (c *Client) diffDeletedLive(ctx context.Context, appName string) ([]models.ManifestDiff, error) {
	liveResources, err := c.getManagedResources(ctx, appName)
	if err != nil {
		return nil, fmt.Errorf("getting live resources for deleted application: %w", err)
	}

	// Diff live resources against nil target — everything is a delete
	return computeLiveVsTargetDiff(liveResources, nil), nil
}

// diffDeletedBoth performs a 3-way diff for a deleted app: base branch manifests,
// empty target, and live cluster state.
func (c *Client) diffDeletedBoth(ctx context.Context, tempMgr *TempAppManager, appName string, opts DiffOptions) ([]models.ManifestDiff, error) {
	if opts.BaseBranch == "" {
		return nil, fmt.Errorf("base branch is required for both diff mode")
	}

	// Get live state
	liveResources, err := c.getManagedResources(ctx, appName)
	if err != nil {
		return nil, fmt.Errorf("getting live resources for deleted application: %w", err)
	}

	// Create temp app from base branch
	baseAppName, err := tempMgr.CreateTempApp(ctx, TempAppConfig{
		OriginalAppName: appName,
		TargetBranch:    opts.BaseBranch,
		PRNumber:        opts.PRNumber,
		PRRepo:          opts.PRRepo,
		Suffix:          "base",
		AppSpecOverride: opts.BaseAppSpec,
	})
	if err != nil {
		return nil, fmt.Errorf("creating base temp app for deleted application: %w", err)
	}
	defer func() {
		_ = tempMgr.DeleteTempApp(context.Background(), baseAppName)
	}()

	baseManifests, err := tempMgr.WaitForManifests(ctx, baseAppName, opts.Timeout)
	if err != nil {
		return nil, fmt.Errorf("waiting for base manifests for deleted application: %w", err)
	}

	// 3-way diff: base manifests, nil target, live resources
	return computeThreeWayDiff(baseManifests, nil, liveResources), nil
}

// GetApplicationDiff computes diff using temporary applications.
// This handles multi-source apps with external Helm charts correctly.
//
// For DiffModeBranch: creates temp apps for base and target branches, compares their manifests.
// For DiffModeLive: compares live cluster state against temp app for target branch.
// For DiffModeBoth: 3-way comparison of base branch, target branch, and live cluster.
func (c *Client) GetApplicationDiff(ctx context.Context, appName string, opts DiffOptions) ([]models.ManifestDiff, error) {
	if opts.Timeout == 0 {
		opts.Timeout = 2 * time.Minute
	}

	tempMgr := NewTempAppManager(c)

	switch opts.Mode {
	case DiffModeBranch:
		return c.diffBranchMode(ctx, tempMgr, appName, opts)
	case DiffModeLive:
		return c.diffLiveMode(ctx, tempMgr, appName, opts)
	case DiffModeBoth:
		return c.diffBothMode(ctx, tempMgr, appName, opts)
	default:
		// Default to branch mode
		return c.diffBranchMode(ctx, tempMgr, appName, opts)
	}
}

// diffBranchMode compares base branch vs target branch.
func (c *Client) diffBranchMode(ctx context.Context, tempMgr *TempAppManager, appName string, opts DiffOptions) ([]models.ManifestDiff, error) {
	if opts.BaseBranch == "" {
		return nil, fmt.Errorf("base branch is required for branch diff mode")
	}
	if opts.TargetBranch == "" {
		return nil, fmt.Errorf("target branch is required for branch diff mode")
	}

	var baseAppName, targetAppName string
	var cleanupFuncs []func()

	defer func() {
		// Cleanup temp apps in reverse order
		for i := len(cleanupFuncs) - 1; i >= 0; i-- {
			cleanupFuncs[i]()
		}
	}()

	// Create temp app for base branch
	var err error
	baseAppName, err = tempMgr.CreateTempApp(ctx, TempAppConfig{
		OriginalAppName: appName,
		TargetBranch:    opts.BaseBranch,
		PRNumber:        opts.PRNumber,
		PRRepo:          opts.PRRepo,
		Suffix:          "base",
		AppSpecOverride: opts.BaseAppSpec,
	})
	if err != nil {
		return nil, fmt.Errorf("creating base temp app: %w", err)
	}
	cleanupFuncs = append(cleanupFuncs, func() {
		_ = tempMgr.DeleteTempApp(context.Background(), baseAppName)
	})

	// Create temp app for target branch
	targetAppName, err = tempMgr.CreateTempApp(ctx, TempAppConfig{
		OriginalAppName: appName,
		TargetBranch:    opts.TargetBranch,
		PRNumber:        opts.PRNumber,
		PRRepo:          opts.PRRepo,
		Suffix:          "head",
		AppSpecOverride: opts.HeadAppSpec,
	})
	if err != nil {
		return nil, fmt.Errorf("creating target temp app: %w", err)
	}
	cleanupFuncs = append(cleanupFuncs, func() {
		_ = tempMgr.DeleteTempApp(context.Background(), targetAppName)
	})

	// Wait for manifests from both apps
	baseManifests, err := tempMgr.WaitForManifests(ctx, baseAppName, opts.Timeout)
	if err != nil {
		return nil, fmt.Errorf("waiting for base manifests: %w", err)
	}

	targetManifests, err := tempMgr.WaitForManifests(ctx, targetAppName, opts.Timeout)
	if err != nil {
		return nil, fmt.Errorf("waiting for target manifests: %w", err)
	}

	// Compute diff between base and target
	return computeManifestsDiff(baseManifests, targetManifests), nil
}

// diffLiveMode compares live cluster state vs target branch.
func (c *Client) diffLiveMode(ctx context.Context, tempMgr *TempAppManager, appName string, opts DiffOptions) ([]models.ManifestDiff, error) {
	if opts.TargetBranch == "" {
		return nil, fmt.Errorf("target branch is required for live diff mode")
	}

	// Get live state from cluster
	liveResources, err := c.getManagedResources(ctx, appName)
	if err != nil {
		return nil, fmt.Errorf("getting live resources: %w", err)
	}

	// Create temp app for target branch
	targetAppName, err := tempMgr.CreateTempApp(ctx, TempAppConfig{
		OriginalAppName: appName,
		TargetBranch:    opts.TargetBranch,
		PRNumber:        opts.PRNumber,
		PRRepo:          opts.PRRepo,
		Suffix:          "head",
		AppSpecOverride: opts.HeadAppSpec,
	})
	if err != nil {
		return nil, fmt.Errorf("creating target temp app: %w", err)
	}
	defer func() {
		_ = tempMgr.DeleteTempApp(context.Background(), targetAppName)
	}()

	// Wait for manifests
	targetManifests, err := tempMgr.WaitForManifests(ctx, targetAppName, opts.Timeout)
	if err != nil {
		return nil, fmt.Errorf("waiting for target manifests: %w", err)
	}

	// Compute diff between live and target
	return computeLiveVsTargetDiff(liveResources, targetManifests), nil
}

// diffBothMode performs a 3-way diff: base branch vs target branch vs live cluster.
func (c *Client) diffBothMode(ctx context.Context, tempMgr *TempAppManager, appName string, opts DiffOptions) ([]models.ManifestDiff, error) {
	if opts.BaseBranch == "" {
		return nil, fmt.Errorf("base branch is required for both diff mode")
	}
	if opts.TargetBranch == "" {
		return nil, fmt.Errorf("target branch is required for both diff mode")
	}

	var cleanupFuncs []func()
	defer func() {
		for i := len(cleanupFuncs) - 1; i >= 0; i-- {
			cleanupFuncs[i]()
		}
	}()

	// Get live state from cluster
	liveResources, err := c.getManagedResources(ctx, appName)
	if err != nil {
		return nil, fmt.Errorf("getting live resources: %w", err)
	}

	// Create temp app for base branch
	baseAppName, err := tempMgr.CreateTempApp(ctx, TempAppConfig{
		OriginalAppName: appName,
		TargetBranch:    opts.BaseBranch,
		PRNumber:        opts.PRNumber,
		PRRepo:          opts.PRRepo,
		Suffix:          "base",
		AppSpecOverride: opts.BaseAppSpec,
	})
	if err != nil {
		return nil, fmt.Errorf("creating base temp app: %w", err)
	}
	cleanupFuncs = append(cleanupFuncs, func() {
		_ = tempMgr.DeleteTempApp(context.Background(), baseAppName)
	})

	// Create temp app for target branch
	targetAppName, err := tempMgr.CreateTempApp(ctx, TempAppConfig{
		OriginalAppName: appName,
		TargetBranch:    opts.TargetBranch,
		PRNumber:        opts.PRNumber,
		PRRepo:          opts.PRRepo,
		Suffix:          "head",
		AppSpecOverride: opts.HeadAppSpec,
	})
	if err != nil {
		return nil, fmt.Errorf("creating target temp app: %w", err)
	}
	cleanupFuncs = append(cleanupFuncs, func() {
		_ = tempMgr.DeleteTempApp(context.Background(), targetAppName)
	})

	// Wait for manifests from both apps
	baseManifests, err := tempMgr.WaitForManifests(ctx, baseAppName, opts.Timeout)
	if err != nil {
		return nil, fmt.Errorf("waiting for base manifests: %w", err)
	}

	targetManifests, err := tempMgr.WaitForManifests(ctx, targetAppName, opts.Timeout)
	if err != nil {
		return nil, fmt.Errorf("waiting for target manifests: %w", err)
	}

	// Compute 3-way diff
	return computeThreeWayDiff(baseManifests, targetManifests, liveResources), nil
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

// computeStateDiff computes a diff between two states based on their existence.
// Returns the diff string and the action taken.
func computeStateDiff(stateA, stateB string, existsA, existsB bool) (string, models.DiffAction) {
	if !existsA && existsB {
		return formatCreateDiff(stateB), models.DiffActionCreate
	}
	if existsA && !existsB {
		return formatDeleteDiff(stateA), models.DiffActionDelete
	}
	if existsA && existsB {
		d := computeDiff(stateA, stateB)
		if d != "" {
			return d, models.DiffActionUpdate
		}
		return "", models.DiffActionNone
	}
	return "", models.DiffActionNone
}

// buildLiveResourceMap builds a map of live resources keyed by resource key,
// filtering out hooks and secrets.
func buildLiveResourceMap(liveResources []ManagedResource) map[string]ManagedResource {
	liveMap := make(map[string]ManagedResource)
	for _, res := range liveResources {
		if res.Hook {
			continue
		}
		if res.Kind == "Secret" && (res.Group == "" || res.Group == "v1") {
			continue
		}
		key := resourceKey(res.Group, res.Kind, res.Namespace, res.Name)
		liveMap[key] = res
	}
	return liveMap
}

// liveResourceState returns the best available state string for a live resource.
func liveResourceState(res ManagedResource) string {
	if res.NormalizedLiveState != "" {
		return res.NormalizedLiveState
	}
	return res.LiveState
}

// computeLiveVsTargetDiff compares live cluster state against target manifests.
func computeLiveVsTargetDiff(liveResources []ManagedResource, targetManifests []models.Manifest) []models.ManifestDiff {
	liveMap := buildLiveResourceMap(liveResources)
	targetMap := buildManifestMap(targetManifests)

	var diffs []models.ManifestDiff

	// Check resources in target (creates and updates)
	for key, targetManifest := range targetMap {
		resource := parseResourceKey(key)
		if resource.Kind == "Secret" && (resource.APIVersion == "" || resource.APIVersion == "v1") {
			continue
		}

		liveRes, exists := liveMap[key]
		liveState := ""
		if exists {
			liveState = liveResourceState(liveRes)
		}

		diffStr, action := computeStateDiff(liveState, targetManifest.Raw, exists, true)
		if diffStr != "" {
			diffs = append(diffs, models.ManifestDiff{
				Resource:    resource,
				LiveState:   liveState,
				TargetState: targetManifest.Raw,
				Diff:        diffStr,
				Action:      action,
			})
		}
	}

	// Check for resources that exist live but not in target (deletes)
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

		liveState := liveResourceState(liveRes)
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

// computeThreeWayDiff compares base branch, target branch, and live cluster state.
// Returns diffs with both BranchDiff (base→target) and LiveDiff (live→target).
func computeThreeWayDiff(baseManifests, targetManifests []models.Manifest, liveResources []ManagedResource) []models.ManifestDiff {
	baseMap := buildManifestMap(baseManifests)
	targetMap := buildManifestMap(targetManifests)
	liveMap := buildLiveResourceMap(liveResources)

	// Collect all unique keys
	allKeys := make(map[string]bool)
	for key := range baseMap {
		allKeys[key] = true
	}
	for key := range targetMap {
		allKeys[key] = true
	}
	for key := range liveMap {
		allKeys[key] = true
	}

	var diffs []models.ManifestDiff

	for key := range allKeys {
		resource := parseResourceKey(key)
		if resource.Kind == "Secret" && (resource.APIVersion == "" || resource.APIVersion == "v1") {
			continue
		}

		baseManifest, baseExists := baseMap[key]
		targetManifest, targetExists := targetMap[key]
		liveRes, liveExists := liveMap[key]

		var baseState, targetState, liveState string
		if baseExists {
			baseState = baseManifest.Raw
		}
		if targetExists {
			targetState = targetManifest.Raw
		}
		if liveExists {
			liveState = liveResourceState(liveRes)
		}

		branchDiff, branchAction := computeStateDiff(baseState, targetState, baseExists, targetExists)
		liveDiff, liveAction := computeStateDiff(liveState, targetState, liveExists, targetExists)

		if branchDiff != "" || liveDiff != "" {
			diffs = append(diffs, models.ManifestDiff{
				Resource:    resource,
				BaseState:   baseState,
				LiveState:   liveState,
				TargetState: targetState,
				Diff:        branchDiff,
				BranchDiff:  branchDiff,
				LiveDiff:    liveDiff,
				Action:      branchAction,
				LiveAction:  liveAction,
			})
		}
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

// knownAutoGeneratedLabels are labels that are auto-generated by ArgoCD
// and should be omitted from diffs to reduce noise.
var knownAutoGeneratedLabels = []string{
	"argocd.argoproj.io/instance",
	"app.kubernetes.io/instance",
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
			// Remove kubectl annotations
			delete(annotations, "kubectl.kubernetes.io/last-applied-configuration")
			// Remove ArgoCD annotations that are auto-generated
			delete(annotations, "argocd.argoproj.io/tracking-id")
			delete(annotations, "argocd.argoproj.io/compare-options")
			delete(annotations, "argocd.argoproj.io/sync-options")
			delete(annotations, "argocd.argoproj.io/manifest-generate-paths")
			if len(annotations) == 0 {
				delete(metadata, "annotations")
			}
		}
		cleanLabels(metadata, "labels")
	}
	cleanSpecSelectors(data)
	delete(data, "status")
}

// cleanLabels removes known auto-generated labels from a labels map
// within the given parent map. If the labels map becomes empty, it is
// removed from the parent.
func cleanLabels(parent map[string]any, key string) {
	labels, ok := parent[key].(map[string]any)
	if !ok {
		return
	}
	for _, label := range knownAutoGeneratedLabels {
		delete(labels, label)
	}
	if len(labels) == 0 {
		delete(parent, key)
	}
}

// cleanSpecSelectors removes known auto-generated labels from label selectors
// in the spec, such as spec.selector.matchLabels, spec.selector (for Services),
// and spec.template.metadata.labels.
func cleanSpecSelectors(data map[string]any) {
	spec, ok := data["spec"].(map[string]any)
	if !ok {
		return
	}

	// Clean spec.selector.matchLabels (Deployment, StatefulSet, DaemonSet, ReplicaSet, Job)
	// and spec.selector direct labels (Service)
	if selector, ok := spec["selector"].(map[string]any); ok {
		cleanLabels(selector, "matchLabels")
		// Service uses spec.selector as a flat label map (no matchLabels wrapper).
		// Clean known labels directly from the selector.
		for _, label := range knownAutoGeneratedLabels {
			delete(selector, label)
		}
	}

	// Clean spec.template.metadata.labels (pod template labels)
	if tmpl, ok := spec["template"].(map[string]any); ok {
		if tmplMeta, ok := tmpl["metadata"].(map[string]any); ok {
			cleanLabels(tmplMeta, "labels")
		}
	}
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
