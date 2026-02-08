package commands

import (
	"context"
	"fmt"

	"github.com/org/lemuria/internal/argocd"
	"github.com/org/lemuria/internal/models"
	"github.com/org/lemuria/internal/vcs"
)

// detectApplicationChanges analyzes PR files to detect new, modified, and deleted applications.
// It compares Application CRs in the PR with existing applications in ArgoCD.
func (e *Executor) detectApplicationChanges(ctx context.Context, event *models.PREvent) (*argocd.ParsedApplications, error) {
	e.logger.Debug("detecting application changes from PR files",
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

	e.logger.Debug("analyzing changed files for Application CRs",
		"total_files", len(files),
	)

	parsed := &argocd.ParsedApplications{}

	for _, file := range files {
		// Only process YAML files
		if !vcs.IsYAMLFile(file.Filename) {
			e.logger.Debug("skipping non-YAML file",
				"file", file.Filename,
			)
			continue
		}

		e.logger.Debug("processing YAML file",
			"file", file.Filename,
			"status", file.Status,
		)

		switch file.Status {
		case "added":
			// New file - parse for new applications
			e.logger.Debug("parsing added file for new applications",
				"file", file.Filename,
				"ref", event.PR.HeadRef,
			)
			apps, err := e.parseAppsFromFile(ctx, event, file.Filename, event.PR.HeadRef)
			if err != nil {
				e.logger.Warn("failed to parse new file", "file", file.Filename, "error", err)
				continue
			}
			e.logger.Debug("found applications in added file",
				"file", file.Filename,
				"count", len(apps),
			)
			for _, app := range apps {
				e.logger.Debug("new application detected",
					"app", app.Name,
					"source_file", file.Filename,
				)
				app.ChangeType = models.ApplicationNew
				parsed.New = append(parsed.New, app)
			}

		case "removed":
			// Deleted file - parse from base branch for deleted applications
			e.logger.Debug("parsing removed file for deleted applications",
				"file", file.Filename,
				"ref", event.PR.BaseRef,
			)
			apps, err := e.parseAppsFromFile(ctx, event, file.Filename, event.PR.BaseRef)
			if err != nil {
				e.logger.Warn("failed to parse deleted file", "file", file.Filename, "error", err)
				continue
			}
			e.logger.Debug("found applications in removed file",
				"file", file.Filename,
				"count", len(apps),
			)
			for _, app := range apps {
				e.logger.Debug("deleted application detected",
					"app", app.Name,
					"source_file", file.Filename,
				)
				app.ChangeType = models.ApplicationDeleted
				parsed.Deleted = append(parsed.Deleted, app)
			}

		case "modified":
			// Modified file - check for added/removed applications within the file
			e.logger.Debug("analyzing modified file for application changes",
				"file", file.Filename,
			)
			newApps, deletedApps, err := e.detectModifiedApps(ctx, event, file.Filename)
			if err != nil {
				e.logger.Warn("failed to detect modified apps", "file", file.Filename, "error", err)
				continue
			}
			e.logger.Debug("modified file analysis result",
				"file", file.Filename,
				"new_apps", len(newApps),
				"deleted_apps", len(deletedApps),
			)
			for _, app := range newApps {
				e.logger.Debug("new application in modified file",
					"app", app.Name,
					"source_file", file.Filename,
				)
				app.ChangeType = models.ApplicationNew
				parsed.New = append(parsed.New, app)
			}
			for _, app := range deletedApps {
				e.logger.Debug("deleted application in modified file",
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
						e.logger.Debug("modified application detected",
							"app", app.Name,
							"source_file", file.Filename,
						)
						parsed.Modified = append(parsed.Modified, app)
					}
				}
			}
		}
	}

	e.logger.Debug("application change detection complete",
		"new_count", len(parsed.New),
		"modified_count", len(parsed.Modified),
		"deleted_count", len(parsed.Deleted),
	)

	return parsed, nil
}

// parseAppsFromFile fetches and parses Application CRs from a file at the given ref.
func (e *Executor) parseAppsFromFile(ctx context.Context, event *models.PREvent, filePath, ref string) ([]models.Application, error) {
	e.logger.Debug("fetching file content",
		"file", filePath,
		"ref", ref,
	)

	content, err := e.vcs.GetFileContent(ctx, event.Repo.Owner, event.Repo.Name, filePath, ref)
	if err != nil {
		e.logger.Debug("failed to fetch file content",
			"file", filePath,
			"ref", ref,
			"error", err,
		)
		return nil, err
	}

	e.logger.Debug("parsing YAML for Application CRs",
		"file", filePath,
		"content_length", len(content),
	)

	apps, err := argocd.ParseApplicationsFromYAML(content, filePath)
	if err != nil {
		e.logger.Debug("failed to parse YAML",
			"file", filePath,
			"error", err,
		)
		return nil, err
	}

	e.logger.Debug("parsed applications from file",
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
	e.logger.Debug("comparing base and head versions of file",
		"file", filePath,
		"base_ref", event.PR.BaseRef,
		"head_ref", event.PR.HeadRef,
	)

	// Parse apps from base branch
	baseApps, baseErr := e.parseAppsFromFile(ctx, event, filePath, event.PR.BaseRef)
	if baseErr != nil {
		e.logger.Debug("failed to parse base version (treating as empty)",
			"file", filePath,
			"error", baseErr,
		)
		// If base doesn't exist, all head apps are new
		baseApps = nil
	}

	// Parse apps from head branch
	headApps, headErr := e.parseAppsFromFile(ctx, event, filePath, event.PR.HeadRef)
	if headErr != nil {
		e.logger.Debug("failed to parse head version",
			"file", filePath,
			"error", headErr,
		)
		return nil, nil, headErr
	}

	e.logger.Debug("comparing application lists",
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
			e.logger.Debug("application added in this PR",
				"app", name,
				"file", filePath,
			)
			newApps = append(newApps, app)
		}
	}

	// Find deleted apps (in base but not in head)
	for name, app := range baseByName {
		if _, exists := headByName[name]; !exists {
			e.logger.Debug("application removed in this PR",
				"app", name,
				"file", filePath,
			)
			deletedApps = append(deletedApps, app)
		}
	}

	e.logger.Debug("modified file comparison complete",
		"file", filePath,
		"new_apps", len(newApps),
		"deleted_apps", len(deletedApps),
	)

	return newApps, deletedApps, nil
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
		e.logger.Debug("no new applications to verify")
		return nil
	}

	e.logger.Debug("verifying new applications against ArgoCD",
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
			e.logger.Debug("application marked as new already exists in ArgoCD",
				"app", app.Name,
				"reclassifying_to", "existing",
			)
			// App already exists, treat as modified
			app.ChangeType = models.ApplicationExisting
			parsed.Modified = append(parsed.Modified, app)
		} else {
			e.logger.Debug("application confirmed as truly new",
				"app", app.Name,
			)
			trulyNew = append(trulyNew, app)
		}
	}
	parsed.New = trulyNew

	e.logger.Debug("new applications verification complete",
		"truly_new_count", len(trulyNew),
		"reclassified_to_modified", len(parsed.New)-len(trulyNew),
	)

	return nil
}

// verifyDeletedAppsExist checks which "deleted" apps actually exist in ArgoCD.
func (e *Executor) verifyDeletedAppsExist(ctx context.Context, parsed *argocd.ParsedApplications) error {
	if len(parsed.Deleted) == 0 {
		e.logger.Debug("no deleted applications to verify")
		return nil
	}

	e.logger.Debug("verifying deleted applications exist in ArgoCD",
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
			e.logger.Debug("deleted application exists in ArgoCD",
				"app", app.Name,
			)
			actuallyDeleted = append(actuallyDeleted, app)
		} else {
			e.logger.Debug("deleted application does not exist in ArgoCD (ignoring)",
				"app", app.Name,
			)
		}
	}

	e.logger.Debug("deleted applications verification complete",
		"confirmed_deleted", len(actuallyDeleted),
		"ignored", len(parsed.Deleted)-len(actuallyDeleted),
	)

	parsed.Deleted = actuallyDeleted

	return nil
}
