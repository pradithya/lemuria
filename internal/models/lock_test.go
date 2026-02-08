package models

import (
	"testing"
)

func TestLockIsHeldByPR(t *testing.T) {
	tests := []struct {
		name     string
		lock     Lock
		repo     string
		prNumber int
		want     bool
	}{
		{
			name:     "matching PR",
			lock:     Lock{Repo: "org/repo", PRNumber: 42},
			repo:     "org/repo",
			prNumber: 42,
			want:     true,
		},
		{
			name:     "different PR number",
			lock:     Lock{Repo: "org/repo", PRNumber: 42},
			repo:     "org/repo",
			prNumber: 43,
			want:     false,
		},
		{
			name:     "different repo",
			lock:     Lock{Repo: "org/repo", PRNumber: 42},
			repo:     "other/repo",
			prNumber: 42,
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.lock.IsHeldByPR(tt.repo, tt.prNumber); got != tt.want {
				t.Errorf("IsHeldByPR() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLockKey(t *testing.T) {
	lock := Lock{Application: "my-app"}
	want := "lemuria:lock:my-app"
	if got := lock.Key(); got != want {
		t.Errorf("Key() = %q, want %q", got, want)
	}
}

func TestPlanKey(t *testing.T) {
	tests := []struct {
		name     string
		app      string
		prNumber int
		want     string
	}{
		{
			name:     "basic",
			app:      "my-app",
			prNumber: 42,
			want:     "lemuria:plan:my-app:42",
		},
		{
			name:     "large PR number",
			app:      "prod-api",
			prNumber: 12345,
			want:     "lemuria:plan:prod-api:12345",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PlanKey(tt.app, tt.prNumber); got != tt.want {
				t.Errorf("PlanKey(%q, %d) = %q, want %q", tt.app, tt.prNumber, got, tt.want)
			}
		})
	}
}
