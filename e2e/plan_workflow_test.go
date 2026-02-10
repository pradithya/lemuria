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

package e2e

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/org/lemuria/internal/commands"
	"github.com/org/lemuria/internal/config"
	"github.com/org/lemuria/internal/models"
)

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

// TestE2EPlanDetectsModifiedExternalChartApp verifies that when a PR modifies
// an Application CR file where the app sources from an external Helm chart
// (not the PR's git repo), the app is still detected as affected.
func TestE2EPlanDetectsModifiedExternalChartApp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E command test in short mode")
	}

	appName := uniqueAppName("e2e-helm")

	// Create an app with an external Helm chart source (not a git repo)
	createTestHelmChartApplication(testCtx, t, argoClient, appName, "e2e-test-apps")
	defer deleteTestApplication(testCtx, t, argoClient, appName)

	// Wait for app to appear in ArgoCD
	waitForAppReady(testCtx, t, argoClient, appName, 120*time.Second)

	// Ensure clean lock state
	defer cleanupForceUnlock(testCtx, t, appName)

	// Configure mock GitHub to simulate a PR that modifies the Application CR file.
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

	// Run plan in auto-detect mode (no -a flag)
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

	// Assert: lock was acquired
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

// TestE2EPlanRevisionPersistedOnLock verifies that after running plan, the lock
// has the PlanRevision field set so that sync can verify plan freshness.
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
	t.Logf("Get: PlanDiffs count = %d, PlanOutput = %q", len(lock.PlanDiffs), lock.PlanOutput)
	if lock.PlanOutput != "" && lock.PlanOutput != "No changes detected" {
		if len(lock.PlanDiffs) == 0 {
			t.Errorf("Get: expected PlanDiffs to be populated when PlanOutput=%q", lock.PlanOutput)
		}
	}

	// Assert: PlanRevision is also set on locks returned by ListByPR
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

	if len(lock.PlanDiffs) > 0 && !strings.Contains(lastComment.Body, "auto-sync enabled") {
		if !strings.Contains(lastComment.Body, "Plan Diff") {
			t.Errorf("Sync comment should include 'Plan Diff' section when PlanDiffs are stored")
		}
	}
}

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
