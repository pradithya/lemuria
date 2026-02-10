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

package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/org/lemuria/internal/models"
)

const (
	// sessionKeyPrefix is the prefix for session keys in Redis.
	sessionKeyPrefix = "lemuria:session:"
	// stateKeyPrefix is the prefix for OAuth state keys in Redis.
	stateKeyPrefix = "lemuria:auth:state:"
	// userRoleKeyPrefix is the prefix for user role override keys.
	userRoleKeyPrefix = "lemuria:user:role:"
	// sessionsIndexKey is the key for tracking active sessions.
	sessionsIndexKey = "lemuria:sessions:active"
	// stateTTL is the TTL for OAuth state (10 minutes).
	stateTTL = 10 * time.Minute
)

// RedisSessionStore implements SessionStore and StateStore using Redis.
type RedisSessionStore struct {
	client     *redis.Client
	sessionTTL time.Duration
}

// NewRedisSessionStore creates a new Redis-based session store.
func NewRedisSessionStore(client *redis.Client, sessionTTL time.Duration) *RedisSessionStore {
	if sessionTTL == 0 {
		sessionTTL = 24 * time.Hour
	}
	return &RedisSessionStore{
		client:     client,
		sessionTTL: sessionTTL,
	}
}

// Create creates a new session for the user.
func (s *RedisSessionStore) Create(ctx context.Context, user *models.User) (*models.Session, error) {
	sessionID, err := generateSessionID()
	if err != nil {
		return nil, fmt.Errorf("generating session ID: %w", err)
	}

	now := time.Now()
	session := &models.Session{
		ID:        sessionID,
		UserID:    user.ID,
		User:      user,
		CreatedAt: now,
		ExpiresAt: now.Add(s.sessionTTL),
	}

	data, err := json.Marshal(session)
	if err != nil {
		return nil, fmt.Errorf("marshaling session: %w", err)
	}

	key := sessionKeyPrefix + sessionID
	if err := s.client.Set(ctx, key, data, s.sessionTTL).Err(); err != nil {
		return nil, fmt.Errorf("storing session: %w", err)
	}

	// Add to sessions index (non-fatal if it fails)
	if err := s.client.SAdd(ctx, sessionsIndexKey, sessionID).Err(); err != nil {
		slog.Warn("failed to add session to index", "sessionID", truncateSessionID(sessionID), "error", err)
	}

	return session, nil
}

// Get retrieves a session by ID.
func (s *RedisSessionStore) Get(ctx context.Context, sessionID string) (*models.Session, error) {
	key := sessionKeyPrefix + sessionID
	data, err := s.client.Get(ctx, key).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, nil
		}
		return nil, fmt.Errorf("getting session: %w", err)
	}

	var session models.Session
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, fmt.Errorf("unmarshaling session: %w", err)
	}

	// Check if expired
	if session.IsExpired() {
		// Clean up expired session
		if err := s.Delete(ctx, sessionID); err != nil {
			slog.Warn("failed to delete expired session", "sessionID", truncateSessionID(sessionID), "error", err)
		}
		return nil, nil
	}

	return &session, nil
}

// Delete removes a session.
func (s *RedisSessionStore) Delete(ctx context.Context, sessionID string) error {
	key := sessionKeyPrefix + sessionID
	if err := s.client.Del(ctx, key).Err(); err != nil {
		return fmt.Errorf("deleting session: %w", err)
	}

	// Remove from sessions index
	s.client.SRem(ctx, sessionsIndexKey, sessionID)

	return nil
}

// Refresh extends session expiration.
func (s *RedisSessionStore) Refresh(ctx context.Context, sessionID string) error {
	session, err := s.Get(ctx, sessionID)
	if err != nil {
		return err
	}
	if session == nil {
		return errors.New("session not found")
	}

	session.ExpiresAt = time.Now().Add(s.sessionTTL)

	data, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("marshaling session: %w", err)
	}

	key := sessionKeyPrefix + sessionID
	if err := s.client.Set(ctx, key, data, s.sessionTTL).Err(); err != nil {
		return fmt.Errorf("refreshing session: %w", err)
	}

	return nil
}

// ListAll returns all active sessions.
func (s *RedisSessionStore) ListAll(ctx context.Context) ([]*models.Session, error) {
	sessionIDs, err := s.client.SMembers(ctx, sessionsIndexKey).Result()
	if err != nil {
		return nil, fmt.Errorf("listing session IDs: %w", err)
	}

	var sessions []*models.Session
	for _, id := range sessionIDs {
		session, err := s.Get(ctx, id)
		if err != nil {
			continue
		}
		if session != nil {
			sessions = append(sessions, session)
		} else {
			// Clean up stale index entry
			s.client.SRem(ctx, sessionsIndexKey, id)
		}
	}

	return sessions, nil
}

// CreateState creates a new OAuth state.
func (s *RedisSessionStore) CreateState(ctx context.Context, redirectURL string) (*models.AuthState, error) {
	stateValue, err := generateSessionID()
	if err != nil {
		return nil, fmt.Errorf("generating state: %w", err)
	}

	state := &models.AuthState{
		State:       stateValue,
		RedirectURL: redirectURL,
		CreatedAt:   time.Now(),
	}

	data, err := json.Marshal(state)
	if err != nil {
		return nil, fmt.Errorf("marshaling state: %w", err)
	}

	key := stateKeyPrefix + stateValue
	if err := s.client.Set(ctx, key, data, stateTTL).Err(); err != nil {
		return nil, fmt.Errorf("storing state: %w", err)
	}

	return state, nil
}

// ValidateState validates and consumes an OAuth state.
func (s *RedisSessionStore) ValidateState(ctx context.Context, stateValue string) (*models.AuthState, error) {
	key := stateKeyPrefix + stateValue
	data, err := s.client.Get(ctx, key).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, errors.New("invalid or expired state")
		}
		return nil, fmt.Errorf("getting state: %w", err)
	}

	// Delete state immediately (one-time use)
	s.client.Del(ctx, key)

	var state models.AuthState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("unmarshaling state: %w", err)
	}

	return &state, nil
}

// SetUserRole sets a persistent role override for a user.
func (s *RedisSessionStore) SetUserRole(ctx context.Context, userID string, role models.Role) error {
	key := userRoleKeyPrefix + userID
	if err := s.client.Set(ctx, key, string(role), 0).Err(); err != nil {
		return fmt.Errorf("setting user role: %w", err)
	}
	return nil
}

// GetUserRole gets the persistent role override for a user, if any.
func (s *RedisSessionStore) GetUserRole(ctx context.Context, userID string) (models.Role, bool, error) {
	key := userRoleKeyPrefix + userID
	role, err := s.client.Get(ctx, key).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("getting user role: %w", err)
	}
	return models.Role(role), true, nil
}

// DeleteUserRole removes a user role override.
func (s *RedisSessionStore) DeleteUserRole(ctx context.Context, userID string) error {
	key := userRoleKeyPrefix + userID
	return s.client.Del(ctx, key).Err()
}

// truncateSessionID returns the first 8 characters of a session ID followed by
// "..." for safe logging. If the session ID is shorter than 8 characters, the
// full value is returned followed by "...".
func truncateSessionID(id string) string {
	if len(id) > 8 {
		return id[:8] + "..."
	}
	return id + "..."
}

// generateSessionID generates a cryptographically secure session ID.
func generateSessionID() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}
