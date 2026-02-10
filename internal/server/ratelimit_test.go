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
	"testing"
	"time"
)

func TestRateLimiterAllow(t *testing.T) {
	rl := NewRateLimiter(3, 1*time.Minute)
	defer rl.Stop()

	key := "192.168.1.1"

	// First 3 attempts should be allowed
	for i := 0; i < 3; i++ {
		if !rl.Allow(key) {
			t.Errorf("attempt %d should be allowed", i+1)
		}
	}

	// 4th attempt should be denied
	if rl.Allow(key) {
		t.Error("4th attempt should be denied")
	}

	// A different key should still be allowed
	if !rl.Allow("10.0.0.1") {
		t.Error("different key should be allowed")
	}
}

func TestRateLimiterWindowExpiry(t *testing.T) {
	// Use a very short window so entries expire quickly
	rl := NewRateLimiter(2, 50*time.Millisecond)
	defer rl.Stop()

	key := "192.168.1.1"

	// Use both attempts
	if !rl.Allow(key) {
		t.Error("1st attempt should be allowed")
	}
	if !rl.Allow(key) {
		t.Error("2nd attempt should be allowed")
	}
	if rl.Allow(key) {
		t.Error("3rd attempt should be denied")
	}

	// Wait for the window to expire
	time.Sleep(60 * time.Millisecond)

	// Should be allowed again after window expires
	if !rl.Allow(key) {
		t.Error("attempt after window expiry should be allowed")
	}
}

func TestRateLimiterIndependentKeys(t *testing.T) {
	rl := NewRateLimiter(1, 1*time.Minute)
	defer rl.Stop()

	if !rl.Allow("key-a") {
		t.Error("key-a first attempt should be allowed")
	}
	if rl.Allow("key-a") {
		t.Error("key-a second attempt should be denied")
	}
	if !rl.Allow("key-b") {
		t.Error("key-b first attempt should be allowed")
	}
	if rl.Allow("key-b") {
		t.Error("key-b second attempt should be denied")
	}
}
