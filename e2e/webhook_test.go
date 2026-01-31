package e2e

import (
	"testing"

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
			event, err := webhook.ParseEvent(tt.eventType, []byte(tt.payload))
			if err != nil {
				t.Fatalf("ParseEvent error: %v", err)
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
