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

package e2e

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/org/lemuria/internal/commands"
	"github.com/org/lemuria/internal/config"
	"github.com/org/lemuria/internal/server"
)

const testWebhookSecret = "test-e2e-webhook-secret"

// newTestServer creates an httptest.Server backed by the real server.Server,
// with a mock VCS client injected for both GitHub and GitLab providers.
func newTestServer(mockVCS *MockVCSClient, cfg *config.Config) *httptest.Server {
	if cfg == nil {
		cfg = &config.Config{
			ArgoCD: config.ArgoCDConfig{
				DiffMode:       "live",
				TempAppTimeout: 15 * time.Second,
			},
			Defaults: config.DefaultsConfig{
				RequireApproval: false,
				AutoMerge:       false,
			},
		}
	}
	cfg.GitHub.WebhookSecret = testWebhookSecret
	cfg.GitLab.WebhookSecret = testWebhookSecret

	executor := commands.NewExecutor(mockVCS, argoClient, lockManager, cfg)

	deps := &server.Dependencies{
		ArgoClient:     argoClient,
		LockManager:    lockManager,
		GithubVCS:      mockVCS,
		GitlabVCS:      mockVCS,
		GithubExecutor: executor,
		GitlabExecutor: executor,
	}

	srv, err := server.NewFromDeps(cfg, deps)
	if err != nil {
		panic(fmt.Sprintf("failed to create test server: %v", err))
	}

	return httptest.NewServer(srv.Handler())
}

// ============================================================================
// Webhook Payload Builders
// ============================================================================

// githubCommentPayload builds a GitHub issue_comment webhook JSON body.
func githubCommentPayload(repo, owner, repoName string, prNumber int, commentBody string) []byte {
	payload := map[string]any{
		"action": "created",
		"issue": map[string]any{
			"number": prNumber,
			"pull_request": map[string]any{
				"url": fmt.Sprintf("https://api.github.com/repos/%s/pulls/%d", repo, prNumber),
			},
		},
		"comment": map[string]any{
			"id":   12345,
			"body": commentBody,
			"user": map[string]any{
				"login": "test-user",
				"id":    1,
			},
			"created_at": time.Now().Format(time.RFC3339),
		},
		"repository": map[string]any{
			"name":      repoName,
			"full_name": repo,
			"owner": map[string]any{
				"login": owner,
			},
			"clone_url": "https://github.com/" + repo + ".git",
			"html_url":  "https://github.com/" + repo,
		},
		"sender": map[string]any{
			"login": "test-user",
			"id":    1,
		},
	}
	data, _ := json.Marshal(payload)
	return data
}

// githubPRPayload builds a GitHub pull_request webhook JSON body.
func githubPRPayload(action, repo, owner, repoName string, prNumber int, headSHA, headRef, baseRef string) []byte {
	state := "open"
	if action == "closed" {
		state = "closed"
	}
	payload := map[string]any{
		"action": action,
		"number": prNumber,
		"pull_request": map[string]any{
			"number": prNumber,
			"title":  "Test PR",
			"state":  state,
			"draft":  false,
			"merged": false,
			"head": map[string]any{
				"sha": headSHA,
				"ref": headRef,
			},
			"base": map[string]any{
				"ref": baseRef,
			},
			"user": map[string]any{
				"login": "test-user",
				"id":    1,
			},
			"html_url": fmt.Sprintf("https://github.com/%s/pull/%d", repo, prNumber),
		},
		"repository": map[string]any{
			"name":      repoName,
			"full_name": repo,
			"owner": map[string]any{
				"login": owner,
			},
			"clone_url": "https://github.com/" + repo + ".git",
			"html_url":  "https://github.com/" + repo,
		},
		"sender": map[string]any{
			"login": "test-user",
			"id":    1,
		},
	}
	data, _ := json.Marshal(payload)
	return data
}

// gitlabNotePayload builds a GitLab Note Hook webhook JSON body.
func gitlabNotePayload(fullPath, projectName string, mrIID int, headSHA, sourceBranch, targetBranch, noteBody string) []byte {
	payload := map[string]any{
		"object_attributes": map[string]any{
			"id":            12345,
			"note":          noteBody,
			"noteable_type": "MergeRequest",
			"created_at":    time.Now().Format("2006-01-02 15:04:05 UTC"),
		},
		"merge_request": map[string]any{
			"iid":           mrIID,
			"title":         "Test MR",
			"state":         "opened",
			"draft":         false,
			"source_branch": sourceBranch,
			"target_branch": targetBranch,
			"last_commit": map[string]any{
				"id": headSHA,
			},
			"url": fmt.Sprintf("https://gitlab.com/%s/-/merge_requests/%d", fullPath, mrIID),
		},
		"project": map[string]any{
			"name":                projectName,
			"path_with_namespace": fullPath,
			"web_url":             "https://gitlab.com/" + fullPath,
			"git_http_url":        "https://gitlab.com/" + fullPath + ".git",
		},
		"user": map[string]any{
			"username": "test-user",
			"name":     "Test User",
			"id":       1,
		},
	}
	data, _ := json.Marshal(payload)
	return data
}

// ============================================================================
// HTTP Request Helpers
// ============================================================================

// signPayload computes the HMAC-SHA256 signature for a GitHub webhook payload.
func signPayload(payload []byte) string {
	mac := hmac.New(sha256.New, []byte(testWebhookSecret))
	mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// sendGitHubWebhook sends a properly signed GitHub webhook to the test server.
func sendGitHubWebhook(t *testing.T, serverURL, eventType string, payload []byte) *http.Response {
	t.Helper()

	req, err := http.NewRequest("POST", serverURL+"/webhook/github", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", eventType)
	req.Header.Set("X-GitHub-Delivery", fmt.Sprintf("e2e-%d", time.Now().UnixNano()))
	req.Header.Set("X-Hub-Signature-256", signPayload(payload))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to send webhook: %v", err)
	}

	return resp
}

// sendGitLabWebhook sends a properly authenticated GitLab webhook.
func sendGitLabWebhook(t *testing.T, serverURL, eventType string, payload []byte) *http.Response {
	t.Helper()

	req, err := http.NewRequest("POST", serverURL+"/webhook/gitlab", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Gitlab-Event", eventType)
	req.Header.Set("X-Gitlab-Token", testWebhookSecret)
	req.Header.Set("X-Gitlab-Event-UUID", fmt.Sprintf("e2e-%d", time.Now().UnixNano()))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to send webhook: %v", err)
	}

	return resp
}

// assertAccepted verifies the webhook response status is 200 OK and closes
// the response body.
func assertAccepted(t *testing.T, resp *http.Response) {
	t.Helper()
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 OK, got %d: %s", resp.StatusCode, body)
	}
}

// ============================================================================
// Polling Helpers
// ============================================================================

// waitForComment polls the mock VCS client until at least n comments are posted.
func waitForComment(t *testing.T, mock *MockVCSClient, n int, timeout time.Duration) []PostedComment {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		comments := mock.GetPostedComments()
		if len(comments) >= n {
			return comments
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("Timed out waiting for %d comment(s), got %d", n, len(mock.GetPostedComments()))
	return nil
}

// waitForUpdatedComment polls until at least n updated comments are recorded.
func waitForUpdatedComment(t *testing.T, mock *MockVCSClient, n int, timeout time.Duration) []UpdatedComment {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		comments := mock.GetUpdatedComments()
		if len(comments) >= n {
			return comments
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("Timed out waiting for %d updated comment(s), got %d", n, len(mock.GetUpdatedComments()))
	return nil
}

// waitForLockRelease polls until the lock for the given app is released.
func waitForLockRelease(t *testing.T, appName string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		lock, err := lockManager.Get(testCtx, appName)
		if err != nil {
			t.Fatalf("Failed to check lock: %v", err)
		}
		if lock == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("Timed out waiting for lock on %s to be released", appName)
}

// waitForMergeCall polls the mock VCS client until at least n merge calls are recorded.
func waitForMergeCall(t *testing.T, mock *MockVCSClient, n int, timeout time.Duration) []MergeCall {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		calls := mock.GetMergeCalls()
		if len(calls) >= n {
			return calls
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("Timed out waiting for %d merge call(s), got %d", n, len(mock.GetMergeCalls()))
	return nil
}

// waitForProcessingDone waits a short period after the primary result is
// observed, allowing secondary side effects (reactions, invalidations) to
// complete before assertions.
func waitForProcessingDone() {
	time.Sleep(500 * time.Millisecond)
}
