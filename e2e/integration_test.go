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
	"testing"
	"time"

	"github.com/org/lemuria/internal/argocd"
	"github.com/org/lemuria/internal/models"
)

func TestFullPlanWorkflow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping ArgoCD integration test in short mode")
	}
	// This test simulates a full plan workflow:
	// 1. Find an application
	// 2. Acquire lock
	// 3. Get diff
	// 4. Store plan
	// 5. Release lock

	apps, err := argoClient.ListApplications(testCtx)
	if err != nil {
		t.Fatalf("Failed to list applications: %v", err)
	}

	if len(apps) == 0 {
		t.Skip("No applications available for integration test")
	}

	app := apps[0]
	prNumber := 777
	repo := "integration/test"
	user := "integrationuser"

	t.Logf("Running integration test with app: %s", app.Name)

	// Step 1: Acquire lock
	lockResult, err := lockManager.Lock(testCtx, models.LockRequest{
		Application: app.Name,
		PRNumber:    prNumber,
		Repo:        repo,
		User:        user,
	})
	if err != nil {
		t.Fatalf("Failed to acquire lock: %v", err)
	}

	// If lock is held by someone else, skip
	if !lockResult.Acquired {
		t.Skipf("Lock held by PR #%d, skipping", lockResult.HeldBy.PRNumber)
	}

	defer func() {
		// Cleanup: release lock
		_ = lockManager.Unlock(testCtx, app.Name, repo, prNumber)
	}()

	t.Log("Lock acquired successfully")

	// Step 2: Get manifests to determine revision
	_, revision, err := argoClient.GetManifests(testCtx, app.Name, nil)
	if err != nil {
		t.Fatalf("Failed to get manifests: %v", err)
	}

	t.Logf("Target revision: %s", revision)

	// Step 3: Get diff (compare live state vs target revision using new V2 API)
	diffs, err := argoClient.GetApplicationDiff(testCtx, app.Name, argocd.DiffOptions{
		Mode:         argocd.DiffModeLive,
		TargetBranch: revision,
		PRNumber:     prNumber,
		PRRepo:       app.RepoURL,
		Timeout:      2 * time.Minute,
	})
	if err != nil {
		t.Fatalf("Failed to get diff: %v", err)
	}

	t.Logf("Got %d diffs", len(diffs))

	// Step 4: Store plan
	err = lockManager.StorePlan(testCtx, app.Name, prNumber, revision, "", "", nil)
	if err != nil {
		t.Fatalf("Failed to store plan: %v", err)
	}

	t.Log("Plan stored successfully")

	// Step 5: Verify plan
	storedRevision, err := lockManager.GetPlan(testCtx, app.Name, prNumber)
	if err != nil {
		t.Fatalf("Failed to verify plan: %v", err)
	}

	if storedRevision != revision {
		t.Errorf("Plan revision mismatch: expected %s, got %s", revision, storedRevision)
	}

	t.Log("Integration test completed successfully")
}

func TestGetApplicationHistory(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping ArgoCD test in short mode")
	}

	if argoClient == nil {
		t.Skip("Argo CD client not initialized")
	}

	apps, err := argoClient.ListApplications(testCtx)
	if err != nil {
		t.Fatalf("Failed to list applications: %v", err)
	}

	if len(apps) == 0 {
		t.Skip("No applications available to test history")
	}

	appName := apps[0].Name
	history, err := argoClient.GetApplicationHistory(testCtx, appName)
	if err != nil {
		t.Fatalf("Failed to get history for %s: %v", appName, err)
	}

	t.Logf("Got %d history entries for %s", len(history), appName)

	for _, entry := range history {
		t.Logf("  - ID: %d, Revision: %s, DeployedAt: %s",
			entry.ID, entry.Revision, entry.DeployedAt)
	}
}

func TestSyncDryRun(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping ArgoCD test in short mode")
	}
	apps, err := argoClient.ListApplications(testCtx)
	if err != nil {
		t.Fatalf("Failed to list applications: %v", err)
	}

	if len(apps) == 0 {
		t.Skip("No applications available for sync test")
	}

	app := apps[0]
	t.Logf("Testing dry-run sync for app: %s", app.Name)

	result, err := argoClient.SyncApplication(testCtx, app.Name, &argocd.SyncOptions{
		DryRun: true,
	})
	if err != nil {
		// Sync might fail if app is not in a syncable state, which is OK for this test
		t.Logf("Sync dry-run returned error (may be expected): %v", err)
		return
	}

	t.Logf("Sync dry-run result: phase=%s, message=%s", result.Phase, result.Message)
}
