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

	"github.com/org/lemuria/internal/models"
)

func TestLockAcquireRelease(t *testing.T) {
	appName := "test-lock-app-" + time.Now().Format("150405")

	// Acquire lock
	result, err := lockManager.Lock(testCtx, models.LockRequest{
		Application: appName,
		PRNumber:    123,
		Repo:        "test/repo",
		User:        "testuser",
	})
	if err != nil {
		t.Fatalf("Failed to acquire lock: %v", err)
	}

	if !result.Acquired {
		t.Error("Expected lock to be acquired")
	}

	// Verify lock exists
	lock, err := lockManager.Get(testCtx, appName)
	if err != nil {
		t.Fatalf("Failed to get lock: %v", err)
	}
	if lock == nil {
		t.Fatal("Expected lock to exist")
		return
	}

	if lock.PRNumber != 123 {
		t.Errorf("Expected PR number 123, got %d", lock.PRNumber)
	}

	// Release lock
	err = lockManager.Unlock(testCtx, appName, "test/repo", 123)
	if err != nil {
		t.Fatalf("Failed to release lock: %v", err)
	}

	// Verify lock is gone
	lock, err = lockManager.Get(testCtx, appName)
	if err != nil {
		t.Fatalf("Failed to get lock after release: %v", err)
	}

	if lock != nil {
		t.Error("Expected lock to be released")
	}
}

func TestLockConflict(t *testing.T) {
	appName := "test-conflict-app-" + time.Now().Format("150405")

	// Acquire lock with PR 1
	result1, err := lockManager.Lock(testCtx, models.LockRequest{
		Application: appName,
		PRNumber:    1,
		Repo:        "test/repo",
		User:        "user1",
	})
	if err != nil {
		t.Fatalf("Failed to acquire first lock: %v", err)
	}

	if !result1.Acquired {
		t.Error("Expected first lock to be acquired")
	}

	// Try to acquire with PR 2 - should fail
	result2, err := lockManager.Lock(testCtx, models.LockRequest{
		Application: appName,
		PRNumber:    2,
		Repo:        "test/repo",
		User:        "user2",
	})
	if err != nil {
		t.Fatalf("Failed to attempt second lock: %v", err)
	}

	if result2.Acquired {
		t.Error("Expected second lock to be denied")
	}

	if result2.HeldBy == nil {
		t.Error("Expected HeldBy to be populated")
	} else if result2.HeldBy.PRNumber != 1 {
		t.Errorf("Expected HeldBy PR to be 1, got %d", result2.HeldBy.PRNumber)
	}

	// Same PR should be able to refresh lock
	result3, err := lockManager.Lock(testCtx, models.LockRequest{
		Application: appName,
		PRNumber:    1,
		Repo:        "test/repo",
		User:        "user1",
	})
	if err != nil {
		t.Fatalf("Failed to refresh lock: %v", err)
	}

	if !result3.Acquired {
		t.Error("Expected same PR to refresh lock")
	}

	// Cleanup
	_ = lockManager.ForceUnlock(testCtx, appName)
}

func TestListLocksByPR(t *testing.T) {
	prefix := "test-list-" + time.Now().Format("150405")
	repo := "test/repo"
	prNumber := 999

	// Create multiple locks for same PR
	apps := []string{prefix + "-app1", prefix + "-app2", prefix + "-app3"}
	for _, app := range apps {
		_, err := lockManager.Lock(testCtx, models.LockRequest{
			Application: app,
			PRNumber:    prNumber,
			Repo:        repo,
			User:        "testuser",
		})
		if err != nil {
			t.Fatalf("Failed to lock %s: %v", app, err)
		}
	}

	// List locks by PR
	locks, err := lockManager.ListByPR(testCtx, repo, prNumber)
	if err != nil {
		t.Fatalf("Failed to list locks by PR: %v", err)
	}

	if len(locks) != len(apps) {
		t.Errorf("Expected %d locks, got %d", len(apps), len(locks))
	}

	// Cleanup
	for _, app := range apps {
		_ = lockManager.ForceUnlock(testCtx, app)
	}
}

func TestStorePlan(t *testing.T) {
	appName := "test-plan-app-" + time.Now().Format("150405")
	prNumber := 456
	revision := "abc123def456"

	// Store plan
	err := lockManager.StorePlan(testCtx, appName, prNumber, revision, "", "", nil)
	if err != nil {
		t.Fatalf("Failed to store plan: %v", err)
	}

	// Retrieve plan
	storedRevision, err := lockManager.GetPlan(testCtx, appName, prNumber)
	if err != nil {
		t.Fatalf("Failed to get plan: %v", err)
	}

	if storedRevision != revision {
		t.Errorf("Expected revision %s, got %s", revision, storedRevision)
	}
}

func TestStorePlanWithDiffs(t *testing.T) {
	appName := "test-plan-diffs-" + time.Now().Format("150405")
	repo := "test/repo"
	prNumber := 457
	revision := "def456abc789"

	// Acquire lock first (StorePlan updates the lock object)
	_, err := lockManager.Lock(testCtx, models.LockRequest{
		Application: appName,
		PRNumber:    prNumber,
		Repo:        repo,
		User:        "testuser",
	})
	if err != nil {
		t.Fatalf("Failed to acquire lock: %v", err)
	}
	defer func() { _ = lockManager.ForceUnlock(testCtx, appName) }()

	// Store plan with diffs
	diffs := []models.PlanDiffEntry{
		{
			Resource: models.ResourceKey{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       "nginx",
				Namespace:  "default",
			},
			Action: models.DiffActionUpdate,
			Diff:   "- replicas: 1\n+ replicas: 3\n",
		},
		{
			Resource: models.ResourceKey{
				APIVersion: "v1",
				Kind:       "ConfigMap",
				Name:       "app-config",
				Namespace:  "default",
			},
			Action: models.DiffActionCreate,
			Diff:   "+ apiVersion: v1\n+ kind: ConfigMap\n+ metadata:\n+   name: app-config\n",
		},
	}

	err = lockManager.StorePlan(testCtx, appName, prNumber, revision, "apps/nginx.yaml", "1 to create, 1 to update", diffs)
	if err != nil {
		t.Fatalf("Failed to store plan with diffs: %v", err)
	}

	// Retrieve via GetPlan (revision only)
	storedRevision, err := lockManager.GetPlan(testCtx, appName, prNumber)
	if err != nil {
		t.Fatalf("Failed to get plan: %v", err)
	}
	if storedRevision != revision {
		t.Errorf("Expected revision %s, got %s", revision, storedRevision)
	}

	// Retrieve via Get (full lock with diffs)
	lock, err := lockManager.Get(testCtx, appName)
	if err != nil {
		t.Fatalf("Failed to get lock: %v", err)
	}
	if lock == nil {
		t.Fatal("Expected lock to exist")
		return
	}
	if lock.PlanRevision != revision {
		t.Errorf("Expected PlanRevision %q, got %q", revision, lock.PlanRevision)
	}
	if lock.SourceFile != "apps/nginx.yaml" {
		t.Errorf("Expected SourceFile %q, got %q", "apps/nginx.yaml", lock.SourceFile)
	}
	if lock.PlanOutput != "1 to create, 1 to update" {
		t.Errorf("Expected PlanOutput %q, got %q", "1 to create, 1 to update", lock.PlanOutput)
	}

	// Verify PlanDiffs were stored
	if len(lock.PlanDiffs) != 2 {
		t.Fatalf("Expected 2 PlanDiffs, got %d", len(lock.PlanDiffs))
	}

	// Check first diff
	d0 := lock.PlanDiffs[0]
	if d0.Resource.Kind != "Deployment" || d0.Resource.Name != "nginx" {
		t.Errorf("Expected first diff to be Deployment/nginx, got %s/%s", d0.Resource.Kind, d0.Resource.Name)
	}
	if d0.Action != models.DiffActionUpdate {
		t.Errorf("Expected first diff action %q, got %q", models.DiffActionUpdate, d0.Action)
	}
	if d0.Diff == "" {
		t.Error("Expected first diff to have non-empty Diff string")
	}

	// Check second diff
	d1 := lock.PlanDiffs[1]
	if d1.Resource.Kind != "ConfigMap" || d1.Resource.Name != "app-config" {
		t.Errorf("Expected second diff to be ConfigMap/app-config, got %s/%s", d1.Resource.Kind, d1.Resource.Name)
	}
	if d1.Action != models.DiffActionCreate {
		t.Errorf("Expected second diff action %q, got %q", models.DiffActionCreate, d1.Action)
	}

	// Verify diffs are also returned via ListByPR
	locks, err := lockManager.ListByPR(testCtx, repo, prNumber)
	if err != nil {
		t.Fatalf("Failed to list locks by PR: %v", err)
	}
	found := false
	for _, l := range locks {
		if l.Application == appName {
			found = true
			if len(l.PlanDiffs) != 2 {
				t.Errorf("ListByPR: expected 2 PlanDiffs, got %d", len(l.PlanDiffs))
			}
			break
		}
	}
	if !found {
		t.Errorf("Expected to find lock for %s in ListByPR results", appName)
	}

	t.Logf("PlanDiffs stored and retrieved successfully: %d entries", len(lock.PlanDiffs))
}
