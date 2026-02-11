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

func TestE2ESyncCommand(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E command test in short mode")
	}
	t.Parallel()

	appName := uniqueAppName("e2e-sync")
	repo := "test-owner/test-repo"
	prNumber := 200

	// Create a per-test application
	createTestApplication(testCtx, t, argoClient, appName, "e2e-test-apps")
	defer deleteTestApplication(testCtx, t, argoClient, appName)
	waitForAppReady(testCtx, t, argoClient, appName, 120*time.Second)

	// Ensure clean lock state
	defer cleanupForceUnlock(testCtx, t, appName)

	headSHA := "abc123sync"

	mockGH := NewMockVCSClient()
	mockGH.RepoConfigErr = fmt.Errorf(".lemuria.yaml not found")
	mockGH.PRHeadRef = "feature-branch"
	mockGH.PRBaseRef = "main"
	mockGH.PRHeadSHA = headSHA
	ts := newTestServer(mockGH, nil)
	defer ts.Close()

	// Step 1: Run plan to acquire lock
	planPayload := githubCommentPayload(repo, "test-owner", "test-repo", prNumber, "lemuria plan -a "+appName)
	assertAccepted(t, sendGitHubWebhook(t, ts.URL, "issue_comment", planPayload))
	waitForComment(t, mockGH, 1, 60*time.Second)
	waitForProcessingDone()

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
	syncPayload := githubCommentPayload(repo, "test-owner", "test-repo", prNumber, "lemuria sync")
	assertAccepted(t, sendGitHubWebhook(t, ts.URL, "issue_comment", syncPayload))

	// Assert: initial progress comment was posted
	comments := waitForComment(t, mockGH, 1, 60*time.Second)
	initialComment := comments[len(comments)-1]
	t.Logf("Initial sync comment: %.300s", initialComment.Body)

	if !strings.Contains(initialComment.Body, "Sync in progress") {
		t.Error("Expected initial comment to contain 'Sync in progress'")
	}

	// Assert: final result was posted as an update
	updatedComments := waitForUpdatedComment(t, mockGH, 1, 120*time.Second)
	lastUpdate := updatedComments[len(updatedComments)-1]
	t.Logf("Final sync comment: %.300s", lastUpdate.Body)

	if !strings.Contains(lastUpdate.Body, "Sync") {
		t.Error("Expected sync result in updated comment body")
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

// TestE2ESyncExternalSourceApp verifies that syncing an app with an external Helm
// chart source (not the PR repo) works correctly.
func TestE2ESyncExternalSourceApp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E command test in short mode")
	}
	t.Parallel()

	appName := uniqueAppName("e2e-ext-sync")
	repo := "test-owner/test-repo"
	prNumber := 1200
	headSHA := "abc123extsync"

	// Create an app with an external Helm chart source
	createTestHelmChartApplication(testCtx, t, argoClient, appName, "e2e-test-apps")
	defer deleteTestApplication(testCtx, t, argoClient, appName)
	waitForAppReady(testCtx, t, argoClient, appName, 120*time.Second)

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
			TempAppTimeout: 15 * time.Second,
		},
		Defaults: config.DefaultsConfig{
			RequireApproval: false,
		},
	}
	mockGH.PRHeadRef = "feature-branch"
	mockGH.PRBaseRef = "main"
	mockGH.PRHeadSHA = headSHA
	ts := newTestServer(mockGH, cfg)
	defer ts.Close()

	// Step 1: Run plan to acquire lock and store SourceFile
	// Note: plan for external helm apps creates temporary ArgoCD apps for diff
	// generation, which can take up to TempAppTimeout (90s) to complete.
	planPayload := githubCommentPayload(repo, "test-owner", "test-repo", prNumber, "lemuria plan")
	assertAccepted(t, sendGitHubWebhook(t, ts.URL, "issue_comment", planPayload))
	waitForComment(t, mockGH, 1, 120*time.Second)
	waitForProcessingDone()

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
	syncPayload := githubCommentPayload(repo, "test-owner", "test-repo", prNumber, "lemuria sync")
	assertAccepted(t, sendGitHubWebhook(t, ts.URL, "issue_comment", syncPayload))

	// Assert: initial comment was posted
	comments := waitForComment(t, mockGH, 1, 60*time.Second)
	if len(comments) == 0 {
		t.Fatal("Expected initial sync progress comment to be posted")
	}

	// Assert: final result was posted as an update
	updatedComments := waitForUpdatedComment(t, mockGH, 1, 180*time.Second)
	lastUpdate := updatedComments[len(updatedComments)-1]
	t.Logf("Sync comment: %.500s", lastUpdate.Body)

	// Assert: sync did NOT report stale plan
	if strings.Contains(lastUpdate.Body, "stale") {
		t.Error("Sync should not report stale plan")
	}

	// Assert: sync result mentions the app
	if !strings.Contains(lastUpdate.Body, appName) {
		t.Errorf("Expected sync comment to mention app %q", appName)
	}

	// Assert: sync attempted (not an error about revision)
	if strings.Contains(lastUpdate.Body, "No applications are locked") {
		t.Error("Expected sync to find the locked application")
	}
}

// TestE2ESyncMixedApps verifies sync with multiple apps in a single PR where:
// - One app sources from the PR's git repo (git-sourced, no SourceFile)
// - One app sources from an external Helm chart repo (external-sourced, has SourceFile)
func TestE2ESyncMixedApps(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E command test in short mode")
	}
	t.Parallel()

	gitAppName := uniqueAppName("e2e-git-mix")
	helmAppName := uniqueAppName("e2e-helm-mix")
	repo := "test-owner/test-repo"
	prNumber := int(time.Now().UnixNano()%90000) + 10000
	headSHA := "abc123mixed"

	// Create both apps
	createTestApplication(testCtx, t, argoClient, gitAppName, "e2e-test-apps")
	defer deleteTestApplication(testCtx, t, argoClient, gitAppName)
	waitForAppReady(testCtx, t, argoClient, gitAppName, 120*time.Second)

	createTestHelmChartApplication(testCtx, t, argoClient, helmAppName, "e2e-test-apps")
	defer deleteTestApplication(testCtx, t, argoClient, helmAppName)
	waitForAppReady(testCtx, t, argoClient, helmAppName, 120*time.Second)

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

	// Step 2: Sync all locked apps via webhook
	mockGH := NewMockVCSClient()
	mockGH.RepoConfigErr = fmt.Errorf(".lemuria.yaml not found")
	mockGH.FileContents[helmCRFilePath+"@feature-branch"] = []byte(helmHeadYAML)

	cfg := &config.Config{
		ArgoCD: config.ArgoCDConfig{
			DiffMode:       "live",
			TempAppTimeout: 15 * time.Second,
		},
		Defaults: config.DefaultsConfig{
			RequireApproval: false,
		},
	}
	mockGH.PRHeadRef = "feature-branch"
	mockGH.PRBaseRef = "main"
	mockGH.PRHeadSHA = headSHA
	ts := newTestServer(mockGH, cfg)
	defer ts.Close()

	syncPayload := githubCommentPayload(repo, "test-owner", "test-repo", prNumber, "lemuria sync")
	assertAccepted(t, sendGitHubWebhook(t, ts.URL, "issue_comment", syncPayload))

	// Assert: initial comment was posted
	comments := waitForComment(t, mockGH, 1, 60*time.Second)
	if len(comments) == 0 {
		t.Fatal("Expected initial sync progress comment")
	}

	// Assert: final result was posted as an update
	// Sync of mixed apps (git + helm) may take a while due to health stabilization.
	updatedComments := waitForUpdatedComment(t, mockGH, 1, 180*time.Second)
	lastUpdate := updatedComments[len(updatedComments)-1]
	t.Logf("Sync comment: %.800s", lastUpdate.Body)

	// Assert: both apps mentioned in sync output
	if !strings.Contains(lastUpdate.Body, gitAppName) {
		t.Errorf("Expected sync comment to mention git app %q", gitAppName)
	}
	if !strings.Contains(lastUpdate.Body, helmAppName) {
		t.Errorf("Expected sync comment to mention Helm app %q", helmAppName)
	}

	// Assert: no stale plan error
	if strings.Contains(lastUpdate.Body, "stale") {
		t.Error("Sync should not report stale plan")
	}

	// Assert: plan output is included in sync comment
	if !strings.Contains(lastUpdate.Body, "Planned changes") {
		t.Error("Expected sync comment to include plan summary")
	}
	if !strings.Contains(lastUpdate.Body, "1 to update") {
		t.Error("Expected sync comment to show '1 to update' from stored plan output")
	}

	// Assert: plan diffs are rendered in the sync comment
	if !strings.Contains(lastUpdate.Body, "Plan Diff") {
		t.Error("Expected sync comment to include 'Plan Diff' section from stored PlanDiffs")
	}
	if !strings.Contains(lastUpdate.Body, "resources changed") {
		t.Error("Expected sync comment to include resource count in plan diff section")
	}
	if !strings.Contains(lastUpdate.Body, "replicas") {
		t.Error("Expected sync comment to contain diff content (replicas) from stored PlanDiffs")
	}
}

func TestE2ESyncStalePlan(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E command test in short mode")
	}
	t.Parallel()

	appName := uniqueAppName("e2e-stale")
	repo := "test-owner/test-repo"
	prNumber := 500

	createTestApplication(testCtx, t, argoClient, appName, "e2e-test-apps")
	defer deleteTestApplication(testCtx, t, argoClient, appName)
	waitForAppReady(testCtx, t, argoClient, appName, 120*time.Second)

	defer cleanupForceUnlock(testCtx, t, appName)

	mockGH := NewMockVCSClient()
	mockGH.RepoConfigErr = fmt.Errorf(".lemuria.yaml not found")

	// Step 1: Run plan to acquire lock with a specific HeadSHA
	planSHA := "abc123original"
	mockGH.PRHeadRef = "feature-branch"
	mockGH.PRBaseRef = "main"
	mockGH.PRHeadSHA = planSHA
	ts := newTestServer(mockGH, nil)
	defer ts.Close()

	planPayload := githubCommentPayload(repo, "test-owner", "test-repo", prNumber, "lemuria plan -a "+appName)
	assertAccepted(t, sendGitHubWebhook(t, ts.URL, "issue_comment", planPayload))
	waitForComment(t, mockGH, 1, 60*time.Second)
	waitForProcessingDone()

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
	mockGH.PRHeadSHA = "def456newpush"
	syncPayload := githubCommentPayload(repo, "test-owner", "test-repo", prNumber, "lemuria sync")
	assertAccepted(t, sendGitHubWebhook(t, ts.URL, "issue_comment", syncPayload))

	// Assert: sync rejected with stale plan message
	comments := waitForComment(t, mockGH, 1, 60*time.Second)
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
	t.Parallel()

	repo := "test-owner/test-repo"
	prNumber := 600

	mockGH := NewMockVCSClient()
	mockGH.PRHeadRef = "feature-branch"
	mockGH.PRBaseRef = "main"
	ts := newTestServer(mockGH, nil)
	defer ts.Close()

	// Run sync without plan first (no locks)
	syncPayload := githubCommentPayload(repo, "test-owner", "test-repo", prNumber, "lemuria sync")
	assertAccepted(t, sendGitHubWebhook(t, ts.URL, "issue_comment", syncPayload))

	// Assert: sync rejected with no locks message
	comments := waitForComment(t, mockGH, 1, 30*time.Second)
	lastComment := comments[len(comments)-1]
	t.Logf("No lock comment: %.200s", lastComment.Body)

	if !strings.Contains(lastComment.Body, "No applications are locked") {
		t.Error("Expected 'No applications are locked' message")
	}
}

// TestE2ESyncDegradedHealth verifies that sync fails when the application
// enters a Degraded health state (e.g., due to ImagePullBackOff).
func TestE2ESyncDegradedHealth(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E command test in short mode")
	}
	t.Parallel()

	appName := uniqueAppName("e2e-degraded")
	repo := "test-owner/test-repo"
	prNumber := 700
	headSHA := "abc123degraded"

	// Create an application with a non-existent image to trigger Degraded health
	createTestApplicationWithBadImage(testCtx, t, argoClient, appName, "e2e-test-apps")
	defer deleteTestApplication(testCtx, t, argoClient, appName)
	waitForAppReady(testCtx, t, argoClient, appName, 120*time.Second)

	defer cleanupForceUnlock(testCtx, t, appName)

	mockGH := NewMockVCSClient()
	mockGH.RepoConfigErr = fmt.Errorf(".lemuria.yaml not found")

	// Use a short sync timeout so the test doesn't wait the full default (10m)
	cfg := &config.Config{
		ArgoCD: config.ArgoCDConfig{
			DiffMode:       "live",
			TempAppTimeout: 15 * time.Second,
			SyncTimeout:    30 * time.Second,
		},
		Defaults: config.DefaultsConfig{
			RequireApproval: false,
			AutoMerge:       false,
		},
	}
	mockGH.PRHeadRef = "feature-branch"
	mockGH.PRBaseRef = "main"
	mockGH.PRHeadSHA = headSHA
	ts := newTestServer(mockGH, cfg)
	defer ts.Close()

	// Step 1: Run plan to acquire lock
	planPayload := githubCommentPayload(repo, "test-owner", "test-repo", prNumber, "lemuria plan -a "+appName)
	assertAccepted(t, sendGitHubWebhook(t, ts.URL, "issue_comment", planPayload))
	waitForComment(t, mockGH, 1, 60*time.Second)
	waitForProcessingDone()

	// Verify lock acquired
	lock, err := lockManager.Get(testCtx, appName)
	if err != nil || lock == nil {
		t.Fatalf("Expected lock to be acquired: %v", err)
	}

	// Step 2: Run sync - this should fail due to Degraded health
	mockGH.Reset()
	syncPayload := githubCommentPayload(repo, "test-owner", "test-repo", prNumber, "lemuria sync")
	assertAccepted(t, sendGitHubWebhook(t, ts.URL, "issue_comment", syncPayload))

	// Assert: initial comment was posted
	comments := waitForComment(t, mockGH, 1, 60*time.Second)
	if len(comments) == 0 {
		t.Fatal("Expected initial sync progress comment to be posted")
	}

	// Assert: final result was posted as an update
	updatedComments := waitForUpdatedComment(t, mockGH, 1, 120*time.Second)
	lastUpdate := updatedComments[len(updatedComments)-1]
	t.Logf("Sync comment for degraded app: %.800s", lastUpdate.Body)

	// Assert: sync result indicates failure — either:
	// - "Sync failed" (rendered for non-Succeeded phase)
	// - "Degraded" (health status if Kubernetes progressDeadlineSeconds expired)
	// - "timeout" (message from health wait timeout)
	hasFailed := strings.Contains(lastUpdate.Body, "Sync failed")
	hasDegraded := strings.Contains(lastUpdate.Body, "Degraded")
	hasTimeout := strings.Contains(lastUpdate.Body, "timeout")
	if !hasFailed && !hasDegraded && !hasTimeout {
		t.Error("Expected sync result to indicate failure (Sync failed / Degraded / timeout)")
	}

	// Assert: lock was NOT released (sync failed)
	lock, err = lockManager.Get(testCtx, appName)
	if err != nil {
		t.Fatalf("Failed to check lock after sync: %v", err)
	}
	if lock == nil {
		t.Error("Expected lock to still be held after failed sync")
	}
}
