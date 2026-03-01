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

	"github.com/org/lemuria/internal/models"
)

// Manager defines the interface for application lock management.
type Manager interface {
	// Lock attempts to acquire a lock for an application.
	Lock(ctx context.Context, req models.LockRequest) (*models.LockResult, error)

	// Unlock releases a lock if held by the specified PR.
	Unlock(ctx context.Context, application, repo string, prNumber int) error

	// ForceUnlock releases a lock regardless of who holds it.
	ForceUnlock(ctx context.Context, application string) error

	// Get returns the current lock for an application, if any.
	Get(ctx context.Context, application string) (*models.Lock, error)

	// ListByPR returns all locks held by a specific PR.
	ListByPR(ctx context.Context, repo string, prNumber int) ([]models.Lock, error)

	// ListAll returns all current locks.
	ListAll(ctx context.Context) ([]models.Lock, error)

	// StorePlan stores the plan revision, source file, plan output, and diffs for verification before sync.
	StorePlan(ctx context.Context, application string, prNumber int, revision, sourceFile, planOutput string, diffs []models.PlanDiffEntry) error

	// GetPlan retrieves the stored plan revision.
	GetPlan(ctx context.Context, application string, prNumber int) (string, error)

	// UpdateLock updates an existing lock's fields (e.g., for storing auto-sync state).
	UpdateLock(ctx context.Context, lock *models.Lock) error

	// StoreAppSetAutoSync stores the original ApplicationSet auto-sync policy for later restoration.
	StoreAppSetAutoSync(ctx context.Context, appSetName, repo string, prNumber int, originalPolicy []byte) error

	// GetAppSetAutoSync retrieves the stored original ApplicationSet auto-sync policy.
	GetAppSetAutoSync(ctx context.Context, appSetName, repo string, prNumber int) ([]byte, error)

	// DeleteAppSetAutoSync removes the stored ApplicationSet auto-sync policy.
	DeleteAppSetAutoSync(ctx context.Context, appSetName, repo string, prNumber int) error

	// StoreParentAutoSync stores the original parent app auto-sync policy for later restoration.
	StoreParentAutoSync(ctx context.Context, parentAppName, repo string, prNumber int, originalPolicy []byte) error

	// ListParentAutoSync returns all stored parent auto-sync policies for a PR.
	ListParentAutoSync(ctx context.Context, repo string, prNumber int) (map[string][]byte, error)

	// DeleteParentAutoSync removes the stored parent app auto-sync policy.
	DeleteParentAutoSync(ctx context.Context, parentAppName, repo string, prNumber int) error

	// Ping checks the connection to the lock backend.
	Ping(ctx context.Context) error

	// Close releases resources held by the manager.
	Close() error
}
