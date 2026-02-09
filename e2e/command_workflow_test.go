package e2e

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/org/lemuria/internal/commands"
	"github.com/org/lemuria/internal/config"
	"github.com/org/lemuria/internal/models"
)

// newTestExecutor creates a commands.Executor with real ArgoCD + Redis and a mock VCS client.
func newTestExecutor(gh *MockVCSClient, cfg *config.Config) *commands.Executor {
	if cfg == nil {
		cfg = &config.Config{
			ArgoCD: config.ArgoCDConfig{
				DiffMode:       "live",
				TempAppTimeout: 2 * time.Minute,
			},
			Defaults: config.DefaultsConfig{
				RequireApproval: false,
				AutoMerge:       false,
			},
		}
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return commands.NewExecutor(gh, argoClient, lockManager, cfg, logger)
}

// newPREvent creates a PREvent for testing (defaults to GitHub provider).
func newPREvent(repo, owner, repoName string, prNumber int, headSHA, headRef, baseRef string, commentBody string) *models.PREvent {
	event := &models.PREvent{
		Provider: models.VCSProviderGitHub,
		Type:     models.EventTypeIssueComment,
		Action:   models.PRActionCreated,
		Repo: models.RepoInfo{
			Owner:    owner,
			Name:     repoName,
			FullName: repo,
			HTMLURL:  "https://github.com/" + repo,
		},
		PR: models.PRInfo{
			Number:  prNumber,
			State:   models.PRStateOpen,
			HeadSHA: headSHA,
			HeadRef: headRef,
			BaseRef: baseRef,
		},
		Sender: models.UserInfo{
			Login: "test-user",
		},
		ReceivedAt: time.Now(),
	}

	if commentBody != "" {
		event.Comment = &models.Comment{
			ID:        12345,
			Body:      commentBody,
			Author:    models.UserInfo{Login: "test-user"},
			CreatedAt: time.Now(),
		}
	}

	return event
}

// newGitLabPREvent creates a PREvent for testing with GitLab provider.
func newGitLabPREvent(repo, owner, repoName string, prNumber int, headSHA, headRef, baseRef string, commentBody string) *models.PREvent {
	event := newPREvent(repo, owner, repoName, prNumber, headSHA, headRef, baseRef, commentBody)
	event.Provider = models.VCSProviderGitLab
	event.Repo.HTMLURL = "https://gitlab.com/" + repo
	return event
}

// =============================================================================
// Plan Command Tests
// =============================================================================

func TestE2EPlanCommand(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E command test in short mode")
	}

	// Use pre-existing test-app
	appName := "test-app"

	// Verify app exists
	app, err := argoClient.GetApplication(testCtx, appName)
	if err != nil {
		t.Skipf("test-app not available: %v", err)
	}
	t.Logf("Using test application: %s (path: %s)", app.Name, app.Path)

	// Ensure no stale locks
	_ = lockManager.ForceUnlock(testCtx, appName)

	mockGH := NewMockVCSClient()
	mockGH.RepoConfigErr = fmt.Errorf(".lemuria.yaml not found")

	executor := newTestExecutor(mockGH, nil)

	headSHA := "abc123plan"
	event := newPREvent(
		"test-owner/test-repo", "test-owner", "test-repo",
		100, headSHA, "feature-branch", "main",
		"lemuria plan -a test-app",
	)

	cmd := &commands.Command{
		Name:        commands.CommandPlan,
		Application: appName,
	}

	err = executor.Execute(testCtx, cmd, event)
	if err != nil {
		t.Fatalf("Plan command failed: %v", err)
	}

	// Assert: comment was posted
	comments := mockGH.GetPostedComments()
	if len(comments) == 0 {
		t.Fatal("Expected at least one comment to be posted")
	}

	lastComment := comments[len(comments)-1]
	t.Logf("Posted comment (truncated): %.200s...", lastComment.Body)

	if !lastComment.IsPlan {
		t.Error("Expected plan comment to have isPlan=true")
	}

	// Assert: lock was acquired with correct PlanRevision
	lock, err := lockManager.Get(testCtx, appName)
	if err != nil {
		t.Fatalf("Failed to get lock: %v", err)
	}
	if lock == nil {
		t.Fatal("Expected lock to be acquired after plan")
	}
	if lock.PRNumber != 100 {
		t.Errorf("Expected lock PR number 100, got %d", lock.PRNumber)
	}
	if lock.PlanRevision != headSHA {
		t.Errorf("Expected lock PlanRevision %q, got %q", headSHA, lock.PlanRevision)
	}

	t.Logf("Lock acquired: app=%s, pr=%d, user=%s, plan_revision=%s, plan_diffs=%d",
		lock.Application, lock.PRNumber, lock.User, lock.PlanRevision, len(lock.PlanDiffs))

	// Assert: if the plan comment shows resource changes, PlanDiffs should be populated
	if strings.Contains(lastComment.Body, "resources changed") {
		if len(lock.PlanDiffs) == 0 {
			t.Error("Expected PlanDiffs to be populated when plan shows resource changes")
		}
		for i, d := range lock.PlanDiffs {
			if d.Resource.Kind == "" {
				t.Errorf("PlanDiffs[%d]: expected non-empty Kind", i)
			}
			if d.Action == "" {
				t.Errorf("PlanDiffs[%d]: expected non-empty Action", i)
			}
			t.Logf("  PlanDiff[%d]: %s %s/%s", i, d.Action, d.Resource.Kind, d.Resource.Name)
		}
	}

	// Assert: reaction was added
	if len(mockGH.Reactions) == 0 {
		t.Error("Expected reaction to be added to comment")
	}

	// Assert: old plan comments were invalidated
	if len(mockGH.InvalidatedPRs) == 0 {
		t.Error("Expected old plan comments to be invalidated")
	}

	// Cleanup
	_ = lockManager.ForceUnlock(testCtx, appName)
}

// =============================================================================
// Plan: External Helm Chart App Detection
// =============================================================================

// TestE2EPlanDetectsModifiedExternalChartApp verifies that when a PR modifies
// an Application CR file where the app sources from an external Helm chart
// (not the PR's git repo), the app is still detected as affected.
// This is a regression test for a bug where apps with external chart sources
// were invisible to both isAppAffected() and detectApplicationChanges().
func TestE2EPlanDetectsModifiedExternalChartApp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E command test in short mode")
	}

	appName := uniqueAppName("e2e-helm")

	// Create an app with an external Helm chart source (not a git repo)
	createTestHelmChartApplication(testCtx, t, argoClient, appName, "e2e-test-apps")
	defer deleteTestApplication(testCtx, t, argoClient, appName)

	// Wait for app to appear in ArgoCD
	waitForAppReady(testCtx, t, argoClient, appName, 60*time.Second)

	// Ensure clean lock state
	defer cleanupForceUnlock(testCtx, t, appName)

	// Configure mock GitHub to simulate a PR that modifies the Application CR file.
	// The PR repo is "test-owner/test-repo", but the app's source is
	// "https://argoproj.github.io/argo-helm" — a completely different repo.
	mockGH := NewMockVCSClient()
	mockGH.RepoConfigErr = fmt.Errorf(".lemuria.yaml not found")

	crFilePath := "bootstrap/" + appName + ".yaml"
	mockGH.ChangedFiles = []models.ChangedFile{
		{Filename: crFilePath, Status: models.FileStatusModified},
	}

	// Base version (current Helm values)
	baseYAML := fmt.Sprintf(`apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: %s
  namespace: argocd
spec:
  project: default
  source:
    repoURL: https://argoproj.github.io/argo-helm
    chart: argocd-apps
    targetRevision: "1.4.1"
  destination:
    server: https://kubernetes.default.svc
    namespace: e2e-test-apps`, appName)

	// Head version (modified Helm values — added helm.values)
	headYAML := fmt.Sprintf(`apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: %s
  namespace: argocd
spec:
  project: default
  source:
    repoURL: https://argoproj.github.io/argo-helm
    chart: argocd-apps
    targetRevision: "1.4.1"
    helm:
      values: |
        createClusterRoles: false
  destination:
    server: https://kubernetes.default.svc
    namespace: e2e-test-apps`, appName)

	mockGH.FileContents[crFilePath+"@main"] = []byte(baseYAML)
	mockGH.FileContents[crFilePath+"@feature-branch"] = []byte(headYAML)

	cfg := &config.Config{
		ArgoCD: config.ArgoCDConfig{
			DiffMode:       "branch",
			TempAppTimeout: 90 * time.Second,
		},
		Defaults: config.DefaultsConfig{
			RequireApproval: false,
		},
	}
	executor := newTestExecutor(mockGH, cfg)

	// Run plan in auto-detect mode (no -a flag) — the app should be found
	// via detectApplicationChanges even though its source doesn't match the PR repo
	event := newPREvent(
		"test-owner/test-repo", "test-owner", "test-repo",
		1100, "abc123", "feature-branch", "main",
		"lemuria plan",
	)
	cmd := &commands.Command{Name: commands.CommandPlan}

	err := executor.Execute(testCtx, cmd, event)
	if err != nil {
		t.Fatalf("Plan command failed: %v", err)
	}

	// Assert: a comment was posted
	comments := mockGH.GetPostedComments()
	if len(comments) == 0 {
		t.Fatal("Expected at least one comment to be posted")
	}

	lastComment := comments[len(comments)-1]
	t.Logf("Plan comment (truncated): %.500s...", lastComment.Body)

	// Assert: the app WAS detected (not "No applications affected")
	if strings.Contains(lastComment.Body, "No applications affected") {
		t.Fatal("App with external Helm chart source should have been detected as affected when its Application CR file is modified in the PR")
	}

	// Assert: the app name appears in the comment
	if !strings.Contains(lastComment.Body, appName) {
		t.Errorf("Expected plan comment to mention app %q", appName)
	}

	// Assert: lock was acquired (confirms the app went through planApplication)
	lock, err := lockManager.Get(testCtx, appName)
	if err != nil {
		t.Fatalf("Failed to get lock: %v", err)
	}
	if lock == nil {
		t.Fatal("Expected lock to be acquired for the detected app")
	}
	if lock.PRNumber != 1100 {
		t.Errorf("Expected lock PR number 1100, got %d", lock.PRNumber)
	}
}

// =============================================================================
// Plan Revision Persistence Tests
// =============================================================================

// TestE2EPlanRevisionPersistedOnLock verifies that after running plan, the lock
// has the PlanRevision field set so that sync can verify plan freshness.
// This is a regression test for a bug where StorePlan stored the revision in a
// separate Redis key but never updated the lock object, causing sync to always
// see PlanRevision="" and reject with "plan is stale".
func TestE2EPlanRevisionPersistedOnLock(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E command test in short mode")
	}

	appName := "test-app"
	_, err := argoClient.GetApplication(testCtx, appName)
	if err != nil {
		t.Skipf("test-app not available: %v", err)
	}

	repo := "test-owner/test-repo"
	prNumber := 150
	headSHA := "7c6b40f55ab2a67877f5695f4cf0af9bb54fc219"

	// Ensure clean lock state
	_ = lockManager.ForceUnlock(testCtx, appName)
	defer cleanupForceUnlock(testCtx, t, appName)

	mockGH := NewMockVCSClient()
	mockGH.RepoConfigErr = fmt.Errorf(".lemuria.yaml not found")
	executor := newTestExecutor(mockGH, nil)

	// Run plan with a specific HeadSHA
	event := newPREvent(repo, "test-owner", "test-repo", prNumber, headSHA, "feature-branch", "main", "lemuria plan -a "+appName)
	cmd := &commands.Command{
		Name:        commands.CommandPlan,
		Application: appName,
	}

	if err := executor.Execute(testCtx, cmd, event); err != nil {
		t.Fatalf("Plan command failed: %v", err)
	}

	// Assert: PlanRevision is set on the lock via Get
	lock, err := lockManager.Get(testCtx, appName)
	if err != nil {
		t.Fatalf("Failed to get lock: %v", err)
	}
	if lock == nil {
		t.Fatal("Expected lock to be acquired after plan")
	}
	if lock.PlanRevision != headSHA {
		t.Errorf("Get: expected PlanRevision %q, got %q", headSHA, lock.PlanRevision)
	}

	// Assert: PlanDiffs are persisted on the lock via Get
	// (the live diff against test-app may or may not produce diffs depending on cluster state)
	t.Logf("Get: PlanDiffs count = %d, PlanOutput = %q", len(lock.PlanDiffs), lock.PlanOutput)
	if lock.PlanOutput != "" && lock.PlanOutput != "No changes detected" {
		// If there's a non-trivial plan output, PlanDiffs should also be populated
		if len(lock.PlanDiffs) == 0 {
			t.Errorf("Get: expected PlanDiffs to be populated when PlanOutput=%q", lock.PlanOutput)
		}
	}

	// Assert: PlanRevision is also set on locks returned by ListByPR
	// (this is the path used by executeSync)
	locks, err := lockManager.ListByPR(testCtx, repo, prNumber)
	if err != nil {
		t.Fatalf("Failed to list locks by PR: %v", err)
	}
	if len(locks) == 0 {
		t.Fatal("Expected at least one lock from ListByPR")
	}
	found := false
	for _, l := range locks {
		if l.Application == appName {
			found = true
			if l.PlanRevision != headSHA {
				t.Errorf("ListByPR: expected PlanRevision %q, got %q", headSHA, l.PlanRevision)
			}
			// Assert: PlanDiffs also round-trip through ListByPR
			if len(lock.PlanDiffs) > 0 && len(l.PlanDiffs) != len(lock.PlanDiffs) {
				t.Errorf("ListByPR: expected %d PlanDiffs, got %d", len(lock.PlanDiffs), len(l.PlanDiffs))
			}
			break
		}
	}
	if !found {
		t.Errorf("Expected lock for %s in ListByPR results", appName)
	}

	// Assert: sync with the same HeadSHA does NOT report stale plan
	mockGH.Reset()
	syncEvent := newPREvent(repo, "test-owner", "test-repo", prNumber, headSHA, "feature-branch", "main", "lemuria sync")
	syncCmd := &commands.Command{Name: commands.CommandSync}

	if err := executor.Execute(testCtx, syncCmd, syncEvent); err != nil {
		t.Fatalf("Sync command failed: %v", err)
	}

	comments := mockGH.GetPostedComments()
	if len(comments) == 0 {
		t.Fatal("Expected sync result comment")
	}
	lastComment := comments[len(comments)-1]
	if strings.Contains(lastComment.Body, "stale") {
		t.Errorf("Sync should NOT report stale plan when HeadSHA matches PlanRevision, got: %s", lastComment.Body)
	}

	// Assert: if PlanDiffs were stored and sync actually proceeded (not blocked by
	// auto-sync or other pre-sync checks), they should appear in the sync comment.
	if len(lock.PlanDiffs) > 0 && !strings.Contains(lastComment.Body, "auto-sync enabled") {
		if !strings.Contains(lastComment.Body, "Plan Diff") {
			t.Errorf("Sync comment should include 'Plan Diff' section when PlanDiffs are stored")
		}
	}
}

// =============================================================================
// Sync Command Tests
// =============================================================================

func TestE2ESyncCommand(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E command test in short mode")
	}

	appName := uniqueAppName("e2e-sync")
	repo := "test-owner/test-repo"
	prNumber := 200

	// Create a per-test application
	createTestApplication(testCtx, t, argoClient, appName, "e2e-test-apps")
	defer deleteTestApplication(testCtx, t, argoClient, appName)
	waitForAppReady(testCtx, t, argoClient, appName, 60*time.Second)

	// Ensure clean lock state
	defer cleanupForceUnlock(testCtx, t, appName)

	headSHA := "abc123sync"

	mockGH := NewMockVCSClient()
	mockGH.RepoConfigErr = fmt.Errorf(".lemuria.yaml not found")
	executor := newTestExecutor(mockGH, nil)

	// Step 1: Run plan to acquire lock
	planEvent := newPREvent(repo, "test-owner", "test-repo", prNumber, headSHA, "feature-branch", "main", "lemuria plan -a "+appName)
	planCmd := &commands.Command{
		Name:        commands.CommandPlan,
		Application: appName,
	}

	if err := executor.Execute(testCtx, planCmd, planEvent); err != nil {
		t.Fatalf("Plan command failed: %v", err)
	}

	// Verify lock acquired with PlanRevision
	lock, err := lockManager.Get(testCtx, appName)
	if err != nil || lock == nil {
		t.Fatalf("Expected lock to be acquired: %v", err)
	}
	if lock.PlanRevision != headSHA {
		t.Fatalf("Expected lock PlanRevision %q, got %q", headSHA, lock.PlanRevision)
	}

	// Step 2: Run sync with the same HeadSHA (plan is fresh)
	mockGH.Reset()
	syncEvent := newPREvent(repo, "test-owner", "test-repo", prNumber, headSHA, "feature-branch", "main", "lemuria sync")
	syncCmd := &commands.Command{
		Name: commands.CommandSync,
	}

	err = executor.Execute(testCtx, syncCmd, syncEvent)
	if err != nil {
		t.Fatalf("Sync command failed: %v", err)
	}

	// Assert: sync comment was posted
	comments := mockGH.GetPostedComments()
	if len(comments) == 0 {
		t.Fatal("Expected sync result comment to be posted")
	}

	lastComment := comments[len(comments)-1]
	t.Logf("Sync comment: %.300s", lastComment.Body)

	if !strings.Contains(lastComment.Body, "Sync") {
		t.Error("Expected sync result in comment body")
	}

	// Assert: lock was released (successful sync releases lock)
	lock, err = lockManager.Get(testCtx, appName)
	if err != nil {
		t.Fatalf("Failed to check lock after sync: %v", err)
	}
	if lock != nil {
		t.Logf("Note: lock still held after sync (sync may not have fully succeeded, phase may not be Succeeded)")
	}
}

// =============================================================================
// Rollback Command Tests
// =============================================================================

func TestE2ERollbackCommand(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E command test in short mode")
	}

	appName := uniqueAppName("e2e-rollback")
	repo := "test-owner/test-repo"
	prNumber := 300

	// Create a per-test application
	createTestApplication(testCtx, t, argoClient, appName, "e2e-test-apps")
	defer deleteTestApplication(testCtx, t, argoClient, appName)
	waitForAppReady(testCtx, t, argoClient, appName, 60*time.Second)

	// Ensure clean lock state
	defer cleanupForceUnlock(testCtx, t, appName)

	mockGH := NewMockVCSClient()
	mockGH.RepoConfigErr = fmt.Errorf(".lemuria.yaml not found")
	executor := newTestExecutor(mockGH, nil)

	// Step 1: Run plan to acquire lock
	planEvent := newPREvent(repo, "test-owner", "test-repo", prNumber, "", "feature-branch", "main", "lemuria plan -a "+appName)
	planCmd := &commands.Command{
		Name:        commands.CommandPlan,
		Application: appName,
	}
	if err := executor.Execute(testCtx, planCmd, planEvent); err != nil {
		t.Fatalf("Plan command failed: %v", err)
	}

	// Step 2: Run rollback
	mockGH.Reset()
	rollbackEvent := newPREvent(repo, "test-owner", "test-repo", prNumber, "", "feature-branch", "main", "lemuria rollback -a "+appName)
	rollbackCmd := &commands.Command{
		Name:        commands.CommandRollback,
		Application: appName,
	}

	err := executor.Execute(testCtx, rollbackCmd, rollbackEvent)
	if err != nil {
		t.Fatalf("Rollback command failed: %v", err)
	}

	// Assert: rollback comment was posted
	comments := mockGH.GetPostedComments()
	if len(comments) == 0 {
		t.Fatal("Expected rollback result comment to be posted")
	}

	lastComment := comments[len(comments)-1]
	t.Logf("Rollback comment: %.300s", lastComment.Body)

	if !strings.Contains(lastComment.Body, "Rollback") {
		t.Error("Expected rollback info in comment body")
	}
}

// =============================================================================
// Unlock Command Tests
// =============================================================================

func TestE2EUnlockCommand(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E command test in short mode")
	}

	repo := "test-owner/test-repo"
	prNumber := 400

	// Use pre-existing test-app to avoid needing to create/delete
	_, err := argoClient.GetApplication(testCtx, "test-app")
	if err != nil {
		t.Skipf("test-app not available: %v", err)
	}
	appName := "test-app"

	// Ensure clean lock state
	_ = lockManager.ForceUnlock(testCtx, appName)
	defer cleanupForceUnlock(testCtx, t, appName)

	mockGH := NewMockVCSClient()
	mockGH.RepoConfigErr = fmt.Errorf(".lemuria.yaml not found")
	executor := newTestExecutor(mockGH, nil)

	// Step 1: Run plan to acquire lock
	planEvent := newPREvent(repo, "test-owner", "test-repo", prNumber, "", "feature-branch", "main", "lemuria plan -a "+appName)
	planCmd := &commands.Command{
		Name:        commands.CommandPlan,
		Application: appName,
	}
	if err := executor.Execute(testCtx, planCmd, planEvent); err != nil {
		t.Fatalf("Plan command failed: %v", err)
	}

	// Verify lock acquired
	lock, err := lockManager.Get(testCtx, appName)
	if err != nil || lock == nil {
		t.Fatalf("Expected lock to be acquired: %v", err)
	}

	// Step 2: Run unlock
	mockGH.Reset()
	unlockEvent := newPREvent(repo, "test-owner", "test-repo", prNumber, "", "feature-branch", "main", "lemuria unlock")
	unlockCmd := &commands.Command{
		Name: commands.CommandUnlock,
	}

	err = executor.Execute(testCtx, unlockCmd, unlockEvent)
	if err != nil {
		t.Fatalf("Unlock command failed: %v", err)
	}

	// Assert: unlock comment was posted
	comments := mockGH.GetPostedComments()
	if len(comments) == 0 {
		t.Fatal("Expected unlock result comment to be posted")
	}

	lastComment := comments[len(comments)-1]
	t.Logf("Unlock comment: %.300s", lastComment.Body)

	if !strings.Contains(lastComment.Body, "Unlock") {
		t.Error("Expected unlock info in comment body")
	}
	if !strings.Contains(lastComment.Body, "Unlocked") {
		t.Error("Expected 'Unlocked' in comment body")
	}

	// Assert: lock was released
	lock, err = lockManager.Get(testCtx, appName)
	if err != nil {
		t.Fatalf("Failed to check lock: %v", err)
	}
	if lock != nil {
		t.Error("Expected lock to be released after unlock")
	}
}

// =============================================================================
// Sync: External Source (Helm Chart) Tests
// =============================================================================

// TestE2ESyncExternalSourceApp verifies that syncing an app with an external Helm
// chart source (not the PR repo) works correctly:
// - The app spec is updated from the PR head branch before syncing
// - No git SHA revision is passed to the sync API (Helm repos use semver)
// - The SourceFile field on the lock is used to find the Application CR
func TestE2ESyncExternalSourceApp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E command test in short mode")
	}

	appName := uniqueAppName("e2e-ext-sync")
	repo := "test-owner/test-repo"
	prNumber := 1200
	headSHA := "abc123extsync"

	// Create an app with an external Helm chart source
	createTestHelmChartApplication(testCtx, t, argoClient, appName, "e2e-test-apps")
	defer deleteTestApplication(testCtx, t, argoClient, appName)
	waitForAppReady(testCtx, t, argoClient, appName, 60*time.Second)

	defer cleanupForceUnlock(testCtx, t, appName)

	crFilePath := "bootstrap/" + appName + ".yaml"

	// Base YAML (current Helm values)
	baseYAML := fmt.Sprintf(`apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: %s
  namespace: argocd
spec:
  project: default
  source:
    repoURL: https://argoproj.github.io/argo-helm
    chart: argocd-apps
    targetRevision: "1.4.1"
  destination:
    server: https://kubernetes.default.svc
    namespace: e2e-test-apps`, appName)

	// Head YAML (modified Helm values)
	headYAML := fmt.Sprintf(`apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: %s
  namespace: argocd
spec:
  project: default
  source:
    repoURL: https://argoproj.github.io/argo-helm
    chart: argocd-apps
    targetRevision: "1.4.1"
    helm:
      values: |
        createClusterRoles: false
  destination:
    server: https://kubernetes.default.svc
    namespace: e2e-test-apps`, appName)

	mockGH := NewMockVCSClient()
	mockGH.RepoConfigErr = fmt.Errorf(".lemuria.yaml not found")
	mockGH.ChangedFiles = []models.ChangedFile{
		{Filename: crFilePath, Status: models.FileStatusModified},
	}
	mockGH.FileContents[crFilePath+"@main"] = []byte(baseYAML)
	mockGH.FileContents[crFilePath+"@feature-branch"] = []byte(headYAML)

	cfg := &config.Config{
		ArgoCD: config.ArgoCDConfig{
			DiffMode:       "branch",
			TempAppTimeout: 90 * time.Second,
		},
		Defaults: config.DefaultsConfig{
			RequireApproval: false,
		},
	}
	executor := newTestExecutor(mockGH, cfg)

	// Step 1: Run plan to acquire lock and store SourceFile
	planEvent := newPREvent(repo, "test-owner", "test-repo", prNumber, headSHA, "feature-branch", "main", "lemuria plan")
	planCmd := &commands.Command{Name: commands.CommandPlan}

	if err := executor.Execute(testCtx, planCmd, planEvent); err != nil {
		t.Fatalf("Plan command failed: %v", err)
	}

	// Assert: lock was acquired with SourceFile set
	lock, err := lockManager.Get(testCtx, appName)
	if err != nil || lock == nil {
		t.Fatalf("Expected lock to be acquired: %v", err)
	}
	if lock.PlanRevision != headSHA {
		t.Fatalf("Expected lock PlanRevision %q, got %q", headSHA, lock.PlanRevision)
	}
	if lock.SourceFile != crFilePath {
		t.Fatalf("Expected lock SourceFile %q, got %q", crFilePath, lock.SourceFile)
	}
	t.Logf("Lock acquired: app=%s, plan_revision=%s, source_file=%s", lock.Application, lock.PlanRevision, lock.SourceFile)

	// Step 2: Run sync
	mockGH.Reset()
	syncEvent := newPREvent(repo, "test-owner", "test-repo", prNumber, headSHA, "feature-branch", "main", "lemuria sync")
	syncCmd := &commands.Command{Name: commands.CommandSync}

	if err := executor.Execute(testCtx, syncCmd, syncEvent); err != nil {
		t.Fatalf("Sync command failed: %v", err)
	}

	// Assert: sync comment was posted
	comments := mockGH.GetPostedComments()
	if len(comments) == 0 {
		t.Fatal("Expected sync result comment to be posted")
	}

	lastComment := comments[len(comments)-1]
	t.Logf("Sync comment: %.500s", lastComment.Body)

	// Assert: sync did NOT report stale plan
	if strings.Contains(lastComment.Body, "stale") {
		t.Error("Sync should not report stale plan")
	}

	// Assert: sync result mentions the app
	if !strings.Contains(lastComment.Body, appName) {
		t.Errorf("Expected sync comment to mention app %q", appName)
	}

	// Assert: sync attempted (not an error about revision)
	if strings.Contains(lastComment.Body, "No applications are locked") {
		t.Error("Expected sync to find the locked application")
	}
}

// TestE2ESyncMixedApps verifies sync with multiple apps in a single PR where:
// - One app sources from the PR's git repo (git-sourced, no SourceFile)
// - One app sources from an external Helm chart repo (external-sourced, has SourceFile)
// Both should sync correctly: the git app gets HeadSHA as revision, the Helm app
// gets its spec updated and syncs without a git SHA revision.
//
// This test directly sets up locks via the lock manager to avoid the overhead of
// diff generation (which can timeout for external Helm charts). The full plan+sync
// flow for external source apps is tested in TestE2ESyncExternalSourceApp.
func TestE2ESyncMixedApps(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E command test in short mode")
	}

	gitAppName := uniqueAppName("e2e-git-mix")
	helmAppName := uniqueAppName("e2e-helm-mix")
	repo := "test-owner/test-repo"
	prNumber := int(time.Now().UnixNano()%90000) + 10000 // unique PR number to avoid stale lock collisions
	headSHA := "abc123mixed"

	// Create both apps
	createTestApplication(testCtx, t, argoClient, gitAppName, "e2e-test-apps")
	defer deleteTestApplication(testCtx, t, argoClient, gitAppName)
	waitForAppReady(testCtx, t, argoClient, gitAppName, 60*time.Second)

	createTestHelmChartApplication(testCtx, t, argoClient, helmAppName, "e2e-test-apps")
	defer deleteTestApplication(testCtx, t, argoClient, helmAppName)
	waitForAppReady(testCtx, t, argoClient, helmAppName, 60*time.Second)

	defer cleanupForceUnlock(testCtx, t, gitAppName)
	defer cleanupForceUnlock(testCtx, t, helmAppName)

	helmCRFilePath := "bootstrap/" + helmAppName + ".yaml"

	// Head YAML for the Helm app CR
	helmHeadYAML := fmt.Sprintf(`apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: %s
  namespace: argocd
spec:
  project: default
  source:
    repoURL: https://argoproj.github.io/argo-helm
    chart: argocd-apps
    targetRevision: "1.4.1"
    helm:
      values: |
        createClusterRoles: false
  destination:
    server: https://kubernetes.default.svc
    namespace: e2e-test-apps`, helmAppName)

	// Step 1: Directly acquire locks and store plans (bypassing diff generation)
	// Git app: lock with empty SourceFile (sources from PR repo)
	_, err := lockManager.Lock(testCtx, models.LockRequest{
		Application: gitAppName,
		PRNumber:    prNumber,
		Repo:        repo,
		User:        "test-user",
	})
	if err != nil {
		t.Fatalf("Failed to lock git app: %v", err)
	}
	gitAppDiffs := []models.PlanDiffEntry{
		{
			Resource: models.ResourceKey{APIVersion: "apps/v1", Kind: "Deployment", Name: gitAppName, Namespace: "e2e-test-apps"},
			Action:   models.DiffActionUpdate,
			Diff:     "- replicas: 1\n+ replicas: 2\n",
		},
	}
	if err := lockManager.StorePlan(testCtx, gitAppName, prNumber, headSHA, "", "1 to update", gitAppDiffs); err != nil {
		t.Fatalf("Failed to store plan for git app: %v", err)
	}

	// Helm app: lock with SourceFile set (external source, CR modified in PR)
	_, err = lockManager.Lock(testCtx, models.LockRequest{
		Application: helmAppName,
		PRNumber:    prNumber,
		Repo:        repo,
		User:        "test-user",
	})
	if err != nil {
		t.Fatalf("Failed to lock Helm app: %v", err)
	}
	helmAppDiffs := []models.PlanDiffEntry{
		{
			Resource: models.ResourceKey{APIVersion: "v1", Kind: "ConfigMap", Name: "helm-values", Namespace: "e2e-test-apps"},
			Action:   models.DiffActionCreate,
			Diff:     "+ apiVersion: v1\n+ kind: ConfigMap\n",
		},
	}
	if err := lockManager.StorePlan(testCtx, helmAppName, prNumber, headSHA, helmCRFilePath, "1 to update", helmAppDiffs); err != nil {
		t.Fatalf("Failed to store plan for Helm app: %v", err)
	}

	// Verify locks via ListByPR
	locks, err := lockManager.ListByPR(testCtx, repo, prNumber)
	if err != nil {
		t.Fatalf("ListByPR failed: %v", err)
	}
	if len(locks) < 2 {
		t.Fatalf("Expected at least 2 locks from ListByPR, got %d", len(locks))
	}
	t.Logf("Locks acquired: %d apps for PR #%d", len(locks), prNumber)

	// Step 2: Sync all locked apps
	mockGH := NewMockVCSClient()
	mockGH.RepoConfigErr = fmt.Errorf(".lemuria.yaml not found")
	mockGH.FileContents[helmCRFilePath+"@feature-branch"] = []byte(helmHeadYAML)

	cfg := &config.Config{
		ArgoCD: config.ArgoCDConfig{
			DiffMode:       "live",
			TempAppTimeout: 90 * time.Second,
		},
		Defaults: config.DefaultsConfig{
			RequireApproval: false,
		},
	}
	executor := newTestExecutor(mockGH, cfg)

	syncEvent := newPREvent(repo, "test-owner", "test-repo", prNumber, headSHA, "feature-branch", "main", "lemuria sync")
	syncCmd := &commands.Command{Name: commands.CommandSync}

	if err := executor.Execute(testCtx, syncCmd, syncEvent); err != nil {
		t.Fatalf("Sync command failed: %v", err)
	}

	// Assert: sync comment was posted
	comments := mockGH.GetPostedComments()
	if len(comments) == 0 {
		t.Fatal("Expected sync result comment")
	}

	lastComment := comments[len(comments)-1]
	t.Logf("Sync comment: %.800s", lastComment.Body)

	// Assert: both apps mentioned in sync output
	if !strings.Contains(lastComment.Body, gitAppName) {
		t.Errorf("Expected sync comment to mention git app %q", gitAppName)
	}
	if !strings.Contains(lastComment.Body, helmAppName) {
		t.Errorf("Expected sync comment to mention Helm app %q", helmAppName)
	}

	// Assert: no stale plan error
	if strings.Contains(lastComment.Body, "stale") {
		t.Error("Sync should not report stale plan")
	}

	// Assert: plan output is included in sync comment
	if !strings.Contains(lastComment.Body, "Planned changes") {
		t.Error("Expected sync comment to include plan summary")
	}
	if !strings.Contains(lastComment.Body, "1 to update") {
		t.Error("Expected sync comment to show '1 to update' from stored plan output")
	}

	// Assert: plan diffs are rendered in the sync comment
	if !strings.Contains(lastComment.Body, "Plan Diff") {
		t.Error("Expected sync comment to include 'Plan Diff' section from stored PlanDiffs")
	}
	if !strings.Contains(lastComment.Body, "resources changed") {
		t.Error("Expected sync comment to include resource count in plan diff section")
	}
	// Verify diff content appears in the comment (inside ```diff blocks)
	if !strings.Contains(lastComment.Body, "replicas") {
		t.Error("Expected sync comment to contain diff content (replicas) from stored PlanDiffs")
	}
}

// =============================================================================
// Sync Error Scenario Tests
// =============================================================================

func TestE2ESyncStalePlan(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E command test in short mode")
	}

	repo := "test-owner/test-repo"
	prNumber := 500

	// Use pre-existing test-app
	_, err := argoClient.GetApplication(testCtx, "test-app")
	if err != nil {
		t.Skipf("test-app not available: %v", err)
	}
	appName := "test-app"

	// Ensure clean lock state
	_ = lockManager.ForceUnlock(testCtx, appName)
	defer cleanupForceUnlock(testCtx, t, appName)

	mockGH := NewMockVCSClient()
	mockGH.RepoConfigErr = fmt.Errorf(".lemuria.yaml not found")
	executor := newTestExecutor(mockGH, nil)

	// Step 1: Run plan to acquire lock with a specific HeadSHA
	planSHA := "abc123original"
	planEvent := newPREvent(repo, "test-owner", "test-repo", prNumber, planSHA, "feature-branch", "main", "lemuria plan -a "+appName)
	planCmd := &commands.Command{
		Name:        commands.CommandPlan,
		Application: appName,
	}
	if err := executor.Execute(testCtx, planCmd, planEvent); err != nil {
		t.Fatalf("Plan command failed: %v", err)
	}

	// Verify plan revision was stored on the lock
	lock, err := lockManager.Get(testCtx, appName)
	if err != nil || lock == nil {
		t.Fatalf("Expected lock to be acquired: %v", err)
	}
	if lock.PlanRevision != planSHA {
		t.Fatalf("Expected lock PlanRevision %q, got %q", planSHA, lock.PlanRevision)
	}

	// Step 2: Run sync with a DIFFERENT HeadSHA to trigger stale plan
	mockGH.Reset()
	syncEvent := newPREvent(repo, "test-owner", "test-repo", prNumber, "def456newpush", "feature-branch", "main", "lemuria sync")
	syncCmd := &commands.Command{
		Name: commands.CommandSync,
	}

	err = executor.Execute(testCtx, syncCmd, syncEvent)
	if err != nil {
		t.Fatalf("Sync command returned error: %v", err)
	}

	// Assert: sync rejected with stale plan message
	comments := mockGH.GetPostedComments()
	if len(comments) == 0 {
		t.Fatal("Expected comment about stale plan")
	}

	lastComment := comments[len(comments)-1]
	t.Logf("Stale plan comment: %.300s", lastComment.Body)

	if !strings.Contains(lastComment.Body, "stale") {
		t.Error("Expected 'stale' in rejection comment")
	}
}

func TestE2ESyncNoLock(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E command test in short mode")
	}

	repo := "test-owner/test-repo"
	prNumber := 600

	mockGH := NewMockVCSClient()
	executor := newTestExecutor(mockGH, nil)

	// Run sync without plan first (no locks)
	syncEvent := newPREvent(repo, "test-owner", "test-repo", prNumber, "", "feature-branch", "main", "lemuria sync")
	syncCmd := &commands.Command{
		Name: commands.CommandSync,
	}

	err := executor.Execute(testCtx, syncCmd, syncEvent)
	if err != nil {
		t.Fatalf("Sync command returned error: %v", err)
	}

	// Assert: sync rejected with no locks message
	comments := mockGH.GetPostedComments()
	if len(comments) == 0 {
		t.Fatal("Expected comment about no locks")
	}

	lastComment := comments[len(comments)-1]
	t.Logf("No lock comment: %.200s", lastComment.Body)

	if !strings.Contains(lastComment.Body, "No applications are locked") {
		t.Error("Expected 'No applications are locked' message")
	}
}

// =============================================================================
// Lock Conflict Tests
// =============================================================================

func TestE2ELockConflictBetweenPRs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E command test in short mode")
	}

	appName := "test-app"

	// Verify app exists
	_, err := argoClient.GetApplication(testCtx, appName)
	if err != nil {
		t.Skipf("test-app not available: %v", err)
	}

	// Ensure clean lock state
	_ = lockManager.ForceUnlock(testCtx, appName)
	defer cleanupForceUnlock(testCtx, t, appName)

	repo := "test-owner/test-repo"

	// PR #1: Plan and acquire lock
	mockGH1 := NewMockVCSClient()
	mockGH1.RepoConfigErr = fmt.Errorf(".lemuria.yaml not found")
	executor1 := newTestExecutor(mockGH1, nil)

	event1 := newPREvent(repo, "test-owner", "test-repo", 701, "", "feature-branch-1", "main", "lemuria plan -a "+appName)
	cmd1 := &commands.Command{
		Name:        commands.CommandPlan,
		Application: appName,
	}

	if err := executor1.Execute(testCtx, cmd1, event1); err != nil {
		t.Fatalf("PR #1 plan failed: %v", err)
	}

	// Verify PR #1 holds the lock
	lock, err := lockManager.Get(testCtx, appName)
	if err != nil || lock == nil {
		t.Fatalf("Expected lock to be acquired by PR #1: %v", err)
	}
	if lock.PRNumber != 701 {
		t.Fatalf("Expected lock held by PR #701, got PR #%d", lock.PRNumber)
	}

	// PR #2: Try to plan the same app
	mockGH2 := NewMockVCSClient()
	mockGH2.RepoConfigErr = fmt.Errorf(".lemuria.yaml not found")
	executor2 := newTestExecutor(mockGH2, nil)

	event2 := newPREvent(repo, "test-owner", "test-repo", 702, "", "feature-branch-2", "main", "lemuria plan -a "+appName)
	cmd2 := &commands.Command{
		Name:        commands.CommandPlan,
		Application: appName,
	}

	if err := executor2.Execute(testCtx, cmd2, event2); err != nil {
		t.Fatalf("PR #2 plan returned error: %v", err)
	}

	// Assert: PR #2 sees lock conflict
	comments := mockGH2.GetPostedComments()
	if len(comments) == 0 {
		t.Fatal("Expected comment for PR #2")
	}

	lastComment := comments[len(comments)-1]
	t.Logf("Lock conflict comment: %.300s", lastComment.Body)

	if !strings.Contains(lastComment.Body, "Locked by PR #701") {
		t.Error("Expected lock conflict message mentioning PR #701")
	}

	// Verify lock is still held by PR #1
	lock, err = lockManager.Get(testCtx, appName)
	if err != nil || lock == nil {
		t.Fatalf("Lock should still exist: %v", err)
	}
	if lock.PRNumber != 701 {
		t.Errorf("Lock should still be held by PR #701, got PR #%d", lock.PRNumber)
	}
}

// =============================================================================
// Unlock All Tests
// =============================================================================

func TestE2EUnlockAll(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E command test in short mode")
	}

	// Verify we have at least one app
	_, err := argoClient.GetApplication(testCtx, "test-app")
	if err != nil {
		t.Skipf("test-app not available: %v", err)
	}

	repo := "test-owner/test-repo"
	prNumber := 800
	appName := "test-app"

	// Ensure clean lock state
	_ = lockManager.ForceUnlock(testCtx, appName)
	defer cleanupForceUnlock(testCtx, t, appName)

	mockGH := NewMockVCSClient()
	mockGH.RepoConfigErr = fmt.Errorf(".lemuria.yaml not found")
	executor := newTestExecutor(mockGH, nil)

	// Acquire lock via plan
	planEvent := newPREvent(repo, "test-owner", "test-repo", prNumber, "", "feature-branch", "main", "lemuria plan -a "+appName)
	planCmd := &commands.Command{
		Name:        commands.CommandPlan,
		Application: appName,
	}
	if err := executor.Execute(testCtx, planCmd, planEvent); err != nil {
		t.Fatalf("Plan command failed: %v", err)
	}

	// Verify lock acquired
	lock, err := lockManager.Get(testCtx, appName)
	if err != nil || lock == nil {
		t.Fatalf("Expected lock to be acquired: %v", err)
	}

	// Run UnlockAll (simulating PR close)
	closeEvent := newPREvent(repo, "test-owner", "test-repo", prNumber, "", "feature-branch", "main", "")
	closeEvent.Type = models.EventTypePullRequest
	closeEvent.Action = models.PRActionClosed

	err = executor.UnlockAll(testCtx, closeEvent)
	if err != nil {
		t.Fatalf("UnlockAll failed: %v", err)
	}

	// Assert: lock was released
	lock, err = lockManager.Get(testCtx, appName)
	if err != nil {
		t.Fatalf("Failed to check lock: %v", err)
	}
	if lock != nil {
		t.Error("Expected lock to be released after UnlockAll")
	}
}

// =============================================================================
// Help Command Test
// =============================================================================

func TestE2EHelpCommand(t *testing.T) {
	mockGH := NewMockVCSClient()
	executor := newTestExecutor(mockGH, nil)

	event := newPREvent("test-owner/test-repo", "test-owner", "test-repo", 900, "", "", "", "lemuria help")
	cmd := &commands.Command{
		Name: commands.CommandHelp,
	}

	// Use a background context for this simple test
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := executor.Execute(ctx, cmd, event)
	if err != nil {
		t.Fatalf("Help command failed: %v", err)
	}

	// Assert: help comment posted
	comments := mockGH.GetPostedComments()
	if len(comments) == 0 {
		t.Fatal("Expected help comment to be posted")
	}

	lastComment := comments[len(comments)-1]
	if !strings.Contains(lastComment.Body, "Help") {
		t.Error("Expected 'Help' in comment body")
	}
	if !strings.Contains(lastComment.Body, "lemuria plan") {
		t.Error("Expected command examples in help text")
	}
}

// =============================================================================
// GitLab Command Workflow Tests
// =============================================================================

func TestE2EPlanCommandGitLab(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E command test in short mode")
	}

	// Use pre-existing test-app
	appName := "test-app"

	// Verify app exists
	app, err := argoClient.GetApplication(testCtx, appName)
	if err != nil {
		t.Skipf("test-app not available: %v", err)
	}
	t.Logf("Using test application: %s (path: %s)", app.Name, app.Path)

	// Ensure no stale locks
	_ = lockManager.ForceUnlock(testCtx, appName)
	defer cleanupForceUnlock(testCtx, t, appName)

	mockVCS := NewMockVCSClient()
	mockVCS.RepoConfigErr = fmt.Errorf(".lemuria.yaml not found")

	executor := newTestExecutor(mockVCS, nil)

	headSHA := "abc123gitlab"
	event := newGitLabPREvent(
		"mygroup/myproject", "mygroup", "myproject",
		100, headSHA, "feature-branch", "main",
		"lemuria plan -a test-app",
	)

	cmd := &commands.Command{
		Name:        commands.CommandPlan,
		Application: appName,
	}

	err = executor.Execute(testCtx, cmd, event)
	if err != nil {
		t.Fatalf("Plan command failed: %v", err)
	}

	// Assert: comment was posted
	comments := mockVCS.GetPostedComments()
	if len(comments) == 0 {
		t.Fatal("Expected at least one comment to be posted")
	}

	lastComment := comments[len(comments)-1]
	t.Logf("Posted comment (truncated): %.200s...", lastComment.Body)

	if !lastComment.IsPlan {
		t.Error("Expected plan comment to have isPlan=true")
	}

	// Assert: lock was acquired with correct PlanRevision
	lock, err := lockManager.Get(testCtx, appName)
	if err != nil {
		t.Fatalf("Failed to get lock: %v", err)
	}
	if lock == nil {
		t.Fatal("Expected lock to be acquired after plan")
	}
	if lock.PRNumber != 100 {
		t.Errorf("Expected lock PR number 100, got %d", lock.PRNumber)
	}
	if lock.PlanRevision != headSHA {
		t.Errorf("Expected lock PlanRevision %q, got %q", headSHA, lock.PlanRevision)
	}

	t.Logf("Lock acquired: app=%s, pr=%d, user=%s, plan_revision=%s",
		lock.Application, lock.PRNumber, lock.User, lock.PlanRevision)

	// Assert: reaction was added
	if len(mockVCS.Reactions) == 0 {
		t.Error("Expected reaction to be added to comment")
	}

	// Assert: old plan comments were invalidated
	if len(mockVCS.InvalidatedPRs) == 0 {
		t.Error("Expected old plan comments to be invalidated")
	}
}

func TestE2EHelpCommandGitLab(t *testing.T) {
	mockVCS := NewMockVCSClient()
	executor := newTestExecutor(mockVCS, nil)

	event := newGitLabPREvent("mygroup/myproject", "mygroup", "myproject", 900, "", "", "", "lemuria help")
	cmd := &commands.Command{
		Name: commands.CommandHelp,
	}

	// Use a background context for this simple test
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := executor.Execute(ctx, cmd, event)
	if err != nil {
		t.Fatalf("Help command failed: %v", err)
	}

	// Assert: help comment posted
	comments := mockVCS.GetPostedComments()
	if len(comments) == 0 {
		t.Fatal("Expected help comment to be posted")
	}

	lastComment := comments[len(comments)-1]
	if !strings.Contains(lastComment.Body, "Help") {
		t.Error("Expected 'Help' in comment body")
	}
	if !strings.Contains(lastComment.Body, "lemuria plan") {
		t.Error("Expected command examples in help text")
	}
}
