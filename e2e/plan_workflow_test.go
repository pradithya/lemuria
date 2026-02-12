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
	ts := newTestServer(mockGH, nil)
	defer ts.Close()

	headSHA := "abc123plan"
	payload := githubCommentPayload(
		"test-owner/test-repo", "test-owner", "test-repo",
		100, "lemuria plan -a test-app",
	)
	mockGH.PRHeadRef = "feature-branch"
	mockGH.PRBaseRef = "main"
	mockGH.PRHeadSHA = headSHA
	resp := sendGitHubWebhook(t, ts.URL, "issue_comment", payload)
	assertAccepted(t, resp)

	// Wait for plan comment
	comments := waitForComment(t, mockGH, 1, 60*time.Second)
	lastComment := comments[len(comments)-1]
	t.Logf("Posted comment (truncated): %.200s...", lastComment.Body)

	// Allow time for secondary effects (reaction, invalidation)
	waitForProcessingDone()

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
	if len(mockGH.GetReactions()) == 0 {
		t.Error("Expected reaction to be added to comment")
	}

	// Assert: old plan comments were invalidated
	if len(mockGH.GetInvalidatedPRs()) == 0 {
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
			TempAppTimeout: 15 * time.Second,
		},
		Defaults: config.DefaultsConfig{
			RequireApproval: false,
		},
	}
	ts := newTestServer(mockGH, cfg)
	defer ts.Close()

	// Run plan in auto-detect mode (no -a flag)
	payload := githubCommentPayload(
		"test-owner/test-repo", "test-owner", "test-repo",
		1100, "lemuria plan",
	)
	mockGH.PRHeadRef = "feature-branch"
	mockGH.PRBaseRef = "main"
	mockGH.PRHeadSHA = "abc123"
	resp := sendGitHubWebhook(t, ts.URL, "issue_comment", payload)
	assertAccepted(t, resp)

	// Wait for plan comment. Plan for external helm apps creates temporary
	// ArgoCD apps for diff generation, which can take up to TempAppTimeout (90s).
	comments := waitForComment(t, mockGH, 1, 120*time.Second)
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
	waitForProcessingDone()
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
	ts := newTestServer(mockGH, nil)
	defer ts.Close()

	mockGH.PRHeadRef = "feature-branch"
	mockGH.PRBaseRef = "main"
	mockGH.PRHeadSHA = headSHA

	// Run plan with a specific HeadSHA
	planPayload := githubCommentPayload(repo, "test-owner", "test-repo", prNumber, "lemuria plan -a "+appName)
	assertAccepted(t, sendGitHubWebhook(t, ts.URL, "issue_comment", planPayload))
	waitForComment(t, mockGH, 1, 60*time.Second)
	waitForProcessingDone()

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
	syncPayload := githubCommentPayload(repo, "test-owner", "test-repo", prNumber, "lemuria sync")
	assertAccepted(t, sendGitHubWebhook(t, ts.URL, "issue_comment", syncPayload))

	syncComments := waitForComment(t, mockGH, 1, 60*time.Second)
	lastComment := syncComments[len(syncComments)-1]
	if strings.Contains(lastComment.Body, "stale") {
		t.Errorf("Sync should NOT report stale plan when HeadSHA matches PlanRevision, got: %s", lastComment.Body)
	}

	// Wait for sync update and check plan diff inclusion
	waitForProcessingDone()
	if len(lock.PlanDiffs) > 0 && !strings.Contains(lastComment.Body, "auto-sync enabled") {
		updatedComments := mockGH.GetUpdatedComments()
		if len(updatedComments) > 0 {
			lastUpdate := updatedComments[len(updatedComments)-1]
			if !strings.Contains(lastUpdate.Body, "Plan Diff") {
				t.Errorf("Sync comment should include 'Plan Diff' section when PlanDiffs are stored")
			}
		}
	}
}

// TestE2EPlanNewStandaloneApp verifies that when a PR adds a new standalone
// Application CR, the plan generates actual diffs showing all resources as creates.
func TestE2EPlanNewStandaloneApp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E command test in short mode")
	}
	t.Parallel()

	appName := uniqueAppName("e2e-new-standalone")
	repo := "test-owner/test-repo"
	prNumber := 2500
	headSHA := "abc123newstandalone"
	crFilePath := "apps/" + appName + ".yaml"

	// The app does NOT exist in ArgoCD (it's new).
	// Head branch YAML defines the Application CR.
	headYAML := fmt.Sprintf(`apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: %s
  namespace: argocd
spec:
  project: default
  source:
    repoURL: https://github.com/argoproj/argocd-example-apps.git
    targetRevision: HEAD
    path: guestbook
  destination:
    server: https://kubernetes.default.svc
    namespace: e2e-test-apps`, appName)

	mockGH := NewMockVCSClient()
	mockGH.RepoConfigErr = fmt.Errorf(".lemuria.yaml not found")
	mockGH.ChangedFiles = []models.ChangedFile{
		{Filename: crFilePath, Status: models.FileStatusAdded},
	}
	mockGH.FileContents[crFilePath+"@feature-branch"] = []byte(headYAML)
	mockGH.PRHeadRef = "feature-branch"
	mockGH.PRBaseRef = "main"
	mockGH.PRHeadSHA = headSHA

	cfg := &config.Config{
		ArgoCD: config.ArgoCDConfig{
			DiffMode:       "branch",
			TempAppTimeout: 30 * time.Second,
		},
		Defaults: config.DefaultsConfig{
			Autoplan:        true,
			RequireApproval: false,
		},
	}
	ts := newTestServer(mockGH, cfg)
	defer ts.Close()

	// Use autoplan event: PR opened
	payload := githubPRPayload("opened", repo, "test-owner", "test-repo", prNumber, headSHA, "feature-branch", "main")
	assertAccepted(t, sendGitHubWebhook(t, ts.URL, "pull_request", payload))

	// Wait for plan comment (longer timeout for temp app manifest rendering)
	comments := waitForComment(t, mockGH, 1, 90*time.Second)
	lastComment := comments[len(comments)-1]
	t.Logf("Plan comment:\n%s", lastComment.Body)

	// Assert: the new app is detected
	if !strings.Contains(lastComment.Body, appName) {
		t.Fatalf("Expected plan comment to mention new app %q", appName)
	}

	// Assert: shows "New application" message
	if !strings.Contains(lastComment.Body, "New application") {
		t.Error("Expected plan comment to contain 'New application'")
	}

	// Assert: diffs are generated with resource creates
	if !strings.Contains(lastComment.Body, "to create") {
		t.Error("Expected plan comment to show 'to create' for new app resources")
	}

	// Assert: diff details are shown (collapsible section)
	if !strings.Contains(lastComment.Body, "resources changed") {
		t.Error("Expected plan comment to contain diff details with resource changes")
	}

	// Assert: the "cannot generate a diff" message is NOT shown
	if strings.Contains(lastComment.Body, "cannot generate a diff") {
		t.Error("Expected plan comment NOT to contain 'cannot generate a diff' when diffs are available")
	}

	// Assert: no error in the plan
	if strings.Contains(lastComment.Body, "❌ **Error:**") {
		t.Errorf("Plan comment should not contain errors")
	}
}

// TestE2EPlanDeletedStandaloneApp verifies that when a PR removes a standalone
// Application CR, the plan generates actual diffs showing all resources as deletes.
func TestE2EPlanDeletedStandaloneApp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E command test in short mode")
	}
	t.Parallel()

	appName := uniqueAppName("e2e-del-standalone")
	repo := "test-owner/test-repo"
	prNumber := 2600
	headSHA := "abc123delstandalone"
	crFilePath := "apps/" + appName + ".yaml"

	// Create the app in ArgoCD so it exists
	createTestApplication(testCtx, t, argoClient, appName, "e2e-test-apps")
	defer deleteTestApplication(testCtx, t, argoClient, appName)
	waitForAppReady(testCtx, t, argoClient, appName, 60*time.Second)

	// Base branch has the Application CR (being removed in the PR)
	baseYAML := fmt.Sprintf(`apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: %s
  namespace: argocd
spec:
  project: default
  source:
    repoURL: https://github.com/argoproj/argocd-example-apps.git
    targetRevision: HEAD
    path: guestbook
  destination:
    server: https://kubernetes.default.svc
    namespace: e2e-test-apps`, appName)

	mockGH := NewMockVCSClient()
	mockGH.RepoConfigErr = fmt.Errorf(".lemuria.yaml not found")
	mockGH.ChangedFiles = []models.ChangedFile{
		{Filename: crFilePath, Status: models.FileStatusRemoved},
	}
	mockGH.FileContents[crFilePath+"@main"] = []byte(baseYAML)
	mockGH.PRHeadRef = "feature-branch"
	mockGH.PRBaseRef = "main"
	mockGH.PRHeadSHA = headSHA

	// Use DiffMode="branch" so the diff creates a temp app from base branch
	// manifests and diffs against empty target. This works even without syncing
	// the app first (unlike DiffMode="live" which requires live cluster state).
	cfg := &config.Config{
		ArgoCD: config.ArgoCDConfig{
			DiffMode:       "branch",
			TempAppTimeout: 30 * time.Second,
		},
		Defaults: config.DefaultsConfig{
			Autoplan:        true,
			RequireApproval: false,
		},
	}
	ts := newTestServer(mockGH, cfg)
	defer ts.Close()

	// Use autoplan event: PR opened
	payload := githubPRPayload("opened", repo, "test-owner", "test-repo", prNumber, headSHA, "feature-branch", "main")
	assertAccepted(t, sendGitHubWebhook(t, ts.URL, "pull_request", payload))

	// Wait for plan comment (longer timeout for temp app manifest rendering)
	comments := waitForComment(t, mockGH, 1, 90*time.Second)
	lastComment := comments[len(comments)-1]
	t.Logf("Plan comment:\n%s", lastComment.Body)

	// Assert: the deleted app is detected
	if !strings.Contains(lastComment.Body, appName) {
		t.Fatalf("Expected plan comment to mention deleted app %q", appName)
	}

	// Assert: shows deletion message
	if !strings.Contains(lastComment.Body, "will be deleted") {
		t.Error("Expected plan comment to contain 'will be deleted'")
	}

	// Assert: shows warning about orphaned/pruned resources
	if !strings.Contains(lastComment.Body, "orphaned or pruned") {
		t.Error("Expected plan comment to contain warning about resource fate")
	}

	// Assert: diffs are generated with resource deletes
	if !strings.Contains(lastComment.Body, "to delete") {
		t.Error("Expected plan comment to show 'to delete' for deleted app resources")
	}

	// Assert: diff details are shown
	if !strings.Contains(lastComment.Body, "resources changed") {
		t.Error("Expected plan comment to contain diff details with resource changes")
	}

	// Assert: no error in the plan
	if strings.Contains(lastComment.Body, "❌ **Error:**") {
		t.Errorf("Plan comment should not contain errors")
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
	ts := newTestServer(mockVCS, nil)
	defer ts.Close()

	headSHA := "abc123gitlab"
	payload := gitlabNotePayload(
		"mygroup/myproject", "myproject",
		100, headSHA, "feature-branch", "main",
		"lemuria plan -a test-app",
	)
	resp := sendGitLabWebhook(t, ts.URL, "Note Hook", payload)
	assertAccepted(t, resp)

	// Wait for plan comment
	comments := waitForComment(t, mockVCS, 1, 60*time.Second)
	lastComment := comments[len(comments)-1]
	t.Logf("Posted comment (truncated): %.200s...", lastComment.Body)

	// Allow time for secondary effects
	waitForProcessingDone()

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
	if len(mockVCS.GetReactions()) == 0 {
		t.Error("Expected reaction to be added to comment")
	}

	// Assert: old plan comments were invalidated
	if len(mockVCS.GetInvalidatedPRs()) == 0 {
		t.Error("Expected old plan comments to be invalidated")
	}
}
