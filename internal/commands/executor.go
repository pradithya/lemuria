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
	"strings"

	"github.com/org/lemuria/internal/argocd"
	"github.com/org/lemuria/internal/config"
	"github.com/org/lemuria/internal/lock"
	"github.com/org/lemuria/internal/models"
	"github.com/org/lemuria/internal/vcs"
	"github.com/org/lemuria/pkg/diff"
)

// Executor handles command execution.
type Executor struct {
	vcs      vcs.Client
	argocd   *argocd.Client
	lock     lock.Manager
	config   *config.Config
	renderer *diff.Renderer
}

// NewExecutor creates a new command executor.
func NewExecutor(vcsClient vcs.Client, argo *argocd.Client, lockMgr lock.Manager, cfg *config.Config) *Executor {
	return &Executor{
		vcs:      vcsClient,
		argocd:   argo,
		lock:     lockMgr,
		config:   cfg,
		renderer: diff.NewRenderer(),
	}
}

// Execute runs a command in the context of a PR event.
func (e *Executor) Execute(ctx context.Context, cmd *Command, event *models.PREvent) error {
	slog.Debug("executing command",
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
	slog.Debug("starting autoplan",
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
	slog.Debug("unlocking all applications for PR",
		"repo", event.Repo.FullName,
		"pr", event.PR.Number,
	)

	locks, err := e.lock.ListByPR(ctx, event.Repo.FullName, event.PR.Number)
	if err != nil {
		return fmt.Errorf("listing locks: %w", err)
	}

	slog.Debug("found locks to release",
		"count", len(locks),
		"repo", event.Repo.FullName,
		"pr", event.PR.Number,
	)

	// On merge or close, revert targetRevision for apps that may have had it
	// rewritten during sync. This covers:
	// - New apps: targetRevision was rewritten to the PR branch during sync
	// - Existing multi-source apps: targetRevision was rewritten to the PR SHA
	//   to work around an ArgoCD bug with multi-source revision resolution
	// On merge, we revert to the base branch. On close without merge, we also
	// revert to restore the original state. The rewriteTargetRevision function
	// is idempotent — for apps whose targetRevision wasn't changed, this is a
	// no-op since the revision already matches.
	for _, l := range locks {
		if l.ChangeType == models.ApplicationDeleted {
			continue
		}
		e.revertTargetRevision(ctx, l, event)
	}

	for _, l := range locks {
		slog.Debug("releasing lock",
			"app", l.Application,
			"repo", event.Repo.FullName,
			"pr", event.PR.Number,
		)
		if err := e.lock.Unlock(ctx, l.Application, event.Repo.FullName, event.PR.Number); err != nil {
			slog.Error("failed to unlock application",
				"app", l.Application,
				"error", err,
			)
		}
	}

	return nil
}

// revertTargetRevision reverts the targetRevision of an application back to
// the base branch after a PR merge or close.
func (e *Executor) revertTargetRevision(ctx context.Context, l models.Lock, event *models.PREvent) {
	app, err := e.argocd.GetApplicationRaw(ctx, l.Application)
	if err != nil {
		slog.Warn("failed to get application for targetRevision revert",
			"app", l.Application, "error", err)
		return
	}

	rewriteTargetRevision(app, event.Repo.HTMLURL, event.PR.BaseRef)

	if err := e.argocd.UpdateApplicationSpec(ctx, l.Application, app.Spec); err != nil {
		slog.Warn("failed to revert targetRevision",
			"app", l.Application, "error", err)
	}
}

// getRepoConfig returns the parsed RepoConfig for the PR's repo.
// The result is cached on the PREvent to avoid redundant GitHub fetches
// within the same command execution.
func (e *Executor) getRepoConfig(ctx context.Context, event *models.PREvent) *config.RepoConfig {
	if event.RepoConfigLoaded {
		return event.RepoConfig
	}

	ref := event.PR.HeadRef

	configData, err := e.vcs.GetRepoConfig(ctx, event.Repo.Owner, event.Repo.Name, ref)
	if err != nil {
		slog.Debug("failed to load .lemuria.yaml", "error", err, "ref", ref)
		event.RepoConfigLoaded = true
		return nil
	}

	repoConfig, err := config.LoadRepoConfig(configData)
	if err != nil {
		slog.Debug("failed to parse .lemuria.yaml", "error", err)
		event.RepoConfigLoaded = true
		return nil
	}

	event.RepoConfig = repoConfig
	event.RepoConfigLoaded = true

	return repoConfig
}

// findAffectedApplications determines which applications are affected by a PR.
// It uses a repo-scan-first approach:
// 1. Get changed files from VCS
// 2. Scan repo for Application/ApplicationSet CRs (head and base branches)
// 3. Match scanned apps against changed files
// 4. Detect new/deleted/modified apps by comparing head vs base
// 5. Fallback: query ArgoCD API for cross-repo apps
func (e *Executor) findAffectedApplications(ctx context.Context, event *models.PREvent) ([]models.Application, error) {
	slog.Debug("finding affected applications",
		"repo", event.Repo.FullName,
		"pr", event.PR.Number,
		"head_ref", event.PR.HeadRef,
		"base_ref", event.PR.BaseRef,
	)

	// Step 0: Get changed files
	files, err := e.vcs.GetChangedFiles(ctx, event.Repo.Owner, event.Repo.Name, event.PR.Number)
	if err != nil {
		return nil, fmt.Errorf("getting changed files: %w", err)
	}

	filePaths := vcs.GetFilePaths(files)
	slog.Debug("retrieved changed files",
		"count", len(filePaths),
		"files", filePaths,
	)

	// Load repo config (cached)
	repoConfig := e.getRepoConfig(ctx, event)
	var crPaths []string
	if repoConfig != nil {
		crPaths = repoConfig.CRPaths
		slog.Debug("loaded .lemuria.yaml",
			"applications_count", len(repoConfig.Applications),
			"cr_paths", crPaths,
			"autoplan", repoConfig.Autoplan,
			"require_approval", repoConfig.RequireApproval,
		)
		for _, mapping := range repoConfig.Applications {
			slog.Debug("repo config application mapping",
				"app_name", mapping.Name,
				"paths", mapping.Paths,
				"applicationset", mapping.ApplicationSet,
			)
		}
	}

	// Step 1-2: Scan repo for all Application/ApplicationSet CRs
	scanned, err := e.scanRepoForApplications(ctx, event, crPaths)
	if err != nil {
		slog.Warn("failed to scan repo for applications", "error", err)
		scanned = &ScannedRepoApps{
			HeadContents: map[string][]byte{},
			BaseContents: map[string][]byte{},
		}
	}

	// Step 3: Match scanned apps against changed files
	var affected []models.Application
	repoURL := event.Repo.HTMLURL
	alreadyDetected := make(map[string]bool)

	for _, app := range scanned.HeadApps {
		if e.isScannedAppAffected(app, repoURL, filePaths, repoConfig) {
			slog.Debug("scanned application affected",
				"app", app.Name,
				"source_file", app.SourceFile,
			)
			app.ChangeType = models.ApplicationExisting
			affected = append(affected, app)
			alreadyDetected[app.Name] = true
		}
	}

	slog.Debug("found affected applications from repo scan",
		"count", len(affected),
	)

	// Expand ApplicationSet mappings from .lemuria.yaml
	if repoConfig != nil {
		for _, mapping := range repoConfig.Applications {
			if mapping.ApplicationSet == "" {
				continue
			}
			matched := vcs.FilterFilesByPatterns(
				filesToChangedFiles(filePaths),
				mapping.Paths,
			)
			if len(matched) == 0 {
				continue
			}
			slog.Debug("applicationset mapping matched",
				"applicationset", mapping.ApplicationSet,
				"matched_files", len(matched),
			)
			expandedApps, err := e.expandApplicationSet(ctx, mapping.ApplicationSet)
			if err != nil {
				slog.Warn("failed to expand applicationset",
					"applicationset", mapping.ApplicationSet,
					"error", err,
				)
				continue
			}
			for _, app := range expandedApps {
				if !containsAppByName(affected, app.Name) {
					app.ChangeType = models.ApplicationExisting
					affected = append(affected, app)
					alreadyDetected[app.Name] = true
				}
			}
		}
	}

	// Step 4: Detect new/deleted/modified apps by comparing head vs base
	parsed := detectApplicationChangesFromScan(scanned)

	slog.Debug("detected application changes from scan",
		"new_count", len(parsed.New),
		"modified_count", len(parsed.Modified),
		"deleted_count", len(parsed.Deleted),
	)

	// Verify new apps don't already exist in ArgoCD
	if err := e.verifyNewAppsExist(ctx, parsed); err != nil {
		slog.Warn("failed to verify new apps", "error", err)
	}

	// Verify deleted apps actually exist in ArgoCD
	if err := e.verifyDeletedAppsExist(ctx, parsed); err != nil {
		slog.Warn("failed to verify deleted apps", "error", err)
	}

	// Add new applications
	for _, app := range parsed.New {
		slog.Debug("adding new application",
			"app", app.Name,
			"source_file", app.SourceFile,
		)
		if !containsAppByName(affected, app.Name) {
			affected = append(affected, app)
			alreadyDetected[app.Name] = true
		}
	}

	// Process modified apps (Application CRs whose content differs between head and base)
	changedFileSet := make(map[string]bool, len(filePaths))
	for _, f := range filePaths {
		changedFileSet[f] = true
	}
	for _, modApp := range parsed.Modified {
		if containsAppByName(affected, modApp.Name) {
			// Already in affected list, just propagate SourceFile
			for i := range affected {
				if affected[i].Name == modApp.Name && affected[i].SourceFile == "" {
					affected[i].SourceFile = modApp.SourceFile
					break
				}
			}
		} else if modApp.SourceFile != "" && changedFileSet[modApp.SourceFile] {
			// App CR file is among the PR's changed files — add as affected
			slog.Debug("adding modified application from scan",
				"app", modApp.Name,
				"source_file", modApp.SourceFile,
			)
			modApp.ChangeType = models.ApplicationExisting
			affected = append(affected, modApp)
			alreadyDetected[modApp.Name] = true
		} else {
			slog.Debug("skipping modified app — CR file not in PR changed files",
				"app", modApp.Name,
				"source_file", modApp.SourceFile,
			)
		}
	}

	// Detect ApplicationSet CR changes
	appSetChanges, appSetErr := e.detectApplicationSetChangesFromScan(ctx, scanned)
	if appSetErr != nil {
		slog.Warn("failed to detect applicationset changes", "error", appSetErr)
	} else {
		slog.Debug("detected applicationset changes",
			"new_apps", len(appSetChanges.NewApps),
			"deleted_apps", len(appSetChanges.DeletedApps),
			"modified_appsets", len(appSetChanges.Modified),
		)

		for _, app := range appSetChanges.NewApps {
			if !containsAppByName(affected, app.Name) && !containsAppByName(parsed.New, app.Name) {
				app.IsGeneratedApp = true
				parsed.New = append(parsed.New, app)
				affected = append(affected, app)
				alreadyDetected[app.Name] = true
			}
		}

		for _, app := range appSetChanges.DeletedApps {
			if !containsAppByName(parsed.Deleted, app.Name) {
				app.IsGeneratedApp = true
				parsed.Deleted = append(parsed.Deleted, app)
			}
		}
	}

	// Add deleted applications
	for _, app := range parsed.Deleted {
		slog.Debug("processing deleted application",
			"app", app.Name,
			"source_file", app.SourceFile,
		)
		if !containsAppByName(affected, app.Name) {
			affected = append(affected, app)
			alreadyDetected[app.Name] = true
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

	// Step 5: Fallback — ArgoCD API for cross-repo apps
	crossRepoApps, crossErr := e.detectCrossRepoAffectedApps(ctx, repoURL, filePaths, alreadyDetected)
	if crossErr != nil {
		slog.Warn("failed to detect cross-repo affected apps", "error", crossErr)
	} else {
		affected = append(affected, crossRepoApps...)
	}

	slog.Debug("final affected applications",
		"count", len(affected),
	)
	for _, app := range affected {
		slog.Debug("affected application",
			"name", app.Name,
			"change_type", app.ChangeType,
			"source_file", app.SourceFile,
		)
	}

	return affected, nil
}

// isScannedAppAffected checks if a scanned application is affected by the changed files.
// It delegates to isAppAffected for the actual path matching logic.
func (e *Executor) isScannedAppAffected(app models.Application, repoURL string, files []string, repoConfig *config.RepoConfig) bool {
	return e.isAppAffected(app, repoURL, files, repoConfig)
}

// isAppAffected checks if an application is affected by the changed files.
func (e *Executor) isAppAffected(app models.Application, repoURL string, files []string, repoConfig *config.RepoConfig) bool {
	// First, check if there's an explicit path mapping in .lemuria.yaml
	// If there is, use it regardless of whether the app's source references this repo
	// (e.g., Helm chart apps where the Application CR is in this repo but the chart is external)
	//
	// Exact name mappings take precedence over wildcard mappings.
	// If an exact mapping exists for this app, its result is authoritative —
	// wildcard mappings will NOT be consulted even if the exact mapping's paths don't match.
	if repoConfig != nil {
		hasExactMapping := false
		for _, mapping := range repoConfig.Applications {
			nameMatches := matchAppName(mapping.Name, app.Name)
			isWildcard := strings.ContainsRune(mapping.Name, '*')
			slog.Debug("checking repo config mapping",
				"app", app.Name,
				"mapping_name", mapping.Name,
				"mapping_paths", mapping.Paths,
				"name_matches", nameMatches,
				"is_wildcard", isWildcard,
			)
			if !nameMatches {
				continue
			}
			// Skip wildcard mappings for now; process them only if no exact mapping matched
			if isWildcard {
				continue
			}
			hasExactMapping = true
			matched := vcs.FilterFilesByPatterns(
				filesToChangedFiles(files),
				mapping.Paths,
			)
			slog.Debug("path pattern matching result",
				"app", app.Name,
				"patterns", mapping.Paths,
				"matched_files", matched,
				"matched_count", len(matched),
			)
			if len(matched) > 0 {
				slog.Debug("application affected via explicit .lemuria.yaml mapping",
					"app", app.Name,
				)
				return true
			}
		}
		// Only check wildcard mappings if no exact mapping was found for this app
		if !hasExactMapping {
			for _, mapping := range repoConfig.Applications {
				if !strings.ContainsRune(mapping.Name, '*') {
					continue
				}
				if !matchAppName(mapping.Name, app.Name) {
					continue
				}
				matched := vcs.FilterFilesByPatterns(
					filesToChangedFiles(files),
					mapping.Paths,
				)
				slog.Debug("path pattern matching result (wildcard)",
					"app", app.Name,
					"patterns", mapping.Paths,
					"matched_files", matched,
					"matched_count", len(matched),
				)
				if len(matched) > 0 {
					slog.Debug("application affected via wildcard .lemuria.yaml mapping",
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
		slog.Debug("comparing repo URLs",
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
		slog.Debug("application does not reference target repo",
			"app", app.Name,
			"app_repos", appRepos,
			"target_repo", repoURL,
		)
		return false
	}

	slog.Debug("application references target repo",
		"app", app.Name,
	)

	// Default: check if any changed file is in the app's path
	if app.Path != "" {
		slog.Debug("checking default path matching",
			"app", app.Name,
			"app_path", app.Path,
			"changed_files_count", len(files),
		)
		for _, f := range files {
			contains := pathContains(app.Path, f)
			if contains {
				slog.Debug("file matches application path",
					"app", app.Name,
					"app_path", app.Path,
					"file", f,
				)
				return true
			}
		}
		slog.Debug("no files match application path",
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

// InvalidatePlanComments marks all existing plan comments on a PR as stale.
func (e *Executor) InvalidatePlanComments(ctx context.Context, event *models.PREvent) error {
	slog.Debug("invalidating old plan comments",
		"repo", event.Repo.FullName,
		"pr", event.PR.Number,
	)
	return e.vcs.InvalidatePlanComments(ctx, event.Repo.Owner, event.Repo.Name, event.PR.Number)
}
