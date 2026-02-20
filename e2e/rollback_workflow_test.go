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
)

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
	waitForAppReady(testCtx, t, argoClient, appName, 120*time.Second)

	// Ensure clean lock state
	defer cleanupForceUnlock(testCtx, t, appName)

	mockGH := NewMockVCSClient()
	mockGH.RepoConfigErr = fmt.Errorf(".lemuria.yaml not found")
	mockGH.PRHeadRef = "feature-branch"
	mockGH.PRBaseRef = "main"
	ts := newTestServer(mockGH, nil)
	defer ts.Close()

	// Step 1: Run plan to acquire lock
	planPayload := githubCommentPayload(repo, "test-owner", "test-repo", prNumber, "lemuria plan -a "+appName)
	assertAccepted(t, sendGitHubWebhook(t, ts.URL, "issue_comment", planPayload))
	waitForComment(t, mockGH, 1, 60*time.Second)
	waitForProcessingDone()

	// Step 2: Run rollback
	mockGH.Reset()
	rollbackPayload := githubCommentPayload(repo, "test-owner", "test-repo", prNumber, "lemuria rollback -a "+appName)
	assertAccepted(t, sendGitHubWebhook(t, ts.URL, "issue_comment", rollbackPayload))

	// Wait for rollback comment
	comments := waitForComment(t, mockGH, 1, 60*time.Second)
	lastComment := comments[len(comments)-1]
	t.Logf("Rollback comment: %.300s", lastComment.Body)

	if !strings.Contains(lastComment.Body, "Rollback") {
		t.Error("Expected rollback info in comment body")
	}
}

// TestE2ERollbackLocksRetained verifies that after a rollback, locks persist
// (they are not released by rollback, only by auto-merge or PR close).
func TestE2ERollbackLocksRetained(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E command test in short mode")
	}
	t.Parallel()

	appName := uniqueAppName("e2e-rb-lockretain")
	repo := "test-owner/test-repo"
	prNumber := int(time.Now().UnixNano()%90000) + 50000

	createTestApplication(testCtx, t, argoClient, appName, "e2e-test-apps")
	defer deleteTestApplication(testCtx, t, argoClient, appName)
	waitForAppReady(testCtx, t, argoClient, appName, 120*time.Second)
	syncAndWaitForHealthy(testCtx, t, argoClient, appName, 120*time.Second)

	defer cleanupForceUnlock(testCtx, t, appName)

	mockGH := NewMockVCSClient()
	mockGH.RepoConfigErr = fmt.Errorf(".lemuria.yaml not found")
	mockGH.PRHeadRef = "feature-branch"
	mockGH.PRBaseRef = "main"
	ts := newTestServer(mockGH, nil)
	defer ts.Close()

	// Step 1: Plan to acquire lock
	planPayload := githubCommentPayload(repo, "test-owner", "test-repo", prNumber, "lemuria plan -a "+appName)
	assertAccepted(t, sendGitHubWebhook(t, ts.URL, "issue_comment", planPayload))
	waitForComment(t, mockGH, 1, 60*time.Second)
	waitForProcessingDone()

	// Verify lock acquired
	lock, err := lockManager.Get(testCtx, appName)
	if err != nil || lock == nil {
		t.Fatalf("Expected lock to be acquired: %v", err)
	}

	// Step 2: Rollback
	mockGH.Reset()
	rollbackPayload := githubCommentPayload(repo, "test-owner", "test-repo", prNumber, "lemuria rollback -a "+appName)
	assertAccepted(t, sendGitHubWebhook(t, ts.URL, "issue_comment", rollbackPayload))

	// Wait for rollback comment
	comments := waitForComment(t, mockGH, 1, 120*time.Second)
	lastComment := comments[len(comments)-1]
	t.Logf("Rollback comment: %.300s", lastComment.Body)

	if !strings.Contains(lastComment.Body, "Rollback") {
		t.Error("Expected rollback info in comment body")
	}
	waitForProcessingDone()

	// Assert: lock is retained after rollback
	lock, err = lockManager.Get(testCtx, appName)
	if err != nil {
		t.Fatalf("Failed to check lock after rollback: %v", err)
	}
	if lock == nil {
		t.Error("Expected lock to still be held after rollback")
	}
}
