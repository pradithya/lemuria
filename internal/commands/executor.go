package commands

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/org/lemuria/internal/argocd"
	"github.com/org/lemuria/internal/config"
	"github.com/org/lemuria/internal/github"
	"github.com/org/lemuria/internal/lock"
	"github.com/org/lemuria/internal/models"
	"github.com/org/lemuria/pkg/diff"
)

// Executor handles command execution.
type Executor struct {
	github  *github.Client
	argocd  *argocd.Client
	lock    lock.Manager
	config  *config.Config
	logger  *slog.Logger
	renderer *diff.Renderer
}

// NewExecutor creates a new command executor.
func NewExecutor(gh *github.Client, argo *argocd.Client, lockMgr lock.Manager, cfg *config.Config, logger *slog.Logger) *Executor {
	return &Executor{
		github:  gh,
		argocd:  argo,
		lock:    lockMgr,
		config:  cfg,
		logger:  logger,
		renderer: diff.NewRenderer(),
	}
}

// Execute runs a command in the context of a PR event.
func (e *Executor) Execute(ctx context.Context, cmd *Command, event *models.PREvent) error {
	switch cmd.Name {
	case CommandPlan:
		return e.executePlan(ctx, cmd, event)
	case CommandSync:
		return e.executeSync(ctx, cmd, event)
	case CommandUnlock:
		return e.executeUnlock(ctx, cmd, event)
	case CommandHelp:
		return e.executeHelp(ctx, event)
	case CommandRollback:
		return e.executeRollback(ctx, cmd, event)
	default:
		return fmt.Errorf("unknown command: %s", cmd.Name)
	}
}

// RunAutoplan runs plan for all affected applications.
func (e *Executor) RunAutoplan(ctx context.Context, event *models.PREvent) error {
	cmd := &Command{
		Name: CommandPlan,
	}
	return e.executePlan(ctx, cmd, event)
}

// UnlockAll releases all locks held by a PR.
func (e *Executor) UnlockAll(ctx context.Context, event *models.PREvent) error {
	locks, err := e.lock.ListByPR(ctx, event.Repo.FullName, event.PR.Number)
	if err != nil {
		return fmt.Errorf("listing locks: %w", err)
	}

	for _, l := range locks {
		if err := e.lock.Unlock(ctx, l.Application, event.Repo.FullName, event.PR.Number); err != nil {
			e.logger.Error("failed to unlock application",
				"app", l.Application,
				"error", err,
			)
		}
	}

	return nil
}

// findAffectedApplications determines which applications are affected by a PR.
// This includes:
// - Existing applications with changed manifest paths
// - New applications being created in this PR
// - Existing applications being deleted in this PR
func (e *Executor) findAffectedApplications(ctx context.Context, event *models.PREvent) ([]models.Application, error) {
	// Get changed files
	files, err := e.github.GetChangedFiles(ctx, event.Repo.Owner, event.Repo.Name, event.PR.Number)
	if err != nil {
		return nil, fmt.Errorf("getting changed files: %w", err)
	}

	filePaths := github.GetFilePaths(files)

	// Load repo config
	var repoConfig *config.RepoConfig
	configData, err := e.github.GetRepoConfig(ctx, event.Repo.Owner, event.Repo.Name, event.PR.HeadRef)
	if err == nil {
		repoConfig, _ = config.LoadRepoConfig(configData)
	}

	// Get all applications from Argo CD
	existingApps, err := e.argocd.ListApplications(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing applications: %w", err)
	}

	// Build map of existing app names
	existingByName := make(map[string]bool)
	for _, app := range existingApps {
		existingByName[app.Name] = true
	}

	// Filter to existing applications affected by this PR
	var affected []models.Application
	repoURL := fmt.Sprintf("https://github.com/%s", event.Repo.FullName)

	for _, app := range existingApps {
		if e.isAppAffected(app, repoURL, filePaths, repoConfig) {
			app.ChangeType = models.ApplicationExisting
			affected = append(affected, app)
		}
	}

	// Detect new and deleted applications from Application CR files
	parsed, err := e.detectApplicationChanges(ctx, event)
	if err != nil {
		e.logger.Warn("failed to detect application changes from files", "error", err)
	} else {
		// Verify new apps don't already exist in ArgoCD
		if err := e.verifyNewAppsExist(ctx, parsed); err != nil {
			e.logger.Warn("failed to verify new apps", "error", err)
		}

		// Verify deleted apps actually exist in ArgoCD
		if err := e.verifyDeletedAppsExist(ctx, parsed); err != nil {
			e.logger.Warn("failed to verify deleted apps", "error", err)
		}

		// Add new applications (not yet in ArgoCD)
		for _, app := range parsed.New {
			if !containsAppByName(affected, app.Name) {
				affected = append(affected, app)
			}
		}

		// Add deleted applications
		for _, app := range parsed.Deleted {
			// Mark existing apps as being deleted if not already in affected list
			if !containsAppByName(affected, app.Name) {
				affected = append(affected, app)
			} else {
				// Update the existing entry to mark as deleted
				for i := range affected {
					if affected[i].Name == app.Name {
						affected[i].ChangeType = models.ApplicationDeleted
						affected[i].SourceFile = app.SourceFile
						break
					}
				}
			}
		}
	}

	return affected, nil
}

// isAppAffected checks if an application is affected by the changed files.
func (e *Executor) isAppAffected(app models.Application, repoURL string, files []string, repoConfig *config.RepoConfig) bool {
	// Check if app references this repo
	appRepos := app.GetRepoURLs()
	repoMatch := false
	for _, appRepo := range appRepos {
		if argocd.NormalizeRepoURL(appRepo) == argocd.NormalizeRepoURL(repoURL) {
			repoMatch = true
			break
		}
	}

	if !repoMatch {
		return false
	}

	// If we have repo config, use path mappings
	if repoConfig != nil {
		for _, mapping := range repoConfig.Applications {
			if matchAppName(mapping.Name, app.Name) {
				matched := github.FilterFilesByPatterns(
					filesToChangedFiles(files),
					mapping.Paths,
				)
				if len(matched) > 0 {
					return true
				}
			}
		}
	}

	// Default: check if any changed file is in the app's path
	if app.Path != "" {
		for _, f := range files {
			if pathContains(app.Path, f) {
				return true
			}
		}
	}

	return false
}

// matchAppName checks if an app name matches a pattern (supports wildcards).
func matchAppName(pattern, name string) bool {
	if pattern == name {
		return true
	}

	// Simple wildcard matching
	if len(pattern) > 0 && pattern[len(pattern)-1] == '*' {
		prefix := pattern[:len(pattern)-1]
		return len(name) >= len(prefix) && name[:len(prefix)] == prefix
	}

	return false
}

// pathContains checks if a file path is within the given directory.
func pathContains(dir, file string) bool {
	if dir == "" || dir == "." {
		return true
	}
	return len(file) > len(dir) && file[:len(dir)] == dir
}

// filesToChangedFiles converts string paths to ChangedFile structs.
func filesToChangedFiles(paths []string) []models.ChangedFile {
	files := make([]models.ChangedFile, len(paths))
	for i, p := range paths {
		files[i] = models.ChangedFile{Filename: p}
	}
	return files
}

// NormalizeRepoURL is re-exported from argocd package for use elsewhere.
func NormalizeRepoURL(url string) string {
	return argocd.NormalizeRepoURL(url)
}
