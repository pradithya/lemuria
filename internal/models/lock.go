package models

import "time"

// Lock represents an application lock held by a PR.
type Lock struct {
	Application  string    `json:"application"`
	PRNumber     int       `json:"pr_number"`
	Repo         string    `json:"repo"`
	User         string    `json:"user"`
	LockedAt     time.Time `json:"locked_at"`
	PlanRevision string    `json:"plan_revision"`
	PlanOutput   string    `json:"plan_output,omitempty"`
}

// LockRequest is used when attempting to acquire a lock.
type LockRequest struct {
	Application string
	PRNumber    int
	Repo        string
	User        string
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
	return "lemuria:plan:" + application + ":" + string(rune(prNumber))
}

// LockStatus represents the current state of locks for a PR.
type LockStatus struct {
	Locks      []Lock `json:"locks"`
	TotalCount int    `json:"totalCount"`
}
