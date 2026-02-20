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

package models

import (
	"strconv"
	"time"
)

// PlanDiffEntry is a lightweight representation of a manifest diff stored with the lock.
// It omits full YAML states (BaseState, LiveState, TargetState) to reduce storage size.
type PlanDiffEntry struct {
	Resource ResourceKey `json:"resource"`
	Action   DiffAction  `json:"action"`
	Diff     string      `json:"diff"`
}

// Lock represents an application lock held by a PR.
type Lock struct {
	Application  string                `json:"application"`
	PRNumber     int                   `json:"pr_number"`
	Repo         string                `json:"repo"`
	RepoURL      string                `json:"repo_url,omitempty"`
	Provider     string                `json:"provider,omitempty"`
	User         string                `json:"user"`
	LockedAt     time.Time             `json:"locked_at"`
	PlanRevision string                `json:"plan_revision"`
	SourceFile   string                `json:"source_file,omitempty"`
	PlanOutput   string                `json:"plan_output,omitempty"`
	PlanDiffs    []PlanDiffEntry       `json:"plan_diffs,omitempty"`
	ChangeType   ApplicationChangeType `json:"change_type,omitempty"`
}

// LockRequest is used when attempting to acquire a lock.
type LockRequest struct {
	Application string
	PRNumber    int
	Repo        string
	RepoURL     string
	Provider    string
	User        string
	ChangeType  ApplicationChangeType
}

// LockResult represents the outcome of a lock operation.
type LockResult struct {
	Acquired bool   `json:"acquired"`
	Lock     *Lock  `json:"lock,omitempty"`
	HeldBy   *Lock  `json:"heldBy,omitempty"`
	Error    string `json:"error,omitempty"`
}

// IsHeldByPR checks if the lock is held by the specified PR.
func (l *Lock) IsHeldByPR(repo string, prNumber int) bool {
	return l.Repo == repo && l.PRNumber == prNumber
}

// Key returns the Redis key for this lock.
func (l *Lock) Key() string {
	return "lemuria:lock:" + l.Application
}

// PlanKey returns the Redis key for storing plan output.
func PlanKey(application string, prNumber int) string {
	return "lemuria:plan:" + application + ":" + strconv.Itoa(prNumber)
}

// LockStatus represents the current state of locks for a PR.
type LockStatus struct {
	Locks      []Lock `json:"locks"`
	TotalCount int    `json:"totalCount"`
}
