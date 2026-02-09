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

package webhook

import (
	"encoding/json"
	"testing"

	"github.com/org/lemuria/internal/models"
)

func TestParseGitHubEvent(t *testing.T) {
	tests := []struct {
		name      string
		eventType string
		payload   string
		wantType  models.EventType
		wantNil   bool
		wantErr   bool
		check     func(t *testing.T, event *models.PREvent)
	}{
		{
			name:      "pull_request opened",
			eventType: "pull_request",
			payload: `{
				"action": "opened",
				"number": 42,
				"pull_request": {
					"number": 42,
					"title": "Add feature",
					"body": "This adds a new feature",
					"state": "open",
					"draft": false,
					"merged": false,
					"head": {"sha": "abc123", "ref": "feature-branch"},
					"base": {"ref": "main"},
					"user": {"login": "developer", "id": 100, "avatar_url": "https://example.com/avatar"},
					"html_url": "https://github.com/org/repo/pull/42",
					"updated_at": "2024-01-15T10:30:00Z"
				},
				"repository": {
					"name": "repo",
					"full_name": "org/repo",
					"owner": {"login": "org"},
					"clone_url": "https://github.com/org/repo.git",
					"html_url": "https://github.com/org/repo"
				},
				"sender": {"login": "developer", "id": 100}
			}`,
			wantType: models.EventTypePullRequest,
			check: func(t *testing.T, event *models.PREvent) {
				if event.Action != models.PRActionOpened {
					t.Errorf("Action = %q, want %q", event.Action, models.PRActionOpened)
				}
				if event.PR.Number != 42 {
					t.Errorf("PR.Number = %d, want %d", event.PR.Number, 42)
				}
				if event.PR.Title != "Add feature" {
					t.Errorf("PR.Title = %q, want %q", event.PR.Title, "Add feature")
				}
				if event.PR.HeadSHA != "abc123" {
					t.Errorf("PR.HeadSHA = %q, want %q", event.PR.HeadSHA, "abc123")
				}
				if event.PR.HeadRef != "feature-branch" {
					t.Errorf("PR.HeadRef = %q, want %q", event.PR.HeadRef, "feature-branch")
				}
				if event.PR.BaseRef != "main" {
					t.Errorf("PR.BaseRef = %q, want %q", event.PR.BaseRef, "main")
				}
				if event.Repo.FullName != "org/repo" {
					t.Errorf("Repo.FullName = %q, want %q", event.Repo.FullName, "org/repo")
				}
				if event.Sender.Login != "developer" {
					t.Errorf("Sender.Login = %q, want %q", event.Sender.Login, "developer")
				}
			},
		},
		{
			name:      "issue_comment on PR",
			eventType: "issue_comment",
			payload: `{
				"action": "created",
				"issue": {
					"number": 42,
					"pull_request": {"url": "https://api.github.com/repos/org/repo/pulls/42"}
				},
				"comment": {
					"id": 12345,
					"body": "lemuria plan",
					"user": {"login": "developer", "id": 100},
					"created_at": "2024-01-15T10:30:00Z"
				},
				"repository": {
					"name": "repo",
					"full_name": "org/repo",
					"owner": {"login": "org"},
					"clone_url": "https://github.com/org/repo.git",
					"html_url": "https://github.com/org/repo"
				},
				"sender": {"login": "developer", "id": 100}
			}`,
			wantType: models.EventTypeIssueComment,
			check: func(t *testing.T, event *models.PREvent) {
				if event.PR.Number != 42 {
					t.Errorf("PR.Number = %d, want %d", event.PR.Number, 42)
				}
				if event.Comment == nil {
					t.Fatal("Comment is nil")
				}
				if event.Comment.Body != "lemuria plan" {
					t.Errorf("Comment.Body = %q, want %q", event.Comment.Body, "lemuria plan")
				}
				if event.Comment.ID != 12345 {
					t.Errorf("Comment.ID = %d, want %d", event.Comment.ID, 12345)
				}
			},
		},
		{
			name:      "issue_comment on non-PR issue returns nil",
			eventType: "issue_comment",
			payload: `{
				"action": "created",
				"issue": {
					"number": 10
				},
				"comment": {
					"id": 99,
					"body": "some comment",
					"user": {"login": "dev", "id": 1}
				},
				"repository": {
					"name": "repo",
					"full_name": "org/repo",
					"owner": {"login": "org"}
				},
				"sender": {"login": "dev", "id": 1}
			}`,
			wantNil: true,
		},
		{
			name:      "pull_request_review submitted",
			eventType: "pull_request_review",
			payload: `{
				"action": "submitted",
				"review": {
					"state": "approved",
					"user": {"login": "reviewer", "id": 200}
				},
				"pull_request": {
					"number": 42,
					"title": "Add feature",
					"state": "open",
					"draft": false,
					"merged": false,
					"head": {"sha": "abc123", "ref": "feature-branch"},
					"base": {"ref": "main"},
					"user": {"login": "developer", "id": 100},
					"html_url": "https://github.com/org/repo/pull/42"
				},
				"repository": {
					"name": "repo",
					"full_name": "org/repo",
					"owner": {"login": "org"},
					"clone_url": "https://github.com/org/repo.git",
					"html_url": "https://github.com/org/repo"
				},
				"sender": {"login": "reviewer", "id": 200}
			}`,
			wantType: models.EventTypePullRequestReview,
			check: func(t *testing.T, event *models.PREvent) {
				if event.Action != models.PRActionSubmitted {
					t.Errorf("Action = %q, want %q", event.Action, models.PRActionSubmitted)
				}
				if event.PR.Number != 42 {
					t.Errorf("PR.Number = %d, want %d", event.PR.Number, 42)
				}
				if event.Sender.Login != "reviewer" {
					t.Errorf("Sender.Login = %q, want %q", event.Sender.Login, "reviewer")
				}
			},
		},
		{
			name:      "pull_request closed",
			eventType: "pull_request",
			payload: `{
				"action": "closed",
				"number": 50,
				"pull_request": {
					"number": 50,
					"title": "Close PR",
					"state": "closed",
					"draft": false,
					"merged": false,
					"head": {"sha": "close123", "ref": "close-branch"},
					"base": {"ref": "main"},
					"user": {"login": "dev", "id": 1},
					"html_url": "https://github.com/org/repo/pull/50"
				},
				"repository": {
					"name": "repo",
					"full_name": "org/repo",
					"owner": {"login": "org"},
					"clone_url": "https://github.com/org/repo.git",
					"html_url": "https://github.com/org/repo"
				},
				"sender": {"login": "dev", "id": 1}
			}`,
			wantType: models.EventTypePullRequest,
			check: func(t *testing.T, event *models.PREvent) {
				if event.Action != models.PRActionClosed {
					t.Errorf("Action = %q, want %q", event.Action, models.PRActionClosed)
				}
				if event.PR.State != models.PRStateClosed {
					t.Errorf("PR.State = %q, want %q", event.PR.State, models.PRStateClosed)
				}
				if event.PR.Merged {
					t.Error("PR.Merged = true, want false")
				}
			},
		},
		{
			name:      "pull_request merged",
			eventType: "pull_request",
			payload: `{
				"action": "closed",
				"number": 51,
				"pull_request": {
					"number": 51,
					"title": "Merge PR",
					"state": "closed",
					"draft": false,
					"merged": true,
					"head": {"sha": "merge123", "ref": "merge-branch"},
					"base": {"ref": "main"},
					"user": {"login": "dev", "id": 1},
					"html_url": "https://github.com/org/repo/pull/51"
				},
				"repository": {
					"name": "repo",
					"full_name": "org/repo",
					"owner": {"login": "org"},
					"clone_url": "https://github.com/org/repo.git",
					"html_url": "https://github.com/org/repo"
				},
				"sender": {"login": "dev", "id": 1}
			}`,
			wantType: models.EventTypePullRequest,
			check: func(t *testing.T, event *models.PREvent) {
				if event.Action != models.PRActionClosed {
					t.Errorf("Action = %q, want %q", event.Action, models.PRActionClosed)
				}
				if !event.PR.Merged {
					t.Error("PR.Merged = false, want true")
				}
			},
		},
		{
			name:      "pull_request synchronize",
			eventType: "pull_request",
			payload: `{
				"action": "synchronize",
				"number": 52,
				"pull_request": {
					"number": 52,
					"title": "Push new commits",
					"state": "open",
					"draft": false,
					"merged": false,
					"head": {"sha": "sync123", "ref": "sync-branch"},
					"base": {"ref": "main"},
					"user": {"login": "dev", "id": 1},
					"html_url": "https://github.com/org/repo/pull/52"
				},
				"repository": {
					"name": "repo",
					"full_name": "org/repo",
					"owner": {"login": "org"},
					"clone_url": "https://github.com/org/repo.git",
					"html_url": "https://github.com/org/repo"
				},
				"sender": {"login": "dev", "id": 1}
			}`,
			wantType: models.EventTypePullRequest,
			check: func(t *testing.T, event *models.PREvent) {
				if event.Action != models.PRActionSynchronize {
					t.Errorf("Action = %q, want %q", event.Action, models.PRActionSynchronize)
				}
				if event.Provider != models.VCSProviderGitHub {
					t.Errorf("Provider = %q, want %q", event.Provider, models.VCSProviderGitHub)
				}
			},
		},
		{
			name:      "issue_comment with invalid JSON",
			eventType: "issue_comment",
			payload:   `{invalid`,
			wantErr:   true,
		},
		{
			name:      "pull_request_review with invalid JSON",
			eventType: "pull_request_review",
			payload:   `not json`,
			wantErr:   true,
		},
		{
			name:      "unhandled event type returns nil",
			eventType: "push",
			payload:   `{}`,
			wantNil:   true,
		},
		{
			name:      "unhandled event type (deployment) returns nil",
			eventType: "deployment",
			payload:   `{}`,
			wantNil:   true,
		},
		{
			name:      "invalid JSON for pull_request",
			eventType: "pull_request",
			payload:   `{invalid json`,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event, err := ParseGitHubEvent(tt.eventType, []byte(tt.payload))
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseGitHubEvent() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}
			if tt.wantNil {
				if event != nil {
					t.Errorf("ParseGitHubEvent() = %v, want nil", event)
				}
				return
			}
			if event == nil {
				t.Fatal("ParseGitHubEvent() returned nil, want non-nil")
			}
			if event.Type != tt.wantType {
				t.Errorf("ParseGitHubEvent() Type = %v, want %v", event.Type, tt.wantType)
			}
			if tt.check != nil {
				tt.check(t, event)
			}
		})
	}
}

func TestParseGitHubEventFieldMapping(t *testing.T) {
	// Verify that fields from a pull_request event are correctly mapped
	payload := map[string]any{
		"action": "synchronize",
		"number": 7,
		"pull_request": map[string]any{
			"number":    7,
			"title":     "Update manifests",
			"body":      "Updated kustomize overlays",
			"state":     "open",
			"draft":     true,
			"merged":    false,
			"mergeable": true,
			"head":      map[string]any{"sha": "def456", "ref": "update-branch"},
			"base":      map[string]any{"ref": "main"},
			"user":      map[string]any{"login": "author", "id": 42, "avatar_url": "https://example.com/a"},
			"html_url":  "https://github.com/org/repo/pull/7",
		},
		"repository": map[string]any{
			"name":      "repo",
			"full_name": "org/repo",
			"owner":     map[string]any{"login": "org"},
			"clone_url": "https://github.com/org/repo.git",
			"html_url":  "https://github.com/org/repo",
		},
		"sender": map[string]any{"login": "author", "id": 42, "avatar_url": "https://example.com/a"},
	}

	data, _ := json.Marshal(payload)
	event, err := ParseGitHubEvent("pull_request", data)
	if err != nil {
		t.Fatalf("ParseGitHubEvent() error = %v", err)
	}

	if event.PR.Draft != true {
		t.Errorf("PR.Draft = %v, want true", event.PR.Draft)
	}
	if event.PR.Body != "Updated kustomize overlays" {
		t.Errorf("PR.Body = %q, want %q", event.PR.Body, "Updated kustomize overlays")
	}
	if event.Repo.CloneURL != "https://github.com/org/repo.git" {
		t.Errorf("Repo.CloneURL = %q, want %q", event.Repo.CloneURL, "https://github.com/org/repo.git")
	}
	if event.PR.Author.Login != "author" {
		t.Errorf("PR.Author.Login = %q, want %q", event.PR.Author.Login, "author")
	}
}
