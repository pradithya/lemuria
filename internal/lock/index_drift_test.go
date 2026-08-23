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
	"time"

	"github.com/org/lemuria/internal/models"
)

// req builds a lock request for the given app/PR.
func req(app string, pr int) models.LockRequest {
	return models.LockRequest{
		Application: app,
		PRNumber:    pr,
		Repo:        "pradithya/argocd-gitops",
		User:        "pradithya",
	}
}

// The PR index is written once, on first acquisition, with a 7-day TTL that is
// never refreshed. The lock keys themselves are rewritten (and their TTL reset)
// on every replan. A long-lived PR that replans keeps its locks alive while the
// index quietly expires underneath them — after which ListByPR reports nothing
// and `lemuria unlock` answers "No applications are locked by this PR" while
// the locks remain, blocking every other PR until the lock TTL runs out.
//
// This reproduces production PR #49: three live locks, no index key.
func TestListByPR_SurvivesExpiredIndex(t *testing.T) {
	mgr, mr := newTestManager(t)
	ctx := context.Background()

	for _, app := range []string{"vault", "external-secrets", "traefik-internal-gateway"} {
		if _, err := mgr.Lock(ctx, req(app, 49)); err != nil {
			t.Fatalf("Lock(%s): %v", app, err)
		}
	}

	// Six days pass; the PR replans, refreshing every lock TTL back to 7 days.
	mr.FastForward(6 * 24 * time.Hour)
	for _, app := range []string{"vault", "external-secrets", "traefik-internal-gateway"} {
		if _, err := mgr.Lock(ctx, req(app, 49)); err != nil {
			t.Fatalf("replan Lock(%s): %v", app, err)
		}
	}

	// Two more days: past the index's original 7-day expiry, but the locks
	// were refreshed at day 6 so they are still very much alive.
	mr.FastForward(2 * 24 * time.Hour)

	locks, err := mgr.ListByPR(ctx, "pradithya/argocd-gitops", 49)
	if err != nil {
		t.Fatalf("ListByPR: %v", err)
	}

	// The locks are still held, so ListByPR must still report them.
	if len(locks) != 3 {
		var live int
		for _, app := range []string{"vault", "external-secrets", "traefik-internal-gateway"} {
			if l, _ := mgr.Get(ctx, app); l != nil {
				live++
			}
		}
		t.Fatalf("ListByPR returned %d locks, want 3 (%d locks are still live in Redis — "+
			"unlock would report 'No applications are locked by this PR' while leaving them held)",
			len(locks), live)
	}
}

// Unlock must clear every lock the PR holds even when the index is gone.
func TestUnlockPath_ClearsLocksWithExpiredIndex(t *testing.T) {
	mgr, mr := newTestManager(t)
	ctx := context.Background()

	if _, err := mgr.Lock(ctx, req("vault", 49)); err != nil {
		t.Fatalf("Lock: %v", err)
	}

	mr.FastForward(6 * 24 * time.Hour)
	if _, err := mgr.Lock(ctx, req("vault", 49)); err != nil {
		t.Fatalf("replan Lock: %v", err)
	}
	mr.FastForward(2 * 24 * time.Hour)

	locks, err := mgr.ListByPR(ctx, "pradithya/argocd-gitops", 49)
	if err != nil {
		t.Fatalf("ListByPR: %v", err)
	}
	for _, l := range locks {
		if err := mgr.Unlock(ctx, l.Application, l.Repo, l.PRNumber); err != nil {
			t.Fatalf("Unlock(%s): %v", l.Application, err)
		}
	}

	remaining, err := mgr.Get(ctx, "vault")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if remaining != nil {
		t.Fatalf("lock on vault survived unlock (held by PR #%d) — the app stays blocked",
			remaining.PRNumber)
	}
}

// A lock refreshed by a replan must not expire before its refreshed TTL.
func TestLock_ReplanRefreshesIndexTTL(t *testing.T) {
	mgr, mr := newTestManager(t)
	ctx := context.Background()

	if _, err := mgr.Lock(ctx, req("vault", 49)); err != nil {
		t.Fatalf("Lock: %v", err)
	}
	mr.FastForward(6 * 24 * time.Hour)
	if _, err := mgr.Lock(ctx, req("vault", 49)); err != nil {
		t.Fatalf("replan Lock: %v", err)
	}

	ttl := mr.TTL(prLocksKeyPrefix + "pradithya/argocd-gitops:49")
	if ttl <= 0 {
		t.Fatalf("PR index TTL = %v after replan; index will expire while the lock lives on", ttl)
	}
	if ttl < 6*24*time.Hour {
		t.Errorf("PR index TTL = %v after replan, want it refreshed to ~7d to match the lock TTL", ttl)
	}
}
