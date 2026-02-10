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
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/org/lemuria/internal/config"
	"github.com/org/lemuria/internal/models"
)

const (
	// lockKeyPrefix is the prefix for lock keys in Redis.
	lockKeyPrefix = "lemuria:lock:"
	// planKeyPrefix is the prefix for plan keys in Redis.
	planKeyPrefix = "lemuria:plan:"
	// prLocksKeyPrefix is the prefix for PR-to-locks index.
	prLocksKeyPrefix = "lemuria:pr-locks:"
	// lockTTL is the default TTL for locks (7 days).
	lockTTL = 7 * 24 * time.Hour
)

// RedisManager implements Manager using Redis.
type RedisManager struct {
	client *redis.Client
}

// NewRedisManager creates a new Redis-based lock manager.
func NewRedisManager(cfg config.RedisConfig) (*RedisManager, error) {
	opts := &redis.Options{
		Addr:     cfg.Address,
		Password: cfg.Password,
		DB:       cfg.DB,
	}
	if cfg.TLS {
		opts.TLSConfig = &tls.Config{
			InsecureSkipVerify: cfg.TLSInsecure,
		}
	}
	client := redis.NewClient(opts)

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("connecting to Redis: %w", err)
	}

	return &RedisManager{client: client}, nil
}

// Lock attempts to acquire a lock for an application.
func (m *RedisManager) Lock(ctx context.Context, req models.LockRequest) (*models.LockResult, error) {
	// Check if lock already exists
	existing, err := m.Get(ctx, req.Application)
	if err != nil && !errors.Is(err, redis.Nil) {
		return nil, fmt.Errorf("checking existing lock: %w", err)
	}

	// If locked by the same PR, refresh the lock
	if existing != nil && existing.IsHeldByPR(req.Repo, req.PRNumber) {
		existing.LockedAt = time.Now()
		if err := m.setLock(ctx, existing); err != nil {
			return nil, err
		}
		return &models.LockResult{
			Acquired: true,
			Lock:     existing,
		}, nil
	}

	// If locked by another PR, return conflict
	if existing != nil {
		return &models.LockResult{
			Acquired: false,
			HeldBy:   existing,
		}, nil
	}

	// Create new lock
	lock := &models.Lock{
		Application: req.Application,
		PRNumber:    req.PRNumber,
		Repo:        req.Repo,
		RepoURL:     req.RepoURL,
		Provider:    req.Provider,
		User:        req.User,
		LockedAt:    time.Now(),
	}

	if err := m.setLock(ctx, lock); err != nil {
		return nil, err
	}

	// Add to PR index
	prKey := m.prLocksKey(req.Repo, req.PRNumber)
	if err := m.client.SAdd(ctx, prKey, req.Application).Err(); err != nil {
		return nil, fmt.Errorf("updating PR index: %w", err)
	}
	m.client.Expire(ctx, prKey, lockTTL)

	return &models.LockResult{
		Acquired: true,
		Lock:     lock,
	}, nil
}

// setLock stores a lock in Redis.
func (m *RedisManager) setLock(ctx context.Context, lock *models.Lock) error {
	data, err := json.Marshal(lock)
	if err != nil {
		return fmt.Errorf("marshaling lock: %w", err)
	}

	key := lockKeyPrefix + lock.Application
	if err := m.client.Set(ctx, key, data, lockTTL).Err(); err != nil {
		return fmt.Errorf("storing lock: %w", err)
	}

	return nil
}

// Unlock releases a lock if held by the specified PR.
func (m *RedisManager) Unlock(ctx context.Context, application, repo string, prNumber int) error {
	lock, err := m.Get(ctx, application)
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil // No lock exists
		}
		return err
	}

	if lock == nil {
		return nil
	}

	// Only unlock if held by the requesting PR
	if !lock.IsHeldByPR(repo, prNumber) {
		return fmt.Errorf("lock not held by PR #%d", prNumber)
	}

	return m.ForceUnlock(ctx, application)
}

// ForceUnlock releases a lock regardless of who holds it.
func (m *RedisManager) ForceUnlock(ctx context.Context, application string) error {
	// Get lock first to remove from PR index
	lock, err := m.Get(ctx, application)
	if err != nil && !errors.Is(err, redis.Nil) {
		return err
	}

	if lock != nil {
		// Remove from PR index
		prKey := m.prLocksKey(lock.Repo, lock.PRNumber)
		m.client.SRem(ctx, prKey, application)

		// Delete plan
		planKey := planKeyPrefix + application + ":" + strconv.Itoa(lock.PRNumber)
		m.client.Del(ctx, planKey)
	}

	// Delete lock
	key := lockKeyPrefix + application
	return m.client.Del(ctx, key).Err()
}

// Get returns the current lock for an application, if any.
func (m *RedisManager) Get(ctx context.Context, application string) (*models.Lock, error) {
	key := lockKeyPrefix + application
	data, err := m.client.Get(ctx, key).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, nil
		}
		return nil, fmt.Errorf("getting lock: %w", err)
	}

	var lock models.Lock
	if err := json.Unmarshal(data, &lock); err != nil {
		return nil, fmt.Errorf("unmarshaling lock: %w", err)
	}

	return &lock, nil
}

// ListByPR returns all locks held by a specific PR.
func (m *RedisManager) ListByPR(ctx context.Context, repo string, prNumber int) ([]models.Lock, error) {
	prKey := m.prLocksKey(repo, prNumber)
	apps, err := m.client.SMembers(ctx, prKey).Result()
	if err != nil {
		return nil, fmt.Errorf("getting PR locks: %w", err)
	}

	var locks []models.Lock
	for _, app := range apps {
		lock, err := m.Get(ctx, app)
		if err != nil {
			continue
		}
		if lock != nil && lock.IsHeldByPR(repo, prNumber) {
			locks = append(locks, *lock)
		}
	}

	return locks, nil
}

// ListAll returns all current locks.
func (m *RedisManager) ListAll(ctx context.Context) ([]models.Lock, error) {
	pattern := lockKeyPrefix + "*"
	keys, err := m.client.Keys(ctx, pattern).Result()
	if err != nil {
		return nil, fmt.Errorf("listing lock keys: %w", err)
	}

	var locks []models.Lock
	for _, key := range keys {
		app := key[len(lockKeyPrefix):]
		lock, err := m.Get(ctx, app)
		if err != nil {
			continue
		}
		if lock != nil {
			locks = append(locks, *lock)
		}
	}

	return locks, nil
}

// StorePlan stores the plan revision, source file, plan output, and diffs for verification before sync.
func (m *RedisManager) StorePlan(ctx context.Context, application string, prNumber int, revision, sourceFile, planOutput string, diffs []models.PlanDiffEntry) error {
	// Store in the dedicated plan key
	key := planKeyPrefix + application + ":" + strconv.Itoa(prNumber)
	if err := m.client.Set(ctx, key, revision, lockTTL).Err(); err != nil {
		return err
	}

	// Also update the lock's PlanRevision and SourceFile so ListByPR returns them
	lock, err := m.Get(ctx, application)
	if err != nil {
		return fmt.Errorf("getting lock to update plan revision: %w", err)
	}
	if lock != nil && lock.PRNumber == prNumber {
		lock.PlanRevision = revision
		lock.SourceFile = sourceFile
		lock.PlanOutput = planOutput
		lock.PlanDiffs = diffs
		return m.setLock(ctx, lock)
	}

	return nil
}

// GetPlan retrieves the stored plan revision.
func (m *RedisManager) GetPlan(ctx context.Context, application string, prNumber int) (string, error) {
	key := planKeyPrefix + application + ":" + strconv.Itoa(prNumber)
	return m.client.Get(ctx, key).Result()
}

// Ping checks the connection to Redis.
func (m *RedisManager) Ping(ctx context.Context) error {
	return m.client.Ping(ctx).Err()
}

// Close releases the Redis connection.
func (m *RedisManager) Close() error {
	return m.client.Close()
}

// prLocksKey returns the Redis key for a PR's lock index.
func (m *RedisManager) prLocksKey(repo string, prNumber int) string {
	return prLocksKeyPrefix + repo + ":" + strconv.Itoa(prNumber)
}
