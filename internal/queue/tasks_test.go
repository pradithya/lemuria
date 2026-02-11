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

package queue

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/org/lemuria/internal/models"
)

func TestNewWebhookTask(t *testing.T) {
	event := &models.PREvent{
		Provider: models.VCSProviderGitHub,
		Type:     models.EventTypePullRequest,
		Action:   models.PRActionOpened,
		Repo: models.RepoInfo{
			Owner:    "org",
			Name:     "repo",
			FullName: "org/repo",
		},
		PR: models.PRInfo{
			Number:  42,
			Title:   "Test PR",
			HeadSHA: "abc123",
			HeadRef: "feature",
			BaseRef: "main",
		},
		ReceivedAt: time.Now(),
	}

	task, err := NewWebhookTask("delivery-123", event)
	if err != nil {
		t.Fatalf("NewWebhookTask() error = %v", err)
	}

	if task.Type() != TypeWebhookProcess {
		t.Errorf("task type = %q, want %q", task.Type(), TypeWebhookProcess)
	}

	// Verify payload roundtrips correctly
	var payload WebhookTaskPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}

	if payload.DeliveryID != "delivery-123" {
		t.Errorf("delivery ID = %q, want %q", payload.DeliveryID, "delivery-123")
	}
	if payload.Event.Provider != models.VCSProviderGitHub {
		t.Errorf("provider = %q, want %q", payload.Event.Provider, models.VCSProviderGitHub)
	}
	if payload.Event.PR.Number != 42 {
		t.Errorf("PR number = %d, want 42", payload.Event.PR.Number)
	}
	if payload.Event.Repo.FullName != "org/repo" {
		t.Errorf("repo = %q, want %q", payload.Event.Repo.FullName, "org/repo")
	}
}

func TestWebhookTaskPayloadExcludesRepoConfig(t *testing.T) {
	event := &models.PREvent{
		Provider: models.VCSProviderGitHub,
		Type:     models.EventTypePullRequest,
		Action:   models.PRActionOpened,
		Repo: models.RepoInfo{
			Owner:    "org",
			Name:     "repo",
			FullName: "org/repo",
		},
		PR: models.PRInfo{
			Number: 1,
		},
		ReceivedAt:       time.Now(),
		RepoConfigLoaded: true,
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// RepoConfig and RepoConfigLoaded have json:"-" tags,
	// so they should not appear in the serialized output
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}

	if _, ok := raw["RepoConfig"]; ok {
		t.Error("RepoConfig should not be present in JSON")
	}
	if _, ok := raw["repoConfigLoaded"]; ok {
		t.Error("RepoConfigLoaded should not be present in JSON")
	}
}
