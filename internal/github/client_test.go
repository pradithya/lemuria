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

package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-github/v60/github"

	"github.com/org/lemuria/internal/models"
)

// mustEncode JSON-encodes v and writes it to w, failing the test on error.
func mustEncode(t *testing.T, w http.ResponseWriter, v interface{}) {
	t.Helper()
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Fatalf("failed to encode response: %v", err)
	}
}

// mustDecode JSON-decodes r.Body into v, failing the test on error.
func mustDecode(t *testing.T, r *http.Request, v interface{}) {
	t.Helper()
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		t.Fatalf("failed to decode request: %v", err)
	}
}

// mustWrite writes data to w, failing the test on error.
func mustWrite(t *testing.T, w http.ResponseWriter, data []byte) {
	t.Helper()
	if _, err := w.Write(data); err != nil {
		t.Fatalf("failed to write response: %v", err)
	}
}

// newTestClient creates a Client with a mock HTTP server. The returned client's
// installationClientFunc is wired so that getInstallationClient returns a
// *github.Client pointing at the same test server. This allows testing all
// methods that depend on installation-level auth without real credentials.
func newTestClient(t *testing.T, mux *http.ServeMux) *Client {
	t.Helper()
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	baseURL, _ := url.Parse(ts.URL + "/")

	// The app-level client (used by GetInstallationClient for listing installations).
	appClient := github.NewClient(nil)
	appClient.BaseURL = baseURL

	c := &Client{
		client: appClient,
	}

	// Override installation client creation so every method that calls
	// getInstallationClient gets a client backed by our test server.
	c.installationClientFunc = func(_ context.Context, _ string) (*github.Client, error) {
		instClient := github.NewClient(nil)
		instClient.BaseURL = baseURL
		return instClient, nil
	}

	return c
}

// ==========================================================================
// loadPrivateKey tests
// ==========================================================================

func TestLoadPrivateKey_FromFile(t *testing.T) {
	content := "test-private-key-content"
	keyFile := filepath.Join(t.TempDir(), "test-key.pem")
	if err := os.WriteFile(keyFile, []byte(content), 0600); err != nil {
		t.Fatalf("failed to write test key file: %v", err)
	}

	got, err := loadPrivateKey(keyFile)
	if err != nil {
		t.Fatalf("loadPrivateKey() error = %v", err)
	}
	if string(got) != content {
		t.Errorf("loadPrivateKey() = %q, want %q", string(got), content)
	}
}

func TestLoadPrivateKey_DirectContent(t *testing.T) {
	content := "-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBA...\n-----END RSA PRIVATE KEY-----"
	got, err := loadPrivateKey(content)
	if err != nil {
		t.Fatalf("loadPrivateKey() error = %v", err)
	}
	if string(got) != content {
		t.Errorf("loadPrivateKey() = %q, want %q", string(got), content)
	}
}

func TestLoadPrivateKey_NonexistentPathTreatedAsContent(t *testing.T) {
	fakeKey := "/nonexistent/path/that/does/not/exist.pem"
	got, err := loadPrivateKey(fakeKey)
	if err != nil {
		t.Fatalf("loadPrivateKey() error = %v", err)
	}
	if string(got) != fakeKey {
		t.Errorf("loadPrivateKey() = %q, want %q", string(got), fakeKey)
	}
}

func TestLoadPrivateKey_EmptyString(t *testing.T) {
	got, err := loadPrivateKey("")
	if err != nil {
		t.Fatalf("loadPrivateKey() error = %v", err)
	}
	if string(got) != "" {
		t.Errorf("loadPrivateKey() = %q, want empty string", string(got))
	}
}

// ==========================================================================
// MaxCommentSize / constants tests
// ==========================================================================

func TestMaxCommentSize(t *testing.T) {
	c := &Client{}
	if got := c.MaxCommentSize(); got != 60000 {
		t.Errorf("MaxCommentSize() = %d, want 60000", got)
	}
}

func TestMaxCommentSizeLessThanGitHubLimit(t *testing.T) {
	c := &Client{}
	if c.MaxCommentSize() >= MaxCommentBodySize {
		t.Errorf("MaxCommentSize() = %d should be less than MaxCommentBodySize = %d",
			c.MaxCommentSize(), MaxCommentBodySize)
	}
}

func TestConstants(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"CommentMarker", CommentMarker, "<!-- lemuria -->"},
		{"AppCommentMarker format", fmt.Sprintf(AppCommentMarker, "my-app"), "<!-- lemuria:app:my-app -->"},
		{"PlanCommentMarker", PlanCommentMarker, "<!-- lemuria:plan -->"},
		{"StaleMarker", StaleMarker, "<!-- lemuria:stale -->"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("got %q, want %q", tt.got, tt.want)
			}
		})
	}
}

func TestMaxCommentBodySizeConstant(t *testing.T) {
	if MaxCommentBodySize != 65536 {
		t.Errorf("MaxCommentBodySize = %d, want 65536", MaxCommentBodySize)
	}
}

func TestStaleNoticeContainsWarning(t *testing.T) {
	if len(StaleNotice) == 0 {
		t.Error("StaleNotice should not be empty")
	}
}

// ==========================================================================
// GetInstallationClient tests (directly, without the override)
// ==========================================================================

func TestGetInstallationClient_NoInstallationFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/app/installations", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		mustEncode(t, w, []*github.Installation{})
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	ghClient := github.NewClient(nil)
	u, _ := url.Parse(ts.URL + "/")
	ghClient.BaseURL = u

	c := &Client{client: ghClient, appID: 1}

	_, err := c.GetInstallationClient(context.Background(), "unknown-owner")
	if err == nil {
		t.Fatal("expected error for unknown owner, got nil")
	}
	if !strings.Contains(err.Error(), "no installation found for owner unknown-owner") {
		t.Errorf("error = %q, want to contain 'no installation found'", err.Error())
	}
}

func TestGetInstallationClient_ListInstallationsError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/app/installations", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	ghClient := github.NewClient(nil)
	u, _ := url.Parse(ts.URL + "/")
	ghClient.BaseURL = u

	c := &Client{client: ghClient, appID: 1}

	_, err := c.GetInstallationClient(context.Background(), "testowner")
	if err == nil {
		t.Fatal("expected error for server error, got nil")
	}
	if !strings.Contains(err.Error(), "listing installations") {
		t.Errorf("error = %q, want to contain 'listing installations'", err.Error())
	}
}

func TestGetInstallationClient_MatchesCorrectOwner(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/app/installations", func(w http.ResponseWriter, _ *http.Request) {
		installations := []*github.Installation{
			{
				ID:      github.Int64(10),
				Account: &github.User{Login: github.String("other-org")},
			},
			{
				ID:      github.Int64(20),
				Account: &github.User{Login: github.String("target-org")},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		mustEncode(t, w, installations)
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	ghClient := github.NewClient(nil)
	u, _ := url.Parse(ts.URL + "/")
	ghClient.BaseURL = u

	// Private key must be valid RSA for ghinstallation.New to succeed.
	// We expect an error from ghinstallation because the key is not real,
	// but we can verify the installation lookup found the right ID by
	// checking the error is about creating the transport (not about missing installation).
	c := &Client{client: ghClient, appID: 1, privateKey: []byte("not-a-real-key")}

	_, err := c.GetInstallationClient(context.Background(), "target-org")
	if err == nil {
		t.Fatal("expected error (bad key), got nil")
	}
	// The error should be about the transport, not about missing installation.
	if strings.Contains(err.Error(), "no installation found") {
		t.Errorf("should have found the installation; error = %q", err.Error())
	}
}

// ==========================================================================
// getInstallationClient override tests
// ==========================================================================

func TestGetInstallationClientFunc_Override(t *testing.T) {
	called := false
	c := &Client{
		installationClientFunc: func(_ context.Context, owner string) (*github.Client, error) {
			called = true
			if owner != "my-org" {
				t.Errorf("owner = %q, want %q", owner, "my-org")
			}
			return github.NewClient(nil), nil
		},
	}

	_, err := c.getInstallationClient(context.Background(), "my-org")
	if err != nil {
		t.Fatalf("getInstallationClient() error = %v", err)
	}
	if !called {
		t.Error("installationClientFunc was not called")
	}
}

// ==========================================================================
// CreateComment tests
// ==========================================================================

func TestCreateComment_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/testowner/testrepo/issues/1/comments", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		var body github.IssueComment
		mustDecode(t, r, &body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		mustEncode(t, w, &github.IssueComment{
			ID:   github.Int64(42),
			Body: body.Body,
		})
	})

	c := newTestClient(t, mux)
	comment, err := c.CreateComment(context.Background(), "testowner", "testrepo", 1, "test body")
	if err != nil {
		t.Fatalf("CreateComment() error = %v", err)
	}
	if comment.GetID() != 42 {
		t.Errorf("comment ID = %d, want 42", comment.GetID())
	}
	if comment.GetBody() != "test body" {
		t.Errorf("comment body = %q, want %q", comment.GetBody(), "test body")
	}
}

func TestCreateComment_APIError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/testowner/testrepo/issues/1/comments", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})

	c := newTestClient(t, mux)
	_, err := c.CreateComment(context.Background(), "testowner", "testrepo", 1, "body")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "creating comment") {
		t.Errorf("error = %q, want to contain 'creating comment'", err.Error())
	}
}

// ==========================================================================
// UpdateComment tests
// ==========================================================================

func TestUpdateComment_PrependsMarkerAndCallsEdit(t *testing.T) {
	var receivedBody string
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/testowner/testrepo/issues/comments/99", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PATCH" {
			t.Errorf("expected PATCH, got %s", r.Method)
		}
		var body github.IssueComment
		mustDecode(t, r, &body)
		receivedBody = body.GetBody()
		w.Header().Set("Content-Type", "application/json")
		mustEncode(t, w, &github.IssueComment{
			ID:   github.Int64(99),
			Body: body.Body,
		})
	})

	c := newTestClient(t, mux)
	err := c.UpdateComment(context.Background(), "testowner", "testrepo", 1, 99, "updated body")
	if err != nil {
		t.Fatalf("UpdateComment() error = %v", err)
	}
	expected := CommentMarker + "\n" + "updated body"
	if receivedBody != expected {
		t.Errorf("sent body = %q, want %q", receivedBody, expected)
	}
}

// ==========================================================================
// DeleteComment tests
// ==========================================================================

func TestDeleteComment_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/testowner/testrepo/issues/comments/55", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		w.WriteHeader(http.StatusNoContent)
	})

	c := newTestClient(t, mux)
	err := c.DeleteComment(context.Background(), "testowner", "testrepo", 55)
	if err != nil {
		t.Fatalf("DeleteComment() error = %v", err)
	}
}

func TestDeleteComment_APIError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/testowner/testrepo/issues/comments/55", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	c := newTestClient(t, mux)
	err := c.DeleteComment(context.Background(), "testowner", "testrepo", 55)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "deleting comment") {
		t.Errorf("error = %q, want to contain 'deleting comment'", err.Error())
	}
}

// ==========================================================================
// FindLemuriComment tests
// ==========================================================================

func TestFindLemuriComment_Found(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/testowner/testrepo/issues/1/comments", func(w http.ResponseWriter, _ *http.Request) {
		comments := []*github.IssueComment{
			{ID: github.Int64(10), Body: github.String("some other comment")},
			{ID: github.Int64(20), Body: github.String(CommentMarker + "\nLemuria plan output")},
		}
		w.Header().Set("Content-Type", "application/json")
		mustEncode(t, w, comments)
	})

	c := newTestClient(t, mux)
	comment, err := c.FindLemuriComment(context.Background(), "testowner", "testrepo", 1, "")
	if err != nil {
		t.Fatalf("FindLemuriComment() error = %v", err)
	}
	if comment == nil {
		t.Fatal("expected to find a comment, got nil")
	}
	if comment.GetID() != 20 {
		t.Errorf("found comment ID = %d, want 20", comment.GetID())
	}
}

func TestFindLemuriComment_WithAppName(t *testing.T) {
	appMarker := fmt.Sprintf(AppCommentMarker, "my-app")
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/testowner/testrepo/issues/1/comments", func(w http.ResponseWriter, _ *http.Request) {
		comments := []*github.IssueComment{
			{ID: github.Int64(10), Body: github.String(CommentMarker + "\ngeneric comment")},
			{ID: github.Int64(30), Body: github.String(appMarker + "\napp-specific comment")},
		}
		w.Header().Set("Content-Type", "application/json")
		mustEncode(t, w, comments)
	})

	c := newTestClient(t, mux)
	comment, err := c.FindLemuriComment(context.Background(), "testowner", "testrepo", 1, "my-app")
	if err != nil {
		t.Fatalf("FindLemuriComment() error = %v", err)
	}
	if comment == nil {
		t.Fatal("expected to find a comment, got nil")
	}
	if comment.GetID() != 30 {
		t.Errorf("found comment ID = %d, want 30", comment.GetID())
	}
}

func TestFindLemuriComment_NotFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/testowner/testrepo/issues/1/comments", func(w http.ResponseWriter, _ *http.Request) {
		comments := []*github.IssueComment{
			{ID: github.Int64(10), Body: github.String("regular comment")},
		}
		w.Header().Set("Content-Type", "application/json")
		mustEncode(t, w, comments)
	})

	c := newTestClient(t, mux)
	comment, err := c.FindLemuriComment(context.Background(), "testowner", "testrepo", 1, "")
	if err != nil {
		t.Fatalf("FindLemuriComment() error = %v", err)
	}
	if comment != nil {
		t.Errorf("expected nil comment, got ID=%d", comment.GetID())
	}
}

func TestFindLemuriComment_Pagination(t *testing.T) {
	// We need to capture the server URL for the Link header, so we declare
	// the mux first and set up the server before registering the handler.
	mux := http.NewServeMux()
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	page := 0
	mux.HandleFunc("/repos/testowner/testrepo/issues/1/comments", func(w http.ResponseWriter, _ *http.Request) {
		page++
		w.Header().Set("Content-Type", "application/json")
		if page == 1 {
			w.Header().Set("Link", fmt.Sprintf(`<%s/repos/testowner/testrepo/issues/1/comments?page=2>; rel="next"`, ts.URL))
			mustEncode(t, w, []*github.IssueComment{
				{ID: github.Int64(1), Body: github.String("not a lemuria comment")},
			})
		} else {
			mustEncode(t, w, []*github.IssueComment{
				{ID: github.Int64(2), Body: github.String(CommentMarker + "\nfound it")},
			})
		}
	})

	baseURL, _ := url.Parse(ts.URL + "/")
	c := &Client{
		installationClientFunc: func(_ context.Context, _ string) (*github.Client, error) {
			ghClient := github.NewClient(nil)
			ghClient.BaseURL = baseURL
			return ghClient, nil
		},
	}

	comment, err := c.FindLemuriComment(context.Background(), "testowner", "testrepo", 1, "")
	if err != nil {
		t.Fatalf("FindLemuriComment() error = %v", err)
	}
	if comment == nil {
		t.Fatal("expected to find comment on page 2, got nil")
	}
	if comment.GetID() != 2 {
		t.Errorf("found comment ID = %d, want 2", comment.GetID())
	}
}

func TestFindLemuriComment_APIError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/testowner/testrepo/issues/1/comments", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	c := newTestClient(t, mux)
	_, err := c.FindLemuriComment(context.Background(), "testowner", "testrepo", 1, "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "listing comments") {
		t.Errorf("error = %q, want to contain 'listing comments'", err.Error())
	}
}

// ==========================================================================
// UpsertComment tests
// ==========================================================================

func TestUpsertComment_CreatesWhenNoExistingComment(t *testing.T) {
	var createdBody string
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/testowner/testrepo/issues/1/comments", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			// No existing comments
			w.Header().Set("Content-Type", "application/json")
			mustEncode(t, w, []*github.IssueComment{})
		case http.MethodPost:
			var body github.IssueComment
			mustDecode(t, r, &body)
			createdBody = body.GetBody()
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			mustEncode(t, w, &github.IssueComment{
				ID:   github.Int64(100),
				Body: body.Body,
			})
		}
	})

	c := newTestClient(t, mux)
	comment, err := c.UpsertComment(context.Background(), "testowner", "testrepo", 1, "", "plan output")
	if err != nil {
		t.Fatalf("UpsertComment() error = %v", err)
	}
	if comment.GetID() != 100 {
		t.Errorf("comment ID = %d, want 100", comment.GetID())
	}
	if !strings.Contains(createdBody, CommentMarker) {
		t.Errorf("created body should contain CommentMarker")
	}
	if !strings.Contains(createdBody, "plan output") {
		t.Errorf("created body should contain 'plan output'")
	}
}

func TestUpsertComment_UpdatesExistingComment(t *testing.T) {
	var editedBody string
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/testowner/testrepo/issues/1/comments", func(w http.ResponseWriter, _ *http.Request) {
		// Return an existing lemuria comment
		w.Header().Set("Content-Type", "application/json")
		mustEncode(t, w, []*github.IssueComment{
			{ID: github.Int64(50), Body: github.String(CommentMarker + "\nold plan")},
		})
	})
	mux.HandleFunc("/repos/testowner/testrepo/issues/comments/50", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PATCH" {
			t.Errorf("expected PATCH, got %s", r.Method)
		}
		var body github.IssueComment
		mustDecode(t, r, &body)
		editedBody = body.GetBody()
		w.Header().Set("Content-Type", "application/json")
		mustEncode(t, w, &github.IssueComment{
			ID:   github.Int64(50),
			Body: body.Body,
		})
	})

	c := newTestClient(t, mux)
	comment, err := c.UpsertComment(context.Background(), "testowner", "testrepo", 1, "", "new plan")
	if err != nil {
		t.Fatalf("UpsertComment() error = %v", err)
	}
	if comment.GetID() != 50 {
		t.Errorf("comment ID = %d, want 50", comment.GetID())
	}
	if !strings.Contains(editedBody, "new plan") {
		t.Errorf("edited body should contain 'new plan', got %q", editedBody)
	}
}

func TestUpsertComment_WithAppName(t *testing.T) {
	appMarker := fmt.Sprintf(AppCommentMarker, "my-app")
	var createdBody string
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/testowner/testrepo/issues/1/comments", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			mustEncode(t, w, []*github.IssueComment{})
		case http.MethodPost:
			var body github.IssueComment
			mustDecode(t, r, &body)
			createdBody = body.GetBody()
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			mustEncode(t, w, &github.IssueComment{
				ID:   github.Int64(200),
				Body: body.Body,
			})
		}
	})

	c := newTestClient(t, mux)
	_, err := c.UpsertComment(context.Background(), "testowner", "testrepo", 1, "my-app", "app plan")
	if err != nil {
		t.Fatalf("UpsertComment() error = %v", err)
	}
	if !strings.Contains(createdBody, appMarker) {
		t.Errorf("created body should contain app marker %q, got %q", appMarker, createdBody)
	}
}

// ==========================================================================
// PostComment tests
// ==========================================================================

func TestPostComment_NonPlan(t *testing.T) {
	var postedBody string
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/testowner/testrepo/issues/1/comments", func(w http.ResponseWriter, r *http.Request) {
		var body github.IssueComment
		mustDecode(t, r, &body)
		postedBody = body.GetBody()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		mustEncode(t, w, &github.IssueComment{
			ID:   github.Int64(300),
			Body: body.Body,
		})
	})

	c := newTestClient(t, mux)
	result, err := c.PostComment(context.Background(), "testowner", "testrepo", 1, "sync output", false)
	if err != nil {
		t.Fatalf("PostComment() error = %v", err)
	}
	if result.ID != 300 {
		t.Errorf("result.ID = %d, want 300", result.ID)
	}
	if !strings.Contains(postedBody, CommentMarker) {
		t.Error("posted body should contain CommentMarker")
	}
	if strings.Contains(postedBody, PlanCommentMarker) {
		t.Error("non-plan comment should not contain PlanCommentMarker")
	}
}

func TestPostComment_Plan(t *testing.T) {
	var postedBody string
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/testowner/testrepo/issues/1/comments", func(w http.ResponseWriter, r *http.Request) {
		var body github.IssueComment
		mustDecode(t, r, &body)
		postedBody = body.GetBody()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		mustEncode(t, w, &github.IssueComment{
			ID:   github.Int64(301),
			Body: body.Body,
		})
	})

	c := newTestClient(t, mux)
	result, err := c.PostComment(context.Background(), "testowner", "testrepo", 1, "diff output", true)
	if err != nil {
		t.Fatalf("PostComment() error = %v", err)
	}
	if result.ID != 301 {
		t.Errorf("result.ID = %d, want 301", result.ID)
	}
	if !strings.Contains(postedBody, CommentMarker) {
		t.Error("posted body should contain CommentMarker")
	}
	if !strings.Contains(postedBody, PlanCommentMarker) {
		t.Error("plan comment should contain PlanCommentMarker")
	}
}

// ==========================================================================
// AddReaction tests
// ==========================================================================

func TestAddReaction_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/testowner/testrepo/issues/comments/42/reactions", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		mustEncode(t, w, &github.Reaction{
			ID:      github.Int64(1),
			Content: github.String("+1"),
		})
	})

	c := newTestClient(t, mux)
	err := c.AddReaction(context.Background(), "testowner", "testrepo", 42, "+1")
	if err != nil {
		t.Fatalf("AddReaction() error = %v", err)
	}
}

func TestAddReaction_APIError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/testowner/testrepo/issues/comments/42/reactions", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})

	c := newTestClient(t, mux)
	err := c.AddReaction(context.Background(), "testowner", "testrepo", 42, "+1")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "adding reaction") {
		t.Errorf("error = %q, want to contain 'adding reaction'", err.Error())
	}
}

// ==========================================================================
// InvalidatePlanComments tests
// ==========================================================================

func TestInvalidatePlanComments_MarksStaleAndEdits(t *testing.T) {
	var editedBody string
	planBody := CommentMarker + "\n" + PlanCommentMarker + "\n## Plan\nsome diff"

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/testowner/testrepo/issues/1/comments", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		mustEncode(t, w, []*github.IssueComment{
			{ID: github.Int64(77), NodeID: github.String("MDEyOk"), Body: github.String(planBody)},
		})
	})
	mux.HandleFunc("/repos/testowner/testrepo/issues/comments/77", func(w http.ResponseWriter, r *http.Request) {
		var body github.IssueComment
		mustDecode(t, r, &body)
		editedBody = body.GetBody()
		w.Header().Set("Content-Type", "application/json")
		mustEncode(t, w, &github.IssueComment{
			ID:   github.Int64(77),
			Body: body.Body,
		})
	})
	mux.HandleFunc("/graphql", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		mustWrite(t, w, []byte(`{"data":{"minimizeComment":{"minimizedComment":{"isMinimized":true}}}}`))
	})

	c := newTestClient(t, mux)
	err := c.InvalidatePlanComments(context.Background(), "testowner", "testrepo", 1)
	if err != nil {
		t.Fatalf("InvalidatePlanComments() error = %v", err)
	}
	if !strings.Contains(editedBody, StaleMarker) {
		t.Errorf("edited body should contain StaleMarker, got %q", editedBody)
	}
	if !strings.Contains(editedBody, StaleNotice) {
		t.Errorf("edited body should contain StaleNotice")
	}
}

func TestInvalidatePlanComments_SkipsAlreadyStale(t *testing.T) {
	staleBody := CommentMarker + "\n" + PlanCommentMarker + "\n" + StaleMarker + "\n## Plan\nsome diff"
	editCalled := false

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/testowner/testrepo/issues/1/comments", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		mustEncode(t, w, []*github.IssueComment{
			{ID: github.Int64(77), Body: github.String(staleBody)},
		})
	})
	mux.HandleFunc("/repos/testowner/testrepo/issues/comments/77", func(w http.ResponseWriter, _ *http.Request) {
		editCalled = true
		w.WriteHeader(http.StatusOK)
	})

	c := newTestClient(t, mux)
	err := c.InvalidatePlanComments(context.Background(), "testowner", "testrepo", 1)
	if err != nil {
		t.Fatalf("InvalidatePlanComments() error = %v", err)
	}
	if editCalled {
		t.Error("should not edit already stale comments")
	}
}

func TestInvalidatePlanComments_SkipsNonPlanComments(t *testing.T) {
	editCalled := false

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/testowner/testrepo/issues/1/comments", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		mustEncode(t, w, []*github.IssueComment{
			{ID: github.Int64(77), Body: github.String(CommentMarker + "\njust a sync comment")},
		})
	})
	mux.HandleFunc("/repos/testowner/testrepo/issues/comments/77", func(w http.ResponseWriter, _ *http.Request) {
		editCalled = true
		w.WriteHeader(http.StatusOK)
	})

	c := newTestClient(t, mux)
	err := c.InvalidatePlanComments(context.Background(), "testowner", "testrepo", 1)
	if err != nil {
		t.Fatalf("InvalidatePlanComments() error = %v", err)
	}
	if editCalled {
		t.Error("should not edit non-plan comments")
	}
}

func TestInvalidatePlanComments_NoComments(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/testowner/testrepo/issues/1/comments", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		mustEncode(t, w, []*github.IssueComment{})
	})

	c := newTestClient(t, mux)
	err := c.InvalidatePlanComments(context.Background(), "testowner", "testrepo", 1)
	if err != nil {
		t.Fatalf("InvalidatePlanComments() error = %v", err)
	}
}

// ==========================================================================
// GetPR tests
// ==========================================================================

func TestGetPR_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/testowner/testrepo/pulls/1", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		mustEncode(t, w, &github.PullRequest{
			Number:    github.Int(1),
			Title:     github.String("Test PR"),
			State:     github.String("open"),
			Draft:     github.Bool(false),
			Merged:    github.Bool(false),
			Mergeable: github.Bool(true),
			Head: &github.PullRequestBranch{
				SHA: github.String("abc123"),
				Ref: github.String("feature-branch"),
			},
			Base: &github.PullRequestBranch{
				Ref: github.String("main"),
			},
		})
	})

	c := newTestClient(t, mux)
	detail, err := c.GetPR(context.Background(), "testowner", "testrepo", 1)
	if err != nil {
		t.Fatalf("GetPR() error = %v", err)
	}
	if detail.Number != 1 {
		t.Errorf("Number = %d, want 1", detail.Number)
	}
	if detail.Title != "Test PR" {
		t.Errorf("Title = %q, want %q", detail.Title, "Test PR")
	}
	if detail.State != models.PRState("open") {
		t.Errorf("State = %q, want %q", detail.State, "open")
	}
	if detail.Draft {
		t.Error("Draft should be false")
	}
	if detail.Merged {
		t.Error("Merged should be false")
	}
	if !detail.Mergeable {
		t.Error("Mergeable should be true")
	}
	if detail.HeadSHA != "abc123" {
		t.Errorf("HeadSHA = %q, want %q", detail.HeadSHA, "abc123")
	}
	if detail.HeadRef != "feature-branch" {
		t.Errorf("HeadRef = %q, want %q", detail.HeadRef, "feature-branch")
	}
	if detail.BaseRef != "main" {
		t.Errorf("BaseRef = %q, want %q", detail.BaseRef, "main")
	}
}

func TestGetPR_APIError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/testowner/testrepo/pulls/1", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	c := newTestClient(t, mux)
	_, err := c.GetPR(context.Background(), "testowner", "testrepo", 1)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestGetPRRaw_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/testowner/testrepo/pulls/5", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		mustEncode(t, w, &github.PullRequest{
			Number: github.Int(5),
			Title:  github.String("Raw PR"),
		})
	})

	c := newTestClient(t, mux)
	pr, err := c.GetPRRaw(context.Background(), "testowner", "testrepo", 5)
	if err != nil {
		t.Fatalf("GetPRRaw() error = %v", err)
	}
	if pr.GetNumber() != 5 {
		t.Errorf("PR number = %d, want 5", pr.GetNumber())
	}
	if pr.GetTitle() != "Raw PR" {
		t.Errorf("PR title = %q, want %q", pr.GetTitle(), "Raw PR")
	}
}

// ==========================================================================
// IsPRApproved tests
// ==========================================================================

func TestIsPRApproved_WithApproval(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/testowner/testrepo/pulls/1/reviews", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		mustEncode(t, w, []*github.PullRequestReview{
			{User: &github.User{Login: github.String("reviewer1")}, State: github.String("APPROVED")},
		})
	})

	c := newTestClient(t, mux)
	approved, err := c.IsPRApproved(context.Background(), "testowner", "testrepo", 1)
	if err != nil {
		t.Fatalf("IsPRApproved() error = %v", err)
	}
	if !approved {
		t.Error("expected PR to be approved")
	}
}

func TestIsPRApproved_ChangesRequestedOverridesApproval(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/testowner/testrepo/pulls/1/reviews", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		mustEncode(t, w, []*github.PullRequestReview{
			{User: &github.User{Login: github.String("reviewer1")}, State: github.String("APPROVED")},
			{User: &github.User{Login: github.String("reviewer1")}, State: github.String("CHANGES_REQUESTED")},
		})
	})

	c := newTestClient(t, mux)
	approved, err := c.IsPRApproved(context.Background(), "testowner", "testrepo", 1)
	if err != nil {
		t.Fatalf("IsPRApproved() error = %v", err)
	}
	if approved {
		t.Error("expected PR not to be approved after changes requested")
	}
}

func TestIsPRApproved_MultipleReviewers(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/testowner/testrepo/pulls/1/reviews", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		mustEncode(t, w, []*github.PullRequestReview{
			{User: &github.User{Login: github.String("reviewer1")}, State: github.String("CHANGES_REQUESTED")},
			{User: &github.User{Login: github.String("reviewer2")}, State: github.String("APPROVED")},
		})
	})

	c := newTestClient(t, mux)
	approved, err := c.IsPRApproved(context.Background(), "testowner", "testrepo", 1)
	if err != nil {
		t.Fatalf("IsPRApproved() error = %v", err)
	}
	if !approved {
		t.Error("expected PR to be approved (reviewer2 approved)")
	}
}

func TestIsPRApproved_NoReviews(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/testowner/testrepo/pulls/1/reviews", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		mustEncode(t, w, []*github.PullRequestReview{})
	})

	c := newTestClient(t, mux)
	approved, err := c.IsPRApproved(context.Background(), "testowner", "testrepo", 1)
	if err != nil {
		t.Fatalf("IsPRApproved() error = %v", err)
	}
	if approved {
		t.Error("expected PR not to be approved with no reviews")
	}
}

func TestIsPRApproved_APIError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/testowner/testrepo/pulls/1/reviews", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	c := newTestClient(t, mux)
	_, err := c.IsPRApproved(context.Background(), "testowner", "testrepo", 1)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "listing reviews") {
		t.Errorf("error = %q, want to contain 'listing reviews'", err.Error())
	}
}

func TestIsPRApproved_ReapprovalAfterChangesRequested(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/testowner/testrepo/pulls/1/reviews", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		mustEncode(t, w, []*github.PullRequestReview{
			{User: &github.User{Login: github.String("reviewer1")}, State: github.String("APPROVED")},
			{User: &github.User{Login: github.String("reviewer1")}, State: github.String("CHANGES_REQUESTED")},
			{User: &github.User{Login: github.String("reviewer1")}, State: github.String("APPROVED")},
		})
	})

	c := newTestClient(t, mux)
	approved, err := c.IsPRApproved(context.Background(), "testowner", "testrepo", 1)
	if err != nil {
		t.Fatalf("IsPRApproved() error = %v", err)
	}
	if !approved {
		t.Error("expected PR to be approved after re-approval")
	}
}

// ==========================================================================
// MergePullRequest tests
// ==========================================================================

func TestMergePullRequest_Success(t *testing.T) {
	var receivedMessage string
	var receivedMethod string
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/testowner/testrepo/pulls/1/merge", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PUT" {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		var body map[string]interface{}
		mustDecode(t, r, &body)
		receivedMessage = body["commit_message"].(string)
		receivedMethod = body["merge_method"].(string)
		w.Header().Set("Content-Type", "application/json")
		mustEncode(t, w, &github.PullRequestMergeResult{
			Merged: github.Bool(true),
		})
	})

	c := newTestClient(t, mux)
	err := c.MergePullRequest(context.Background(), "testowner", "testrepo", 1, "PR Title", "Custom message", "squash")
	if err != nil {
		t.Fatalf("MergePullRequest() error = %v", err)
	}
	if receivedMessage != "Custom message" {
		t.Errorf("commit message = %q, want %q", receivedMessage, "Custom message")
	}
	if receivedMethod != "squash" {
		t.Errorf("merge method = %q, want %q", receivedMethod, "squash")
	}
}

func TestMergePullRequest_FallsBackToTitle(t *testing.T) {
	var receivedMessage string
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/testowner/testrepo/pulls/1/merge", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		mustDecode(t, r, &body)
		receivedMessage = body["commit_message"].(string)
		w.Header().Set("Content-Type", "application/json")
		mustEncode(t, w, &github.PullRequestMergeResult{
			Merged: github.Bool(true),
		})
	})

	c := newTestClient(t, mux)
	err := c.MergePullRequest(context.Background(), "testowner", "testrepo", 1, "PR Title", "", "merge")
	if err != nil {
		t.Fatalf("MergePullRequest() error = %v", err)
	}
	if receivedMessage != "PR Title" {
		t.Errorf("commit message = %q, want %q", receivedMessage, "PR Title")
	}
}

func TestMergePullRequest_APIError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/testowner/testrepo/pulls/1/merge", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
	})

	c := newTestClient(t, mux)
	err := c.MergePullRequest(context.Background(), "testowner", "testrepo", 1, "title", "msg", "merge")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "merging PR #1") {
		t.Errorf("error = %q, want to contain 'merging PR #1'", err.Error())
	}
}

// ==========================================================================
// DeleteBranch tests
// ==========================================================================

func TestDeleteBranch_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/testowner/testrepo/git/refs/heads/feature-branch", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		w.WriteHeader(http.StatusNoContent)
	})

	c := newTestClient(t, mux)
	err := c.DeleteBranch(context.Background(), "testowner", "testrepo", "feature-branch")
	if err != nil {
		t.Fatalf("DeleteBranch() error = %v", err)
	}
}

func TestDeleteBranch_APIError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/testowner/testrepo/git/refs/heads/feature-branch", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	c := newTestClient(t, mux)
	err := c.DeleteBranch(context.Background(), "testowner", "testrepo", "feature-branch")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "deleting branch feature-branch") {
		t.Errorf("error = %q, want to contain 'deleting branch feature-branch'", err.Error())
	}
}

// ==========================================================================
// GetRepoConfig tests
// ==========================================================================

func TestGetRepoConfig_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/testowner/testrepo/contents/.lemuria.yaml", func(w http.ResponseWriter, _ *http.Request) {
		// "version: 1" base64 encoded
		w.Header().Set("Content-Type", "application/json")
		mustEncode(t, w, &github.RepositoryContent{
			Type:     github.String("file"),
			Encoding: github.String("base64"),
			Content:  github.String("dmVyc2lvbjogMQ=="),
		})
	})

	c := newTestClient(t, mux)
	data, err := c.GetRepoConfig(context.Background(), "testowner", "testrepo", "main")
	if err != nil {
		t.Fatalf("GetRepoConfig() error = %v", err)
	}
	if string(data) != "version: 1" {
		t.Errorf("config content = %q, want %q", string(data), "version: 1")
	}
}

func TestGetRepoConfig_NotFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/testowner/testrepo/contents/.lemuria.yaml", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	c := newTestClient(t, mux)
	_, err := c.GetRepoConfig(context.Background(), "testowner", "testrepo", "main")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "getting .lemuria.yaml") {
		t.Errorf("error = %q, want to contain 'getting .lemuria.yaml'", err.Error())
	}
}

// ==========================================================================
// GetChangedFiles tests
// ==========================================================================

func TestGetChangedFiles_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/testowner/testrepo/pulls/1/files", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		mustEncode(t, w, []*github.CommitFile{
			{
				Filename:         github.String("apps/deployment.yaml"),
				PreviousFilename: github.String("apps/old-deployment.yaml"),
				Status:           github.String("renamed"),
				Additions:        github.Int(10),
				Deletions:        github.Int(5),
				Changes:          github.Int(15),
				Patch:            github.String("@@ -1,5 +1,10 @@"),
			},
			{
				Filename: github.String("apps/service.yaml"),
				Status:   github.String("added"),
			},
		})
	})

	c := newTestClient(t, mux)
	files, err := c.GetChangedFiles(context.Background(), "testowner", "testrepo", 1)
	if err != nil {
		t.Fatalf("GetChangedFiles() error = %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("got %d files, want 2", len(files))
	}
	if files[0].Filename != "apps/deployment.yaml" {
		t.Errorf("file[0].Filename = %q, want %q", files[0].Filename, "apps/deployment.yaml")
	}
	if files[0].PreviousFilename != "apps/old-deployment.yaml" {
		t.Errorf("file[0].PreviousFilename = %q, want %q", files[0].PreviousFilename, "apps/old-deployment.yaml")
	}
	if files[0].Status != models.FileStatus("renamed") {
		t.Errorf("file[0].Status = %q, want %q", files[0].Status, "renamed")
	}
	if files[0].Additions != 10 {
		t.Errorf("file[0].Additions = %d, want 10", files[0].Additions)
	}
	if files[0].Deletions != 5 {
		t.Errorf("file[0].Deletions = %d, want 5", files[0].Deletions)
	}
	if files[0].Changes != 15 {
		t.Errorf("file[0].Changes = %d, want 15", files[0].Changes)
	}
	if files[0].Patch != "@@ -1,5 +1,10 @@" {
		t.Errorf("file[0].Patch = %q, want %q", files[0].Patch, "@@ -1,5 +1,10 @@")
	}
	if files[1].Filename != "apps/service.yaml" {
		t.Errorf("file[1].Filename = %q, want %q", files[1].Filename, "apps/service.yaml")
	}
	if files[1].Status != models.FileStatus("added") {
		t.Errorf("file[1].Status = %q, want %q", files[1].Status, "added")
	}
}

func TestGetChangedFiles_Pagination(t *testing.T) {
	mux := http.NewServeMux()
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	page := 0
	mux.HandleFunc("/repos/testowner/testrepo/pulls/1/files", func(w http.ResponseWriter, _ *http.Request) {
		page++
		w.Header().Set("Content-Type", "application/json")
		if page == 1 {
			w.Header().Set("Link", fmt.Sprintf(`<%s/repos/testowner/testrepo/pulls/1/files?page=2>; rel="next"`, ts.URL))
			mustEncode(t, w, []*github.CommitFile{
				{Filename: github.String("file1.yaml"), Status: github.String("modified")},
			})
		} else {
			mustEncode(t, w, []*github.CommitFile{
				{Filename: github.String("file2.yaml"), Status: github.String("added")},
			})
		}
	})

	baseURL, _ := url.Parse(ts.URL + "/")
	c := &Client{
		installationClientFunc: func(_ context.Context, _ string) (*github.Client, error) {
			ghClient := github.NewClient(nil)
			ghClient.BaseURL = baseURL
			return ghClient, nil
		},
	}
	files, err := c.GetChangedFiles(context.Background(), "testowner", "testrepo", 1)
	if err != nil {
		t.Fatalf("GetChangedFiles() error = %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("got %d files, want 2", len(files))
	}
	if files[0].Filename != "file1.yaml" {
		t.Errorf("file[0] = %q, want file1.yaml", files[0].Filename)
	}
	if files[1].Filename != "file2.yaml" {
		t.Errorf("file[1] = %q, want file2.yaml", files[1].Filename)
	}
}

func TestGetChangedFiles_APIError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/testowner/testrepo/pulls/1/files", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	c := newTestClient(t, mux)
	_, err := c.GetChangedFiles(context.Background(), "testowner", "testrepo", 1)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "listing PR files") {
		t.Errorf("error = %q, want to contain 'listing PR files'", err.Error())
	}
}

func TestGetChangedFiles_Empty(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/testowner/testrepo/pulls/1/files", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		mustEncode(t, w, []*github.CommitFile{})
	})

	c := newTestClient(t, mux)
	files, err := c.GetChangedFiles(context.Background(), "testowner", "testrepo", 1)
	if err != nil {
		t.Fatalf("GetChangedFiles() error = %v", err)
	}
	if len(files) != 0 {
		t.Errorf("got %d files, want 0", len(files))
	}
}

// ==========================================================================
// GetFileContent tests
// ==========================================================================

func TestGetFileContent_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/testowner/testrepo/contents/path/to/file.yaml", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("ref") != "feature-branch" {
			t.Errorf("ref = %q, want feature-branch", r.URL.Query().Get("ref"))
		}
		w.Header().Set("Content-Type", "application/json")
		mustEncode(t, w, &github.RepositoryContent{
			Type:     github.String("file"),
			Encoding: github.String("base64"),
			Content:  github.String("aGVsbG8gd29ybGQ="), // "hello world"
		})
	})

	c := newTestClient(t, mux)
	data, err := c.GetFileContent(context.Background(), "testowner", "testrepo", "path/to/file.yaml", "feature-branch")
	if err != nil {
		t.Fatalf("GetFileContent() error = %v", err)
	}
	if string(data) != "hello world" {
		t.Errorf("content = %q, want %q", string(data), "hello world")
	}
}

func TestGetFileContent_NotFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/testowner/testrepo/contents/missing.yaml", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	c := newTestClient(t, mux)
	_, err := c.GetFileContent(context.Background(), "testowner", "testrepo", "missing.yaml", "main")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "getting file content for missing.yaml") {
		t.Errorf("error = %q, want to contain 'getting file content for missing.yaml'", err.Error())
	}
}

// ==========================================================================
// GetFileContents (batch) tests
// ==========================================================================

func TestGetFileContents_EmptyPaths(t *testing.T) {
	c := &Client{}
	result, err := c.GetFileContents(context.Background(), "owner", "repo", nil, "main")
	if err != nil {
		t.Fatalf("GetFileContents() error = %v", err)
	}
	if len(result) != 0 {
		t.Errorf("GetFileContents() returned %d entries, want 0", len(result))
	}
}

func TestGetFileContents_EmptySlice(t *testing.T) {
	c := &Client{}
	result, err := c.GetFileContents(context.Background(), "owner", "repo", []string{}, "main")
	if err != nil {
		t.Fatalf("GetFileContents() error = %v", err)
	}
	if len(result) != 0 {
		t.Errorf("GetFileContents() returned %d entries, want 0", len(result))
	}
}

// ==========================================================================
// AppCommentMarker formatting
// ==========================================================================

func TestAppCommentMarker_Formatting(t *testing.T) {
	tests := []struct {
		appName string
		want    string
	}{
		{"my-app", "<!-- lemuria:app:my-app -->"},
		{"frontend", "<!-- lemuria:app:frontend -->"},
		{"backend-service", "<!-- lemuria:app:backend-service -->"},
		{"app/with/slashes", "<!-- lemuria:app:app/with/slashes -->"},
	}

	for _, tt := range tests {
		t.Run(tt.appName, func(t *testing.T) {
			got := fmt.Sprintf(AppCommentMarker, tt.appName)
			if got != tt.want {
				t.Errorf("AppCommentMarker(%q) = %q, want %q", tt.appName, got, tt.want)
			}
		})
	}
}
