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
	"strings"
	"testing"
	"time"
)

func TestE2EHelpCommand(t *testing.T) {
	mockGH := NewMockVCSClient()
	ts := newTestServer(mockGH, nil)
	defer ts.Close()

	payload := githubCommentPayload("test-owner/test-repo", "test-owner", "test-repo", 900, "lemuria help")
	mockGH.PRHeadRef = "feature-branch"
	mockGH.PRBaseRef = "main"
	resp := sendGitHubWebhook(t, ts.URL, "issue_comment", payload)
	assertAccepted(t, resp)

	// Wait for help comment
	comments := waitForComment(t, mockGH, 1, 30*time.Second)
	lastComment := comments[len(comments)-1]

	if !strings.Contains(lastComment.Body, "Help") {
		t.Error("Expected 'Help' in comment body")
	}
	if !strings.Contains(lastComment.Body, "lemuria plan") {
		t.Error("Expected command examples in help text")
	}
}

func TestE2EHelpCommandGitLab(t *testing.T) {
	mockVCS := NewMockVCSClient()
	ts := newTestServer(mockVCS, nil)
	defer ts.Close()

	payload := gitlabNotePayload("mygroup/myproject", "myproject", 900, "", "feature-branch", "main", "lemuria help")
	resp := sendGitLabWebhook(t, ts.URL, "Note Hook", payload)
	assertAccepted(t, resp)

	// Wait for help comment
	comments := waitForComment(t, mockVCS, 1, 30*time.Second)
	lastComment := comments[len(comments)-1]

	if !strings.Contains(lastComment.Body, "Help") {
		t.Error("Expected 'Help' in comment body")
	}
	if !strings.Contains(lastComment.Body, "lemuria plan") {
		t.Error("Expected command examples in help text")
	}
}
