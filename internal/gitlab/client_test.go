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

package gitlab

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/org/lemuria/internal/config"
	"github.com/org/lemuria/internal/models"
)

// newTestClient creates a Client that talks to the given httptest server.
func newTestClient(t *testing.T, serverURL string) *Client {
	t.Helper()
	c, err := NewClient(config.GitLabConfig{
		URL:   serverURL,
		Token: "test-token",
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

func TestNewClient(t *testing.T) {
	tests := []struct {
		name    string
		cfg     config.GitLabConfig
		wantErr bool
	}{
		{
			name: "valid config with custom URL",
			cfg: config.GitLabConfig{
				URL:   "https://gitlab.example.com",
				Token: "my-token",
			},
			wantErr: false,
		},
		{
			name: "valid config with default URL",
			cfg: config.GitLabConfig{
				Token: "my-token",
			},
			wantErr: false,
		},
		{
			name: "empty token still creates client",
			cfg: config.GitLabConfig{
				URL: "https://gitlab.example.com",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewClient(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewClient() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && client == nil {
				t.Error("NewClient() returned nil client without error")
			}
		})
	}
}

func TestNormalizeState(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  models.PRState
	}{
		{
			name:  "opened maps to open",
			input: "opened",
			want:  models.PRStateOpen,
		},
		{
			name:  "merged maps to closed",
			input: "merged",
			want:  models.PRStateClosed,
		},
		{
			name:  "closed passes through",
			input: "closed",
			want:  models.PRStateClosed,
		},
		{
			name:  "unknown state passes through",
			input: "locked",
			want:  models.PRState("locked"),
		},
		{
			name:  "empty string passes through",
			input: "",
			want:  models.PRState(""),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeState(tt.input)
			if got != tt.want {
				t.Errorf("normalizeState(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestProjectPath(t *testing.T) {
	tests := []struct {
		name  string
		owner string
		repo  string
		want  string
	}{
		{
			name:  "simple owner and repo",
			owner: "mygroup",
			repo:  "myproject",
			want:  "mygroup/myproject",
		},
		{
			name:  "nested group",
			owner: "org/subgroup",
			repo:  "project",
			want:  "org/subgroup/project",
		},
		{
			name:  "empty owner",
			owner: "",
			repo:  "project",
			want:  "/project",
		},
		{
			name:  "empty repo",
			owner: "group",
			repo:  "",
			want:  "group/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := projectPath(tt.owner, tt.repo)
			if got != tt.want {
				t.Errorf("projectPath(%q, %q) = %q, want %q", tt.owner, tt.repo, got, tt.want)
			}
		})
	}
}

func TestGetPR(t *testing.T) {
	tests := []struct {
		name       string
		mrResponse map[string]any
		wantPR     *models.PullRequestDetail
		wantErr    bool
	}{
		{
			name: "open merge request",
			mrResponse: map[string]any{
				"iid":                   42,
				"title":                 "Add feature",
				"state":                 "opened",
				"draft":                 false,
				"detailed_merge_status": "mergeable",
				"sha":                   "abc123",
				"source_branch":         "feature-branch",
				"target_branch":         "main",
			},
			wantPR: &models.PullRequestDetail{
				Number:    42,
				Title:     "Add feature",
				State:     models.PRStateOpen,
				Draft:     false,
				Merged:    false,
				Mergeable: true,
				HeadSHA:   "abc123",
				HeadRef:   "feature-branch",
				BaseRef:   "main",
			},
		},
		{
			name: "merged merge request",
			mrResponse: map[string]any{
				"iid":                   10,
				"title":                 "Merged MR",
				"state":                 "merged",
				"draft":                 false,
				"detailed_merge_status": "not_open",
				"sha":                   "def456",
				"source_branch":         "fix-branch",
				"target_branch":         "main",
			},
			wantPR: &models.PullRequestDetail{
				Number:    10,
				Title:     "Merged MR",
				State:     models.PRStateClosed,
				Draft:     false,
				Merged:    true,
				Mergeable: false,
				HeadSHA:   "def456",
				HeadRef:   "fix-branch",
				BaseRef:   "main",
			},
		},
		{
			name: "draft merge request",
			mrResponse: map[string]any{
				"iid":                   5,
				"title":                 "WIP: Draft MR",
				"state":                 "opened",
				"draft":                 true,
				"detailed_merge_status": "draft_status",
				"sha":                   "ghi789",
				"source_branch":         "draft-branch",
				"target_branch":         "develop",
			},
			wantPR: &models.PullRequestDetail{
				Number:    5,
				Title:     "WIP: Draft MR",
				State:     models.PRStateOpen,
				Draft:     true,
				Merged:    false,
				Mergeable: false,
				HeadSHA:   "ghi789",
				HeadRef:   "draft-branch",
				BaseRef:   "develop",
			},
		},
		{
			name: "closed merge request",
			mrResponse: map[string]any{
				"iid":                   99,
				"title":                 "Closed MR",
				"state":                 "closed",
				"draft":                 false,
				"detailed_merge_status": "not_open",
				"sha":                   "jkl012",
				"source_branch":         "closed-branch",
				"target_branch":         "main",
			},
			wantPR: &models.PullRequestDetail{
				Number:    99,
				Title:     "Closed MR",
				State:     models.PRStateClosed,
				Draft:     false,
				Merged:    false,
				Mergeable: false,
				HeadSHA:   "jkl012",
				HeadRef:   "closed-branch",
				BaseRef:   "main",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(tt.mrResponse)
			}))
			defer srv.Close()

			client := newTestClient(t, srv.URL)
			pr, err := client.GetPR(context.Background(), "mygroup", "myproject", 42)
			if (err != nil) != tt.wantErr {
				t.Fatalf("GetPR() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}

			if pr.Number != tt.wantPR.Number {
				t.Errorf("Number = %d, want %d", pr.Number, tt.wantPR.Number)
			}
			if pr.Title != tt.wantPR.Title {
				t.Errorf("Title = %q, want %q", pr.Title, tt.wantPR.Title)
			}
			if pr.State != tt.wantPR.State {
				t.Errorf("State = %q, want %q", pr.State, tt.wantPR.State)
			}
			if pr.Draft != tt.wantPR.Draft {
				t.Errorf("Draft = %v, want %v", pr.Draft, tt.wantPR.Draft)
			}
			if pr.Merged != tt.wantPR.Merged {
				t.Errorf("Merged = %v, want %v", pr.Merged, tt.wantPR.Merged)
			}
			if pr.Mergeable != tt.wantPR.Mergeable {
				t.Errorf("Mergeable = %v, want %v", pr.Mergeable, tt.wantPR.Mergeable)
			}
			if pr.HeadSHA != tt.wantPR.HeadSHA {
				t.Errorf("HeadSHA = %q, want %q", pr.HeadSHA, tt.wantPR.HeadSHA)
			}
			if pr.HeadRef != tt.wantPR.HeadRef {
				t.Errorf("HeadRef = %q, want %q", pr.HeadRef, tt.wantPR.HeadRef)
			}
			if pr.BaseRef != tt.wantPR.BaseRef {
				t.Errorf("BaseRef = %q, want %q", pr.BaseRef, tt.wantPR.BaseRef)
			}
		})
	}
}

func TestGetPR_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := newTestClient(t, srv.URL)
	_, err := client.GetPR(context.Background(), "mygroup", "myproject", 1)
	if err == nil {
		t.Fatal("expected error from GetPR when API returns 500")
	}
}

func TestIsPRApproved(t *testing.T) {
	tests := []struct {
		name     string
		response map[string]any
		want     bool
	}{
		{
			name: "approved by one reviewer",
			response: map[string]any{
				"approved_by": []map[string]any{
					{"user": map[string]any{"id": 1, "username": "reviewer"}},
				},
			},
			want: true,
		},
		{
			name: "approved by multiple reviewers",
			response: map[string]any{
				"approved_by": []map[string]any{
					{"user": map[string]any{"id": 1, "username": "reviewer1"}},
					{"user": map[string]any{"id": 2, "username": "reviewer2"}},
				},
			},
			want: true,
		},
		{
			name: "not approved",
			response: map[string]any{
				"approved_by": []map[string]any{},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(tt.response)
			}))
			defer srv.Close()

			client := newTestClient(t, srv.URL)
			got, err := client.IsPRApproved(context.Background(), "group", "project", 1)
			if err != nil {
				t.Fatalf("IsPRApproved() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("IsPRApproved() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsPRApproved_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	client := newTestClient(t, srv.URL)
	_, err := client.IsPRApproved(context.Background(), "group", "project", 1)
	if err == nil {
		t.Fatal("expected error from IsPRApproved when API returns 403")
	}
}

func TestMergePullRequest(t *testing.T) {
	tests := []struct {
		name       string
		title      string
		message    string
		method     string
		wantSquash bool
		wantMsg    string
	}{
		{
			name:       "merge with message",
			title:      "My Title",
			message:    "My commit message",
			method:     "merge",
			wantSquash: false,
			wantMsg:    "My commit message",
		},
		{
			name:       "squash merge with message",
			title:      "Title",
			message:    "Squash message",
			method:     "squash",
			wantSquash: true,
			wantMsg:    "Squash message",
		},
		{
			name:       "merge with title only",
			title:      "My Title",
			message:    "",
			method:     "merge",
			wantSquash: false,
			wantMsg:    "My Title",
		},
		{
			name:       "merge with empty title and message",
			title:      "",
			message:    "",
			method:     "merge",
			wantSquash: false,
			wantMsg:    "",
		},
		{
			name:       "squash with title fallback",
			title:      "Squash Title",
			message:    "",
			method:     "squash",
			wantSquash: true,
			wantMsg:    "Squash Title",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var receivedBody map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPut {
					_ = json.NewDecoder(r.Body).Decode(&receivedBody)
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"iid":   1,
					"state": "merged",
				})
			}))
			defer srv.Close()

			client := newTestClient(t, srv.URL)
			err := client.MergePullRequest(context.Background(), "group", "project", 1, tt.title, tt.message, tt.method)
			if err != nil {
				t.Fatalf("MergePullRequest() error = %v", err)
			}

			if receivedBody != nil {
				if tt.wantSquash {
					if squash, ok := receivedBody["squash"].(bool); !ok || !squash {
						t.Error("expected squash=true in request body")
					}
				}
				if tt.wantMsg != "" {
					if msg, ok := receivedBody["merge_commit_message"].(string); ok && msg != tt.wantMsg {
						t.Errorf("merge_commit_message = %q, want %q", msg, tt.wantMsg)
					}
				}
			}
		})
	}
}

func TestMergePullRequest_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusMethodNotAllowed)
	}))
	defer srv.Close()

	client := newTestClient(t, srv.URL)
	err := client.MergePullRequest(context.Background(), "group", "project", 1, "title", "msg", "merge")
	if err == nil {
		t.Fatal("expected error from MergePullRequest when API returns 405")
	}
}

func TestDeleteBranch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE method, got %s", r.Method)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	client := newTestClient(t, srv.URL)
	err := client.DeleteBranch(context.Background(), "group", "project", "feature-branch")
	if err != nil {
		t.Fatalf("DeleteBranch() error = %v", err)
	}
}

func TestDeleteBranch_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := newTestClient(t, srv.URL)
	err := client.DeleteBranch(context.Background(), "group", "project", "nonexistent")
	if err == nil {
		t.Fatal("expected error from DeleteBranch when API returns 404")
	}
}
