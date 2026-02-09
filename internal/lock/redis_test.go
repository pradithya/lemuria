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
	"testing"

	"github.com/org/lemuria/internal/config"
)

func TestNewRedisManagerInvalidAddress(t *testing.T) {
	cfg := config.RedisConfig{
		Address: "localhost:0",
	}
	_, err := NewRedisManager(cfg)
	if err == nil {
		t.Fatal("expected error for invalid Redis address, got nil")
	}
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
