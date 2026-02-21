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
	"strings"
	"testing"
)

func TestBuildStaleBody(t *testing.T) {
	body := CommentMarker + "\n" + PlanCommentMarker + "\n" +
		"## Lemuria Plan\n\n" +
		"### Application: `my-app`\n\n" +
		"Some diff content\n"

	result := buildStaleBody(body)

	// Should contain stale marker
	if !strings.Contains(result, StaleMarker) {
		t.Error("expected stale marker in output")
	}

	// Should contain stale notice
	if !strings.Contains(result, StaleNotice) {
		t.Error("expected stale notice in output")
	}

	// Should wrap content in collapsible details block
	if !strings.Contains(result, "<details>") {
		t.Error("expected <details> tag in output")
	}
	if !strings.Contains(result, "<summary>Show outdated plan</summary>") {
		t.Error("expected collapsible summary in output")
	}
	if !strings.Contains(result, "</details>") {
		t.Error("expected closing </details> tag in output")
	}

	// The plan content should be inside the details block
	detailsStart := strings.Index(result, "<details>")
	detailsEnd := strings.Index(result, "</details>")
	planIdx := strings.Index(result, "## Lemuria Plan")
	if planIdx < detailsStart || planIdx > detailsEnd {
		t.Error("expected plan content to be inside <details> block")
	}

	// Stale notice should be before the details block
	noticeIdx := strings.Index(result, StaleNotice)
	if noticeIdx > detailsStart {
		t.Error("expected stale notice to be before <details> block")
	}
}

func TestBuildStaleBodyNoHeading(t *testing.T) {
	body := CommentMarker + "\n" + PlanCommentMarker + "\n" +
		"Some content without headings\n"

	result := buildStaleBody(body)

	// Should still contain stale marker
	if !strings.Contains(result, StaleMarker) {
		t.Error("expected stale marker in output")
	}

	// Without a heading, content is not wrapped in details
	if strings.Contains(result, "<details>") {
		t.Error("expected no <details> tag when no heading found")
	}
}

func TestBuildStaleBody_TableDriven(t *testing.T) {
	tests := []struct {
		name           string
		body           string
		wantStale      bool
		wantDetails    bool
		wantNotice     bool
		wantContains   []string
		wantNotContain []string
	}{
		{
			name: "heading with ##",
			body: CommentMarker + "\n" + PlanCommentMarker + "\n" +
				"## Plan Output\n\nSome content",
			wantStale:   true,
			wantDetails: true,
			wantNotice:  true,
			wantContains: []string{
				StaleMarker,
				"<details>",
				"</details>",
				"<summary>Show outdated plan</summary>",
				"## Plan Output",
			},
		},
		{
			name: "heading with single #",
			body: CommentMarker + "\n" + PlanCommentMarker + "\n" +
				"# Big Heading\n\nContent here",
			wantStale:   true,
			wantDetails: true,
			wantNotice:  true,
			wantContains: []string{
				StaleMarker,
				"<details>",
				"# Big Heading",
			},
		},
		{
			name: "no heading at all",
			body: CommentMarker + "\n" + PlanCommentMarker + "\n" +
				"Just plain text without any heading",
			wantStale:      true,
			wantDetails:    false,
			wantNotice:     false,
			wantContains:   []string{StaleMarker},
			wantNotContain: []string{"<details>", StaleNotice},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildStaleBody(tt.body)

			for _, s := range tt.wantContains {
				if !strings.Contains(result, s) {
					t.Errorf("expected result to contain %q", s)
				}
			}
			for _, s := range tt.wantNotContain {
				if strings.Contains(result, s) {
					t.Errorf("expected result NOT to contain %q", s)
				}
			}
		})
	}
}

func TestMaxCommentSize(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	client := newTestClient(t, srv.URL)
	got := client.MaxCommentSize()
	if got != 990000 {
		t.Errorf("MaxCommentSize() = %d, want 990000", got)
	}

	// Must be less than the GitLab absolute limit
	if got >= MaxNoteBodySize {
		t.Errorf("MaxCommentSize() = %d should be less than MaxNoteBodySize = %d", got, MaxNoteBodySize)
	}
}

func TestPostComment(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		isPlan     bool
		wantMarker string
	}{
		{
			name:       "non-plan comment",
			body:       "Sync completed successfully",
			isPlan:     false,
			wantMarker: CommentMarker,
		},
		{
			name:       "plan comment",
			body:       "## Plan\nSome diff",
			isPlan:     true,
			wantMarker: PlanCommentMarker,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var receivedBody string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var reqBody map[string]string
				_ = json.NewDecoder(r.Body).Decode(&reqBody)
				receivedBody = reqBody["body"]

				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"id":   123,
					"body": receivedBody,
				})
			}))
			defer srv.Close()

			client := newTestClient(t, srv.URL)
			result, err := client.PostComment(context.Background(), "group", "project", 1, tt.body, tt.isPlan)
			if err != nil {
				t.Fatalf("PostComment() error = %v", err)
			}

			if result.ID != 123 {
				t.Errorf("comment ID = %d, want 123", result.ID)
			}

			// Verify the posted body contains the appropriate markers
			if !strings.Contains(receivedBody, CommentMarker) {
				t.Error("posted body should always contain CommentMarker")
			}
			if tt.isPlan && !strings.Contains(receivedBody, PlanCommentMarker) {
				t.Error("plan comment should contain PlanCommentMarker")
			}
			if !tt.isPlan && strings.Contains(receivedBody, PlanCommentMarker) {
				t.Error("non-plan comment should not contain PlanCommentMarker")
			}
			if !strings.Contains(receivedBody, tt.body) {
				t.Errorf("posted body should contain the original body %q", tt.body)
			}
		})
	}
}

func TestPostComment_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	client := newTestClient(t, srv.URL)
	_, err := client.PostComment(context.Background(), "group", "project", 1, "test", false)
	if err == nil {
		t.Fatal("expected error from PostComment when API returns 403")
	}
}

func TestUpdateComment(t *testing.T) {
	var receivedBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]string
		_ = json.NewDecoder(r.Body).Decode(&reqBody)
		receivedBody = reqBody["body"]

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":   456,
			"body": receivedBody,
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv.URL)
	err := client.UpdateComment(context.Background(), "group", "project", 1, 456, "Updated body")
	if err != nil {
		t.Fatalf("UpdateComment() error = %v", err)
	}

	if !strings.Contains(receivedBody, CommentMarker) {
		t.Error("updated body should contain CommentMarker")
	}
	if !strings.Contains(receivedBody, "Updated body") {
		t.Error("updated body should contain the new text")
	}
}

func TestUpdateComment_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := newTestClient(t, srv.URL)
	err := client.UpdateComment(context.Background(), "group", "project", 1, 999, "body")
	if err == nil {
		t.Fatal("expected error from UpdateComment when API returns 404")
	}
}

func TestInvalidatePlanComments(t *testing.T) {
	tests := []struct {
		name           string
		notes          []map[string]any
		wantUpdated    int
		wantNotUpdated int
	}{
		{
			name: "invalidates plan comment",
			notes: []map[string]any{
				{
					"id":   1,
					"body": CommentMarker + "\n" + PlanCommentMarker + "\n## Plan\nDiff here",
				},
			},
			wantUpdated: 1,
		},
		{
			name: "skips already stale comment",
			notes: []map[string]any{
				{
					"id":   1,
					"body": CommentMarker + "\n" + PlanCommentMarker + "\n" + StaleMarker + "\n## Plan\nDiff here",
				},
			},
			wantUpdated: 0,
		},
		{
			name: "skips non-plan comment",
			notes: []map[string]any{
				{
					"id":   1,
					"body": CommentMarker + "\nSync completed",
				},
			},
			wantUpdated: 0,
		},
		{
			name:        "no notes",
			notes:       []map[string]any{},
			wantUpdated: 0,
		},
		{
			name: "mixed notes - only updates plan comments",
			notes: []map[string]any{
				{
					"id":   1,
					"body": CommentMarker + "\n" + PlanCommentMarker + "\n## Plan\nDiff1",
				},
				{
					"id":   2,
					"body": CommentMarker + "\nSync completed",
				},
				{
					"id":   3,
					"body": CommentMarker + "\n" + PlanCommentMarker + "\n" + StaleMarker + "\n## Plan\nAlready stale",
				},
			},
			wantUpdated: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			updateCount := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.Method {
				case http.MethodGet:
					// List notes
					_ = json.NewEncoder(w).Encode(tt.notes)
				case http.MethodPut:
					// Update note
					updateCount++
					_ = json.NewEncoder(w).Encode(map[string]any{"id": 1, "body": "updated"})
				}
			}))
			defer srv.Close()

			client := newTestClient(t, srv.URL)
			err := client.InvalidatePlanComments(context.Background(), "group", "project", 1)
			if err != nil {
				t.Fatalf("InvalidatePlanComments() error = %v", err)
			}

			if updateCount != tt.wantUpdated {
				t.Errorf("update calls = %d, want %d", updateCount, tt.wantUpdated)
			}
		})
	}
}

func TestInvalidatePlanComments_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := newTestClient(t, srv.URL)
	err := client.InvalidatePlanComments(context.Background(), "group", "project", 1)
	if err == nil {
		t.Fatal("expected error from InvalidatePlanComments when API returns 500")
	}
}

func TestAddReaction(t *testing.T) {
	tests := []struct {
		name      string
		reaction  string
		wantEmoji string
	}{
		{name: "thumbs up", reaction: "+1", wantEmoji: "thumbsup"},
		{name: "thumbs down", reaction: "-1", wantEmoji: "thumbsdown"},
		{name: "hooray", reaction: "hooray", wantEmoji: "tada"},
		{name: "tada", reaction: "tada", wantEmoji: "tada"},
		{name: "laugh", reaction: "laugh", wantEmoji: "laughing"},
		{name: "passthrough emoji", reaction: "rocket", wantEmoji: "rocket"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedEmoji string
			requestCount := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requestCount++
				w.Header().Set("Content-Type", "application/json")

				if requestCount == 1 {
					// First request: GET note info to resolve MR IID
					_ = json.NewEncoder(w).Encode(map[string]any{
						"id":            100,
						"noteable_iid":  42,
						"noteable_type": "MergeRequest",
					})
					return
				}

				// Second request: create award emoji
				var body map[string]string
				_ = json.NewDecoder(r.Body).Decode(&body)
				capturedEmoji = body["name"]

				_ = json.NewEncoder(w).Encode(map[string]any{
					"id":   1,
					"name": capturedEmoji,
				})
			}))
			defer srv.Close()

			client := newTestClient(t, srv.URL)
			err := client.AddReaction(context.Background(), "group", "project", 100, tt.reaction)
			if err != nil {
				t.Fatalf("AddReaction() error = %v", err)
			}

			if capturedEmoji != tt.wantEmoji {
				t.Errorf("emoji sent = %q, want %q", capturedEmoji, tt.wantEmoji)
			}
		})
	}
}

func TestAddReaction_NoteInfoError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := newTestClient(t, srv.URL)
	err := client.AddReaction(context.Background(), "group", "project", 999, "+1")
	if err == nil {
		t.Fatal("expected error from AddReaction when note lookup fails")
	}
}

func TestAddReaction_ZeroNoteableIID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":           100,
			"noteable_iid": 0,
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv.URL)
	err := client.AddReaction(context.Background(), "group", "project", 100, "+1")
	if err == nil {
		t.Fatal("expected error from AddReaction when noteable_iid is 0")
	}
	if !strings.Contains(err.Error(), "could not determine MR IID") {
		t.Errorf("expected 'could not determine MR IID' error, got: %v", err)
	}
}
