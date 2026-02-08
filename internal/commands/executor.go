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
	github   GitHubClient
	argocd   *argocd.Client
	lock     lock.Manager
	config   *config.Config
	logger   *slog.Logger
	renderer *diff.Renderer
}

// NewExecutor creates a new command executor.
func NewExecutor(gh GitHubClient, argo *argocd.Client, lockMgr lock.Manager, cfg *config.Config, logger *slog.Logger) *Executor {
	return &Executor{
		github:   gh,
		argocd:   argo,
		lock:     lockMgr,
		config:   cfg,
		logger:   logger,
		renderer: diff.NewRenderer(),
	}
}

// Execute runs a command in the context of a PR event.
func (e *Executor) Execute(ctx context.Context, cmd *Command, event *models.PREvent) error {
	e.logger.Debug("executing command",
		"command", cmd.Name,
		"application", cmd.Application,
		"all", cmd.All,
		"repo", event.Repo.FullName,
		"pr", event.PR.Number,
		"head_ref", event.PR.HeadRef,
		"head_sha", event.PR.HeadSHA,
	)

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
	e.logger.Debug("starting autoplan",
		"repo", event.Repo.FullName,
		"pr", event.PR.Number,
		"head_ref", event.PR.HeadRef,
	)

	cmd := &Command{
		Name: CommandPlan,
	}
	return e.executePlan(ctx, cmd, event)
}

// UnlockAll releases all locks held by a PR.
func (e *Executor) UnlockAll(ctx context.Context, event *models.PREvent) error {
	e.logger.Debug("unlocking all applications for PR",
		"repo", event.Repo.FullName,
		"pr", event.PR.Number,
	)

	locks, err := e.lock.ListByPR(ctx, event.Repo.FullName, event.PR.Number)
	if err != nil {
		return fmt.Errorf("listing locks: %w", err)
	}

	e.logger.Debug("found locks to release",
		"count", len(locks),
		"repo", event.Repo.FullName,
		"pr", event.PR.Number,
	)

	for _, l := range locks {
		e.logger.Debug("releasing lock",
			"app", l.Application,
			"repo", event.Repo.FullName,
			"pr", event.PR.Number,
		)
		if err := e.lock.Unlock(ctx, l.Application, event.Repo.FullName, event.PR.Number); err != nil {
			e.logger.Error("failed to unlock application",
				"app", l.Application,
				"error", err,
			)
		}
	}

	return nil
}

// getRepoConfig returns the parsed RepoConfig for the PR's repo, using a Redis cache
// to avoid redundant GitHub fetches within the same request.
func (e *Executor) getRepoConfig(ctx context.Context, event *models.PREvent) *config.RepoConfig {
	// Check cache first
	cached, err := e.lock.GetRepoConfig(ctx, event.Repo.FullName)
	if err != nil {
		e.logger.Debug("failed to check repo config cache", "error", err)
	}
	if cached != nil {
		e.logger.Debug("repo config cache hit", "repo", event.Repo.FullName)
		return cached
	}

	// Cache miss — fetch from GitHub
	configData, err := e.github.GetRepoConfig(ctx, event.Repo.Owner, event.Repo.Name, event.PR.HeadRef)
	if err != nil {
		e.logger.Debug("failed to load .lemuria.yaml", "error", err, "ref", event.PR.HeadRef)
		return nil
	}

	repoConfig, err := config.LoadRepoConfig(configData)
	if err != nil {
		e.logger.Debug("failed to parse .lemuria.yaml", "error", err)
		return nil
	}

	// Store in cache
	if err := e.lock.SetRepoConfig(ctx, event.Repo.FullName, repoConfig); err != nil {
		e.logger.Debug("failed to cache repo config", "error", err)
	}

	return repoConfig
}

// findAffectedApplications determines which applications are affected by a PR.
// This includes:
// - Existing applications with changed manifest paths
// - New applications being created in this PR
// - Existing applications being deleted in this PR
func (e *Executor) findAffectedApplications(ctx context.Context, event *models.PREvent) ([]models.Application, error) {
	e.logger.Debug("finding affected applications",
		"repo", event.Repo.FullName,
		"pr", event.PR.Number,
		"head_ref", event.PR.HeadRef,
		"base_ref", event.PR.BaseRef,
	)

	// Get changed files
	files, err := e.github.GetChangedFiles(ctx, event.Repo.Owner, event.Repo.Name, event.PR.Number)
	if err != nil {
		return nil, fmt.Errorf("getting changed files: %w", err)
	}

	filePaths := github.GetFilePaths(files)
	e.logger.Debug("retrieved changed files",
		"count", len(filePaths),
		"files", filePaths,
	)

	// Load repo config (cached)
	repoConfig := e.getRepoConfig(ctx, event)
	if repoConfig != nil {
		e.logger.Debug("loaded .lemuria.yaml",
			"applications_count", len(repoConfig.Applications),
			"autoplan", repoConfig.Autoplan,
			"require_approval", repoConfig.RequireApproval,
		)
		for _, mapping := range repoConfig.Applications {
			e.logger.Debug("repo config application mapping",
				"app_name", mapping.Name,
				"paths", mapping.Paths,
				"applicationset", mapping.ApplicationSet,
			)
		}
	}

	// Get all applications from Argo CD
	existingApps, err := e.argocd.ListApplications(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing applications: %w", err)
	}
	e.logger.Debug("retrieved ArgoCD applications",
		"count", len(existingApps),
	)

	// Build map of existing app names
	existingByName := make(map[string]bool)
	for _, app := range existingApps {
		existingByName[app.Name] = true
		e.logger.Debug("existing ArgoCD application",
			"name", app.Name,
			"repo_urls", app.GetRepoURLs(),
			"path", app.Path,
			"has_autosync", app.HasAutoSync(),
		)
	}

	// Filter to existing applications affected by this PR
	var affected []models.Application
	repoURL := fmt.Sprintf("https://github.com/%s", event.Repo.FullName)
	e.logger.Debug("checking applications against repo URL",
		"repo_url", repoURL,
	)

	for _, app := range existingApps {
		isAffected := e.isAppAffected(app, repoURL, filePaths, repoConfig)
		e.logger.Debug("checked if application is affected",
			"app", app.Name,
			"affected", isAffected,
		)
		if isAffected {
			app.ChangeType = models.ApplicationExisting
			affected = append(affected, app)
		}
	}

	e.logger.Debug("found existing affected applications",
		"count", len(affected),
	)

	// Detect new and deleted applications from Application CR files
	parsed, err := e.detectApplicationChanges(ctx, event)
	if err != nil {
		e.logger.Warn("failed to detect application changes from files", "error", err)
	} else {
		e.logger.Debug("detected application changes from files",
			"new_count", len(parsed.New),
			"modified_count", len(parsed.Modified),
			"deleted_count", len(parsed.Deleted),
		)

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
			e.logger.Debug("adding new application",
				"app", app.Name,
				"source_file", app.SourceFile,
			)
			if !containsAppByName(affected, app.Name) {
				affected = append(affected, app)
			}
		}

		// Process modified apps (Application CRs modified in the PR)
		for _, modApp := range parsed.Modified {
			if containsAppByName(affected, modApp.Name) {
				// Already in affected list, just propagate SourceFile
				for i := range affected {
					if affected[i].Name == modApp.Name && affected[i].SourceFile == "" {
						affected[i].SourceFile = modApp.SourceFile
						break
					}
				}
			} else if existingByName[modApp.Name] {
				// App exists in ArgoCD but wasn't detected by isAppAffected
				// (e.g., app uses external Helm chart but its CR was modified)
				for _, existingApp := range existingApps {
					if existingApp.Name == modApp.Name {
						e.logger.Debug("adding modified application with external source",
							"app", modApp.Name,
							"source_file", modApp.SourceFile,
						)
						existingApp.ChangeType = models.ApplicationExisting
						existingApp.SourceFile = modApp.SourceFile
						affected = append(affected, existingApp)
						break
					}
				}
			}
		}

		// Add deleted applications
		for _, app := range parsed.Deleted {
			e.logger.Debug("processing deleted application",
				"app", app.Name,
				"source_file", app.SourceFile,
			)
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

	e.logger.Debug("final affected applications",
		"count", len(affected),
	)
	for _, app := range affected {
		e.logger.Debug("affected application",
			"name", app.Name,
			"change_type", app.ChangeType,
			"source_file", app.SourceFile,
		)
	}

	return affected, nil
}

// isAppAffected checks if an application is affected by the changed files.
func (e *Executor) isAppAffected(app models.Application, repoURL string, files []string, repoConfig *config.RepoConfig) bool {
	// First, check if there's an explicit path mapping in .lemuria.yaml
	// If there is, use it regardless of whether the app's source references this repo
	// (e.g., Helm chart apps where the Application CR is in this repo but the chart is external)
	if repoConfig != nil {
		for _, mapping := range repoConfig.Applications {
			nameMatches := matchAppName(mapping.Name, app.Name)
			e.logger.Debug("checking repo config mapping",
				"app", app.Name,
				"mapping_name", mapping.Name,
				"mapping_paths", mapping.Paths,
				"name_matches", nameMatches,
			)
			if nameMatches {
				matched := github.FilterFilesByPatterns(
					filesToChangedFiles(files),
					mapping.Paths,
				)
				e.logger.Debug("path pattern matching result",
					"app", app.Name,
					"patterns", mapping.Paths,
					"matched_files", matched,
					"matched_count", len(matched),
				)
				if len(matched) > 0 {
					e.logger.Debug("application affected via explicit .lemuria.yaml mapping",
						"app", app.Name,
					)
					return true
				}
			}
		}
	}

	// Check if app references this repo
	appRepos := app.GetRepoURLs()
	repoMatch := false
	for _, appRepo := range appRepos {
		normalizedAppRepo := argocd.NormalizeRepoURL(appRepo)
		normalizedRepoURL := argocd.NormalizeRepoURL(repoURL)
		e.logger.Debug("comparing repo URLs",
			"app", app.Name,
			"app_repo", appRepo,
			"normalized_app_repo", normalizedAppRepo,
			"target_repo", repoURL,
			"normalized_target_repo", normalizedRepoURL,
		)
		if normalizedAppRepo == normalizedRepoURL {
			repoMatch = true
			break
		}
	}

	if !repoMatch {
		e.logger.Debug("application does not reference target repo",
			"app", app.Name,
			"app_repos", appRepos,
			"target_repo", repoURL,
		)
		return false
	}

	e.logger.Debug("application references target repo",
		"app", app.Name,
	)

	// Default: check if any changed file is in the app's path
	if app.Path != "" {
		e.logger.Debug("checking default path matching",
			"app", app.Name,
			"app_path", app.Path,
			"changed_files_count", len(files),
		)
		for _, f := range files {
			contains := pathContains(app.Path, f)
			if contains {
				e.logger.Debug("file matches application path",
					"app", app.Name,
					"app_path", app.Path,
					"file", f,
				)
				return true
			}
		}
		e.logger.Debug("no files match application path",
			"app", app.Name,
			"app_path", app.Path,
		)
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

// InvalidatePlanComments marks all existing plan comments on a PR as stale.
func (e *Executor) InvalidatePlanComments(ctx context.Context, event *models.PREvent) error {
	e.logger.Debug("invalidating old plan comments",
		"repo", event.Repo.FullName,
		"pr", event.PR.Number,
	)
	return e.github.InvalidatePlanComments(ctx, event.Repo.Owner, event.Repo.Name, event.PR.Number)
}
