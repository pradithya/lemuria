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
