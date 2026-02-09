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
	"testing"
)

func TestPREventIsPROpen(t *testing.T) {
	tests := []struct {
		name   string
		state  PRState
		merged bool
		want   bool
	}{
		{"open and not merged", PRStateOpen, false, true},
		{"open but merged", PRStateOpen, true, false},
		{"closed and not merged", PRStateClosed, false, false},
		{"closed and merged", PRStateClosed, true, false},
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
		state  PRState
		merged bool
		want   bool
	}{
		{"closed not merged", PRStateClosed, false, true},
		{"closed and merged", PRStateClosed, true, false},
		{"open", PRStateOpen, false, false},
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
		action    PRAction
		want      bool
	}{
		{"PR opened", EventTypePullRequest, PRActionOpened, true},
		{"PR synchronize", EventTypePullRequest, PRActionSynchronize, true},
		{"PR closed", EventTypePullRequest, PRActionClosed, false},
		{"PR edited", EventTypePullRequest, "edited", false},
		{"comment event", EventTypeIssueComment, PRActionCreated, false},
		{"review event", EventTypePullRequestReview, PRActionSubmitted, false},
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
		action    PRAction
		want      bool
	}{
		{"PR closed", EventTypePullRequest, PRActionClosed, true},
		{"PR opened", EventTypePullRequest, PRActionOpened, false},
		{"PR synchronize", EventTypePullRequest, PRActionSynchronize, false},
		{"comment event", EventTypeIssueComment, PRActionCreated, false},
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
