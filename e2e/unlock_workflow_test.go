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

func TestE2EUnlockCommand(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E command test in short mode")
	}
	t.Parallel()

	appName := uniqueAppName("e2e-unlock")
	repo := "test-owner/test-repo"
	prNumber := 400

	createTestApplication(testCtx, t, argoClient, appName, "")
	defer deleteTestApplication(testCtx, t, argoClient, appName)
	waitForAppReady(testCtx, t, argoClient, appName, 120*time.Second)

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

	// Verify lock acquired
	lock, err := lockManager.Get(testCtx, appName)
	if err != nil || lock == nil {
		t.Fatalf("Expected lock to be acquired: %v", err)
	}

	// Step 2: Run unlock
	mockGH.Reset()
	unlockPayload := githubCommentPayload(repo, "test-owner", "test-repo", prNumber, "lemuria unlock")
	assertAccepted(t, sendGitHubWebhook(t, ts.URL, "issue_comment", unlockPayload))

	// Wait for unlock comment
	comments := waitForComment(t, mockGH, 1, 30*time.Second)
	lastComment := comments[len(comments)-1]
	t.Logf("Unlock comment: %.300s", lastComment.Body)

	if !strings.Contains(lastComment.Body, "Unlock") {
		t.Error("Expected unlock info in comment body")
	}
	if !strings.Contains(lastComment.Body, "Unlocked") {
		t.Error("Expected 'Unlocked' in comment body")
	}

	// Assert: lock was released
	waitForProcessingDone()
	lock, err = lockManager.Get(testCtx, appName)
	if err != nil {
		t.Fatalf("Failed to check lock: %v", err)
	}
	if lock != nil {
		t.Error("Expected lock to be released after unlock")
	}
}

func TestE2ELockConflictBetweenPRs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E command test in short mode")
	}
	t.Parallel()

	appName := uniqueAppName("e2e-conflict")

	createTestApplication(testCtx, t, argoClient, appName, "")
	defer deleteTestApplication(testCtx, t, argoClient, appName)
	waitForAppReady(testCtx, t, argoClient, appName, 120*time.Second)

	defer cleanupForceUnlock(testCtx, t, appName)

	repo := "test-owner/test-repo"

	// PR #1: Plan and acquire lock
	mockGH1 := NewMockVCSClient()
	mockGH1.RepoConfigErr = fmt.Errorf(".lemuria.yaml not found")
	mockGH1.PRHeadRef = "feature-branch-1"
	mockGH1.PRBaseRef = "main"
	ts1 := newTestServer(mockGH1, nil)
	defer ts1.Close()

	planPayload1 := githubCommentPayload(repo, "test-owner", "test-repo", 701, "lemuria plan -a "+appName)
	assertAccepted(t, sendGitHubWebhook(t, ts1.URL, "issue_comment", planPayload1))
	waitForComment(t, mockGH1, 1, 60*time.Second)
	waitForProcessingDone()

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
	mockGH2.PRHeadRef = "feature-branch-2"
	mockGH2.PRBaseRef = "main"
	ts2 := newTestServer(mockGH2, nil)
	defer ts2.Close()

	planPayload2 := githubCommentPayload(repo, "test-owner", "test-repo", 702, "lemuria plan -a "+appName)
	assertAccepted(t, sendGitHubWebhook(t, ts2.URL, "issue_comment", planPayload2))

	// Wait for conflict comment on PR #2
	comments := waitForComment(t, mockGH2, 1, 60*time.Second)
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

func TestE2EUnlockAll(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E command test in short mode")
	}
	t.Parallel()

	appName := uniqueAppName("e2e-unlockall")
	repo := "test-owner/test-repo"
	prNumber := 800

	createTestApplication(testCtx, t, argoClient, appName, "")
	defer deleteTestApplication(testCtx, t, argoClient, appName)
	waitForAppReady(testCtx, t, argoClient, appName, 120*time.Second)

	defer cleanupForceUnlock(testCtx, t, appName)

	mockGH := NewMockVCSClient()
	mockGH.RepoConfigErr = fmt.Errorf(".lemuria.yaml not found")
	mockGH.PRHeadRef = "feature-branch"
	mockGH.PRBaseRef = "main"
	ts := newTestServer(mockGH, nil)
	defer ts.Close()

	// Acquire lock via plan
	planPayload := githubCommentPayload(repo, "test-owner", "test-repo", prNumber, "lemuria plan -a "+appName)
	assertAccepted(t, sendGitHubWebhook(t, ts.URL, "issue_comment", planPayload))
	waitForComment(t, mockGH, 1, 60*time.Second)
	waitForProcessingDone()

	// Verify lock acquired
	lock, err := lockManager.Get(testCtx, appName)
	if err != nil || lock == nil {
		t.Fatalf("Expected lock to be acquired: %v", err)
	}

	// Send PR closed event (pull_request webhook with action=closed)
	closePayload := githubPRPayload("closed", repo, "test-owner", "test-repo", prNumber, "", "feature-branch", "main")
	assertAccepted(t, sendGitHubWebhook(t, ts.URL, "pull_request", closePayload))

	// Wait for lock to be released (UnlockAll doesn't post a comment)
	waitForLockRelease(t, appName, 30*time.Second)
}
