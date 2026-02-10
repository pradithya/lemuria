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
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/org/lemuria/internal/config"
)

func TestGitLabHandlerHandle(t *testing.T) {
	const secret = "gl-test-secret"

	validMRPayload := `{
		"object_kind": "merge_request",
		"event_type": "merge_request",
		"project": {
			"path_with_namespace": "group/repo",
			"web_url": "https://gitlab.com/group/repo",
			"http_url_to_repo": "https://gitlab.com/group/repo.git"
		},
		"object_attributes": {
			"iid": 1,
			"title": "Test MR",
			"description": "Test description",
			"state": "opened",
			"action": "open",
			"work_in_progress": false,
			"draft": false,
			"source_branch": "feature",
			"target_branch": "main",
			"last_commit": {"id": "abc123"},
			"url": "https://gitlab.com/group/repo/-/merge_requests/1"
		},
		"user": {"username": "dev", "id": 1, "avatar_url": ""}
	}`

	notePayload := `{
		"object_kind": "note",
		"event_type": "note",
		"project": {
			"path_with_namespace": "group/repo",
			"web_url": "https://gitlab.com/group/repo",
			"http_url_to_repo": "https://gitlab.com/group/repo.git"
		},
		"object_attributes": {
			"id": 123,
			"note": "just a regular comment",
			"noteable_type": "MergeRequest",
			"created_at": "2024-01-15 10:30:00 UTC"
		},
		"merge_request": {
			"iid": 5,
			"title": "Some MR",
			"state": "opened",
			"source_branch": "feature",
			"target_branch": "main",
			"last_commit": {"id": "def456"},
			"url": "https://gitlab.com/group/repo/-/merge_requests/5"
		},
		"user": {"username": "commenter", "id": 2}
	}`

	tests := []struct {
		name          string
		webhookSecret string
		token         string
		body          string
		eventType     string
		allowedRepos  []string
		autoplan      bool
		wantStatus    int
		wantJSON      string
	}{
		{
			name:          "invalid token returns 401",
			webhookSecret: secret,
			token:         "wrong-token",
			body:          validMRPayload,
			eventType:     "Merge Request Hook",
			wantStatus:    http.StatusUnauthorized,
		},
		{
			name:          "missing token when secret configured returns 401",
			webhookSecret: secret,
			token:         "",
			body:          validMRPayload,
			eventType:     "Merge Request Hook",
			wantStatus:    http.StatusUnauthorized,
		},
		{
			name:          "no secret configured skips validation",
			webhookSecret: "",
			token:         "",
			body:          validMRPayload,
			eventType:     "Merge Request Hook",
			autoplan:      false,
			wantStatus:    http.StatusOK,
			wantJSON:      "accepted",
		},
		{
			name:          "malformed JSON returns 400",
			webhookSecret: secret,
			token:         secret,
			body:          `{not valid json`,
			eventType:     "Merge Request Hook",
			wantStatus:    http.StatusBadRequest,
		},
		{
			name:          "unhandled event type returns ignored",
			webhookSecret: secret,
			token:         secret,
			body:          `{}`,
			eventType:     "Push Hook",
			wantStatus:    http.StatusOK,
			wantJSON:      "ignored",
		},
		{
			name:          "unhandled event type (Tag Push Hook) returns ignored",
			webhookSecret: secret,
			token:         secret,
			body:          `{}`,
			eventType:     "Tag Push Hook",
			wantStatus:    http.StatusOK,
			wantJSON:      "ignored",
		},
		{
			name:          "repo not in allowlist",
			webhookSecret: secret,
			token:         secret,
			body:          validMRPayload,
			eventType:     "Merge Request Hook",
			allowedRepos:  []string{"other-group/other-repo"},
			wantStatus:    http.StatusOK,
			wantJSON:      "repo not allowed",
		},
		{
			name:          "valid MR event accepted (autoplan off)",
			webhookSecret: secret,
			token:         secret,
			body:          validMRPayload,
			eventType:     "Merge Request Hook",
			autoplan:      false,
			wantStatus:    http.StatusOK,
			wantJSON:      "accepted",
		},
		{
			name:          "valid MR event accepted (empty allowlist allows all)",
			webhookSecret: secret,
			token:         secret,
			body:          validMRPayload,
			eventType:     "Merge Request Hook",
			autoplan:      false,
			wantStatus:    http.StatusOK,
			wantJSON:      "accepted",
		},
		{
			name:          "valid MR event accepted (repo matches allowlist)",
			webhookSecret: secret,
			token:         secret,
			body:          validMRPayload,
			eventType:     "Merge Request Hook",
			allowedRepos:  []string{"group/*"},
			autoplan:      false,
			wantStatus:    http.StatusOK,
			wantJSON:      "accepted",
		},
		{
			name:          "note event on MR accepted",
			webhookSecret: secret,
			token:         secret,
			body:          notePayload,
			eventType:     "Note Hook",
			autoplan:      false,
			wantStatus:    http.StatusOK,
			wantJSON:      "accepted",
		},
	}

	// Oversized payload test - body exceeds 10 MB limit
	t.Run("oversized payload returns 400", func(t *testing.T) {
		cfg := &config.Config{
			GitLab: config.GitLabConfig{
				WebhookSecret: secret,
			},
		}

		h := &GitLabHandler{
			config: cfg,
			logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		}

		// Create a body that exceeds 10 MB
		oversized := make([]byte, 10<<20+1) // 10 MB + 1 byte
		for i := range oversized {
			oversized[i] = 'A'
		}

		req := httptest.NewRequest(http.MethodPost, "/webhook/gitlab", bytes.NewReader(oversized))
		req.Header.Set("X-Gitlab-Token", secret)
		req.Header.Set("X-Gitlab-Event", "Merge Request Hook")

		rec := httptest.NewRecorder()
		h.Handle(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want %d for oversized payload", rec.Code, http.StatusBadRequest)
		}
	})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				GitLab: config.GitLabConfig{
					WebhookSecret: tt.webhookSecret,
				},
				Defaults: config.DefaultsConfig{
					Autoplan:     tt.autoplan,
					AllowedRepos: tt.allowedRepos,
				},
			}

			h := &GitLabHandler{
				config: cfg,
				logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
			}

			req := httptest.NewRequest(http.MethodPost, "/webhook/gitlab", bytes.NewReader([]byte(tt.body)))
			req.Header.Set("X-Gitlab-Token", tt.token)
			req.Header.Set("X-Gitlab-Event", tt.eventType)

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

func TestGitLabHandlerValidateToken(t *testing.T) {
	tests := []struct {
		name          string
		webhookSecret string
		token         string
		want          bool
	}{
		{
			name:          "no secret configured allows any token",
			webhookSecret: "",
			token:         "",
			want:          true,
		},
		{
			name:          "no secret configured allows non-empty token",
			webhookSecret: "",
			token:         "some-token",
			want:          true,
		},
		{
			name:          "correct token",
			webhookSecret: "my-secret",
			token:         "my-secret",
			want:          true,
		},
		{
			name:          "wrong token",
			webhookSecret: "my-secret",
			token:         "wrong",
			want:          false,
		},
		{
			name:          "empty token when secret required",
			webhookSecret: "my-secret",
			token:         "",
			want:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &GitLabHandler{
				config: &config.Config{
					GitLab: config.GitLabConfig{
						WebhookSecret: tt.webhookSecret,
					},
				},
			}
			if got := h.validateToken(tt.token); got != tt.want {
				t.Errorf("validateToken() = %v, want %v", got, tt.want)
			}
		})
	}
}
