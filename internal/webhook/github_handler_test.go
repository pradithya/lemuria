package webhook

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/org/lemuria/internal/config"
)

func TestGitHubHandlerHandle(t *testing.T) {
	const secret = "gh-test-secret"

	validPRPayload := `{
		"action": "opened",
		"number": 1,
		"pull_request": {
			"number": 1,
			"title": "Test PR",
			"state": "open",
			"draft": false,
			"merged": false,
			"head": {"sha": "abc123", "ref": "feature"},
			"base": {"ref": "main"},
			"user": {"login": "dev", "id": 1},
			"html_url": "https://github.com/org/repo/pull/1"
		},
		"repository": {
			"name": "repo",
			"full_name": "org/repo",
			"owner": {"login": "org"},
			"clone_url": "https://github.com/org/repo.git",
			"html_url": "https://github.com/org/repo"
		},
		"sender": {"login": "dev", "id": 1}
	}`

	issueCommentPayload := `{
		"action": "created",
		"issue": {
			"number": 5,
			"pull_request": {"url": "https://api.github.com/repos/org/repo/pulls/5"}
		},
		"comment": {
			"id": 999,
			"body": "looks good to me",
			"user": {"login": "reviewer", "id": 2},
			"created_at": "2024-01-15T10:30:00Z"
		},
		"repository": {
			"name": "repo",
			"full_name": "org/repo",
			"owner": {"login": "org"},
			"clone_url": "https://github.com/org/repo.git",
			"html_url": "https://github.com/org/repo"
		},
		"sender": {"login": "reviewer", "id": 2}
	}`

	tests := []struct {
		name         string
		body         string
		eventType    string
		signature    string
		allowedRepos []string
		autoplan     bool
		wantStatus   int
		wantJSON     string // expected "status" field in JSON response
	}{
		{
			name:       "missing signature returns 401",
			body:       validPRPayload,
			eventType:  "pull_request",
			signature:  "",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "wrong signature returns 401",
			body:       validPRPayload,
			eventType:  "pull_request",
			signature:  "sha256=0000000000000000000000000000000000000000000000000000000000000000",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "malformed JSON returns 400",
			body:       `{invalid json`,
			eventType:  "pull_request",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "unhandled event type returns ignored",
			body:       `{}`,
			eventType:  "push",
			wantStatus: http.StatusOK,
			wantJSON:   "ignored",
		},
		{
			name:         "repo not in allowlist",
			body:         validPRPayload,
			eventType:    "pull_request",
			allowedRepos: []string{"other-org/other-repo"},
			wantStatus:   http.StatusOK,
			wantJSON:     "repo not allowed",
		},
		{
			name:       "valid PR event accepted (autoplan off)",
			body:       validPRPayload,
			eventType:  "pull_request",
			autoplan:   false,
			wantStatus: http.StatusOK,
			wantJSON:   "accepted",
		},
		{
			name:       "valid PR event accepted (empty allowlist allows all)",
			body:       validPRPayload,
			eventType:  "pull_request",
			autoplan:   false,
			wantStatus: http.StatusOK,
			wantJSON:   "accepted",
		},
		{
			name:         "valid PR event accepted (repo matches allowlist)",
			body:         validPRPayload,
			eventType:    "pull_request",
			allowedRepos: []string{"org/*"},
			autoplan:     false,
			wantStatus:   http.StatusOK,
			wantJSON:     "accepted",
		},
		{
			name:       "issue_comment on PR accepted",
			body:       issueCommentPayload,
			eventType:  "issue_comment",
			autoplan:   false,
			wantStatus: http.StatusOK,
			wantJSON:   "accepted",
		},
		{
			name:       "pull_request_review accepted",
			body: `{
				"action": "submitted",
				"review": {
					"state": "approved",
					"user": {"login": "reviewer", "id": 2}
				},
				"pull_request": {
					"number": 1,
					"title": "Test",
					"state": "open",
					"draft": false,
					"merged": false,
					"head": {"sha": "abc", "ref": "feat"},
					"base": {"ref": "main"},
					"user": {"login": "dev", "id": 1},
					"html_url": "https://github.com/org/repo/pull/1"
				},
				"repository": {
					"name": "repo",
					"full_name": "org/repo",
					"owner": {"login": "org"},
					"clone_url": "https://github.com/org/repo.git",
					"html_url": "https://github.com/org/repo"
				},
				"sender": {"login": "reviewer", "id": 2}
			}`,
			eventType:  "pull_request_review",
			wantStatus: http.StatusOK,
			wantJSON:   "accepted",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				GitHub: config.GitHubConfig{
					WebhookSecret: secret,
				},
				Defaults: config.DefaultsConfig{
					Autoplan:     tt.autoplan,
					AllowedRepos: tt.allowedRepos,
				},
			}

			h := &GitHubHandler{
				config:    cfg,
				logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
				validator: NewValidator(secret),
			}

			body := []byte(tt.body)
			sig := tt.signature
			if sig == "" && tt.wantStatus != http.StatusUnauthorized {
				// Auto-compute valid signature for tests that need it
				sig = computeHMAC(body, secret)
			}

			req := httptest.NewRequest(http.MethodPost, "/webhook/github", bytes.NewReader(body))
			req.Header.Set("X-Hub-Signature-256", sig)
			req.Header.Set("X-GitHub-Event", tt.eventType)
			req.Header.Set("X-GitHub-Delivery", "test-delivery-id")

			rec := httptest.NewRecorder()
			h.Handle(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}

			if tt.wantJSON != "" {
				var resp map[string]string
				if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
					t.Fatalf("failed to decode response: %v", err)
				}
				if got := resp["status"]; got != tt.wantJSON {
					t.Errorf("status = %q, want %q", got, tt.wantJSON)
				}
			}
		})
	}
}
