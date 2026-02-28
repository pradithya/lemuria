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

package commands

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"sort"

	"github.com/org/lemuria/internal/argocd"
	"github.com/org/lemuria/internal/models"
)

// ScannedRepoApps holds the result of scanning both branches for Application/ApplicationSet CRs.
type ScannedRepoApps struct {
	HeadApps     []models.Application
	HeadAppSets  []argocd.ParsedAppSet
	BaseApps     []models.Application
	BaseAppSets  []argocd.ParsedAppSet
	HeadContents map[string][]byte
	BaseContents map[string][]byte
}

// scanRepoForApplications fetches all YAML files from both head and base branches,
// parses Application and ApplicationSet CRs from them, and returns the results.
func (e *Executor) scanRepoForApplications(ctx context.Context, event *models.PREvent, crPaths []string) (*ScannedRepoApps, error) {
	slog.Debug("scanning repo for application CRs",
		"repo", event.Repo.FullName,
		"head_ref", event.PR.HeadRef,
		"base_ref", event.PR.BaseRef,
		"cr_paths", crPaths,
	)

	patterns := []string{"*.yaml", "*.yml"}

	// Fetch all YAML files from head branch
	headContents, err := e.vcs.GetFilesByPattern(ctx, event.Repo.Owner, event.Repo.Name, event.PR.HeadRef, patterns, crPaths)
	if err != nil {
		return nil, fmt.Errorf("fetching head branch YAML files: %w", err)
	}

	// Fetch all YAML files from base branch
	baseContents, err := e.vcs.GetFilesByPattern(ctx, event.Repo.Owner, event.Repo.Name, event.PR.BaseRef, patterns, crPaths)
	if err != nil {
		return nil, fmt.Errorf("fetching base branch YAML files: %w", err)
	}

	slog.Debug("fetched YAML files from both branches",
		"head_files", len(headContents),
		"base_files", len(baseContents),
	)

	result := &ScannedRepoApps{
		HeadContents: headContents,
		BaseContents: baseContents,
	}

	// Sort file paths for deterministic iteration order
	headPaths := sortedKeys(headContents)
	basePaths := sortedKeys(baseContents)

	// Parse Application CRs from head branch
	for _, filePath := range headPaths {
		content := headContents[filePath]
		apps, err := argocd.ParseApplicationsFromYAML(content, filePath)
		if err != nil {
			slog.Debug("failed to parse applications from head file", "file", filePath, "error", err)
			continue
		}
		result.HeadApps = append(result.HeadApps, apps...)

		appSets, err := argocd.ParseApplicationSetsFromYAML(content, filePath)
		if err != nil {
			slog.Debug("failed to parse applicationsets from head file", "file", filePath, "error", err)
			continue
		}
		for _, as := range appSets {
			result.HeadAppSets = append(result.HeadAppSets, argocd.ParsedAppSet{
				AppSet:     as.DeepCopy(),
				SourceFile: filePath,
			})
		}
	}

	// Parse Application CRs from base branch
	for _, filePath := range basePaths {
		content := baseContents[filePath]
		apps, err := argocd.ParseApplicationsFromYAML(content, filePath)
		if err != nil {
			slog.Debug("failed to parse applications from base file", "file", filePath, "error", err)
			continue
		}
		result.BaseApps = append(result.BaseApps, apps...)

		appSets, err := argocd.ParseApplicationSetsFromYAML(content, filePath)
		if err != nil {
			slog.Debug("failed to parse applicationsets from base file", "file", filePath, "error", err)
			continue
		}
		for _, as := range appSets {
			result.BaseAppSets = append(result.BaseAppSets, argocd.ParsedAppSet{
				AppSet:     as.DeepCopy(),
				SourceFile: filePath,
			})
		}
	}

	slog.Debug("parsed application CRs from both branches",
		"head_apps", len(result.HeadApps),
		"head_appsets", len(result.HeadAppSets),
		"base_apps", len(result.BaseApps),
		"base_appsets", len(result.BaseAppSets),
	)

	return result, nil
}

// detectApplicationChangesFromScan compares head and base applications to find new, deleted, and modified apps.
func detectApplicationChangesFromScan(scanned *ScannedRepoApps) *argocd.ParsedApplications {
	parsed := &argocd.ParsedApplications{}

	// Build maps by name
	headByName := make(map[string]models.Application)
	for _, app := range scanned.HeadApps {
		headByName[app.Name] = app
	}

	baseByName := make(map[string]models.Application)
	for _, app := range scanned.BaseApps {
		baseByName[app.Name] = app
	}

	// Sort names for deterministic iteration order
	headNames := sortedStringKeys(headByName)
	baseNames := sortedStringKeys(baseByName)

	// New apps: in head but not in base
	for _, name := range headNames {
		app := headByName[name]
		if _, exists := baseByName[name]; !exists {
			slog.Debug("new application detected via scan",
				"app", name,
				"source_file", app.SourceFile,
			)
			app.ChangeType = models.ApplicationNew
			parsed.New = append(parsed.New, app)
		}
	}

	// Deleted apps: in base but not in head
	for _, name := range baseNames {
		app := baseByName[name]
		if _, exists := headByName[name]; !exists {
			slog.Debug("deleted application detected via scan",
				"app", name,
				"source_file", app.SourceFile,
			)
			app.ChangeType = models.ApplicationDeleted
			parsed.Deleted = append(parsed.Deleted, app)
		}
	}

	// Modified apps: in both head and base, but only if the CR content actually changed
	for _, name := range headNames {
		headApp := headByName[name]
		baseApp, exists := baseByName[name]
		if !exists {
			continue
		}
		// Compare the raw CR file content between branches
		headContent, headHasFile := scanned.HeadContents[headApp.SourceFile]
		baseContent, baseHasFile := scanned.BaseContents[baseApp.SourceFile]
		if !headHasFile || !baseHasFile || !bytes.Equal(headContent, baseContent) {
			slog.Debug("modified application detected via scan",
				"app", name,
				"source_file", headApp.SourceFile,
			)
			parsed.Modified = append(parsed.Modified, headApp)
		}
	}

	slog.Debug("application change detection from scan complete",
		"new_count", len(parsed.New),
		"modified_count", len(parsed.Modified),
		"deleted_count", len(parsed.Deleted),
	)

	return parsed
}

// ParsedApplicationSetChanges holds the result of detecting ApplicationSet CR changes in a PR.
type ParsedApplicationSetChanges struct {
	// NewApps are apps that would be generated by newly added/modified AppSets but don't exist currently
	NewApps []models.Application
	// DeletedApps are apps that currently exist but would be removed by modified/deleted AppSets
	DeletedApps []models.Application
	// Modified contains ApplicationSets whose generator changed, with per-AppSet new/removed apps
	Modified []AppSetModification
}

// AppSetModification describes changes from a single ApplicationSet CR modification.
type AppSetModification struct {
	Name        string
	SourceFile  string
	NewApps     []models.Application
	RemovedApps []models.Application
}

// detectApplicationSetChangesFromScan compares head and base ApplicationSets to find new, deleted, and modified appsets.
func (e *Executor) detectApplicationSetChangesFromScan(ctx context.Context, scanned *ScannedRepoApps) (*ParsedApplicationSetChanges, error) {
	slog.Debug("detecting applicationset changes from scan",
		"head_appsets", len(scanned.HeadAppSets),
		"base_appsets", len(scanned.BaseAppSets),
	)

	result := &ParsedApplicationSetChanges{}

	headByName := make(map[string]argocd.ParsedAppSet)
	for _, pas := range scanned.HeadAppSets {
		headByName[pas.AppSet.Name] = pas
	}

	baseByName := make(map[string]argocd.ParsedAppSet)
	for _, pas := range scanned.BaseAppSets {
		baseByName[pas.AppSet.Name] = pas
	}

	// Sort names for deterministic iteration order
	headAppSetNames := sortedParsedAppSetKeys(headByName)
	baseAppSetNames := sortedParsedAppSetKeys(baseByName)

	// AppSets in head but not in base → new
	for _, name := range headAppSetNames {
		headPAS := headByName[name]
		if _, exists := baseByName[name]; !exists {
			apps, err := e.argocd.GenerateApplications(ctx, headPAS.AppSet)
			if err != nil {
				slog.Warn("failed to generate apps for new appset",
					"appset", name, "error", err)
				continue
			}
			for j := range apps {
				apps[j].ChangeType = models.ApplicationNew
				apps[j].ApplicationSetName = name
				apps[j].SourceFile = headPAS.SourceFile
			}
			result.NewApps = append(result.NewApps, apps...)
			slog.Debug("new applicationset detected",
				"appset", name,
				"source_file", headPAS.SourceFile,
				"generated_apps", len(apps),
			)
		}
	}

	// AppSets in base but not in head → deleted
	for _, name := range baseAppSetNames {
		basePAS := baseByName[name]
		if _, exists := headByName[name]; !exists {
			apps, err := e.argocd.GetApplicationsByApplicationSet(ctx, name)
			if err != nil {
				slog.Warn("failed to get apps for deleted appset",
					"appset", name, "error", err)
				continue
			}
			for j := range apps {
				apps[j].ChangeType = models.ApplicationDeleted
				apps[j].ApplicationSetName = name
				apps[j].SourceFile = basePAS.SourceFile
			}
			result.DeletedApps = append(result.DeletedApps, apps...)
			slog.Debug("deleted applicationset detected",
				"appset", name,
				"source_file", basePAS.SourceFile,
				"existing_apps", len(apps),
			)
		}
	}

	// AppSets in both → compare generated apps
	for _, name := range headAppSetNames {
		headPAS := headByName[name]
		basePAS, exists := baseByName[name]
		if !exists {
			continue
		}

		headApps, err := e.argocd.GenerateApplications(ctx, headPAS.AppSet)
		if err != nil {
			slog.Warn("failed to generate head apps for modified appset",
				"appset", name, "error", err)
			continue
		}

		baseApps, err := e.argocd.GenerateApplications(ctx, basePAS.AppSet)
		if err != nil {
			slog.Warn("failed to generate base apps for modified appset",
				"appset", name, "error", err)
			continue
		}

		baseAppNames := make(map[string]bool)
		for _, a := range baseApps {
			baseAppNames[a.Name] = true
		}

		headAppNames := make(map[string]bool)
		for _, a := range headApps {
			headAppNames[a.Name] = true
		}

		mod := AppSetModification{
			Name:       name,
			SourceFile: headPAS.SourceFile,
		}

		for _, a := range headApps {
			if !baseAppNames[a.Name] {
				a.ChangeType = models.ApplicationNew
				a.ApplicationSetName = name
				a.SourceFile = headPAS.SourceFile
				mod.NewApps = append(mod.NewApps, a)
				result.NewApps = append(result.NewApps, a)
			}
		}

		for _, a := range baseApps {
			if !headAppNames[a.Name] {
				a.ChangeType = models.ApplicationDeleted
				a.ApplicationSetName = name
				a.SourceFile = headPAS.SourceFile
				mod.RemovedApps = append(mod.RemovedApps, a)
				result.DeletedApps = append(result.DeletedApps, a)
			}
		}

		if len(mod.NewApps) > 0 || len(mod.RemovedApps) > 0 {
			result.Modified = append(result.Modified, mod)
			slog.Debug("applicationset generator changed",
				"appset", name,
				"source_file", headPAS.SourceFile,
				"new_apps", len(mod.NewApps),
				"removed_apps", len(mod.RemovedApps),
			)
		}
	}

	slog.Debug("applicationset change detection complete",
		"new_apps", len(result.NewApps),
		"deleted_apps", len(result.DeletedApps),
		"modified_appsets", len(result.Modified),
	)

	return result, nil
}

// detectCrossRepoAffectedApps queries ArgoCD API for apps that reference this repo
// but whose Application CRs live in a different repo (cross-repo apps).
func (e *Executor) detectCrossRepoAffectedApps(ctx context.Context, repoURL string, filePaths []string, alreadyDetected map[string]bool) ([]models.Application, error) {
	slog.Debug("detecting cross-repo affected applications",
		"repo_url", repoURL,
		"changed_files", len(filePaths),
		"already_detected", len(alreadyDetected),
	)

	existingApps, err := e.argocd.ListApplications(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing applications for cross-repo detection: %w", err)
	}

	var affected []models.Application
	for _, app := range existingApps {
		// Skip apps already detected via repo scan
		if alreadyDetected[app.Name] {
			continue
		}

		// Check if app references this repo
		repoMatch := false
		for _, appRepo := range app.GetRepoURLs() {
			if argocd.NormalizeRepoURL(appRepo) == argocd.NormalizeRepoURL(repoURL) {
				repoMatch = true
				break
			}
		}
		if !repoMatch {
			continue
		}

		// Check if any changed file is in the app's path
		if app.Path != "" {
			for _, f := range filePaths {
				if pathContains(app.Path, f) {
					slog.Debug("cross-repo application affected",
						"app", app.Name,
						"app_path", app.Path,
						"file", f,
					)
					app.ChangeType = models.ApplicationExisting
					affected = append(affected, app)
					break
				}
			}
		}
	}

	slog.Debug("cross-repo detection complete",
		"affected_count", len(affected),
	)

	return affected, nil
}

// containsAppByName checks if an app with the given name exists in the slice.
func containsAppByName(apps []models.Application, name string) bool {
	for _, app := range apps {
		if app.Name == name {
			return true
		}
	}
	return false
}

// setToSlice converts a string set to a slice.
func setToSlice(s map[string]struct{}) []string {
	result := make([]string, 0, len(s))
	for k := range s {
		result = append(result, k)
	}
	return result
}

// verifyNewAppsExist logs which "new" apps already exist in ArgoCD.
// These apps are kept in parsed.New (not reclassified) because the new-app
// diff path (DiffNewApp) handles idempotency — it works correctly whether
// the app already exists or not. Reclassifying to "existing" would cause
// failures when the base branch doesn't contain the app's source files.
func (e *Executor) verifyNewAppsExist(ctx context.Context, parsed *argocd.ParsedApplications) error {
	if len(parsed.New) == 0 {
		slog.Debug("no new applications to verify")
		return nil
	}

	slog.Debug("verifying new applications against ArgoCD",
		"new_apps_count", len(parsed.New),
	)

	existingApps, err := e.argocd.ListApplications(ctx)
	if err != nil {
		return fmt.Errorf("listing existing applications: %w", err)
	}

	existingByName := make(map[string]bool)
	for _, app := range existingApps {
		existingByName[app.Name] = true
	}

	for _, app := range parsed.New {
		if existingByName[app.Name] {
			slog.Debug("application marked as new already exists in ArgoCD (keeping as new for idempotent diff)",
				"app", app.Name,
			)
		} else {
			slog.Debug("application confirmed as truly new",
				"app", app.Name,
			)
		}
	}

	return nil
}

// verifyDeletedAppsExist checks which "deleted" apps actually exist in ArgoCD.
func (e *Executor) verifyDeletedAppsExist(ctx context.Context, parsed *argocd.ParsedApplications) error {
	if len(parsed.Deleted) == 0 {
		slog.Debug("no deleted applications to verify")
		return nil
	}

	slog.Debug("verifying deleted applications exist in ArgoCD",
		"deleted_apps_count", len(parsed.Deleted),
	)

	existingApps, err := e.argocd.ListApplications(ctx)
	if err != nil {
		return fmt.Errorf("listing existing applications: %w", err)
	}

	existingByName := make(map[string]bool)
	for _, app := range existingApps {
		existingByName[app.Name] = true
	}

	// Only keep deleted apps that actually exist
	var actuallyDeleted []models.Application
	for _, app := range parsed.Deleted {
		if existingByName[app.Name] {
			slog.Debug("deleted application exists in ArgoCD",
				"app", app.Name,
			)
			actuallyDeleted = append(actuallyDeleted, app)
		} else {
			slog.Debug("deleted application does not exist in ArgoCD (ignoring)",
				"app", app.Name,
			)
		}
	}

	slog.Debug("deleted applications verification complete",
		"confirmed_deleted", len(actuallyDeleted),
		"ignored", len(parsed.Deleted)-len(actuallyDeleted),
	)

	parsed.Deleted = actuallyDeleted

	return nil
}

// sortedStringKeys returns the keys of a map[string]models.Application sorted alphabetically.
func sortedStringKeys(m map[string]models.Application) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// sortedParsedAppSetKeys returns the keys of a map[string]argocd.ParsedAppSet sorted alphabetically.
func sortedParsedAppSetKeys(m map[string]argocd.ParsedAppSet) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// sortedKeys returns the keys of a map sorted alphabetically.
func sortedKeys(m map[string][]byte) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
