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
	"path"
	"regexp"
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

	filePaths := vcs.GetAllFilePaths(files)
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
		return nil, fmt.Errorf("scanning repo for applications: %w", err)
	}

	// Step 3: Match scanned apps against changed files
	var affected []models.Application
	repoURL := event.Repo.HTMLURL
	alreadyDetected := make(map[string]bool)

	for _, app := range scanned.HeadApps {
		if e.isAppAffected(app, repoURL, filePaths, repoConfig) {
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
				if !alreadyDetected[app.Name] {
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

	// Fetch all existing apps once for verification and cross-repo detection
	existingApps, listErr := e.argocd.ListApplications(ctx)
	if listErr != nil {
		slog.Warn("failed to list existing applications", "error", listErr)
	}
	existingByName := make(map[string]bool)
	for _, app := range existingApps {
		existingByName[app.Name] = true
	}

	// Verify new apps don't already exist in ArgoCD
	verifyNewAppsExist(parsed, existingByName)

	// Verify deleted apps actually exist in ArgoCD
	verifyDeletedAppsExist(parsed, existingByName)

	// Add new applications
	for _, app := range parsed.New {
		slog.Debug("adding new application",
			"app", app.Name,
			"source_file", app.SourceFile,
		)
		if !alreadyDetected[app.Name] {
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
		if alreadyDetected[modApp.Name] {
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
			if !alreadyDetected[app.Name] {
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
		if !alreadyDetected[app.Name] {
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
	if listErr == nil {
		crossRepoApps := detectCrossRepoAffectedApps(repoURL, filePaths, alreadyDetected, existingApps)
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
			isWildcard := isPatternMatch(mapping.Name)
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
				if !isPatternMatch(mapping.Name) {
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
	normalizedRepoURL := argocd.NormalizeRepoURL(repoURL)
	repoMatch := false
	for _, appRepo := range appRepos {
		if argocd.NormalizeRepoURL(appRepo) == normalizedRepoURL {
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

	// Default: check if any changed file matches the app's source paths (including multi-source and Helm valueFiles)
	slog.Debug("checking default path matching",
		"app", app.Name,
		"app_path", app.Path,
		"is_multi_source", app.IsMultiSource(),
		"changed_files_count", len(files),
	)
	for _, f := range files {
		if fileMatchesAppSources(app, repoURL, f) {
			slog.Debug("file matches application source",
				"app", app.Name,
				"file", f,
			)
			return true
		}
	}
	slog.Debug("no files match application sources",
		"app", app.Name,
	)

	return false
}

// matchAppName checks if an app name matches a pattern.
// Supports exact match and regex patterns (delimited by /.../).
// For backward compatibility, simple glob-style trailing '*' is converted to regex.
func matchAppName(pattern, name string) bool {
	if pattern == name {
		return true
	}

	// Regex pattern: /pattern/
	if len(pattern) >= 2 && pattern[0] == '/' && pattern[len(pattern)-1] == '/' {
		re, err := regexp.Compile(pattern[1 : len(pattern)-1])
		if err != nil {
			slog.Warn("invalid regex pattern in app name matching",
				"pattern", pattern, "error", err)
			return false
		}
		return re.MatchString(name)
	}

	// Backward-compatible glob: convert * to regex
	if strings.ContainsRune(pattern, '*') {
		// Escape regex metacharacters except *, then replace * with .*
		escaped := regexp.QuoteMeta(strings.ReplaceAll(pattern, "*", "\x00"))
		regexStr := "^" + strings.ReplaceAll(escaped, "\x00", ".*") + "$"
		re, err := regexp.Compile(regexStr)
		if err != nil {
			slog.Warn("invalid glob pattern in app name matching",
				"pattern", pattern, "error", err)
			return false
		}
		return re.MatchString(name)
	}

	return false
}

// isPatternMatch returns true if the pattern contains wildcards or is a regex,
// i.e. it's not an exact name match.
func isPatternMatch(pattern string) bool {
	if len(pattern) >= 2 && pattern[0] == '/' && pattern[len(pattern)-1] == '/' {
		return true
	}
	return strings.ContainsRune(pattern, '*')
}

// resolveValueFilePath resolves a Helm valueFile path relative to a source path,
// following ArgoCD's resolution rules:
//   - Absolute paths (starting with /) are repo-root-relative (leading / stripped, then cleaned)
//   - Relative paths are resolved relative to the source path
//   - Paths with .. are resolved via path.Join (e.g., "../common/values.yaml" with sourcePath="charts/app" → "charts/common/values.yaml")
func resolveValueFilePath(sourcePath, valueFile string) string {
	if strings.HasPrefix(valueFile, "/") {
		return path.Clean(strings.TrimPrefix(valueFile, "/"))
	}
	if sourcePath == "" || sourcePath == "." {
		return path.Clean(valueFile)
	}
	return path.Join(sourcePath, valueFile)
}

// fileMatchesAppSources checks if a changed file matches any of an application's source paths
// or Helm valueFile paths. For multi-source apps, it checks each source whose RepoURL matches
// the target repo for direct path and non-$ref valueFile matches. For single-source apps, it
// falls back to the existing pathContains check on app.Path.
//
// For $ref valueFile references (e.g., "$values/apps/harbor/values.yaml"), it resolves
// the reference by finding the source with a matching Ref name and checking if that source
// (which may have a different RepoURL) supplies the referenced valueFile in the target repo.
func fileMatchesAppSources(app models.Application, repoURL, file string) bool {
	normalizedRepoURL := argocd.NormalizeRepoURL(repoURL)

	if app.IsMultiSource() {
		// Build a map of ref name → source for resolving $ref valueFile references
		refSources := buildRefSourceMap(app.Sources)

		for _, src := range app.Sources {
			srcRepoMatch := argocd.NormalizeRepoURL(src.RepoURL) == normalizedRepoURL

			// Check if the file is under the source path (only if repo matches)
			if srcRepoMatch && src.Path != "" && pathContains(src.Path, file) {
				return true
			}

			// Check Helm valueFiles
			if src.Helm != nil {
				for _, vf := range src.Helm.ValueFiles {
					if strings.HasPrefix(vf, "$") {
						// Resolve $ref cross-source reference
						if resolveRefValueFile(vf, refSources, normalizedRepoURL, file) {
							return true
						}
						continue
					}
					if srcRepoMatch {
						resolved := resolveValueFilePath(src.Path, vf)
						if resolved == file {
							return true
						}
					}
				}
			}
		}
		return false
	}

	// Single-source: existing behavior
	if app.Path != "" {
		return pathContains(app.Path, file)
	}
	return false
}

// buildRefSourceMap builds a map from ref name to ApplicationSource for all sources
// that have a Ref field set.
func buildRefSourceMap(sources []models.ApplicationSource) map[string]models.ApplicationSource {
	m := make(map[string]models.ApplicationSource)
	for _, src := range sources {
		if src.Ref != "" {
			m[src.Ref] = src
		}
	}
	return m
}

// resolveRefValueFile resolves a $ref-prefixed valueFile path (e.g., "$values/apps/harbor/values.yaml")
// against the ref sources map. It returns true if the referenced source's repo matches the target
// repo and the resolved file path matches the changed file.
func resolveRefValueFile(valueFile string, refSources map[string]models.ApplicationSource, normalizedRepoURL, file string) bool {
	// Parse "$refName/path/to/file" from the valueFile
	withoutDollar := valueFile[1:] // strip leading "$"
	slashIdx := strings.Index(withoutDollar, "/")
	if slashIdx < 0 {
		// No path component after ref name (e.g., "$values") — can't match a file
		return false
	}

	refName := withoutDollar[:slashIdx]
	refPath := withoutDollar[slashIdx+1:]

	refSrc, ok := refSources[refName]
	if !ok {
		// No source with this ref name — skip
		return false
	}

	// Check if the ref source's repo matches the target repo
	if argocd.NormalizeRepoURL(refSrc.RepoURL) != normalizedRepoURL {
		return false
	}

	// Normalize both paths so dot segments or extra slashes still compare equal
	return path.Clean(refPath) == path.Clean(file)
}

// pathContains checks if a file path is within the given directory.
func pathContains(dir, file string) bool {
	if dir == "" || dir == "." {
		return true
	}
	// Ensure directory separator after the prefix to avoid false positives
	// e.g. dir="apps/my-app" should not match file="apps/my-app-other/deploy.yaml"
	d := dir
	if !strings.HasSuffix(d, "/") {
		d += "/"
	}
	return strings.HasPrefix(file, d)
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
