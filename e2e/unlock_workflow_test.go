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

	"github.com/org/lemuria/internal/commands"
	"github.com/org/lemuria/internal/models"
)

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
