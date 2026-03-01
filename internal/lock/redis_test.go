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

package lock

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/org/lemuria/internal/config"
	"github.com/org/lemuria/internal/models"
)

// newTestManager creates a RedisManager backed by miniredis for testing.
func newTestManager(t *testing.T) (*RedisManager, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	t.Cleanup(func() {
		_ = client.Close()
	})
	return &RedisManager{client: client}, mr
}

func TestNewRedisManagerInvalidAddress(t *testing.T) {
	cfg := config.RedisConfig{
		Address: "localhost:0",
	}
	_, err := NewRedisManager(cfg)
	if err == nil {
		t.Fatal("expected error for invalid Redis address, got nil")
	}
}

func TestNewRedisManagerValidAddress(t *testing.T) {
	mr := miniredis.RunT(t)
	cfg := config.RedisConfig{
		Address: mr.Addr(),
	}
	mgr, err := NewRedisManager(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = mgr.Close() }()
}

func TestPrLocksKey(t *testing.T) {
	mgr := &RedisManager{}
	tests := []struct {
		name     string
		repo     string
		prNumber int
		want     string
	}{
		{
			name:     "basic key",
			repo:     "owner/repo",
			prNumber: 42,
			want:     "lemuria:pr-locks:owner/repo:42",
		},
		{
			name:     "pr number 1",
			repo:     "org/project",
			prNumber: 1,
			want:     "lemuria:pr-locks:org/project:1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mgr.prLocksKey(tt.repo, tt.prNumber)
			if got != tt.want {
				t.Errorf("prLocksKey() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestKeyPrefixes(t *testing.T) {
	if lockKeyPrefix != "lemuria:lock:" {
		t.Errorf("lockKeyPrefix = %q, want %q", lockKeyPrefix, "lemuria:lock:")
	}
	if planKeyPrefix != "lemuria:plan:" {
		t.Errorf("planKeyPrefix = %q, want %q", planKeyPrefix, "lemuria:plan:")
	}
	if prLocksKeyPrefix != "lemuria:pr-locks:" {
		t.Errorf("prLocksKeyPrefix = %q, want %q", prLocksKeyPrefix, "lemuria:pr-locks:")
	}
}

func TestLock(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(t *testing.T, mgr *RedisManager)
		req        models.LockRequest
		wantAcq    bool
		wantHeldBy bool // expect HeldBy to be non-nil (conflict)
		wantErr    bool
	}{
		{
			name:  "new lock acquired",
			setup: nil,
			req: models.LockRequest{
				Application: "app1",
				PRNumber:    10,
				Repo:        "owner/repo",
				RepoURL:     "https://github.com/owner/repo",
				Provider:    "github",
				User:        "alice",
				ChangeType:  models.ApplicationExisting,
			},
			wantAcq: true,
		},
		{
			name: "refresh lock same PR",
			setup: func(t *testing.T, mgr *RedisManager) {
				t.Helper()
				_, err := mgr.Lock(context.Background(), models.LockRequest{
					Application: "app1",
					PRNumber:    10,
					Repo:        "owner/repo",
					User:        "alice",
				})
				if err != nil {
					t.Fatalf("setup lock failed: %v", err)
				}
			},
			req: models.LockRequest{
				Application: "app1",
				PRNumber:    10,
				Repo:        "owner/repo",
				User:        "alice",
				ChangeType:  models.ApplicationExisting,
			},
			wantAcq: true,
		},
		{
			name: "conflict different PR same repo",
			setup: func(t *testing.T, mgr *RedisManager) {
				t.Helper()
				_, err := mgr.Lock(context.Background(), models.LockRequest{
					Application: "app1",
					PRNumber:    10,
					Repo:        "owner/repo",
					User:        "alice",
				})
				if err != nil {
					t.Fatalf("setup lock failed: %v", err)
				}
			},
			req: models.LockRequest{
				Application: "app1",
				PRNumber:    20,
				Repo:        "owner/repo",
				User:        "bob",
			},
			wantAcq:    false,
			wantHeldBy: true,
		},
		{
			name: "conflict different PR different repo",
			setup: func(t *testing.T, mgr *RedisManager) {
				t.Helper()
				_, err := mgr.Lock(context.Background(), models.LockRequest{
					Application: "app1",
					PRNumber:    10,
					Repo:        "owner/repo",
					User:        "alice",
				})
				if err != nil {
					t.Fatalf("setup lock failed: %v", err)
				}
			},
			req: models.LockRequest{
				Application: "app1",
				PRNumber:    10,
				Repo:        "other/repo",
				User:        "bob",
			},
			wantAcq:    false,
			wantHeldBy: true,
		},
		{
			name:  "different applications do not conflict",
			setup: nil,
			req: models.LockRequest{
				Application: "app2",
				PRNumber:    10,
				Repo:        "owner/repo",
				User:        "alice",
			},
			wantAcq: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr, _ := newTestManager(t)
			ctx := context.Background()

			if tt.setup != nil {
				tt.setup(t, mgr)
			}

			result, err := mgr.Lock(ctx, tt.req)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.Acquired != tt.wantAcq {
				t.Errorf("Acquired = %v, want %v", result.Acquired, tt.wantAcq)
			}
			if tt.wantAcq {
				if result.Lock == nil {
					t.Fatal("expected Lock to be set when acquired")
				}
				if result.Lock.Application != tt.req.Application {
					t.Errorf("Lock.Application = %q, want %q", result.Lock.Application, tt.req.Application)
				}
				if result.Lock.PRNumber != tt.req.PRNumber {
					t.Errorf("Lock.PRNumber = %d, want %d", result.Lock.PRNumber, tt.req.PRNumber)
				}
				if result.Lock.Repo != tt.req.Repo {
					t.Errorf("Lock.Repo = %q, want %q", result.Lock.Repo, tt.req.Repo)
				}
				if result.Lock.User != tt.req.User {
					t.Errorf("Lock.User = %q, want %q", result.Lock.User, tt.req.User)
				}
			}
			if tt.wantHeldBy {
				if result.HeldBy == nil {
					t.Fatal("expected HeldBy to be set on conflict")
				}
			}
		})
	}
}

func TestLockRefreshUpdatesChangeType(t *testing.T) {
	mgr, _ := newTestManager(t)
	ctx := context.Background()

	// First lock with no change type
	_, err := mgr.Lock(ctx, models.LockRequest{
		Application: "app1",
		PRNumber:    10,
		Repo:        "owner/repo",
		User:        "alice",
	})
	if err != nil {
		t.Fatalf("first lock failed: %v", err)
	}

	// Refresh with ChangeTypeNew
	result, err := mgr.Lock(ctx, models.LockRequest{
		Application: "app1",
		PRNumber:    10,
		Repo:        "owner/repo",
		User:        "alice",
		ChangeType:  models.ApplicationNew,
	})
	if err != nil {
		t.Fatalf("refresh lock failed: %v", err)
	}
	if !result.Acquired {
		t.Fatal("expected lock to be acquired on refresh")
	}
	if result.Lock.ChangeType != models.ApplicationNew {
		t.Errorf("ChangeType = %q, want %q", result.Lock.ChangeType, models.ApplicationNew)
	}
}

func TestLockAddsApplicationToPRIndex(t *testing.T) {
	mgr, _ := newTestManager(t)
	ctx := context.Background()

	// Lock two different applications with the same PR
	_, err := mgr.Lock(ctx, models.LockRequest{
		Application: "app1",
		PRNumber:    10,
		Repo:        "owner/repo",
		User:        "alice",
	})
	if err != nil {
		t.Fatalf("lock app1 failed: %v", err)
	}

	_, err = mgr.Lock(ctx, models.LockRequest{
		Application: "app2",
		PRNumber:    10,
		Repo:        "owner/repo",
		User:        "alice",
	})
	if err != nil {
		t.Fatalf("lock app2 failed: %v", err)
	}

	locks, err := mgr.ListByPR(ctx, "owner/repo", 10)
	if err != nil {
		t.Fatalf("ListByPR failed: %v", err)
	}
	if len(locks) != 2 {
		t.Errorf("expected 2 locks for PR, got %d", len(locks))
	}
}

func TestUnlock(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(t *testing.T, mgr *RedisManager)
		app      string
		repo     string
		prNumber int
		wantErr  bool
	}{
		{
			name: "unlock by owning PR",
			setup: func(t *testing.T, mgr *RedisManager) {
				t.Helper()
				_, err := mgr.Lock(context.Background(), models.LockRequest{
					Application: "app1",
					PRNumber:    10,
					Repo:        "owner/repo",
					User:        "alice",
				})
				if err != nil {
					t.Fatalf("setup lock failed: %v", err)
				}
			},
			app:      "app1",
			repo:     "owner/repo",
			prNumber: 10,
			wantErr:  false,
		},
		{
			name: "unlock by different PR returns error",
			setup: func(t *testing.T, mgr *RedisManager) {
				t.Helper()
				_, err := mgr.Lock(context.Background(), models.LockRequest{
					Application: "app1",
					PRNumber:    10,
					Repo:        "owner/repo",
					User:        "alice",
				})
				if err != nil {
					t.Fatalf("setup lock failed: %v", err)
				}
			},
			app:      "app1",
			repo:     "owner/repo",
			prNumber: 20,
			wantErr:  true,
		},
		{
			name: "unlock by different repo returns error",
			setup: func(t *testing.T, mgr *RedisManager) {
				t.Helper()
				_, err := mgr.Lock(context.Background(), models.LockRequest{
					Application: "app1",
					PRNumber:    10,
					Repo:        "owner/repo",
					User:        "alice",
				})
				if err != nil {
					t.Fatalf("setup lock failed: %v", err)
				}
			},
			app:      "app1",
			repo:     "other/repo",
			prNumber: 10,
			wantErr:  true,
		},
		{
			name:     "unlock non-existent lock succeeds",
			setup:    nil,
			app:      "nonexistent",
			repo:     "owner/repo",
			prNumber: 10,
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr, _ := newTestManager(t)
			ctx := context.Background()

			if tt.setup != nil {
				tt.setup(t, mgr)
			}

			err := mgr.Unlock(ctx, tt.app, tt.repo, tt.prNumber)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Verify lock is gone after successful unlock
			lock, err := mgr.Get(ctx, tt.app)
			if err != nil {
				t.Fatalf("Get after unlock failed: %v", err)
			}
			if lock != nil {
				t.Error("expected lock to be nil after unlock")
			}
		})
	}
}

func TestUnlockRemovesFromPRIndex(t *testing.T) {
	mgr, _ := newTestManager(t)
	ctx := context.Background()

	// Lock two apps for the same PR
	for _, app := range []string{"app1", "app2"} {
		_, err := mgr.Lock(ctx, models.LockRequest{
			Application: app,
			PRNumber:    10,
			Repo:        "owner/repo",
			User:        "alice",
		})
		if err != nil {
			t.Fatalf("lock %s failed: %v", app, err)
		}
	}

	// Unlock one
	if err := mgr.Unlock(ctx, "app1", "owner/repo", 10); err != nil {
		t.Fatalf("unlock app1 failed: %v", err)
	}

	locks, err := mgr.ListByPR(ctx, "owner/repo", 10)
	if err != nil {
		t.Fatalf("ListByPR failed: %v", err)
	}
	if len(locks) != 1 {
		t.Errorf("expected 1 lock remaining, got %d", len(locks))
	}
	if len(locks) > 0 && locks[0].Application != "app2" {
		t.Errorf("expected remaining lock to be app2, got %s", locks[0].Application)
	}
}

func TestForceUnlock(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, mgr *RedisManager)
		app   string
	}{
		{
			name: "force unlock existing lock",
			setup: func(t *testing.T, mgr *RedisManager) {
				t.Helper()
				_, err := mgr.Lock(context.Background(), models.LockRequest{
					Application: "app1",
					PRNumber:    10,
					Repo:        "owner/repo",
					User:        "alice",
				})
				if err != nil {
					t.Fatalf("setup lock failed: %v", err)
				}
			},
			app: "app1",
		},
		{
			name:  "force unlock non-existent lock",
			setup: nil,
			app:   "nonexistent",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr, _ := newTestManager(t)
			ctx := context.Background()

			if tt.setup != nil {
				tt.setup(t, mgr)
			}

			err := mgr.ForceUnlock(ctx, tt.app)
			if err != nil {
				t.Fatalf("ForceUnlock failed: %v", err)
			}

			// Verify lock is gone
			lock, err := mgr.Get(ctx, tt.app)
			if err != nil {
				t.Fatalf("Get after ForceUnlock failed: %v", err)
			}
			if lock != nil {
				t.Error("expected lock to be nil after ForceUnlock")
			}
		})
	}
}

func TestForceUnlockRemovesPlanAndPRIndex(t *testing.T) {
	mgr, _ := newTestManager(t)
	ctx := context.Background()

	// Lock and store a plan
	_, err := mgr.Lock(ctx, models.LockRequest{
		Application: "app1",
		PRNumber:    10,
		Repo:        "owner/repo",
		User:        "alice",
	})
	if err != nil {
		t.Fatalf("lock failed: %v", err)
	}

	err = mgr.StorePlan(ctx, "app1", 10, "abc123", "values.yaml", "some plan output", nil)
	if err != nil {
		t.Fatalf("StorePlan failed: %v", err)
	}

	// Force unlock
	if err := mgr.ForceUnlock(ctx, "app1"); err != nil {
		t.Fatalf("ForceUnlock failed: %v", err)
	}

	// Plan should be gone
	_, err = mgr.GetPlan(ctx, "app1", 10)
	if err == nil {
		t.Error("expected error getting plan after force unlock, got nil")
	}

	// PR index should be empty
	locks, err := mgr.ListByPR(ctx, "owner/repo", 10)
	if err != nil {
		t.Fatalf("ListByPR failed: %v", err)
	}
	if len(locks) != 0 {
		t.Errorf("expected 0 locks in PR index, got %d", len(locks))
	}
}

func TestForceUnlockByNonOwner(t *testing.T) {
	mgr, _ := newTestManager(t)
	ctx := context.Background()

	// Lock by PR 10
	_, err := mgr.Lock(ctx, models.LockRequest{
		Application: "app1",
		PRNumber:    10,
		Repo:        "owner/repo",
		User:        "alice",
	})
	if err != nil {
		t.Fatalf("lock failed: %v", err)
	}

	// Force unlock should work regardless of who calls it
	if err := mgr.ForceUnlock(ctx, "app1"); err != nil {
		t.Fatalf("ForceUnlock failed: %v", err)
	}

	lock, err := mgr.Get(ctx, "app1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if lock != nil {
		t.Error("expected lock to be nil after ForceUnlock")
	}
}

func TestGet(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(t *testing.T, mgr *RedisManager)
		app      string
		wantNil  bool
		wantApp  string
		wantPR   int
		wantUser string
	}{
		{
			name:    "non-existent application returns nil",
			setup:   nil,
			app:     "nonexistent",
			wantNil: true,
		},
		{
			name: "existing lock is returned",
			setup: func(t *testing.T, mgr *RedisManager) {
				t.Helper()
				_, err := mgr.Lock(context.Background(), models.LockRequest{
					Application: "app1",
					PRNumber:    10,
					Repo:        "owner/repo",
					RepoURL:     "https://github.com/owner/repo",
					Provider:    "github",
					User:        "alice",
				})
				if err != nil {
					t.Fatalf("setup lock failed: %v", err)
				}
			},
			app:      "app1",
			wantNil:  false,
			wantApp:  "app1",
			wantPR:   10,
			wantUser: "alice",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr, _ := newTestManager(t)
			ctx := context.Background()

			if tt.setup != nil {
				tt.setup(t, mgr)
			}

			lock, err := mgr.Get(ctx, tt.app)
			if err != nil {
				t.Fatalf("Get failed: %v", err)
			}

			if tt.wantNil {
				if lock != nil {
					t.Errorf("expected nil lock, got %+v", lock)
				}
				return
			}

			if lock == nil {
				t.Fatal("expected non-nil lock")
			}
			if lock.Application != tt.wantApp {
				t.Errorf("Application = %q, want %q", lock.Application, tt.wantApp)
			}
			if lock.PRNumber != tt.wantPR {
				t.Errorf("PRNumber = %d, want %d", lock.PRNumber, tt.wantPR)
			}
			if lock.User != tt.wantUser {
				t.Errorf("User = %q, want %q", lock.User, tt.wantUser)
			}
		})
	}
}

func TestGetPreservesAllFields(t *testing.T) {
	mgr, _ := newTestManager(t)
	ctx := context.Background()

	req := models.LockRequest{
		Application: "app1",
		PRNumber:    42,
		Repo:        "owner/repo",
		RepoURL:     "https://github.com/owner/repo",
		Provider:    "github",
		User:        "alice",
		ChangeType:  models.ApplicationNew,
	}

	_, err := mgr.Lock(ctx, req)
	if err != nil {
		t.Fatalf("Lock failed: %v", err)
	}

	lock, err := mgr.Get(ctx, "app1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if lock == nil {
		t.Fatal("expected non-nil lock")
	}
	if lock.RepoURL != req.RepoURL {
		t.Errorf("RepoURL = %q, want %q", lock.RepoURL, req.RepoURL)
	}
	if lock.Provider != req.Provider {
		t.Errorf("Provider = %q, want %q", lock.Provider, req.Provider)
	}
	if lock.ChangeType != req.ChangeType {
		t.Errorf("ChangeType = %q, want %q", lock.ChangeType, req.ChangeType)
	}
	if lock.LockedAt.IsZero() {
		t.Error("expected LockedAt to be set")
	}
}

func TestListByPR(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(t *testing.T, mgr *RedisManager)
		repo     string
		prNumber int
		wantLen  int
	}{
		{
			name:     "no locks for PR",
			setup:    nil,
			repo:     "owner/repo",
			prNumber: 10,
			wantLen:  0,
		},
		{
			name: "single lock for PR",
			setup: func(t *testing.T, mgr *RedisManager) {
				t.Helper()
				_, err := mgr.Lock(context.Background(), models.LockRequest{
					Application: "app1",
					PRNumber:    10,
					Repo:        "owner/repo",
					User:        "alice",
				})
				if err != nil {
					t.Fatalf("setup lock failed: %v", err)
				}
			},
			repo:     "owner/repo",
			prNumber: 10,
			wantLen:  1,
		},
		{
			name: "multiple locks for same PR",
			setup: func(t *testing.T, mgr *RedisManager) {
				t.Helper()
				ctx := context.Background()
				for _, app := range []string{"app1", "app2", "app3"} {
					_, err := mgr.Lock(ctx, models.LockRequest{
						Application: app,
						PRNumber:    10,
						Repo:        "owner/repo",
						User:        "alice",
					})
					if err != nil {
						t.Fatalf("setup lock %s failed: %v", app, err)
					}
				}
			},
			repo:     "owner/repo",
			prNumber: 10,
			wantLen:  3,
		},
		{
			name: "does not return locks from other PRs",
			setup: func(t *testing.T, mgr *RedisManager) {
				t.Helper()
				ctx := context.Background()
				_, err := mgr.Lock(ctx, models.LockRequest{
					Application: "app1",
					PRNumber:    10,
					Repo:        "owner/repo",
					User:        "alice",
				})
				if err != nil {
					t.Fatalf("setup lock failed: %v", err)
				}
				_, err = mgr.Lock(ctx, models.LockRequest{
					Application: "app2",
					PRNumber:    20,
					Repo:        "owner/repo",
					User:        "bob",
				})
				if err != nil {
					t.Fatalf("setup lock failed: %v", err)
				}
			},
			repo:     "owner/repo",
			prNumber: 10,
			wantLen:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr, _ := newTestManager(t)
			ctx := context.Background()

			if tt.setup != nil {
				tt.setup(t, mgr)
			}

			locks, err := mgr.ListByPR(ctx, tt.repo, tt.prNumber)
			if err != nil {
				t.Fatalf("ListByPR failed: %v", err)
			}
			if len(locks) != tt.wantLen {
				t.Errorf("ListByPR returned %d locks, want %d", len(locks), tt.wantLen)
			}
		})
	}
}

func TestListByPRSkipsStaleIndex(t *testing.T) {
	mgr, _ := newTestManager(t)
	ctx := context.Background()

	// Lock app1 for PR 10
	_, err := mgr.Lock(ctx, models.LockRequest{
		Application: "app1",
		PRNumber:    10,
		Repo:        "owner/repo",
		User:        "alice",
	})
	if err != nil {
		t.Fatalf("lock failed: %v", err)
	}

	// Force unlock app1 (removes from index and deletes lock key)
	if err := mgr.ForceUnlock(ctx, "app1"); err != nil {
		t.Fatalf("force unlock failed: %v", err)
	}

	// The PR index should be clean, ListByPR should return 0
	locks, err := mgr.ListByPR(ctx, "owner/repo", 10)
	if err != nil {
		t.Fatalf("ListByPR failed: %v", err)
	}
	if len(locks) != 0 {
		t.Errorf("expected 0 locks, got %d", len(locks))
	}
}

func TestListAll(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(t *testing.T, mgr *RedisManager)
		wantLen int
	}{
		{
			name:    "no locks",
			setup:   nil,
			wantLen: 0,
		},
		{
			name: "single lock",
			setup: func(t *testing.T, mgr *RedisManager) {
				t.Helper()
				_, err := mgr.Lock(context.Background(), models.LockRequest{
					Application: "app1",
					PRNumber:    10,
					Repo:        "owner/repo",
					User:        "alice",
				})
				if err != nil {
					t.Fatalf("setup lock failed: %v", err)
				}
			},
			wantLen: 1,
		},
		{
			name: "multiple locks from different PRs",
			setup: func(t *testing.T, mgr *RedisManager) {
				t.Helper()
				ctx := context.Background()
				reqs := []models.LockRequest{
					{Application: "app1", PRNumber: 10, Repo: "owner/repo", User: "alice"},
					{Application: "app2", PRNumber: 20, Repo: "owner/repo", User: "bob"},
					{Application: "app3", PRNumber: 30, Repo: "other/repo", User: "charlie"},
				}
				for _, req := range reqs {
					_, err := mgr.Lock(ctx, req)
					if err != nil {
						t.Fatalf("setup lock %s failed: %v", req.Application, err)
					}
				}
			},
			wantLen: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr, _ := newTestManager(t)
			ctx := context.Background()

			if tt.setup != nil {
				tt.setup(t, mgr)
			}

			locks, err := mgr.ListAll(ctx)
			if err != nil {
				t.Fatalf("ListAll failed: %v", err)
			}
			if len(locks) != tt.wantLen {
				t.Errorf("ListAll returned %d locks, want %d", len(locks), tt.wantLen)
			}
		})
	}
}

func TestStorePlanAndGetPlan(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(t *testing.T, mgr *RedisManager)
		app        string
		prNumber   int
		revision   string
		sourceFile string
		planOutput string
		diffs      []models.PlanDiffEntry
	}{
		{
			name: "store and retrieve plan",
			setup: func(t *testing.T, mgr *RedisManager) {
				t.Helper()
				_, err := mgr.Lock(context.Background(), models.LockRequest{
					Application: "app1",
					PRNumber:    10,
					Repo:        "owner/repo",
					User:        "alice",
				})
				if err != nil {
					t.Fatalf("setup lock failed: %v", err)
				}
			},
			app:        "app1",
			prNumber:   10,
			revision:   "abc123",
			sourceFile: "values.yaml",
			planOutput: "Plan output text",
			diffs: []models.PlanDiffEntry{
				{
					Resource: models.ResourceKey{
						Kind: "Deployment",
						Name: "my-app",
					},
					Action: "update",
					Diff:   "--- a\n+++ b",
				},
			},
		},
		{
			name: "store plan with empty diffs",
			setup: func(t *testing.T, mgr *RedisManager) {
				t.Helper()
				_, err := mgr.Lock(context.Background(), models.LockRequest{
					Application: "app2",
					PRNumber:    20,
					Repo:        "owner/repo",
					User:        "bob",
				})
				if err != nil {
					t.Fatalf("setup lock failed: %v", err)
				}
			},
			app:        "app2",
			prNumber:   20,
			revision:   "def456",
			sourceFile: "",
			planOutput: "",
			diffs:      nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr, _ := newTestManager(t)
			ctx := context.Background()

			if tt.setup != nil {
				tt.setup(t, mgr)
			}

			err := mgr.StorePlan(ctx, tt.app, tt.prNumber, tt.revision, tt.sourceFile, tt.planOutput, tt.diffs)
			if err != nil {
				t.Fatalf("StorePlan failed: %v", err)
			}

			// GetPlan returns the revision string
			rev, err := mgr.GetPlan(ctx, tt.app, tt.prNumber)
			if err != nil {
				t.Fatalf("GetPlan failed: %v", err)
			}
			if rev != tt.revision {
				t.Errorf("GetPlan revision = %q, want %q", rev, tt.revision)
			}

			// Verify the lock was updated with plan details
			lock, err := mgr.Get(ctx, tt.app)
			if err != nil {
				t.Fatalf("Get failed: %v", err)
			}
			if lock == nil {
				t.Fatal("expected non-nil lock")
			}
			if lock.PlanRevision != tt.revision {
				t.Errorf("lock.PlanRevision = %q, want %q", lock.PlanRevision, tt.revision)
			}
			if lock.SourceFile != tt.sourceFile {
				t.Errorf("lock.SourceFile = %q, want %q", lock.SourceFile, tt.sourceFile)
			}
			if lock.PlanOutput != tt.planOutput {
				t.Errorf("lock.PlanOutput = %q, want %q", lock.PlanOutput, tt.planOutput)
			}
			if len(lock.PlanDiffs) != len(tt.diffs) {
				t.Errorf("lock.PlanDiffs length = %d, want %d", len(lock.PlanDiffs), len(tt.diffs))
			}
		})
	}
}

func TestGetPlanNonExistent(t *testing.T) {
	mgr, _ := newTestManager(t)
	ctx := context.Background()

	_, err := mgr.GetPlan(ctx, "nonexistent", 99)
	if err == nil {
		t.Error("expected error for non-existent plan, got nil")
	}
}

func TestStorePlanWithoutLock(t *testing.T) {
	mgr, _ := newTestManager(t)
	ctx := context.Background()

	// StorePlan when no lock exists should still store the revision in the plan key
	err := mgr.StorePlan(ctx, "app1", 10, "abc123", "values.yaml", "output", nil)
	if err != nil {
		t.Fatalf("StorePlan without lock failed: %v", err)
	}

	// The revision should be retrievable via GetPlan
	rev, err := mgr.GetPlan(ctx, "app1", 10)
	if err != nil {
		t.Fatalf("GetPlan failed: %v", err)
	}
	if rev != "abc123" {
		t.Errorf("revision = %q, want %q", rev, "abc123")
	}
}

func TestStorePlanMismatchedPR(t *testing.T) {
	mgr, _ := newTestManager(t)
	ctx := context.Background()

	// Lock with PR 10
	_, err := mgr.Lock(ctx, models.LockRequest{
		Application: "app1",
		PRNumber:    10,
		Repo:        "owner/repo",
		User:        "alice",
	})
	if err != nil {
		t.Fatalf("lock failed: %v", err)
	}

	// Store plan with different PR number
	err = mgr.StorePlan(ctx, "app1", 20, "abc123", "values.yaml", "output", nil)
	if err != nil {
		t.Fatalf("StorePlan failed: %v", err)
	}

	// Plan key should be set
	rev, err := mgr.GetPlan(ctx, "app1", 20)
	if err != nil {
		t.Fatalf("GetPlan failed: %v", err)
	}
	if rev != "abc123" {
		t.Errorf("revision = %q, want %q", rev, "abc123")
	}

	// Lock should NOT be updated because PR numbers don't match
	lock, err := mgr.Get(ctx, "app1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if lock.PlanRevision != "" {
		t.Errorf("expected empty PlanRevision on lock, got %q", lock.PlanRevision)
	}
}

func TestPing(t *testing.T) {
	mgr, _ := newTestManager(t)
	ctx := context.Background()

	if err := mgr.Ping(ctx); err != nil {
		t.Fatalf("Ping failed: %v", err)
	}
}

func TestClose(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	mgr := &RedisManager{client: client}

	if err := mgr.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// After close, operations should fail
	err := mgr.Ping(context.Background())
	if err == nil {
		t.Error("expected error after Close, got nil")
	}
}

func TestLockThenUnlockThenRelockDifferentPR(t *testing.T) {
	mgr, _ := newTestManager(t)
	ctx := context.Background()

	// PR 10 locks app1
	result, err := mgr.Lock(ctx, models.LockRequest{
		Application: "app1",
		PRNumber:    10,
		Repo:        "owner/repo",
		User:        "alice",
	})
	if err != nil {
		t.Fatalf("first lock failed: %v", err)
	}
	if !result.Acquired {
		t.Fatal("expected first lock to be acquired")
	}

	// Unlock
	if err := mgr.Unlock(ctx, "app1", "owner/repo", 10); err != nil {
		t.Fatalf("unlock failed: %v", err)
	}

	// PR 20 should now be able to lock app1
	result, err = mgr.Lock(ctx, models.LockRequest{
		Application: "app1",
		PRNumber:    20,
		Repo:        "owner/repo",
		User:        "bob",
	})
	if err != nil {
		t.Fatalf("second lock failed: %v", err)
	}
	if !result.Acquired {
		t.Fatal("expected second lock to be acquired after unlock")
	}
	if result.Lock.User != "bob" {
		t.Errorf("Lock.User = %q, want %q", result.Lock.User, "bob")
	}
}

func TestListAllAfterUnlocks(t *testing.T) {
	mgr, _ := newTestManager(t)
	ctx := context.Background()

	// Lock 3 apps
	for _, app := range []string{"app1", "app2", "app3"} {
		_, err := mgr.Lock(ctx, models.LockRequest{
			Application: app,
			PRNumber:    10,
			Repo:        "owner/repo",
			User:        "alice",
		})
		if err != nil {
			t.Fatalf("lock %s failed: %v", app, err)
		}
	}

	// Unlock 2
	for _, app := range []string{"app1", "app3"} {
		if err := mgr.Unlock(ctx, app, "owner/repo", 10); err != nil {
			t.Fatalf("unlock %s failed: %v", app, err)
		}
	}

	locks, err := mgr.ListAll(ctx)
	if err != nil {
		t.Fatalf("ListAll failed: %v", err)
	}
	if len(locks) != 1 {
		t.Errorf("expected 1 lock, got %d", len(locks))
	}
	if len(locks) > 0 && locks[0].Application != "app2" {
		t.Errorf("expected remaining lock for app2, got %s", locks[0].Application)
	}
}

func TestStorePlanOverwrite(t *testing.T) {
	mgr, _ := newTestManager(t)
	ctx := context.Background()

	_, err := mgr.Lock(ctx, models.LockRequest{
		Application: "app1",
		PRNumber:    10,
		Repo:        "owner/repo",
		User:        "alice",
	})
	if err != nil {
		t.Fatalf("lock failed: %v", err)
	}

	// Store first plan
	err = mgr.StorePlan(ctx, "app1", 10, "rev1", "file1.yaml", "output1", nil)
	if err != nil {
		t.Fatalf("first StorePlan failed: %v", err)
	}

	// Overwrite with second plan
	err = mgr.StorePlan(ctx, "app1", 10, "rev2", "file2.yaml", "output2", []models.PlanDiffEntry{
		{Resource: models.ResourceKey{Kind: "Service", Name: "svc1"}, Action: "create", Diff: "+++ new"},
	})
	if err != nil {
		t.Fatalf("second StorePlan failed: %v", err)
	}

	// Verify the latest plan is returned
	rev, err := mgr.GetPlan(ctx, "app1", 10)
	if err != nil {
		t.Fatalf("GetPlan failed: %v", err)
	}
	if rev != "rev2" {
		t.Errorf("revision = %q, want %q", rev, "rev2")
	}

	lock, err := mgr.Get(ctx, "app1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if lock.PlanRevision != "rev2" {
		t.Errorf("PlanRevision = %q, want %q", lock.PlanRevision, "rev2")
	}
	if lock.SourceFile != "file2.yaml" {
		t.Errorf("SourceFile = %q, want %q", lock.SourceFile, "file2.yaml")
	}
	if lock.PlanOutput != "output2" {
		t.Errorf("PlanOutput = %q, want %q", lock.PlanOutput, "output2")
	}
	if len(lock.PlanDiffs) != 1 {
		t.Errorf("PlanDiffs length = %d, want 1", len(lock.PlanDiffs))
	}
}

func TestUpdateLock(t *testing.T) {
	mgr, _ := newTestManager(t)
	ctx := context.Background()

	// First acquire a lock
	_, err := mgr.Lock(ctx, models.LockRequest{
		Application: "test-app",
		PRNumber:    1,
		Repo:        "org/repo",
		User:        "user1",
	})
	if err != nil {
		t.Fatalf("Lock failed: %v", err)
	}

	// Get and update the lock
	lock, err := mgr.Get(ctx, "test-app")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	lock.AutoSyncDisabled = true
	lock.OriginalSyncPolicy = []byte(`{"prune":true}`)
	lock.IsParentApp = true

	if err := mgr.UpdateLock(ctx, lock); err != nil {
		t.Fatalf("UpdateLock failed: %v", err)
	}

	// Verify the update persisted
	updated, err := mgr.Get(ctx, "test-app")
	if err != nil {
		t.Fatalf("Get after update failed: %v", err)
	}
	if !updated.AutoSyncDisabled {
		t.Error("expected AutoSyncDisabled to be true")
	}
	if string(updated.OriginalSyncPolicy) != `{"prune":true}` {
		t.Errorf("OriginalSyncPolicy = %s, want {\"prune\":true}", updated.OriginalSyncPolicy)
	}
	if !updated.IsParentApp {
		t.Error("expected IsParentApp to be true")
	}
}

func TestAppSetAutoSync(t *testing.T) {
	mgr, _ := newTestManager(t)
	ctx := context.Background()

	policy := []byte(`{"prune":true,"selfHeal":true}`)

	// Store
	err := mgr.StoreAppSetAutoSync(ctx, "my-appset", "org/repo", 42, policy)
	if err != nil {
		t.Fatalf("StoreAppSetAutoSync failed: %v", err)
	}

	// Get
	got, err := mgr.GetAppSetAutoSync(ctx, "my-appset", "org/repo", 42)
	if err != nil {
		t.Fatalf("GetAppSetAutoSync failed: %v", err)
	}
	if string(got) != string(policy) {
		t.Errorf("GetAppSetAutoSync = %s, want %s", got, policy)
	}

	// Get non-existent returns nil
	got, err = mgr.GetAppSetAutoSync(ctx, "other-appset", "org/repo", 42)
	if err != nil {
		t.Fatalf("GetAppSetAutoSync for missing key failed: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for missing key, got %s", got)
	}

	// Delete
	err = mgr.DeleteAppSetAutoSync(ctx, "my-appset", "org/repo", 42)
	if err != nil {
		t.Fatalf("DeleteAppSetAutoSync failed: %v", err)
	}

	// Verify deletion
	got, err = mgr.GetAppSetAutoSync(ctx, "my-appset", "org/repo", 42)
	if err != nil {
		t.Fatalf("GetAppSetAutoSync after delete failed: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil after delete, got %s", got)
	}
}

func TestParentAutoSync(t *testing.T) {
	mgr, _ := newTestManager(t)
	ctx := context.Background()

	policy1 := []byte(`{"prune":true}`)
	policy2 := []byte(`{"selfHeal":true}`)

	// Store two parent policies
	if err := mgr.StoreParentAutoSync(ctx, "parent-1", "org/repo", 10, policy1); err != nil {
		t.Fatalf("StoreParentAutoSync failed: %v", err)
	}
	if err := mgr.StoreParentAutoSync(ctx, "parent-2", "org/repo", 10, policy2); err != nil {
		t.Fatalf("StoreParentAutoSync failed: %v", err)
	}

	// List
	result, err := mgr.ListParentAutoSync(ctx, "org/repo", 10)
	if err != nil {
		t.Fatalf("ListParentAutoSync failed: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 parents, got %d", len(result))
	}
	if string(result["parent-1"]) != string(policy1) {
		t.Errorf("parent-1 policy = %s, want %s", result["parent-1"], policy1)
	}
	if string(result["parent-2"]) != string(policy2) {
		t.Errorf("parent-2 policy = %s, want %s", result["parent-2"], policy2)
	}

	// List for different PR returns empty
	result, err = mgr.ListParentAutoSync(ctx, "org/repo", 99)
	if err != nil {
		t.Fatalf("ListParentAutoSync for other PR failed: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected 0 parents for other PR, got %d", len(result))
	}

	// Delete one parent
	if err := mgr.DeleteParentAutoSync(ctx, "parent-1", "org/repo", 10); err != nil {
		t.Fatalf("DeleteParentAutoSync failed: %v", err)
	}

	// Verify only parent-2 remains
	result, err = mgr.ListParentAutoSync(ctx, "org/repo", 10)
	if err != nil {
		t.Fatalf("ListParentAutoSync after delete failed: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 parent after delete, got %d", len(result))
	}
	if _, ok := result["parent-2"]; !ok {
		t.Error("expected parent-2 to remain")
	}
}
