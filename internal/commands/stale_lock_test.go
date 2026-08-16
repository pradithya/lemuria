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

package commands

import (
	"context"
	"errors"
	"testing"

	"github.com/org/lemuria/internal/config"
	"github.com/org/lemuria/internal/models"
	"github.com/org/lemuria/pkg/diff"
)

// reclaimLock is a lock manager that models a real Redis lock: the first
// acquisition attempt conflicts with an existing holder, and the conflict
// clears once the lock is force-unlocked.
type reclaimLock struct {
	mockLock
	heldBy        *models.Lock
	lockCalls     int
	forceUnlockOK bool
}

func (m *reclaimLock) Lock(_ context.Context, _ models.LockRequest) (*models.LockResult, error) {
	m.lockCalls++
	if m.heldBy != nil {
		return &models.LockResult{Acquired: false, HeldBy: m.heldBy}, nil
	}
	return &models.LockResult{Acquired: true}, nil
}

func (m *reclaimLock) ForceUnlock(_ context.Context, application string) error {
	m.forceUnlocked = append(m.forceUnlocked, application)
	if m.forceUnlockErr != nil {
		return m.forceUnlockErr
	}
	m.forceUnlockOK = true
	m.heldBy = nil
	return nil
}

func staleLockExecutor(vcsMock *mockVCS, lockMock *reclaimLock) *Executor {
	return &Executor{
		vcs:      vcsMock,
		lock:     lockMock,
		config:   &config.Config{},
		renderer: diff.NewRenderer(),
	}
}

func heldLock() *models.Lock {
	return &models.Lock{
		Application: "app-a",
		PRNumber:    45,
		Repo:        "org/repo",
		User:        "other-user",
	}
}

// A lock whose holding PR was merged is orphaned — the `closed` webhook that
// should have released it never arrived. It must be reclaimed so the new PR
// can plan, rather than blocking until the 7-day TTL.
func TestLockAndStorePlan_ReclaimsLockFromMergedPR(t *testing.T) {
	lockMock := &reclaimLock{heldBy: heldLock()}
	vcsMock := &mockVCS{
		pr: &models.PullRequestDetail{
			Number: 45,
			State:  models.PRStateClosed,
			Merged: true,
		},
	}
	exec := staleLockExecutor(vcsMock, lockMock)

	result := &appPlanResult{}
	app := models.Application{Name: "app-a"}

	err := exec.lockAndStorePlan(context.Background(), result, app, defaultEvent(), models.ApplicationExisting)
	if err != nil {
		t.Fatalf("lockAndStorePlan returned error: %v", err)
	}
	if result.LockStatus != "Locked by this PR" {
		t.Errorf("LockStatus = %q, want 'Locked by this PR'", result.LockStatus)
	}
	if len(lockMock.forceUnlocked) != 1 || lockMock.forceUnlocked[0] != "app-a" {
		t.Errorf("forceUnlocked = %v, want [app-a]", lockMock.forceUnlocked)
	}
	if lockMock.lockCalls != 2 {
		t.Errorf("lockCalls = %d, want 2 (initial attempt + retry)", lockMock.lockCalls)
	}
}

// Same reclaim path for a PR closed without merging.
func TestLockAndStorePlan_ReclaimsLockFromClosedUnmergedPR(t *testing.T) {
	lockMock := &reclaimLock{heldBy: heldLock()}
	vcsMock := &mockVCS{
		pr: &models.PullRequestDetail{
			Number: 45,
			State:  models.PRStateClosed,
			Merged: false,
		},
	}
	exec := staleLockExecutor(vcsMock, lockMock)

	result := &appPlanResult{}
	err := exec.lockAndStorePlan(context.Background(), result, models.Application{Name: "app-a"}, defaultEvent(), models.ApplicationExisting)
	if err != nil {
		t.Fatalf("lockAndStorePlan returned error: %v", err)
	}
	if result.LockStatus != "Locked by this PR" {
		t.Errorf("LockStatus = %q, want 'Locked by this PR'", result.LockStatus)
	}
}

// A lock held by a PR that is still open is legitimate and must be respected.
func TestLockAndStorePlan_KeepsLockFromOpenPR(t *testing.T) {
	lockMock := &reclaimLock{heldBy: heldLock()}
	vcsMock := &mockVCS{
		pr: &models.PullRequestDetail{
			Number: 45,
			State:  models.PRStateOpen,
		},
	}
	exec := staleLockExecutor(vcsMock, lockMock)

	result := &appPlanResult{}
	err := exec.lockAndStorePlan(context.Background(), result, models.Application{Name: "app-a"}, defaultEvent(), models.ApplicationExisting)
	if err == nil {
		t.Fatal("expected a lock conflict error, got nil")
	}
	if result.LockStatus != "Locked by PR #45 (other-user)" {
		t.Errorf("LockStatus = %q, want 'Locked by PR #45 (other-user)'", result.LockStatus)
	}
	if len(lockMock.forceUnlocked) != 0 {
		t.Errorf("forceUnlocked = %v, want none — an open PR's lock must not be stolen", lockMock.forceUnlocked)
	}
}

// If the PR state cannot be determined, fail safe: keep the lock.
func TestLockAndStorePlan_KeepsLockWhenPRLookupFails(t *testing.T) {
	lockMock := &reclaimLock{heldBy: heldLock()}
	vcsMock := &mockVCS{prErr: errors.New("api unavailable")}
	exec := staleLockExecutor(vcsMock, lockMock)

	result := &appPlanResult{}
	err := exec.lockAndStorePlan(context.Background(), result, models.Application{Name: "app-a"}, defaultEvent(), models.ApplicationExisting)
	if err == nil {
		t.Fatal("expected a lock conflict error, got nil")
	}
	if len(lockMock.forceUnlocked) != 0 {
		t.Errorf("forceUnlocked = %v, want none on lookup failure", lockMock.forceUnlocked)
	}
}

// If releasing the stale lock fails, report the conflict rather than
// pretending the lock was acquired.
func TestLockAndStorePlan_ForceUnlockFailureKeepsConflict(t *testing.T) {
	lockMock := &reclaimLock{heldBy: heldLock()}
	lockMock.forceUnlockErr = errors.New("redis down")
	vcsMock := &mockVCS{
		pr: &models.PullRequestDetail{Number: 45, State: models.PRStateClosed, Merged: true},
	}
	exec := staleLockExecutor(vcsMock, lockMock)

	result := &appPlanResult{}
	err := exec.lockAndStorePlan(context.Background(), result, models.Application{Name: "app-a"}, defaultEvent(), models.ApplicationExisting)
	if err == nil {
		t.Fatal("expected a lock conflict error, got nil")
	}
	if result.LockStatus != "Locked by PR #45 (other-user)" {
		t.Errorf("LockStatus = %q, want the conflict status", result.LockStatus)
	}
}

func TestReclaimStaleLock_GuardsBadInput(t *testing.T) {
	tests := []struct {
		name string
		held *models.Lock
	}{
		{"nil holder", nil},
		{"empty repo", &models.Lock{Application: "app-a", PRNumber: 45}},
		{"malformed repo", &models.Lock{Application: "app-a", PRNumber: 45, Repo: "no-slash"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lockMock := &reclaimLock{}
			exec := staleLockExecutor(&mockVCS{
				pr: &models.PullRequestDetail{State: models.PRStateClosed, Merged: true},
			}, lockMock)

			if exec.reclaimStaleLock(context.Background(), tt.held, "app-a") {
				t.Error("reclaimStaleLock = true, want false for un-verifiable holder")
			}
			if len(lockMock.forceUnlocked) != 0 {
				t.Errorf("forceUnlocked = %v, want none", lockMock.forceUnlocked)
			}
		})
	}
}

func TestSplitRepoFullName(t *testing.T) {
	tests := []struct {
		in          string
		owner, repo string
		ok          bool
	}{
		{"org/repo", "org", "repo", true},
		{"org/sub/repo", "org", "sub/repo", true},
		{"no-slash", "", "", false},
		{"", "", "", false},
		{"/repo", "", "", false},
		{"org/", "", "", false},
	}

	for _, tt := range tests {
		owner, repo, ok := splitRepoFullName(tt.in)
		if owner != tt.owner || repo != tt.repo || ok != tt.ok {
			t.Errorf("splitRepoFullName(%q) = (%q, %q, %v), want (%q, %q, %v)",
				tt.in, owner, repo, ok, tt.owner, tt.repo, tt.ok)
		}
	}
}
