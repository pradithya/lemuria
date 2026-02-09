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
	"testing"

	"github.com/org/lemuria/internal/models"
)

func TestParseGitLabEvent(t *testing.T) {
	tests := []struct {
		name      string
		eventType string
		payload   string
		wantNil   bool
		wantErr   bool
		wantType  models.EventType
		check     func(t *testing.T, event *models.PREvent)
	}{
		{
			name:      "merge request opened",
			eventType: "Merge Request Hook",
			payload: `{
				"object_attributes": {
					"iid": 10,
					"title": "Add feature",
					"description": "Implements new feature",
					"state": "opened",
					"action": "open",
					"draft": false,
					"work_in_progress": false,
					"source_branch": "feature-branch",
					"target_branch": "main",
					"last_commit": {"id": "abc123def456"},
					"url": "https://gitlab.com/mygroup/myproject/-/merge_requests/10",
					"updated_at": "2024-06-01 12:00:00 UTC"
				},
				"project": {
					"name": "myproject",
					"path_with_namespace": "mygroup/myproject",
					"web_url": "https://gitlab.com/mygroup/myproject",
					"git_http_url": "https://gitlab.com/mygroup/myproject.git"
				},
				"user": {"username": "testuser", "name": "Test User", "id": 42, "avatar_url": "https://gitlab.com/avatar"}
			}`,
			wantType: models.EventTypePullRequest,
			check: func(t *testing.T, event *models.PREvent) {
				if event.Provider != models.VCSProviderGitLab {
					t.Errorf("Provider = %q, want %q", event.Provider, models.VCSProviderGitLab)
				}
				if event.Action != models.PRActionOpened {
					t.Errorf("Action = %q, want %q", event.Action, models.PRActionOpened)
				}
				if event.PR.Number != 10 {
					t.Errorf("PR.Number = %d, want %d", event.PR.Number, 10)
				}
				if event.PR.Title != "Add feature" {
					t.Errorf("PR.Title = %q, want %q", event.PR.Title, "Add feature")
				}
				if event.PR.Body != "Implements new feature" {
					t.Errorf("PR.Body = %q, want %q", event.PR.Body, "Implements new feature")
				}
				if event.PR.State != models.PRStateOpen {
					t.Errorf("PR.State = %q, want %q", event.PR.State, models.PRStateOpen)
				}
				if event.PR.Draft {
					t.Error("PR.Draft = true, want false")
				}
				if event.PR.Merged {
					t.Error("PR.Merged = true, want false")
				}
				if event.PR.HeadSHA != "abc123def456" {
					t.Errorf("PR.HeadSHA = %q, want %q", event.PR.HeadSHA, "abc123def456")
				}
				if event.PR.HeadRef != "feature-branch" {
					t.Errorf("PR.HeadRef = %q, want %q", event.PR.HeadRef, "feature-branch")
				}
				if event.PR.BaseRef != "main" {
					t.Errorf("PR.BaseRef = %q, want %q", event.PR.BaseRef, "main")
				}
				if event.PR.HTMLURL != "https://gitlab.com/mygroup/myproject/-/merge_requests/10" {
					t.Errorf("PR.HTMLURL = %q", event.PR.HTMLURL)
				}
				if event.PR.Author.Login != "testuser" {
					t.Errorf("PR.Author.Login = %q, want %q", event.PR.Author.Login, "testuser")
				}
				if event.PR.Author.ID != 42 {
					t.Errorf("PR.Author.ID = %d, want %d", event.PR.Author.ID, 42)
				}
				if event.Repo.Owner != "mygroup" {
					t.Errorf("Repo.Owner = %q, want %q", event.Repo.Owner, "mygroup")
				}
				if event.Repo.Name != "myproject" {
					t.Errorf("Repo.Name = %q, want %q", event.Repo.Name, "myproject")
				}
				if event.Repo.FullName != "mygroup/myproject" {
					t.Errorf("Repo.FullName = %q, want %q", event.Repo.FullName, "mygroup/myproject")
				}
				if event.Repo.CloneURL != "https://gitlab.com/mygroup/myproject.git" {
					t.Errorf("Repo.CloneURL = %q", event.Repo.CloneURL)
				}
				if event.Repo.HTMLURL != "https://gitlab.com/mygroup/myproject" {
					t.Errorf("Repo.HTMLURL = %q", event.Repo.HTMLURL)
				}
				if event.Sender.Login != "testuser" {
					t.Errorf("Sender.Login = %q, want %q", event.Sender.Login, "testuser")
				}
				if event.Sender.ID != 42 {
					t.Errorf("Sender.ID = %d, want %d", event.Sender.ID, 42)
				}
			},
		},
		{
			name:      "merge request updated (synchronize)",
			eventType: "Merge Request Hook",
			payload: `{
				"object_attributes": {
					"iid": 11,
					"title": "Update feature",
					"state": "opened",
					"action": "update",
					"draft": false,
					"source_branch": "feature-branch",
					"target_branch": "main",
					"last_commit": {"id": "def456"},
					"url": "https://gitlab.com/g/p/-/merge_requests/11",
					"updated_at": "2024-06-01 12:00:00 UTC"
				},
				"project": {
					"name": "p",
					"path_with_namespace": "g/p",
					"web_url": "https://gitlab.com/g/p",
					"git_http_url": "https://gitlab.com/g/p.git"
				},
				"user": {"username": "dev", "id": 1}
			}`,
			wantType: models.EventTypePullRequest,
			check: func(t *testing.T, event *models.PREvent) {
				if event.Action != models.PRActionSynchronize {
					t.Errorf("Action = %q, want %q", event.Action, models.PRActionSynchronize)
				}
				if event.PR.Number != 11 {
					t.Errorf("PR.Number = %d, want %d", event.PR.Number, 11)
				}
				if event.PR.State != models.PRStateOpen {
					t.Errorf("PR.State = %q, want %q", event.PR.State, models.PRStateOpen)
				}
				if event.PR.Merged {
					t.Error("PR.Merged = true, want false")
				}
			},
		},
		{
			name:      "merge request closed",
			eventType: "Merge Request Hook",
			payload: `{
				"object_attributes": {
					"iid": 12,
					"title": "Close me",
					"state": "closed",
					"action": "close",
					"draft": false,
					"source_branch": "feature-branch",
					"target_branch": "main",
					"last_commit": {"id": "ghi789"},
					"url": "https://gitlab.com/g/p/-/merge_requests/12",
					"updated_at": "2024-06-01 12:00:00 UTC"
				},
				"project": {
					"name": "p",
					"path_with_namespace": "g/p",
					"web_url": "https://gitlab.com/g/p",
					"git_http_url": "https://gitlab.com/g/p.git"
				},
				"user": {"username": "dev", "id": 1}
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
			name:      "merge request merged",
			eventType: "Merge Request Hook",
			payload: `{
				"object_attributes": {
					"iid": 13,
					"title": "Merge me",
					"state": "merged",
					"action": "merge",
					"draft": false,
					"source_branch": "feature-branch",
					"target_branch": "main",
					"last_commit": {"id": "jkl012"},
					"url": "https://gitlab.com/g/p/-/merge_requests/13",
					"updated_at": "2024-06-01 12:00:00 UTC"
				},
				"project": {
					"name": "p",
					"path_with_namespace": "g/p",
					"web_url": "https://gitlab.com/g/p",
					"git_http_url": "https://gitlab.com/g/p.git"
				},
				"user": {"username": "dev", "id": 1}
			}`,
			wantType: models.EventTypePullRequest,
			check: func(t *testing.T, event *models.PREvent) {
				if event.Action != models.PRActionClosed {
					t.Errorf("Action = %q, want %q", event.Action, models.PRActionClosed)
				}
				if event.PR.State != models.PRStateClosed {
					t.Errorf("PR.State = %q, want %q", event.PR.State, models.PRStateClosed)
				}
				if !event.PR.Merged {
					t.Error("PR.Merged = false, want true")
				}
			},
		},
		{
			name:      "draft/WIP merge request",
			eventType: "Merge Request Hook",
			payload: `{
				"object_attributes": {
					"iid": 14,
					"title": "Draft: WIP feature",
					"state": "opened",
					"action": "open",
					"draft": true,
					"work_in_progress": true,
					"source_branch": "draft-branch",
					"target_branch": "main",
					"last_commit": {"id": "pqr678"},
					"url": "https://gitlab.com/g/p/-/merge_requests/14",
					"updated_at": "2024-06-01 12:00:00 UTC"
				},
				"project": {
					"name": "p",
					"path_with_namespace": "g/p",
					"web_url": "https://gitlab.com/g/p",
					"git_http_url": "https://gitlab.com/g/p.git"
				},
				"user": {"username": "dev", "id": 1}
			}`,
			wantType: models.EventTypePullRequest,
			check: func(t *testing.T, event *models.PREvent) {
				if !event.PR.Draft {
					t.Error("PR.Draft = false, want true")
				}
			},
		},
		{
			name:      "work_in_progress only sets draft",
			eventType: "Merge Request Hook",
			payload: `{
				"object_attributes": {
					"iid": 15,
					"title": "WIP: something",
					"state": "opened",
					"action": "open",
					"draft": false,
					"work_in_progress": true,
					"source_branch": "wip-branch",
					"target_branch": "main",
					"last_commit": {"id": "aaa111"},
					"url": "https://gitlab.com/g/p/-/merge_requests/15",
					"updated_at": "2024-06-01 12:00:00 UTC"
				},
				"project": {
					"name": "p",
					"path_with_namespace": "g/p",
					"web_url": "https://gitlab.com/g/p",
					"git_http_url": "https://gitlab.com/g/p.git"
				},
				"user": {"username": "dev", "id": 1}
			}`,
			wantType: models.EventTypePullRequest,
			check: func(t *testing.T, event *models.PREvent) {
				if !event.PR.Draft {
					t.Error("PR.Draft = false, want true (work_in_progress=true)")
				}
			},
		},
		{
			name:      "note on merge request",
			eventType: "Note Hook",
			payload: `{
				"object_attributes": {
					"id": 999,
					"note": "lemuria plan -a my-app",
					"noteable_type": "MergeRequest",
					"created_at": "2024-06-01 12:00:00 UTC"
				},
				"merge_request": {
					"iid": 20,
					"title": "MR with comment",
					"description": "Some MR description",
					"state": "opened",
					"draft": false,
					"source_branch": "feature",
					"target_branch": "main",
					"last_commit": {"id": "mno345"},
					"url": "https://gitlab.com/mygroup/myproject/-/merge_requests/20"
				},
				"project": {
					"name": "myproject",
					"path_with_namespace": "mygroup/myproject",
					"web_url": "https://gitlab.com/mygroup/myproject",
					"git_http_url": "https://gitlab.com/mygroup/myproject.git"
				},
				"user": {"username": "commenter", "id": 5, "avatar_url": "https://gitlab.com/commenter/avatar"}
			}`,
			wantType: models.EventTypeIssueComment,
			check: func(t *testing.T, event *models.PREvent) {
				if event.Provider != models.VCSProviderGitLab {
					t.Errorf("Provider = %q, want %q", event.Provider, models.VCSProviderGitLab)
				}
				if event.Action != models.PRActionCreated {
					t.Errorf("Action = %q, want %q", event.Action, models.PRActionCreated)
				}
				if event.PR.Number != 20 {
					t.Errorf("PR.Number = %d, want %d", event.PR.Number, 20)
				}
				if event.PR.Title != "MR with comment" {
					t.Errorf("PR.Title = %q, want %q", event.PR.Title, "MR with comment")
				}
				if event.PR.Body != "Some MR description" {
					t.Errorf("PR.Body = %q, want %q", event.PR.Body, "Some MR description")
				}
				if event.PR.State != models.PRStateOpen {
					t.Errorf("PR.State = %q, want %q", event.PR.State, models.PRStateOpen)
				}
				if event.PR.HeadSHA != "mno345" {
					t.Errorf("PR.HeadSHA = %q, want %q", event.PR.HeadSHA, "mno345")
				}
				if event.PR.HeadRef != "feature" {
					t.Errorf("PR.HeadRef = %q, want %q", event.PR.HeadRef, "feature")
				}
				if event.PR.BaseRef != "main" {
					t.Errorf("PR.BaseRef = %q, want %q", event.PR.BaseRef, "main")
				}
				if event.Comment == nil {
					t.Fatal("Comment is nil")
				}
				if event.Comment.ID != 999 {
					t.Errorf("Comment.ID = %d, want %d", event.Comment.ID, 999)
				}
				if event.Comment.Body != "lemuria plan -a my-app" {
					t.Errorf("Comment.Body = %q, want %q", event.Comment.Body, "lemuria plan -a my-app")
				}
				if event.Comment.Author.Login != "commenter" {
					t.Errorf("Comment.Author.Login = %q, want %q", event.Comment.Author.Login, "commenter")
				}
				if event.Sender.Login != "commenter" {
					t.Errorf("Sender.Login = %q, want %q", event.Sender.Login, "commenter")
				}
				if event.Repo.Owner != "mygroup" {
					t.Errorf("Repo.Owner = %q, want %q", event.Repo.Owner, "mygroup")
				}
			},
		},
		{
			name:      "note on MR does not set PR.Author from comment user",
			eventType: "Note Hook",
			payload: `{
				"object_attributes": {
					"id": 1001,
					"note": "checking author",
					"noteable_type": "MergeRequest",
					"created_at": "2024-06-01 12:00:00 UTC"
				},
				"merge_request": {
					"iid": 50,
					"title": "Author test MR",
					"state": "opened",
					"source_branch": "feature",
					"target_branch": "main",
					"last_commit": {"id": "aut123"},
					"url": "https://gitlab.com/g/p/-/merge_requests/50"
				},
				"project": {
					"name": "p",
					"path_with_namespace": "g/p",
					"web_url": "https://gitlab.com/g/p",
					"git_http_url": "https://gitlab.com/g/p.git"
				},
				"user": {"username": "commenter", "id": 99}
			}`,
			wantType: models.EventTypeIssueComment,
			check: func(t *testing.T, event *models.PREvent) {
				// PR.Author should be empty — the webhook user is the comment author, not the MR author
				if event.PR.Author.Login != "" {
					t.Errorf("PR.Author.Login = %q, want empty", event.PR.Author.Login)
				}
				// Comment.Author and Sender should be the comment user
				if event.Comment.Author.Login != "commenter" {
					t.Errorf("Comment.Author.Login = %q, want %q", event.Comment.Author.Login, "commenter")
				}
				if event.Sender.Login != "commenter" {
					t.Errorf("Sender.Login = %q, want %q", event.Sender.Login, "commenter")
				}
			},
		},
		{
			name:      "merge request with http_url_to_repo fallback",
			eventType: "Merge Request Hook",
			payload: `{
				"object_attributes": {
					"iid": 40,
					"title": "Fallback URL MR",
					"state": "opened",
					"action": "open",
					"draft": false,
					"source_branch": "feature",
					"target_branch": "main",
					"last_commit": {"id": "fb123"},
					"url": "https://gitlab.example.com/group/repo/-/merge_requests/40",
					"updated_at": "2024-06-01 12:00:00 UTC"
				},
				"project": {
					"name": "repo",
					"path_with_namespace": "group/repo",
					"web_url": "https://gitlab.example.com/group/repo",
					"http_url_to_repo": "https://gitlab.example.com/group/repo.git"
				},
				"user": {"username": "dev", "id": 1}
			}`,
			wantType: models.EventTypePullRequest,
			check: func(t *testing.T, event *models.PREvent) {
				if event.Repo.CloneURL != "https://gitlab.example.com/group/repo.git" {
					t.Errorf("Repo.CloneURL = %q, want %q", event.Repo.CloneURL, "https://gitlab.example.com/group/repo.git")
				}
			},
		},
		{
			name:      "note with http_url_to_repo fallback",
			eventType: "Note Hook",
			payload: `{
				"object_attributes": {
					"id": 1002,
					"note": "test",
					"noteable_type": "MergeRequest",
					"created_at": "2024-06-01 12:00:00 UTC"
				},
				"merge_request": {
					"iid": 41,
					"title": "Note fallback URL",
					"state": "opened",
					"source_branch": "feature",
					"target_branch": "main",
					"last_commit": {"id": "nfb123"},
					"url": "https://gitlab.example.com/group/repo/-/merge_requests/41"
				},
				"project": {
					"name": "repo",
					"path_with_namespace": "group/repo",
					"web_url": "https://gitlab.example.com/group/repo",
					"http_url_to_repo": "https://gitlab.example.com/group/repo.git"
				},
				"user": {"username": "dev", "id": 1}
			}`,
			wantType: models.EventTypeIssueComment,
			check: func(t *testing.T, event *models.PREvent) {
				if event.Repo.CloneURL != "https://gitlab.example.com/group/repo.git" {
					t.Errorf("Repo.CloneURL = %q, want %q", event.Repo.CloneURL, "https://gitlab.example.com/group/repo.git")
				}
			},
		},
		{
			name:      "note on merge request with draft MR",
			eventType: "Note Hook",
			payload: `{
				"object_attributes": {
					"id": 1000,
					"note": "some note",
					"noteable_type": "MergeRequest",
					"created_at": "2024-06-01 12:00:00 UTC"
				},
				"merge_request": {
					"iid": 21,
					"title": "Draft: WIP",
					"state": "opened",
					"draft": true,
					"work_in_progress": false,
					"source_branch": "draft",
					"target_branch": "main",
					"last_commit": {"id": "xyz"},
					"url": "https://gitlab.com/g/p/-/merge_requests/21"
				},
				"project": {
					"name": "p",
					"path_with_namespace": "g/p",
					"web_url": "https://gitlab.com/g/p",
					"git_http_url": "https://gitlab.com/g/p.git"
				},
				"user": {"username": "u", "id": 1}
			}`,
			wantType: models.EventTypeIssueComment,
			check: func(t *testing.T, event *models.PREvent) {
				if !event.PR.Draft {
					t.Error("PR.Draft = false, want true")
				}
			},
		},
		{
			name:      "note on issue (not MR) returns nil",
			eventType: "Note Hook",
			payload: `{
				"object_attributes": {
					"id": 888,
					"note": "just a comment",
					"noteable_type": "Issue",
					"created_at": "2024-06-01 12:00:00 UTC"
				},
				"project": {
					"name": "p",
					"path_with_namespace": "g/p",
					"web_url": "https://gitlab.com/g/p",
					"git_http_url": "https://gitlab.com/g/p.git"
				},
				"user": {"username": "user", "id": 3}
			}`,
			wantNil: true,
		},
		{
			name:      "note on commit returns nil",
			eventType: "Note Hook",
			payload: `{
				"object_attributes": {
					"id": 777,
					"note": "commit comment",
					"noteable_type": "Commit",
					"created_at": "2024-06-01 12:00:00 UTC"
				},
				"project": {
					"name": "p",
					"path_with_namespace": "g/p",
					"web_url": "https://gitlab.com/g/p",
					"git_http_url": "https://gitlab.com/g/p.git"
				},
				"user": {"username": "user", "id": 3}
			}`,
			wantNil: true,
		},
		{
			name:      "note on snippet returns nil",
			eventType: "Note Hook",
			payload: `{
				"object_attributes": {
					"id": 666,
					"note": "snippet comment",
					"noteable_type": "Snippet",
					"created_at": "2024-06-01 12:00:00 UTC"
				},
				"project": {
					"name": "p",
					"path_with_namespace": "g/p",
					"web_url": "https://gitlab.com/g/p",
					"git_http_url": "https://gitlab.com/g/p.git"
				},
				"user": {"username": "user", "id": 3}
			}`,
			wantNil: true,
		},
		{
			name:      "unknown event type (Push Hook) returns nil",
			eventType: "Push Hook",
			payload:   `{"ref": "refs/heads/main"}`,
			wantNil:   true,
		},
		{
			name:      "unknown event type (Tag Push Hook) returns nil",
			eventType: "Tag Push Hook",
			payload:   `{"ref": "refs/tags/v1.0"}`,
			wantNil:   true,
		},
		{
			name:      "unknown event type (Pipeline Hook) returns nil",
			eventType: "Pipeline Hook",
			payload:   `{}`,
			wantNil:   true,
		},
		{
			name:      "merge request with invalid JSON",
			eventType: "Merge Request Hook",
			payload:   `{invalid json`,
			wantErr:   true,
		},
		{
			name:      "note with invalid JSON",
			eventType: "Note Hook",
			payload:   `{invalid json`,
			wantErr:   true,
		},
		{
			name:      "merge request with subgroup project",
			eventType: "Merge Request Hook",
			payload: `{
				"object_attributes": {
					"iid": 30,
					"title": "Subgroup MR",
					"state": "opened",
					"action": "open",
					"draft": false,
					"source_branch": "feature",
					"target_branch": "main",
					"last_commit": {"id": "sub123"},
					"url": "https://gitlab.com/org/team/backend/-/merge_requests/30",
					"updated_at": "2024-06-01 12:00:00 UTC"
				},
				"project": {
					"name": "backend",
					"path_with_namespace": "org/team/backend",
					"web_url": "https://gitlab.com/org/team/backend",
					"git_http_url": "https://gitlab.com/org/team/backend.git"
				},
				"user": {"username": "dev", "id": 1}
			}`,
			wantType: models.EventTypePullRequest,
			check: func(t *testing.T, event *models.PREvent) {
				if event.Repo.Owner != "org/team" {
					t.Errorf("Repo.Owner = %q, want %q", event.Repo.Owner, "org/team")
				}
				if event.Repo.Name != "backend" {
					t.Errorf("Repo.Name = %q, want %q", event.Repo.Name, "backend")
				}
				if event.Repo.FullName != "org/team/backend" {
					t.Errorf("Repo.FullName = %q, want %q", event.Repo.FullName, "org/team/backend")
				}
			},
		},
		{
			name:      "merge request with deeply nested group",
			eventType: "Merge Request Hook",
			payload: `{
				"object_attributes": {
					"iid": 31,
					"title": "Deep MR",
					"state": "opened",
					"action": "open",
					"draft": false,
					"source_branch": "feature",
					"target_branch": "main",
					"last_commit": {"id": "deep123"},
					"url": "https://gitlab.com/a/b/c/d/-/merge_requests/31",
					"updated_at": "2024-06-01 12:00:00 UTC"
				},
				"project": {
					"name": "d",
					"path_with_namespace": "a/b/c/d",
					"web_url": "https://gitlab.com/a/b/c/d",
					"git_http_url": "https://gitlab.com/a/b/c/d.git"
				},
				"user": {"username": "dev", "id": 1}
			}`,
			wantType: models.EventTypePullRequest,
			check: func(t *testing.T, event *models.PREvent) {
				if event.Repo.Owner != "a/b/c" {
					t.Errorf("Repo.Owner = %q, want %q", event.Repo.Owner, "a/b/c")
				}
				if event.Repo.Name != "d" {
					t.Errorf("Repo.Name = %q, want %q", event.Repo.Name, "d")
				}
			},
		},
		{
			name:      "note on MR in subgroup project",
			eventType: "Note Hook",
			payload: `{
				"object_attributes": {
					"id": 555,
					"note": "lemuria plan",
					"noteable_type": "MergeRequest",
					"created_at": "2024-06-01 12:00:00 UTC"
				},
				"merge_request": {
					"iid": 32,
					"title": "Subgroup MR comment",
					"state": "opened",
					"source_branch": "feature",
					"target_branch": "main",
					"last_commit": {"id": "sub456"},
					"url": "https://gitlab.com/org/team/backend/-/merge_requests/32"
				},
				"project": {
					"name": "backend",
					"path_with_namespace": "org/team/backend",
					"web_url": "https://gitlab.com/org/team/backend",
					"git_http_url": "https://gitlab.com/org/team/backend.git"
				},
				"user": {"username": "commenter", "id": 5}
			}`,
			wantType: models.EventTypeIssueComment,
			check: func(t *testing.T, event *models.PREvent) {
				if event.Repo.Owner != "org/team" {
					t.Errorf("Repo.Owner = %q, want %q", event.Repo.Owner, "org/team")
				}
				if event.Repo.Name != "backend" {
					t.Errorf("Repo.Name = %q, want %q", event.Repo.Name, "backend")
				}
			},
		},
		{
			name:      "note on MR with closed state",
			eventType: "Note Hook",
			payload: `{
				"object_attributes": {
					"id": 444,
					"note": "late comment",
					"noteable_type": "MergeRequest",
					"created_at": "2024-06-01 12:00:00 UTC"
				},
				"merge_request": {
					"iid": 33,
					"title": "Closed MR",
					"state": "closed",
					"source_branch": "old-feature",
					"target_branch": "main",
					"last_commit": {"id": "closed123"},
					"url": "https://gitlab.com/g/p/-/merge_requests/33"
				},
				"project": {
					"name": "p",
					"path_with_namespace": "g/p",
					"web_url": "https://gitlab.com/g/p",
					"git_http_url": "https://gitlab.com/g/p.git"
				},
				"user": {"username": "u", "id": 1}
			}`,
			wantType: models.EventTypeIssueComment,
			check: func(t *testing.T, event *models.PREvent) {
				if event.PR.State != models.PRStateClosed {
					t.Errorf("PR.State = %q, want %q", event.PR.State, models.PRStateClosed)
				}
			},
		},
		{
			name:      "note on MR with merged state",
			eventType: "Note Hook",
			payload: `{
				"object_attributes": {
					"id": 333,
					"note": "post-merge comment",
					"noteable_type": "MergeRequest",
					"created_at": "2024-06-01 12:00:00 UTC"
				},
				"merge_request": {
					"iid": 34,
					"title": "Merged MR",
					"state": "merged",
					"source_branch": "merged-feature",
					"target_branch": "main",
					"last_commit": {"id": "merged123"},
					"url": "https://gitlab.com/g/p/-/merge_requests/34"
				},
				"project": {
					"name": "p",
					"path_with_namespace": "g/p",
					"web_url": "https://gitlab.com/g/p",
					"git_http_url": "https://gitlab.com/g/p.git"
				},
				"user": {"username": "u", "id": 1}
			}`,
			wantType: models.EventTypeIssueComment,
			check: func(t *testing.T, event *models.PREvent) {
				if event.PR.State != models.PRStateClosed {
					t.Errorf("PR.State = %q, want %q", event.PR.State, models.PRStateClosed)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event, err := ParseGitLabEvent(tt.eventType, []byte(tt.payload))
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseGitLabEvent() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}
			if tt.wantNil {
				if event != nil {
					t.Errorf("ParseGitLabEvent() = %v, want nil", event)
				}
				return
			}
			if event == nil {
				t.Fatal("ParseGitLabEvent() returned nil, want non-nil")
			}
			if event.Type != tt.wantType {
				t.Errorf("ParseGitLabEvent() Type = %v, want %v", event.Type, tt.wantType)
			}
			if tt.check != nil {
				tt.check(t, event)
			}
		})
	}
}

func TestMapGitLabMRAction(t *testing.T) {
	tests := []struct {
		input string
		want  models.PRAction
	}{
		{"open", models.PRActionOpened},
		{"update", models.PRActionSynchronize},
		{"close", models.PRActionClosed},
		{"merge", models.PRActionClosed},
		{"reopen", models.PRAction("reopen")},
		{"approved", models.PRAction("approved")},
		{"unapproved", models.PRAction("unapproved")},
		{"", models.PRAction("")},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := mapGitLabMRAction(tt.input)
			if got != tt.want {
				t.Errorf("mapGitLabMRAction(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNormalizeGitLabState(t *testing.T) {
	tests := []struct {
		input string
		want  models.PRState
	}{
		{"opened", models.PRStateOpen},
		{"merged", models.PRStateClosed},
		{"closed", models.PRStateClosed},
		{"locked", models.PRState("locked")},
		{"", models.PRState("")},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizeGitLabState(tt.input)
			if got != tt.want {
				t.Errorf("normalizeGitLabState(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSplitPathWithNamespace(t *testing.T) {
	tests := []struct {
		name              string
		pathWithNamespace string
		projectName       string
		wantOwner         string
		wantName          string
	}{
		{
			name:              "simple group/project",
			pathWithNamespace: "mygroup/myproject",
			projectName:       "myproject",
			wantOwner:         "mygroup",
			wantName:          "myproject",
		},
		{
			name:              "nested group/subgroup/project",
			pathWithNamespace: "org/team/backend",
			projectName:       "backend",
			wantOwner:         "org/team",
			wantName:          "backend",
		},
		{
			name:              "deeply nested",
			pathWithNamespace: "a/b/c/d",
			projectName:       "d",
			wantOwner:         "a/b/c",
			wantName:          "d",
		},
		{
			name:              "no slash falls back to projectName",
			pathWithNamespace: "soloproject",
			projectName:       "soloproject",
			wantOwner:         "soloproject",
			wantName:          "soloproject",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, name := splitPathWithNamespace(tt.pathWithNamespace, tt.projectName)
			if owner != tt.wantOwner {
				t.Errorf("owner = %q, want %q", owner, tt.wantOwner)
			}
			if name != tt.wantName {
				t.Errorf("name = %q, want %q", name, tt.wantName)
			}
		})
	}
}
