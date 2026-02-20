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
	"context"
	"fmt"
	"log/slog"

	"github.com/org/lemuria/internal/argocd"
	"github.com/org/lemuria/internal/models"
	"github.com/org/lemuria/internal/vcs"
)

// detectApplicationChanges analyzes PR files to detect new, modified, and deleted applications.
// It compares Application CRs in the PR with existing applications in ArgoCD.
//
// Instead of fetching each file individually, it batch-fetches all needed files
// via archive downloads (max 2 HTTP calls: one per ref).
func (e *Executor) detectApplicationChanges(ctx context.Context, event *models.PREvent) (*argocd.ParsedApplications, map[string][]byte, map[string][]byte, error) {
	slog.Debug("detecting application changes from PR files",
		"repo", event.Repo.FullName,
		"pr", event.PR.Number,
		"head_ref", event.PR.HeadRef,
		"base_ref", event.PR.BaseRef,
	)

	// Get changed files
	files, err := e.vcs.GetChangedFiles(ctx, event.Repo.Owner, event.Repo.Name, event.PR.Number)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("getting changed files: %w", err)
	}

	slog.Debug("analyzing changed files for Application CRs",
		"total_files", len(files),
	)

	// --- Collect phase: determine which file paths are needed per ref ---
	headPathSet := make(map[string]struct{})
	basePathSet := make(map[string]struct{})

	var yamlFiles []models.ChangedFile
	for _, file := range files {
		if !vcs.IsYAMLFile(file.Filename) {
			slog.Debug("skipping non-YAML file", "file", file.Filename)
			continue
		}
		yamlFiles = append(yamlFiles, file)

		switch file.Status {
		case models.FileStatusAdded:
			headPathSet[file.Filename] = struct{}{}
		case models.FileStatusRemoved:
			basePathSet[file.Filename] = struct{}{}
		case models.FileStatusModified, models.FileStatusRenamed:
			headPathSet[file.Filename] = struct{}{}
			basePathSet[file.Filename] = struct{}{}
			// For renames, the base branch has the file at the old path
			if file.PreviousFilename != "" {
				basePathSet[file.PreviousFilename] = struct{}{}
			}
		}
	}

	headPaths := setToSlice(headPathSet)
	basePaths := setToSlice(basePathSet)

	slog.Debug("batch fetching file contents",
		"head_paths", len(headPaths),
		"base_paths", len(basePaths),
	)

	// --- Fetch phase: at most 2 HTTP calls (one per ref) ---
	var headContents, baseContents map[string][]byte

	if len(headPaths) > 0 {
		headContents, err = e.vcs.GetFileContents(ctx, event.Repo.Owner, event.Repo.Name, headPaths, event.PR.HeadRef)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("batch fetching head ref files: %w", err)
		}
	} else {
		headContents = map[string][]byte{}
	}

	if len(basePaths) > 0 {
		baseContents, err = e.vcs.GetFileContents(ctx, event.Repo.Owner, event.Repo.Name, basePaths, event.PR.BaseRef)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("batch fetching base ref files: %w", err)
		}
	} else {
		baseContents = map[string][]byte{}
	}

	// --- Process phase: use pre-fetched content ---
	parsed := &argocd.ParsedApplications{}

	for _, file := range yamlFiles {
		slog.Debug("processing YAML file",
			"file", file.Filename,
			"status", file.Status,
		)

		switch file.Status {
		case models.FileStatusAdded:
			content, ok := headContents[file.Filename]
			if !ok {
				slog.Warn("added file not found in head archive", "file", file.Filename)
				continue
			}
			apps, err := argocd.ParseApplicationsFromYAML(content, file.Filename)
			if err != nil {
				slog.Warn("failed to parse new file", "file", file.Filename, "error", err)
				continue
			}
			slog.Debug("found applications in added file",
				"file", file.Filename,
				"count", len(apps),
			)
			for _, app := range apps {
				slog.Debug("new application detected",
					"app", app.Name,
					"source_file", file.Filename,
				)
				app.ChangeType = models.ApplicationNew
				parsed.New = append(parsed.New, app)
			}

		case models.FileStatusRemoved:
			content, ok := baseContents[file.Filename]
			if !ok {
				slog.Warn("removed file not found in base archive", "file", file.Filename)
				continue
			}
			apps, err := argocd.ParseApplicationsFromYAML(content, file.Filename)
			if err != nil {
				slog.Warn("failed to parse deleted file", "file", file.Filename, "error", err)
				continue
			}
			slog.Debug("found applications in removed file",
				"file", file.Filename,
				"count", len(apps),
			)
			for _, app := range apps {
				slog.Debug("deleted application detected",
					"app", app.Name,
					"source_file", file.Filename,
				)
				app.ChangeType = models.ApplicationDeleted
				parsed.Deleted = append(parsed.Deleted, app)
			}

		case models.FileStatusModified, models.FileStatusRenamed:
			headContent := headContents[file.Filename]
			baseContent := baseContents[file.Filename]

			newApps, deletedApps, err := detectModifiedAppsFromContent(baseContent, headContent, file.Filename)
			if err != nil {
				slog.Warn("failed to detect modified apps", "file", file.Filename, "error", err)
				continue
			}
			slog.Debug("modified file analysis result",
				"file", file.Filename,
				"new_apps", len(newApps),
				"deleted_apps", len(deletedApps),
			)
			for _, app := range newApps {
				slog.Debug("new application in modified file",
					"app", app.Name,
					"source_file", file.Filename,
				)
				app.ChangeType = models.ApplicationNew
				parsed.New = append(parsed.New, app)
			}
			for _, app := range deletedApps {
				slog.Debug("deleted application in modified file",
					"app", app.Name,
					"source_file", file.Filename,
				)
				app.ChangeType = models.ApplicationDeleted
				parsed.Deleted = append(parsed.Deleted, app)
			}
			// Modified apps that exist in both are tracked separately.
			// Use the already-fetched head content (no redundant fetch).
			if headContent != nil {
				modifiedApps, err := argocd.ParseApplicationsFromYAML(headContent, file.Filename)
				if err == nil {
					for _, app := range modifiedApps {
						if !containsAppByName(parsed.New, app.Name) && !containsAppByName(parsed.Deleted, app.Name) {
							slog.Debug("modified application detected",
								"app", app.Name,
								"source_file", file.Filename,
							)
							parsed.Modified = append(parsed.Modified, app)
						}
					}
				}
			}
		}
	}

	slog.Debug("application change detection complete",
		"new_count", len(parsed.New),
		"modified_count", len(parsed.Modified),
		"deleted_count", len(parsed.Deleted),
	)

	return parsed, headContents, baseContents, nil
}

// setToSlice converts a string set to a slice.
func setToSlice(s map[string]struct{}) []string {
	result := make([]string, 0, len(s))
	for k := range s {
		result = append(result, k)
	}
	return result
}

// detectModifiedAppsFromContent compares pre-fetched base and head content of a file
// to find added/removed apps. This avoids per-file HTTP calls.
func detectModifiedAppsFromContent(baseContent, headContent []byte, filePath string) (newApps, deletedApps []models.Application, err error) {
	slog.Debug("comparing base and head versions of file", "file", filePath)

	// Parse apps from base content
	var baseApps []models.Application
	if baseContent != nil {
		baseApps, err = argocd.ParseApplicationsFromYAML(baseContent, filePath)
		if err != nil {
			slog.Debug("failed to parse base version (treating as empty)",
				"file", filePath,
				"error", err,
			)
			baseApps = nil
		}
	}

	// Parse apps from head content
	var headApps []models.Application
	if headContent != nil {
		headApps, err = argocd.ParseApplicationsFromYAML(headContent, filePath)
		if err != nil {
			slog.Debug("failed to parse head version",
				"file", filePath,
				"error", err,
			)
			return nil, nil, err
		}
	}

	slog.Debug("comparing application lists",
		"file", filePath,
		"base_apps_count", len(baseApps),
		"head_apps_count", len(headApps),
	)

	// Build maps for comparison
	baseByName := make(map[string]models.Application)
	for _, app := range baseApps {
		baseByName[app.Name] = app
	}

	headByName := make(map[string]models.Application)
	for _, app := range headApps {
		headByName[app.Name] = app
	}

	// Find new apps (in head but not in base)
	for name, app := range headByName {
		if _, exists := baseByName[name]; !exists {
			slog.Debug("application added in this PR",
				"app", name,
				"file", filePath,
			)
			newApps = append(newApps, app)
		}
	}

	// Find deleted apps (in base but not in head)
	for name, app := range baseByName {
		if _, exists := headByName[name]; !exists {
			slog.Debug("application removed in this PR",
				"app", name,
				"file", filePath,
			)
			deletedApps = append(deletedApps, app)
		}
	}

	slog.Debug("modified file comparison complete",
		"file", filePath,
		"new_apps", len(newApps),
		"deleted_apps", len(deletedApps),
	)

	return newApps, deletedApps, nil
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

// detectApplicationSetChanges analyzes PR files to detect ApplicationSet CR changes
// and previews the resulting application additions/removals using the ArgoCD Generate API.
// It uses pre-fetched headContents/baseContents maps to avoid per-file VCS API calls.
func (e *Executor) detectApplicationSetChanges(ctx context.Context, event *models.PREvent, files []models.ChangedFile, headContents, baseContents map[string][]byte) (*ParsedApplicationSetChanges, error) {
	slog.Debug("detecting applicationset changes from PR files",
		"repo", event.Repo.FullName,
		"pr", event.PR.Number,
	)

	result := &ParsedApplicationSetChanges{}

	for _, file := range files {
		if !vcs.IsYAMLFile(file.Filename) {
			continue
		}

		switch file.Status {
		case models.FileStatusAdded:
			e.detectAppSetAddedFile(ctx, file.Filename, headContents, result)

		case models.FileStatusRemoved:
			e.detectAppSetRemovedFile(ctx, file.Filename, baseContents, result)

		case models.FileStatusModified, models.FileStatusRenamed:
			e.detectAppSetModifiedFile(ctx, file, headContents, baseContents, result)
		}
	}

	slog.Debug("applicationset change detection complete",
		"new_apps", len(result.NewApps),
		"deleted_apps", len(result.DeletedApps),
		"modified_appsets", len(result.Modified),
	)

	return result, nil
}

// detectAppSetAddedFile parses an added file for ApplicationSet CRs and generates preview apps.
func (e *Executor) detectAppSetAddedFile(ctx context.Context, filePath string, headContents map[string][]byte, result *ParsedApplicationSetChanges) {
	content, ok := headContents[filePath]
	if !ok {
		slog.Warn("added file not found in head contents for appset detection", "file", filePath)
		return
	}

	appSets, err := argocd.ParseApplicationSetsFromYAML(content, filePath)
	if err != nil {
		slog.Warn("failed to parse appsets from added file", "file", filePath, "error", err)
		return
	}

	for i := range appSets {
		apps, err := e.argocd.GenerateApplications(ctx, &appSets[i])
		if err != nil {
			slog.Warn("failed to generate apps for new appset",
				"appset", appSets[i].Name, "error", err)
			continue
		}

		for j := range apps {
			apps[j].ChangeType = models.ApplicationNew
			apps[j].ApplicationSetName = appSets[i].Name
			apps[j].SourceFile = filePath
		}
		result.NewApps = append(result.NewApps, apps...)

		slog.Debug("new applicationset detected",
			"appset", appSets[i].Name,
			"generated_apps", len(apps),
			"file", filePath,
		)
	}
}

// detectAppSetRemovedFile parses a removed file for ApplicationSet CRs and finds existing apps that will be deleted.
func (e *Executor) detectAppSetRemovedFile(ctx context.Context, filePath string, baseContents map[string][]byte, result *ParsedApplicationSetChanges) {
	content, ok := baseContents[filePath]
	if !ok {
		slog.Warn("removed file not found in base contents for appset detection", "file", filePath)
		return
	}

	appSets, err := argocd.ParseApplicationSetsFromYAML(content, filePath)
	if err != nil {
		slog.Warn("failed to parse appsets from removed file", "file", filePath, "error", err)
		return
	}

	for _, appSet := range appSets {
		apps, err := e.argocd.GetApplicationsByApplicationSet(ctx, appSet.Name)
		if err != nil {
			slog.Warn("failed to get apps for deleted appset",
				"appset", appSet.Name, "error", err)
			continue
		}

		for j := range apps {
			apps[j].ChangeType = models.ApplicationDeleted
			apps[j].ApplicationSetName = appSet.Name
			apps[j].SourceFile = filePath
		}
		result.DeletedApps = append(result.DeletedApps, apps...)

		slog.Debug("deleted applicationset detected",
			"appset", appSet.Name,
			"existing_apps", len(apps),
			"file", filePath,
		)
	}
}

// detectAppSetModifiedFile compares base and head versions of a file to find ApplicationSet changes.
func (e *Executor) detectAppSetModifiedFile(ctx context.Context, file models.ChangedFile, headContents, baseContents map[string][]byte, result *ParsedApplicationSetChanges) {
	filePath := file.Filename

	// For renames, the base branch has the file at the old path
	baseFilePath := filePath
	if file.PreviousFilename != "" {
		baseFilePath = file.PreviousFilename
	}

	baseContent, baseOK := baseContents[baseFilePath]
	if !baseOK {
		slog.Warn("base appset file not found in pre-fetched contents", "file", baseFilePath)
		// Only treat as added if this is NOT a rename — for renames, failing to fetch
		// the old path is unexpected and should not silently classify apps as new.
		if file.PreviousFilename == "" {
			slog.Debug("treating as new appset file", "file", filePath)
			e.detectAppSetAddedFile(ctx, filePath, headContents, result)
		}
		return
	}

	headContent, headOK := headContents[filePath]
	if !headOK {
		slog.Warn("head appset file not found in pre-fetched contents", "file", filePath)
		return
	}

	baseAppSets, err := argocd.ParseApplicationSetsFromYAML(baseContent, baseFilePath)
	if err != nil {
		slog.Warn("failed to parse base appsets", "file", baseFilePath, "error", err)
		baseAppSets = nil
	}

	headAppSets, err := argocd.ParseApplicationSetsFromYAML(headContent, filePath)
	if err != nil {
		slog.Warn("failed to parse head appsets", "file", filePath, "error", err)
		return
	}

	baseByName := make(map[string]*argocd.ParsedAppSet)
	for i, as := range baseAppSets {
		baseByName[as.Name] = &argocd.ParsedAppSet{AppSet: &baseAppSets[i], SourceFile: filePath}
	}

	headByName := make(map[string]*argocd.ParsedAppSet)
	for i, as := range headAppSets {
		headByName[as.Name] = &argocd.ParsedAppSet{AppSet: &headAppSets[i], SourceFile: filePath}
	}

	// AppSets in head but not in base → new
	for name, headAS := range headByName {
		if _, exists := baseByName[name]; !exists {
			apps, err := e.argocd.GenerateApplications(ctx, headAS.AppSet)
			if err != nil {
				slog.Warn("failed to generate apps for new appset in modified file",
					"appset", name, "error", err)
				continue
			}
			for j := range apps {
				apps[j].ChangeType = models.ApplicationNew
				apps[j].ApplicationSetName = name
				apps[j].SourceFile = filePath
			}
			result.NewApps = append(result.NewApps, apps...)
		}
	}

	// AppSets in base but not in head → deleted
	for name := range baseByName {
		if _, exists := headByName[name]; !exists {
			apps, err := e.argocd.GetApplicationsByApplicationSet(ctx, name)
			if err != nil {
				slog.Warn("failed to get apps for removed appset in modified file",
					"appset", name, "error", err)
				continue
			}
			for j := range apps {
				apps[j].ChangeType = models.ApplicationDeleted
				apps[j].ApplicationSetName = name
				apps[j].SourceFile = filePath
			}
			result.DeletedApps = append(result.DeletedApps, apps...)
		}
	}

	// AppSets in both → compare generated apps
	for name, headAS := range headByName {
		baseAS, exists := baseByName[name]
		if !exists {
			continue
		}

		headApps, err := e.argocd.GenerateApplications(ctx, headAS.AppSet)
		if err != nil {
			slog.Warn("failed to generate head apps for modified appset",
				"appset", name, "error", err)
			continue
		}

		baseApps, err := e.argocd.GenerateApplications(ctx, baseAS.AppSet)
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
			SourceFile: filePath,
		}

		for _, a := range headApps {
			if !baseAppNames[a.Name] {
				a.ChangeType = models.ApplicationNew
				a.ApplicationSetName = name
				a.SourceFile = filePath
				mod.NewApps = append(mod.NewApps, a)
				result.NewApps = append(result.NewApps, a)
			}
		}

		for _, a := range baseApps {
			if !headAppNames[a.Name] {
				a.ChangeType = models.ApplicationDeleted
				a.ApplicationSetName = name
				a.SourceFile = filePath
				mod.RemovedApps = append(mod.RemovedApps, a)
				result.DeletedApps = append(result.DeletedApps, a)
			}
		}

		if len(mod.NewApps) > 0 || len(mod.RemovedApps) > 0 {
			result.Modified = append(result.Modified, mod)
			slog.Debug("applicationset generator changed",
				"appset", name,
				"new_apps", len(mod.NewApps),
				"removed_apps", len(mod.RemovedApps),
			)
		}
	}
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
