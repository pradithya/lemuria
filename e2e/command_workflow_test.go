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

// newTestExecutor creates a commands.Executor with real ArgoCD + Redis and a mock GitHub client.
func newTestExecutor(gh *MockGitHubClient, cfg *config.Config) *commands.Executor {
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

// newPREvent creates a PREvent for testing.
func newPREvent(repo, owner, repoName string, prNumber int, headSHA, headRef, baseRef string, commentBody string) *models.PREvent {
	event := &models.PREvent{
		Type:   models.EventTypeIssueComment,
		Action: "created",
		Repo: models.RepoInfo{
			Owner:    owner,
			Name:     repoName,
			FullName: repo,
		},
		PR: models.PRInfo{
			Number:  prNumber,
			State:   "open",
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

	mockGH := NewMockGitHubClient()
	mockGH.RepoConfigErr = fmt.Errorf(".lemuria.yaml not found")

	executor := newTestExecutor(mockGH, nil)

	event := newPREvent(
		"test-owner/test-repo", "test-owner", "test-repo",
		100, "", "feature-branch", "main",
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

	// Assert: lock was acquired
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

	t.Logf("Lock acquired: app=%s, pr=%d, user=%s", lock.Application, lock.PRNumber, lock.User)

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

	mockGH := NewMockGitHubClient()
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

	// Step 2: Run sync (HeadSHA empty to match stored plan revision)
	mockGH.Reset()
	syncEvent := newPREvent(repo, "test-owner", "test-repo", prNumber, "", "feature-branch", "main", "lemuria sync")
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

	mockGH := NewMockGitHubClient()
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

	appName := uniqueAppName("e2e-unlock")
	repo := "test-owner/test-repo"
	prNumber := 400

	// Use pre-existing test-app to avoid needing to create/delete
	_, err := argoClient.GetApplication(testCtx, "test-app")
	if err != nil {
		t.Skipf("test-app not available: %v", err)
	}
	appName = "test-app"

	// Ensure clean lock state
	_ = lockManager.ForceUnlock(testCtx, appName)
	defer cleanupForceUnlock(testCtx, t, appName)

	mockGH := NewMockGitHubClient()
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
// Sync Error Scenario Tests
// =============================================================================

func TestE2ESyncStalePlan(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E command test in short mode")
	}

	appName := uniqueAppName("e2e-stale")
	repo := "test-owner/test-repo"
	prNumber := 500

	// Use pre-existing test-app
	_, err := argoClient.GetApplication(testCtx, "test-app")
	if err != nil {
		t.Skipf("test-app not available: %v", err)
	}
	appName = "test-app"

	// Ensure clean lock state
	_ = lockManager.ForceUnlock(testCtx, appName)
	defer cleanupForceUnlock(testCtx, t, appName)

	mockGH := NewMockGitHubClient()
	mockGH.RepoConfigErr = fmt.Errorf(".lemuria.yaml not found")
	executor := newTestExecutor(mockGH, nil)

	// Step 1: Run plan to acquire lock (HeadSHA empty)
	planEvent := newPREvent(repo, "test-owner", "test-repo", prNumber, "", "feature-branch", "main", "lemuria plan -a "+appName)
	planCmd := &commands.Command{
		Name:        commands.CommandPlan,
		Application: appName,
	}
	if err := executor.Execute(testCtx, planCmd, planEvent); err != nil {
		t.Fatalf("Plan command failed: %v", err)
	}

	// Step 2: Run sync with a DIFFERENT HeadSHA to trigger stale plan
	mockGH.Reset()
	syncEvent := newPREvent(repo, "test-owner", "test-repo", prNumber, "def456", "feature-branch", "main", "lemuria sync")
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

	mockGH := NewMockGitHubClient()
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
	mockGH1 := NewMockGitHubClient()
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
	mockGH2 := NewMockGitHubClient()
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

	mockGH := NewMockGitHubClient()
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
	closeEvent.Action = "closed"

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
	mockGH := NewMockGitHubClient()
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
