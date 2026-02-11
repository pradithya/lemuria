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
func (e *Executor) detectApplicationChanges(ctx context.Context, event *models.PREvent) (*argocd.ParsedApplications, error) {
	slog.Debug("detecting application changes from PR files",
		"repo", event.Repo.FullName,
		"pr", event.PR.Number,
		"head_ref", event.PR.HeadRef,
		"base_ref", event.PR.BaseRef,
	)

	// Get changed files
	files, err := e.vcs.GetChangedFiles(ctx, event.Repo.Owner, event.Repo.Name, event.PR.Number)
	if err != nil {
		return nil, fmt.Errorf("getting changed files: %w", err)
	}

	slog.Debug("analyzing changed files for Application CRs",
		"total_files", len(files),
	)

	parsed := &argocd.ParsedApplications{}

	for _, file := range files {
		// Only process YAML files
		if !vcs.IsYAMLFile(file.Filename) {
			slog.Debug("skipping non-YAML file",
				"file", file.Filename,
			)
			continue
		}

		slog.Debug("processing YAML file",
			"file", file.Filename,
			"status", file.Status,
		)

		switch file.Status {
		case models.FileStatusAdded:
			// New file - parse for new applications
			slog.Debug("parsing added file for new applications",
				"file", file.Filename,
				"ref", event.PR.HeadRef,
			)
			apps, err := e.parseAppsFromFile(ctx, event, file.Filename, event.PR.HeadRef)
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
			// Deleted file - parse from base branch for deleted applications
			slog.Debug("parsing removed file for deleted applications",
				"file", file.Filename,
				"ref", event.PR.BaseRef,
			)
			apps, err := e.parseAppsFromFile(ctx, event, file.Filename, event.PR.BaseRef)
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
			// Modified or renamed file - check for added/removed applications within the file
			slog.Debug("analyzing modified file for application changes",
				"file", file.Filename,
			)
			newApps, deletedApps, err := e.detectModifiedApps(ctx, event, file.Filename)
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
			// Modified apps that exist in both are tracked separately
			modifiedApps, err := e.parseAppsFromFile(ctx, event, file.Filename, event.PR.HeadRef)
			if err == nil {
				for _, app := range modifiedApps {
					// Only add if not in new or deleted list
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

	slog.Debug("application change detection complete",
		"new_count", len(parsed.New),
		"modified_count", len(parsed.Modified),
		"deleted_count", len(parsed.Deleted),
	)

	return parsed, nil
}

// parseAppsFromFile fetches and parses Application CRs from a file at the given ref.
func (e *Executor) parseAppsFromFile(ctx context.Context, event *models.PREvent, filePath, ref string) ([]models.Application, error) {
	slog.Debug("fetching file content",
		"file", filePath,
		"ref", ref,
	)

	content, err := e.vcs.GetFileContent(ctx, event.Repo.Owner, event.Repo.Name, filePath, ref)
	if err != nil {
		slog.Debug("failed to fetch file content",
			"file", filePath,
			"ref", ref,
			"error", err,
		)
		return nil, err
	}

	slog.Debug("parsing YAML for Application CRs",
		"file", filePath,
		"content_length", len(content),
	)

	apps, err := argocd.ParseApplicationsFromYAML(content, filePath)
	if err != nil {
		slog.Debug("failed to parse YAML",
			"file", filePath,
			"error", err,
		)
		return nil, err
	}

	slog.Debug("parsed applications from file",
		"file", filePath,
		"total_apps", len(apps),
	)

	// All Application CRs in changed files are relevant — the CR definition itself
	// was modified, regardless of whether the app sources from this repo or an
	// external chart/repo (e.g., Helm chart apps with inline values).
	return apps, nil
}

// detectModifiedApps compares base and head versions of a file to find added/removed apps.
func (e *Executor) detectModifiedApps(ctx context.Context, event *models.PREvent, filePath string) (newApps, deletedApps []models.Application, err error) {
	slog.Debug("comparing base and head versions of file",
		"file", filePath,
		"base_ref", event.PR.BaseRef,
		"head_ref", event.PR.HeadRef,
	)

	// Parse apps from base branch
	baseApps, baseErr := e.parseAppsFromFile(ctx, event, filePath, event.PR.BaseRef)
	if baseErr != nil {
		slog.Debug("failed to parse base version (treating as empty)",
			"file", filePath,
			"error", baseErr,
		)
		// If base doesn't exist, all head apps are new
		baseApps = nil
	}

	// Parse apps from head branch
	headApps, headErr := e.parseAppsFromFile(ctx, event, filePath, event.PR.HeadRef)
	if headErr != nil {
		slog.Debug("failed to parse head version",
			"file", filePath,
			"error", headErr,
		)
		return nil, nil, headErr
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
// It accepts the already-fetched changed files to avoid redundant VCS API calls.
func (e *Executor) detectApplicationSetChanges(ctx context.Context, event *models.PREvent, files []models.ChangedFile) (*ParsedApplicationSetChanges, error) {
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
			e.detectAppSetAddedFile(ctx, event, file.Filename, result)

		case models.FileStatusRemoved:
			e.detectAppSetRemovedFile(ctx, event, file.Filename, result)

		case models.FileStatusModified, models.FileStatusRenamed:
			e.detectAppSetModifiedFile(ctx, event, file, result)
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
func (e *Executor) detectAppSetAddedFile(ctx context.Context, event *models.PREvent, filePath string, result *ParsedApplicationSetChanges) {
	content, err := e.vcs.GetFileContent(ctx, event.Repo.Owner, event.Repo.Name, filePath, event.PR.HeadRef)
	if err != nil {
		slog.Warn("failed to fetch added file for appset detection", "file", filePath, "error", err)
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
func (e *Executor) detectAppSetRemovedFile(ctx context.Context, event *models.PREvent, filePath string, result *ParsedApplicationSetChanges) {
	content, err := e.vcs.GetFileContent(ctx, event.Repo.Owner, event.Repo.Name, filePath, event.PR.BaseRef)
	if err != nil {
		slog.Warn("failed to fetch removed file for appset detection", "file", filePath, "error", err)
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
func (e *Executor) detectAppSetModifiedFile(ctx context.Context, event *models.PREvent, file models.ChangedFile, result *ParsedApplicationSetChanges) {
	filePath := file.Filename

	// For renames, the base branch has the file at the old path
	baseFilePath := filePath
	if file.PreviousFilename != "" {
		baseFilePath = file.PreviousFilename
	}

	baseContent, err := e.vcs.GetFileContent(ctx, event.Repo.Owner, event.Repo.Name, baseFilePath, event.PR.BaseRef)
	if err != nil {
		slog.Warn("failed to fetch base appset file", "file", baseFilePath, "error", err)
		// Only treat as added if this is NOT a rename — for renames, failing to fetch
		// the old path is unexpected and should not silently classify apps as new.
		if file.PreviousFilename == "" {
			slog.Debug("treating as new appset file", "file", filePath)
			e.detectAppSetAddedFile(ctx, event, filePath, result)
		}
		return
	}

	headContent, err := e.vcs.GetFileContent(ctx, event.Repo.Owner, event.Repo.Name, filePath, event.PR.HeadRef)
	if err != nil {
		slog.Warn("failed to fetch head appset file", "file", filePath, "error", err)
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

// verifyNewAppsExist checks which "new" apps already exist in ArgoCD (created outside the PR).
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

	// Partition new apps into truly new vs already existing
	var trulyNew []models.Application
	for _, app := range parsed.New {
		if existingByName[app.Name] {
			slog.Debug("application marked as new already exists in ArgoCD",
				"app", app.Name,
				"reclassifying_to", "existing",
			)
			// App already exists, treat as modified
			app.ChangeType = models.ApplicationExisting
			parsed.Modified = append(parsed.Modified, app)
		} else {
			slog.Debug("application confirmed as truly new",
				"app", app.Name,
			)
			trulyNew = append(trulyNew, app)
		}
	}
	parsed.New = trulyNew

	slog.Debug("new applications verification complete",
		"truly_new_count", len(trulyNew),
		"reclassified_to_modified", len(parsed.New)-len(trulyNew),
	)

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
