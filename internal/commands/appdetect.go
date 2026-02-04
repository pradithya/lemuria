package commands

import (
	"context"
	"fmt"

	"github.com/org/lemuria/internal/argocd"
	"github.com/org/lemuria/internal/github"
	"github.com/org/lemuria/internal/models"
)

// detectApplicationChanges analyzes PR files to detect new, modified, and deleted applications.
// It compares Application CRs in the PR with existing applications in ArgoCD.
func (e *Executor) detectApplicationChanges(ctx context.Context, event *models.PREvent) (*argocd.ParsedApplications, error) {
	// Get changed files
	files, err := e.github.GetChangedFiles(ctx, event.Repo.Owner, event.Repo.Name, event.PR.Number)
	if err != nil {
		return nil, fmt.Errorf("getting changed files: %w", err)
	}

	parsed := &argocd.ParsedApplications{}

	for _, file := range files {
		// Only process YAML files
		if !github.IsYAMLFile(file.Filename) {
			continue
		}

		switch file.Status {
		case "added":
			// New file - parse for new applications
			apps, err := e.parseAppsFromFile(ctx, event, file.Filename, event.PR.HeadRef)
			if err != nil {
				e.logger.Warn("failed to parse new file", "file", file.Filename, "error", err)
				continue
			}
			for _, app := range apps {
				app.ChangeType = models.ApplicationNew
				parsed.New = append(parsed.New, app)
			}

		case "removed":
			// Deleted file - parse from base branch for deleted applications
			apps, err := e.parseAppsFromFile(ctx, event, file.Filename, event.PR.BaseRef)
			if err != nil {
				e.logger.Warn("failed to parse deleted file", "file", file.Filename, "error", err)
				continue
			}
			for _, app := range apps {
				app.ChangeType = models.ApplicationDeleted
				parsed.Deleted = append(parsed.Deleted, app)
			}

		case "modified":
			// Modified file - check for added/removed applications within the file
			newApps, deletedApps, err := e.detectModifiedApps(ctx, event, file.Filename)
			if err != nil {
				e.logger.Warn("failed to detect modified apps", "file", file.Filename, "error", err)
				continue
			}
			for _, app := range newApps {
				app.ChangeType = models.ApplicationNew
				parsed.New = append(parsed.New, app)
			}
			for _, app := range deletedApps {
				app.ChangeType = models.ApplicationDeleted
				parsed.Deleted = append(parsed.Deleted, app)
			}
			// Modified apps that exist in both are tracked separately
			modifiedApps, err := e.parseAppsFromFile(ctx, event, file.Filename, event.PR.HeadRef)
			if err == nil {
				for _, app := range modifiedApps {
					// Only add if not in new or deleted list
					if !containsAppByName(parsed.New, app.Name) && !containsAppByName(parsed.Deleted, app.Name) {
						parsed.Modified = append(parsed.Modified, app)
					}
				}
			}
		}
	}

	return parsed, nil
}

// parseAppsFromFile fetches and parses Application CRs from a file at the given ref.
func (e *Executor) parseAppsFromFile(ctx context.Context, event *models.PREvent, filePath, ref string) ([]models.Application, error) {
	content, err := e.github.GetFileContent(ctx, event.Repo.Owner, event.Repo.Name, filePath, ref)
	if err != nil {
		return nil, err
	}

	apps, err := argocd.ParseApplicationsFromYAML(content, filePath)
	if err != nil {
		return nil, err
	}

	// Filter to apps that reference this repo
	repoURL := fmt.Sprintf("https://github.com/%s", event.Repo.FullName)
	var filtered []models.Application
	for _, app := range apps {
		if e.appReferencesRepo(app, repoURL) {
			filtered = append(filtered, app)
		}
	}

	return filtered, nil
}

// detectModifiedApps compares base and head versions of a file to find added/removed apps.
func (e *Executor) detectModifiedApps(ctx context.Context, event *models.PREvent, filePath string) (newApps, deletedApps []models.Application, err error) {
	// Parse apps from base branch
	baseApps, baseErr := e.parseAppsFromFile(ctx, event, filePath, event.PR.BaseRef)
	if baseErr != nil {
		// If base doesn't exist, all head apps are new
		baseApps = nil
	}

	// Parse apps from head branch
	headApps, headErr := e.parseAppsFromFile(ctx, event, filePath, event.PR.HeadRef)
	if headErr != nil {
		return nil, nil, headErr
	}

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
			newApps = append(newApps, app)
		}
	}

	// Find deleted apps (in base but not in head)
	for name, app := range baseByName {
		if _, exists := headByName[name]; !exists {
			deletedApps = append(deletedApps, app)
		}
	}

	return newApps, deletedApps, nil
}

// appReferencesRepo checks if an application references the given repository.
func (e *Executor) appReferencesRepo(app models.Application, repoURL string) bool {
	normalizedTarget := argocd.NormalizeRepoURL(repoURL)

	for _, appRepoURL := range app.GetRepoURLs() {
		if argocd.NormalizeRepoURL(appRepoURL) == normalizedTarget {
			return true
		}
	}

	return false
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
		return nil
	}

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
			// App already exists, treat as modified
			app.ChangeType = models.ApplicationExisting
			parsed.Modified = append(parsed.Modified, app)
		} else {
			trulyNew = append(trulyNew, app)
		}
	}
	parsed.New = trulyNew

	return nil
}

// verifyDeletedAppsExist checks which "deleted" apps actually exist in ArgoCD.
func (e *Executor) verifyDeletedAppsExist(ctx context.Context, parsed *argocd.ParsedApplications) error {
	if len(parsed.Deleted) == 0 {
		return nil
	}

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
			actuallyDeleted = append(actuallyDeleted, app)
		}
	}
	parsed.Deleted = actuallyDeleted

	return nil
}
