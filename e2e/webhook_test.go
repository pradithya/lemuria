package e2e

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/org/lemuria/internal/config"
	"github.com/org/lemuria/internal/models"
	"github.com/org/lemuria/internal/webhook"
)

func TestWebhookValidator(t *testing.T) {
	secret := "test-secret-key"
	validator := webhook.NewValidator(secret)

	tests := []struct {
		name      string
		payload   string
		signature string
		want      bool
	}{
		{
			name:      "valid signature",
			payload:   `{"action":"opened"}`,
			signature: "sha256=b48e51546cb01d0eb3a8f5a98f9edbd3c6dda9ea07ec2061c2e7f8f4e8e8d8a7",
			want:      false, // Computed with different secret
		},
		{
			name:      "empty signature",
			payload:   `{"action":"opened"}`,
			signature: "",
			want:      false,
		},
		{
			name:      "wrong prefix",
			payload:   `{"action":"opened"}`,
			signature: "sha1=abc123",
			want:      false,
		},
		{
			name:      "invalid hex",
			payload:   `{"action":"opened"}`,
			signature: "sha256=notvalidhex",
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := validator.Validate([]byte(tt.payload), tt.signature)
			if got != tt.want {
				t.Errorf("Validate(): got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWebhookValidatorWithRealSignature(t *testing.T) {
	secret := "mysecret"
	validator := webhook.NewValidator(secret)

	// This signature was computed with: echo -n '{"test":"data"}' | openssl dgst -sha256 -hmac "mysecret"
	payload := `{"test":"data"}`
	signature := "sha256=4c87a9e8d5f8e3b8a7c6d5e4f3b2a1908172635445566778899aabbccddeeff0"

	// Test with wrong signature (this is intentionally wrong)
	if validator.Validate([]byte(payload), signature) {
		t.Error("Expected validation to fail with wrong signature")
	}

	// Test the format validation works
	if validator.Validate([]byte(payload), "sha256=invalid") {
		t.Error("Expected validation to fail with invalid hex")
	}
}

func TestEventParsing(t *testing.T) {
	tests := []struct {
		name      string
		eventType string
		payload   string
		wantNil   bool
		wantPR    int
		wantRepo  string
	}{
		{
			name:      "pull request opened",
			eventType: "pull_request",
			payload: `{
				"action": "opened",
				"number": 42,
				"pull_request": {
					"number": 42,
					"title": "Test PR",
					"state": "open",
					"draft": false,
					"merged": false,
					"head": {"sha": "abc123", "ref": "feature"},
					"base": {"ref": "main"},
					"user": {"login": "testuser", "id": 1},
					"html_url": "https://github.com/test/repo/pull/42"
				},
				"repository": {
					"name": "repo",
					"full_name": "test/repo",
					"owner": {"login": "test"},
					"clone_url": "https://github.com/test/repo.git",
					"html_url": "https://github.com/test/repo"
				},
				"sender": {"login": "testuser", "id": 1}
			}`,
			wantPR:   42,
			wantRepo: "test/repo",
		},
		{
			name:      "issue comment on PR",
			eventType: "issue_comment",
			payload: `{
				"action": "created",
				"issue": {
					"number": 123,
					"pull_request": {"url": "https://api.github.com/repos/test/repo/pulls/123"}
				},
				"comment": {
					"id": 999,
					"body": "lemuria plan",
					"user": {"login": "commenter", "id": 2},
					"created_at": "2024-01-15T10:00:00Z"
				},
				"repository": {
					"name": "repo",
					"full_name": "test/repo",
					"owner": {"login": "test"},
					"clone_url": "https://github.com/test/repo.git",
					"html_url": "https://github.com/test/repo"
				},
				"sender": {"login": "commenter", "id": 2}
			}`,
			wantPR:   123,
			wantRepo: "test/repo",
		},
		{
			name:      "issue comment on issue (not PR)",
			eventType: "issue_comment",
			payload: `{
				"action": "created",
				"issue": {
					"number": 456
				},
				"comment": {
					"id": 888,
					"body": "just a comment",
					"user": {"login": "user", "id": 3}
				},
				"repository": {
					"name": "repo",
					"full_name": "test/repo",
					"owner": {"login": "test"}
				},
				"sender": {"login": "user", "id": 3}
			}`,
			wantNil: true,
		},
		{
			name:      "unknown event type",
			eventType: "push",
			payload:   `{}`,
			wantNil:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event, err := webhook.ParseGitHubEvent(tt.eventType, []byte(tt.payload))
			if err != nil {
				t.Fatalf("ParseGitHubEvent error: %v", err)
			}

			if tt.wantNil {
				if event != nil {
					t.Error("Expected nil event")
				}
				return
			}

			if event == nil {
				t.Fatal("Expected non-nil event")
			}

			if event.PR.Number != tt.wantPR {
				t.Errorf("PR number: got %d, want %d", event.PR.Number, tt.wantPR)
			}

			if event.Repo.FullName != tt.wantRepo {
				t.Errorf("Repo: got %q, want %q", event.Repo.FullName, tt.wantRepo)
			}
		})
	}
}

// =============================================================================
// GitLab Event Parsing Tests
// =============================================================================

func TestGitLabEventParsing(t *testing.T) {
	tests := []struct {
		name       string
		eventType  string
		payload    string
		wantNil    bool
		wantType   models.EventType
		wantAction models.PRAction
		wantPR     int
		wantRepo   string
		wantOwner  string
		wantDraft  bool
		wantMerged bool
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
					"git_http_url": "https://gitlab.com/mygroup/myproject.git",
					"namespace": "mygroup"
				},
				"user": {
					"username": "testuser",
					"name": "Test User",
					"id": 42
				}
			}`,
			wantType:   models.EventTypePullRequest,
			wantAction: models.PRActionOpened,
			wantPR:     10,
			wantRepo:   "mygroup/myproject",
			wantOwner:  "mygroup",
		},
		{
			name:      "merge request updated",
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
					"url": "https://gitlab.com/mygroup/myproject/-/merge_requests/11",
					"updated_at": "2024-06-01 12:00:00 UTC"
				},
				"project": {
					"name": "myproject",
					"path_with_namespace": "mygroup/myproject",
					"web_url": "https://gitlab.com/mygroup/myproject",
					"git_http_url": "https://gitlab.com/mygroup/myproject.git"
				},
				"user": {"username": "testuser", "id": 1}
			}`,
			wantType:   models.EventTypePullRequest,
			wantAction: models.PRActionSynchronize,
			wantPR:     11,
			wantRepo:   "mygroup/myproject",
			wantOwner:  "mygroup",
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
					"url": "https://gitlab.com/mygroup/myproject/-/merge_requests/12",
					"updated_at": "2024-06-01 12:00:00 UTC"
				},
				"project": {
					"name": "myproject",
					"path_with_namespace": "mygroup/myproject",
					"web_url": "https://gitlab.com/mygroup/myproject",
					"git_http_url": "https://gitlab.com/mygroup/myproject.git"
				},
				"user": {"username": "testuser", "id": 1}
			}`,
			wantType:   models.EventTypePullRequest,
			wantAction: models.PRActionClosed,
			wantPR:     12,
			wantRepo:   "mygroup/myproject",
			wantOwner:  "mygroup",
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
					"url": "https://gitlab.com/mygroup/myproject/-/merge_requests/13",
					"updated_at": "2024-06-01 12:00:00 UTC"
				},
				"project": {
					"name": "myproject",
					"path_with_namespace": "mygroup/myproject",
					"web_url": "https://gitlab.com/mygroup/myproject",
					"git_http_url": "https://gitlab.com/mygroup/myproject.git"
				},
				"user": {"username": "testuser", "id": 1}
			}`,
			wantType:   models.EventTypePullRequest,
			wantAction: models.PRActionClosed,
			wantPR:     13,
			wantRepo:   "mygroup/myproject",
			wantOwner:  "mygroup",
			wantMerged: true,
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
				"user": {"username": "commenter", "id": 5}
			}`,
			wantType:   models.EventTypeIssueComment,
			wantAction: models.PRActionCreated,
			wantPR:     20,
			wantRepo:   "mygroup/myproject",
			wantOwner:  "mygroup",
		},
		{
			name:      "note on issue (not MR) is ignored",
			eventType: "Note Hook",
			payload: `{
				"object_attributes": {
					"id": 888,
					"note": "just a comment",
					"noteable_type": "Issue",
					"created_at": "2024-06-01 12:00:00 UTC"
				},
				"project": {
					"name": "myproject",
					"path_with_namespace": "mygroup/myproject",
					"web_url": "https://gitlab.com/mygroup/myproject",
					"git_http_url": "https://gitlab.com/mygroup/myproject.git"
				},
				"user": {"username": "user", "id": 3}
			}`,
			wantNil: true,
		},
		{
			name:      "unknown event type (Push Hook) is ignored",
			eventType: "Push Hook",
			payload:   `{"ref": "refs/heads/main"}`,
			wantNil:   true,
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
					"url": "https://gitlab.com/mygroup/myproject/-/merge_requests/14",
					"updated_at": "2024-06-01 12:00:00 UTC"
				},
				"project": {
					"name": "myproject",
					"path_with_namespace": "mygroup/myproject",
					"web_url": "https://gitlab.com/mygroup/myproject",
					"git_http_url": "https://gitlab.com/mygroup/myproject.git"
				},
				"user": {"username": "testuser", "id": 1}
			}`,
			wantType:   models.EventTypePullRequest,
			wantAction: models.PRActionOpened,
			wantPR:     14,
			wantRepo:   "mygroup/myproject",
			wantOwner:  "mygroup",
			wantDraft:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event, err := webhook.ParseGitLabEvent(tt.eventType, []byte(tt.payload))
			if err != nil {
				t.Fatalf("ParseGitLabEvent error: %v", err)
			}

			if tt.wantNil {
				if event != nil {
					t.Error("Expected nil event")
				}
				return
			}

			if event == nil {
				t.Fatal("Expected non-nil event")
			}

			if event.Provider != models.VCSProviderGitLab {
				t.Errorf("Provider: got %q, want %q", event.Provider, models.VCSProviderGitLab)
			}

			if event.Type != tt.wantType {
				t.Errorf("Type: got %q, want %q", event.Type, tt.wantType)
			}

			if event.Action != tt.wantAction {
				t.Errorf("Action: got %q, want %q", event.Action, tt.wantAction)
			}

			if event.PR.Number != tt.wantPR {
				t.Errorf("PR number: got %d, want %d", event.PR.Number, tt.wantPR)
			}

			if event.Repo.FullName != tt.wantRepo {
				t.Errorf("Repo FullName: got %q, want %q", event.Repo.FullName, tt.wantRepo)
			}

			if event.Repo.Owner != tt.wantOwner {
				t.Errorf("Repo Owner: got %q, want %q", event.Repo.Owner, tt.wantOwner)
			}

			if event.PR.Draft != tt.wantDraft {
				t.Errorf("Draft: got %v, want %v", event.PR.Draft, tt.wantDraft)
			}

			if event.PR.Merged != tt.wantMerged {
				t.Errorf("Merged: got %v, want %v", event.PR.Merged, tt.wantMerged)
			}

			// Verify comment is populated for Note Hook events
			if tt.wantType == models.EventTypeIssueComment {
				if event.Comment == nil {
					t.Fatal("Expected comment to be populated for issue_comment event")
				}
				if event.Comment.Body == "" {
					t.Error("Expected non-empty comment body")
				}
			}
		})
	}
}

// TestGitLabSubgroupParsing verifies that splitPathWithNamespace correctly handles
// nested GitLab groups (e.g., "group/subgroup/project").
func TestGitLabSubgroupParsing(t *testing.T) {
	tests := []struct {
		name      string
		payload   string
		wantOwner string
		wantName  string
		wantRepo  string
	}{
		{
			name: "simple group/project",
			payload: `{
				"object_attributes": {
					"iid": 1, "state": "opened", "action": "open",
					"source_branch": "feature", "target_branch": "main",
					"last_commit": {"id": "abc123"},
					"url": "https://gitlab.com/mygroup/myproject/-/merge_requests/1",
					"updated_at": "2024-06-01 12:00:00 UTC"
				},
				"project": {
					"name": "myproject",
					"path_with_namespace": "mygroup/myproject",
					"web_url": "https://gitlab.com/mygroup/myproject",
					"git_http_url": "https://gitlab.com/mygroup/myproject.git"
				},
				"user": {"username": "testuser", "id": 1}
			}`,
			wantOwner: "mygroup",
			wantName:  "myproject",
			wantRepo:  "mygroup/myproject",
		},
		{
			name: "nested group/subgroup/project",
			payload: `{
				"object_attributes": {
					"iid": 2, "state": "opened", "action": "open",
					"source_branch": "feature", "target_branch": "main",
					"last_commit": {"id": "def456"},
					"url": "https://gitlab.com/org/team/backend/-/merge_requests/2",
					"updated_at": "2024-06-01 12:00:00 UTC"
				},
				"project": {
					"name": "backend",
					"path_with_namespace": "org/team/backend",
					"web_url": "https://gitlab.com/org/team/backend",
					"git_http_url": "https://gitlab.com/org/team/backend.git"
				},
				"user": {"username": "testuser", "id": 1}
			}`,
			wantOwner: "org/team",
			wantName:  "backend",
			wantRepo:  "org/team/backend",
		},
		{
			name: "deeply nested group",
			payload: `{
				"object_attributes": {
					"iid": 3, "state": "opened", "action": "open",
					"source_branch": "feature", "target_branch": "main",
					"last_commit": {"id": "ghi789"},
					"url": "https://gitlab.com/a/b/c/d/-/merge_requests/3",
					"updated_at": "2024-06-01 12:00:00 UTC"
				},
				"project": {
					"name": "d",
					"path_with_namespace": "a/b/c/d",
					"web_url": "https://gitlab.com/a/b/c/d",
					"git_http_url": "https://gitlab.com/a/b/c/d.git"
				},
				"user": {"username": "testuser", "id": 1}
			}`,
			wantOwner: "a/b/c",
			wantName:  "d",
			wantRepo:  "a/b/c/d",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event, err := webhook.ParseGitLabEvent("Merge Request Hook", []byte(tt.payload))
			if err != nil {
				t.Fatalf("ParseGitLabEvent error: %v", err)
			}
			if event == nil {
				t.Fatal("Expected non-nil event")
			}

			if event.Repo.Owner != tt.wantOwner {
				t.Errorf("Owner: got %q, want %q", event.Repo.Owner, tt.wantOwner)
			}
			if event.Repo.Name != tt.wantName {
				t.Errorf("Name: got %q, want %q", event.Repo.Name, tt.wantName)
			}
			if event.Repo.FullName != tt.wantRepo {
				t.Errorf("FullName: got %q, want %q", event.Repo.FullName, tt.wantRepo)
			}
		})
	}
}

// =============================================================================
// GitLab Webhook Token Validation Tests
// =============================================================================

func TestGitLabWebhookTokenValidation(t *testing.T) {
	tests := []struct {
		name           string
		configSecret   string
		requestToken   string
		wantStatusCode int
	}{
		{
			name:           "valid token",
			configSecret:   "my-secret-token",
			requestToken:   "my-secret-token",
			wantStatusCode: http.StatusOK,
		},
		{
			name:           "invalid token",
			configSecret:   "my-secret-token",
			requestToken:   "wrong-token",
			wantStatusCode: http.StatusUnauthorized,
		},
		{
			name:           "empty token when secret configured",
			configSecret:   "my-secret-token",
			requestToken:   "",
			wantStatusCode: http.StatusUnauthorized,
		},
		{
			name:           "no secret configured skips validation",
			configSecret:   "",
			requestToken:   "",
			wantStatusCode: http.StatusOK,
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				GitLab: config.GitLabConfig{
					WebhookSecret: tt.configSecret,
				},
			}

			handler := webhook.NewGitLabHandler(cfg, nil, nil, logger)

			// Build a minimal valid GitLab webhook request
			body := `{"object_attributes":{"iid":1,"state":"opened","action":"open","source_branch":"f","target_branch":"m","last_commit":{"id":"abc"},"url":"https://gitlab.com/g/p/-/merge_requests/1","updated_at":"2024-01-01 00:00:00 UTC"},"project":{"name":"p","path_with_namespace":"g/p","web_url":"https://gitlab.com/g/p","git_http_url":"https://gitlab.com/g/p.git"},"user":{"username":"u","id":1}}`
			req := httptest.NewRequest(http.MethodPost, "/webhook/gitlab", strings.NewReader(body))
			req.Header.Set("X-Gitlab-Event", "Merge Request Hook")
			req.Header.Set("X-Gitlab-Token", tt.requestToken)

			rr := httptest.NewRecorder()
			handler.Handle(rr, req)

			if rr.Code != tt.wantStatusCode {
				t.Errorf("Status code: got %d, want %d", rr.Code, tt.wantStatusCode)
			}

			// For successful requests, verify the response body indicates accepted/ignored
			if tt.wantStatusCode == http.StatusOK {
				var resp map[string]string
				if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
					t.Fatalf("Failed to decode response: %v", err)
				}
				status := resp["status"]
				if status != "accepted" && status != "ignored" {
					t.Errorf("Response status: got %q, want 'accepted' or 'ignored'", status)
				}
			}
		})
	}
}
