package lock

import (
	"context"

	"github.com/org/lemuria/internal/config"
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

	// GetRepoConfig retrieves a cached RepoConfig for the given repo.
	// Returns nil, nil on cache miss.
	GetRepoConfig(ctx context.Context, repo string) (*config.RepoConfig, error)

	// SetRepoConfig caches a RepoConfig for the given repo with a short TTL.
	SetRepoConfig(ctx context.Context, repo string, cfg *config.RepoConfig) error

	// Ping checks the connection to the lock backend.
	Ping(ctx context.Context) error

	// Close releases resources held by the manager.
	Close() error
}
