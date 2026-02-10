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

package server

import (
	"sync"
	"time"
)

// RateLimiter provides a simple in-memory sliding window rate limiter.
// It tracks timestamps of recent attempts per key and rejects requests
// that exceed the configured maximum within the time window.
type RateLimiter struct {
	mu          sync.Mutex
	attempts    map[string][]time.Time
	maxAttempts int
	window      time.Duration
	stopCleanup chan struct{}
}

// NewRateLimiter creates a RateLimiter that allows maxAttempts requests per
// key within the given time window. It starts a background goroutine that
// periodically removes stale entries.
func NewRateLimiter(maxAttempts int, window time.Duration) *RateLimiter {
	rl := &RateLimiter{
		attempts:    make(map[string][]time.Time),
		maxAttempts: maxAttempts,
		window:      window,
		stopCleanup: make(chan struct{}),
	}

	go rl.cleanup()

	return rl
}

// Allow checks whether the given key is allowed to proceed. It records the
// current timestamp and returns true if the number of attempts within the
// sliding window is at or below the maximum. Returns false if the limit has
// been exceeded (the attempt is NOT recorded when denied).
func (rl *RateLimiter) Allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-rl.window)

	// Filter out expired entries for this key
	existing := rl.attempts[key]
	valid := existing[:0]
	for _, t := range existing {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}

	if len(valid) >= rl.maxAttempts {
		// Over limit -- store the pruned slice but do not add the new attempt
		rl.attempts[key] = valid
		return false
	}

	// Under limit -- record this attempt
	rl.attempts[key] = append(valid, now)
	return true
}

// Stop terminates the background cleanup goroutine.
func (rl *RateLimiter) Stop() {
	close(rl.stopCleanup)
}

// cleanup runs periodically to remove keys whose entries have all expired.
func (rl *RateLimiter) cleanup() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			rl.mu.Lock()
			now := time.Now()
			cutoff := now.Add(-rl.window)
			for key, timestamps := range rl.attempts {
				valid := timestamps[:0]
				for _, t := range timestamps {
					if t.After(cutoff) {
						valid = append(valid, t)
					}
				}
				if len(valid) == 0 {
					delete(rl.attempts, key)
				} else {
					rl.attempts[key] = valid
				}
			}
			rl.mu.Unlock()
		case <-rl.stopCleanup:
			return
		}
	}
}
