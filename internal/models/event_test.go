package models

import (
	"testing"
)

func TestPREventIsPROpen(t *testing.T) {
	tests := []struct {
		name  string
		state string
		merged bool
		want  bool
	}{
		{"open and not merged", "open", false, true},
		{"open but merged", "open", true, false},
		{"closed and not merged", "closed", false, false},
		{"closed and merged", "closed", true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &PREvent{PR: PRInfo{State: tt.state, Merged: tt.merged}}
			if got := e.IsPROpen(); got != tt.want {
				t.Errorf("IsPROpen() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPREventIsPRMerged(t *testing.T) {
	tests := []struct {
		name   string
		merged bool
		want   bool
	}{
		{"merged", true, true},
		{"not merged", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &PREvent{PR: PRInfo{Merged: tt.merged}}
			if got := e.IsPRMerged(); got != tt.want {
				t.Errorf("IsPRMerged() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPREventIsPRClosed(t *testing.T) {
	tests := []struct {
		name   string
		state  string
		merged bool
		want   bool
	}{
		{"closed not merged", "closed", false, true},
		{"closed and merged", "closed", true, false},
		{"open", "open", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &PREvent{PR: PRInfo{State: tt.state, Merged: tt.merged}}
			if got := e.IsPRClosed(); got != tt.want {
				t.Errorf("IsPRClosed() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPREventShouldAutoplan(t *testing.T) {
	tests := []struct {
		name      string
		eventType EventType
		action    string
		want      bool
	}{
		{"PR opened", EventTypePullRequest, "opened", true},
		{"PR synchronize", EventTypePullRequest, "synchronize", true},
		{"PR closed", EventTypePullRequest, "closed", false},
		{"PR edited", EventTypePullRequest, "edited", false},
		{"comment event", EventTypeIssueComment, "created", false},
		{"review event", EventTypePullRequestReview, "submitted", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &PREvent{Type: tt.eventType, Action: tt.action}
			if got := e.ShouldAutoplan(); got != tt.want {
				t.Errorf("ShouldAutoplan() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPREventShouldUnlockAll(t *testing.T) {
	tests := []struct {
		name      string
		eventType EventType
		action    string
		want      bool
	}{
		{"PR closed", EventTypePullRequest, "closed", true},
		{"PR opened", EventTypePullRequest, "opened", false},
		{"PR synchronize", EventTypePullRequest, "synchronize", false},
		{"comment event", EventTypeIssueComment, "created", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &PREvent{Type: tt.eventType, Action: tt.action}
			if got := e.ShouldUnlockAll(); got != tt.want {
				t.Errorf("ShouldUnlockAll() = %v, want %v", got, tt.want)
			}
		})
	}
}
