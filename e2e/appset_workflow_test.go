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

func TestE2EPlanCommandWithApplicationSet(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E command test in short mode")
	}
	t.Parallel()

	appSetName := uniqueAppName("e2e-appset")
	repo := "test-owner/test-repo"
	prNumber := 2000
	headSHA := "abc123appset"

	// Create ApplicationSet with 2 envs (dev, staging)
	createTestApplicationSet(testCtx, t, argoClient, appSetName)
	defer deleteTestApplicationSet(testCtx, t, argoClient, appSetName)

	// Wait for generated apps to be ready
	appNames := waitForAppSetAppsReady(testCtx, t, argoClient, appSetName, 2, 90*time.Second)
	for _, name := range appNames {
		defer cleanupForceUnlock(testCtx, t, name)
	}

	mockGH := NewMockVCSClient()
	mockGH.RepoConfigErr = fmt.Errorf(".lemuria.yaml not found")
	mockGH.PRHeadRef = "feature-branch"
	mockGH.PRBaseRef = "main"
	mockGH.PRHeadSHA = headSHA
	ts := newTestServer(mockGH, nil)
	defer ts.Close()

	// Run plan with -a <appset-name>
	payload := githubCommentPayload(repo, "test-owner", "test-repo", prNumber, "lemuria plan -a "+appSetName)
	assertAccepted(t, sendGitHubWebhook(t, ts.URL, "issue_comment", payload))

	// Wait for plan comment
	comments := waitForComment(t, mockGH, 1, 60*time.Second)
	lastComment := comments[len(comments)-1]
	t.Logf("Posted comment (truncated): %.500s...", lastComment.Body)

	// Allow time for secondary effects
	waitForProcessingDone()

	// Assert: plan comment mentions all generated apps
	for _, name := range appNames {
		if !strings.Contains(lastComment.Body, name) {
			t.Errorf("Expected plan comment to mention generated app %q", name)
		}
	}

	// Assert: plan comment groups apps under ApplicationSet header
	if !strings.Contains(lastComment.Body, "ApplicationSet: `"+appSetName+"`") {
		t.Error("Expected plan comment to contain ApplicationSet group header")
	}

	// Assert: plan comment is not an error
	if strings.Contains(lastComment.Body, "❌ **Error:**") {
		t.Errorf("Plan comment should not contain errors, got:\n%s", lastComment.Body)
	}

	// Assert: each generated app has a diff result scoped to its section
	for i, name := range appNames {
		appSection := "### Application: `" + name + "`"
		sectionIdx := strings.Index(lastComment.Body, appSection)
		if sectionIdx == -1 {
			t.Errorf("Expected plan comment to have section header for %q", name)
			continue
		}

		// Scope assertion to this app's section (up to the next section or end of body)
		sectionStart := sectionIdx
		nextSection := len(lastComment.Body)
		if i+1 < len(appNames) {
			nextHeader := "### Application: `" + appNames[i+1] + "`"
			if idx := strings.Index(lastComment.Body[sectionStart+len(appSection):], nextHeader); idx != -1 {
				nextSection = sectionStart + len(appSection) + idx
			}
		}
		section := lastComment.Body[sectionStart:nextSection]

		hasDiff := strings.Contains(section, "resources changed")
		hasNoChanges := strings.Contains(section, "No changes detected")
		hasChangeSummary := strings.Contains(section, "**Changes:**")
		if !hasDiff && !hasNoChanges && !hasChangeSummary {
			t.Errorf("Expected plan for %q to show diff results or 'No changes detected'", name)
		}
	}

	// Assert: plan comment should have lock status for each app
	if !strings.Contains(lastComment.Body, "Locked by this PR") {
		t.Error("Expected plan comment to show 'Locked by this PR' status")
	}

	// Assert: locks acquired for each generated app with PlanRevision set
	for _, name := range appNames {
		lock, err := lockManager.Get(testCtx, name)
		if err != nil {
			t.Fatalf("Failed to get lock for %s: %v", name, err)
		}
		if lock == nil {
			t.Errorf("Expected lock to be acquired for generated app %s", name)
			continue
		}
		if lock.PRNumber != prNumber {
			t.Errorf("Expected lock PR number %d for %s, got %d", prNumber, name, lock.PRNumber)
		}
		if lock.PlanRevision != headSHA {
			t.Errorf("Expected lock PlanRevision %q for %s, got %q", headSHA, name, lock.PlanRevision)
		}

		t.Logf("Lock for %s: plan_revision=%s, plan_output=%q, plan_diffs=%d",
			name, lock.PlanRevision, lock.PlanOutput, len(lock.PlanDiffs))

		// If the plan comment shows resource changes for this app, PlanDiffs should be populated
		if strings.Contains(lastComment.Body, "resources changed") && len(lock.PlanDiffs) > 0 {
			for i, d := range lock.PlanDiffs {
				if d.Resource.Kind == "" {
					t.Errorf("PlanDiffs[%d] for %s: expected non-empty Kind", i, name)
				}
				if d.Action == "" {
					t.Errorf("PlanDiffs[%d] for %s: expected non-empty Action", i, name)
				}
				t.Logf("  PlanDiff[%d]: %s %s/%s", i, d.Action, d.Resource.Kind, d.Resource.Name)
			}
		}
	}

	// Assert: the plan comment is a plan comment (isPlan=true)
	if !lastComment.IsPlan {
		t.Error("Expected plan comment to have isPlan=true")
	}

	// Assert: reaction was added
	if len(mockGH.GetReactions()) == 0 {
		t.Error("Expected reaction to be added to comment")
	}

	// Assert: old plan comments were invalidated
	if len(mockGH.GetInvalidatedPRs()) == 0 {
		t.Error("Expected old plan comments to be invalidated")
	}
}

func TestE2ESyncCommandWithApplicationSet(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E command test in short mode")
	}
	t.Parallel()

	appSetName := uniqueAppName("e2e-appset-sync")
	repo := "test-owner/test-repo"
	prNumber := 2100
	headSHA := "abc123appsetsync"

	// Create ApplicationSet
	createTestApplicationSet(testCtx, t, argoClient, appSetName)
	defer deleteTestApplicationSet(testCtx, t, argoClient, appSetName)

	// Wait for generated apps
	appNames := waitForAppSetAppsReady(testCtx, t, argoClient, appSetName, 2, 90*time.Second)
	for _, name := range appNames {
		defer cleanupForceUnlock(testCtx, t, name)
	}

	mockGH := NewMockVCSClient()
	mockGH.RepoConfigErr = fmt.Errorf(".lemuria.yaml not found")
	mockGH.PRHeadRef = "feature-branch"
	mockGH.PRBaseRef = "main"
	mockGH.PRHeadSHA = headSHA
	ts := newTestServer(mockGH, nil)
	defer ts.Close()

	// Step 1: Run plan to acquire locks for all generated apps
	planPayload := githubCommentPayload(repo, "test-owner", "test-repo", prNumber, "lemuria plan -a "+appSetName)
	assertAccepted(t, sendGitHubWebhook(t, ts.URL, "issue_comment", planPayload))
	waitForComment(t, mockGH, 1, 60*time.Second)
	waitForProcessingDone()

	// Verify locks acquired
	for _, name := range appNames {
		lock, err := lockManager.Get(testCtx, name)
		if err != nil || lock == nil {
			t.Fatalf("Expected lock for %s: %v", name, err)
		}
	}

	// Step 2: Run sync (all at once, no -a flag)
	mockGH.Reset()
	syncPayload := githubCommentPayload(repo, "test-owner", "test-repo", prNumber, "lemuria sync")
	assertAccepted(t, sendGitHubWebhook(t, ts.URL, "issue_comment", syncPayload))

	// Assert: sync comment was posted
	comments := waitForComment(t, mockGH, 1, 60*time.Second)
	lastComment := comments[len(comments)-1]
	t.Logf("Sync comment: %.500s", lastComment.Body)

	// Assert: sync comment mentions all generated apps
	for _, name := range appNames {
		if !strings.Contains(lastComment.Body, name) {
			t.Errorf("Expected sync comment to mention generated app %q", name)
		}
	}

	// Assert: no stale plan error
	if strings.Contains(lastComment.Body, "stale") {
		t.Error("Sync should not report stale plan")
	}
}

func TestE2EPlanAutoDetectApplicationSet(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E command test in short mode")
	}
	t.Parallel()

	appSetName := uniqueAppName("e2e-appset-auto")
	repo := "test-owner/test-repo"
	prNumber := 2200
	headSHA := "abc123appsetauto"

	// Create ApplicationSet
	createTestApplicationSet(testCtx, t, argoClient, appSetName)
	defer deleteTestApplicationSet(testCtx, t, argoClient, appSetName)

	// Wait for generated apps
	appNames := waitForAppSetAppsReady(testCtx, t, argoClient, appSetName, 2, 90*time.Second)
	for _, name := range appNames {
		defer cleanupForceUnlock(testCtx, t, name)
	}

	// Configure mock VCS with .lemuria.yaml containing applicationset mapping
	repoConfigYAML := fmt.Sprintf(`version: 1
applications:
  - applicationset: %s
    paths:
      - "apps/guestbook/**"
`, appSetName)

	mockGH := NewMockVCSClient()
	mockGH.RepoConfigData = []byte(repoConfigYAML)
	mockGH.ChangedFiles = []models.ChangedFile{
		{Filename: "apps/guestbook/values.yaml", Status: models.FileStatusModified},
	}

	mockGH.PRHeadRef = "feature-branch"
	mockGH.PRBaseRef = "main"
	mockGH.PRHeadSHA = headSHA
	ts := newTestServer(mockGH, nil)
	defer ts.Close()

	// Run plan without -a flag (auto-detect mode) via issue_comment webhook
	payload := githubCommentPayload(repo, "test-owner", "test-repo", prNumber, "lemuria plan")
	assertAccepted(t, sendGitHubWebhook(t, ts.URL, "issue_comment", payload))

	// Wait for plan comment
	comments := waitForComment(t, mockGH, 1, 60*time.Second)
	lastComment := comments[len(comments)-1]
	t.Logf("Plan comment (truncated): %.500s...", lastComment.Body)

	// Assert: generated apps were detected and planned
	for _, name := range appNames {
		if !strings.Contains(lastComment.Body, name) {
			t.Errorf("Expected plan comment to mention auto-detected app %q", name)
		}
	}

	// Assert: not "No applications affected"
	if strings.Contains(lastComment.Body, "No applications affected") {
		t.Fatal("Expected ApplicationSet apps to be detected as affected")
	}
}

// =============================================================================
// ApplicationSet CR Change Detection Tests
// =============================================================================

func TestE2EPlanDetectsNewAppsFromAppSetChange(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E command test in short mode")
	}
	t.Parallel()

	appSetName := uniqueAppName("e2e-appset-gen")
	repo := "test-owner/test-repo"
	prNumber := 2300
	headSHA := "abc123appsetgen"

	// Create ApplicationSet with 2 envs (dev, staging)
	createTestApplicationSet(testCtx, t, argoClient, appSetName)
	defer deleteTestApplicationSet(testCtx, t, argoClient, appSetName)

	// Wait for generated apps to be ready
	appNames := waitForAppSetAppsReady(testCtx, t, argoClient, appSetName, 2, 90*time.Second)
	for _, name := range appNames {
		defer cleanupForceUnlock(testCtx, t, name)
	}
	// Also clean up the new app that would be generated
	defer cleanupForceUnlock(testCtx, t, appSetName+"-prod")

	crFilePath := "bootstrap/" + appSetName + ".yaml"

	// Base YAML: list generator with dev, staging
	baseYAML := fmt.Sprintf(`apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
metadata:
  name: %s
  namespace: argocd
spec:
  goTemplate: true
  goTemplateOptions:
    - missingkey=error
  generators:
    - list:
        elements:
          - env: dev
          - env: staging
  template:
    metadata:
      name: %s-{{ .env }}
      namespace: argocd
    spec:
      project: default
      source:
        repoURL: https://github.com/argoproj/argocd-example-apps.git
        targetRevision: HEAD
        path: guestbook
      destination:
        server: https://kubernetes.default.svc
        namespace: e2e-test-apps
`, appSetName, appSetName)

	// Head YAML: list generator with dev, staging, prod (added prod)
	headYAML := fmt.Sprintf(`apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
metadata:
  name: %s
  namespace: argocd
spec:
  goTemplate: true
  goTemplateOptions:
    - missingkey=error
  generators:
    - list:
        elements:
          - env: dev
          - env: staging
          - env: prod
  template:
    metadata:
      name: %s-{{ .env }}
      namespace: argocd
    spec:
      project: default
      source:
        repoURL: https://github.com/argoproj/argocd-example-apps.git
        targetRevision: HEAD
        path: guestbook
      destination:
        server: https://kubernetes.default.svc
        namespace: e2e-test-apps
`, appSetName, appSetName)

	mockGH := NewMockVCSClient()
	mockGH.RepoConfigErr = fmt.Errorf(".lemuria.yaml not found")
	mockGH.ChangedFiles = []models.ChangedFile{
		{Filename: crFilePath, Status: models.FileStatusModified},
	}
	mockGH.FileContents[crFilePath+"@main"] = []byte(baseYAML)
	mockGH.FileContents[crFilePath+"@feature-branch"] = []byte(headYAML)

	// Autoplan requires Autoplan=true in config and a PR opened event
	cfg := &config.Config{
		ArgoCD: config.ArgoCDConfig{
			DiffMode:       "live",
			TempAppTimeout: 15 * time.Second,
		},
		Defaults: config.DefaultsConfig{
			Autoplan:        true,
			RequireApproval: false,
		},
	}
	mockGH.PRHeadRef = "feature-branch"
	mockGH.PRBaseRef = "main"
	mockGH.PRHeadSHA = headSHA
	ts := newTestServer(mockGH, cfg)
	defer ts.Close()

	// Use autoplan event: PR opened (pull_request webhook with action "opened")
	payload := githubPRPayload("opened", repo, "test-owner", "test-repo", prNumber, headSHA, "feature-branch", "main")
	assertAccepted(t, sendGitHubWebhook(t, ts.URL, "pull_request", payload))

	// Wait for plan comment
	comments := waitForComment(t, mockGH, 1, 60*time.Second)
	lastComment := comments[len(comments)-1]
	t.Logf("Plan comment (truncated): %.800s...", lastComment.Body)

	// Assert: new prod app is detected as new generated application
	prodAppName := appSetName + "-prod"
	if !strings.Contains(lastComment.Body, prodAppName) {
		t.Errorf("Expected plan comment to mention new generated app %q", prodAppName)
	}

	// Assert: the message indicates it will be generated by the ApplicationSet
	if !strings.Contains(lastComment.Body, "will be generated by ApplicationSet") {
		t.Error("Expected plan comment to mention that app will be generated by ApplicationSet")
	}

	// Assert: the plan groups the new app under the ApplicationSet
	if !strings.Contains(lastComment.Body, "ApplicationSet:") {
		t.Error("Expected plan comment to group the new app under the ApplicationSet")
	}
}

func TestE2EPlanDetectsDeletedAppsFromAppSetChange(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E command test in short mode")
	}
	t.Parallel()

	appSetName := uniqueAppName("e2e-appset-del")
	repo := "test-owner/test-repo"
	prNumber := 2400
	headSHA := "abc123appsetdel"

	// Create ApplicationSet with 2 envs (dev, staging)
	createTestApplicationSet(testCtx, t, argoClient, appSetName)
	defer deleteTestApplicationSet(testCtx, t, argoClient, appSetName)

	// Wait for generated apps to be ready
	appNames := waitForAppSetAppsReady(testCtx, t, argoClient, appSetName, 2, 90*time.Second)
	for _, name := range appNames {
		defer cleanupForceUnlock(testCtx, t, name)
	}

	crFilePath := "bootstrap/" + appSetName + ".yaml"

	// Base YAML: list generator with dev, staging
	baseYAML := fmt.Sprintf(`apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
metadata:
  name: %s
  namespace: argocd
spec:
  goTemplate: true
  goTemplateOptions:
    - missingkey=error
  generators:
    - list:
        elements:
          - env: dev
          - env: staging
  template:
    metadata:
      name: %s-{{ .env }}
      namespace: argocd
    spec:
      project: default
      source:
        repoURL: https://github.com/argoproj/argocd-example-apps.git
        targetRevision: HEAD
        path: guestbook
      destination:
        server: https://kubernetes.default.svc
        namespace: e2e-test-apps
`, appSetName, appSetName)

	// Head YAML: list generator with only dev (removed staging)
	headYAML := fmt.Sprintf(`apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
metadata:
  name: %s
  namespace: argocd
spec:
  goTemplate: true
  goTemplateOptions:
    - missingkey=error
  generators:
    - list:
        elements:
          - env: dev
  template:
    metadata:
      name: %s-{{ .env }}
      namespace: argocd
    spec:
      project: default
      source:
        repoURL: https://github.com/argoproj/argocd-example-apps.git
        targetRevision: HEAD
        path: guestbook
      destination:
        server: https://kubernetes.default.svc
        namespace: e2e-test-apps
`, appSetName, appSetName)

	mockGH := NewMockVCSClient()
	mockGH.RepoConfigErr = fmt.Errorf(".lemuria.yaml not found")
	mockGH.ChangedFiles = []models.ChangedFile{
		{Filename: crFilePath, Status: models.FileStatusModified},
	}
	mockGH.FileContents[crFilePath+"@main"] = []byte(baseYAML)
	mockGH.FileContents[crFilePath+"@feature-branch"] = []byte(headYAML)

	// Autoplan requires Autoplan=true in config and a PR opened event
	cfg := &config.Config{
		ArgoCD: config.ArgoCDConfig{
			DiffMode:       "live",
			TempAppTimeout: 15 * time.Second,
		},
		Defaults: config.DefaultsConfig{
			Autoplan:        true,
			RequireApproval: false,
		},
	}
	mockGH.PRHeadRef = "feature-branch"
	mockGH.PRBaseRef = "main"
	mockGH.PRHeadSHA = headSHA
	ts := newTestServer(mockGH, cfg)
	defer ts.Close()

	// Use autoplan event: PR opened (pull_request webhook with action "opened")
	payload := githubPRPayload("opened", repo, "test-owner", "test-repo", prNumber, headSHA, "feature-branch", "main")
	assertAccepted(t, sendGitHubWebhook(t, ts.URL, "pull_request", payload))

	// Wait for plan comment
	comments := waitForComment(t, mockGH, 1, 60*time.Second)
	lastComment := comments[len(comments)-1]
	t.Logf("Plan comment (truncated): %.800s...", lastComment.Body)

	// Assert: staging app is detected as to-be-removed
	stagingAppName := appSetName + "-staging"
	if !strings.Contains(lastComment.Body, stagingAppName) {
		t.Errorf("Expected plan comment to mention removed app %q", stagingAppName)
	}

	// Assert: the message indicates the app will be removed by ApplicationSet
	if !strings.Contains(lastComment.Body, "generator no longer produces this app") {
		t.Error("Expected plan comment to mention that app will be removed by ApplicationSet generator change")
	}
}
