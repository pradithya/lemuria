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

func TestNewWebhookTask_EmptyDeliveryID(t *testing.T) {
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
		ReceivedAt: time.Now(),
	}

	task, err := NewWebhookTask("", event)
	if err != nil {
		t.Fatalf("NewWebhookTask() error = %v", err)
	}

	// Verify the task is created with empty delivery ID
	var payload WebhookTaskPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}

	if payload.DeliveryID != "" {
		t.Errorf("delivery ID = %q, want empty string", payload.DeliveryID)
	}
	if payload.Event == nil {
		t.Fatal("event should not be nil")
	}
}

func TestNewWebhookTask_NilEvent(t *testing.T) {
	task, err := NewWebhookTask("delivery-nil", nil)
	if err != nil {
		t.Fatalf("NewWebhookTask() error = %v", err)
	}

	// A nil event should serialize successfully (as JSON null)
	var payload WebhookTaskPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}

	if payload.DeliveryID != "delivery-nil" {
		t.Errorf("delivery ID = %q, want %q", payload.DeliveryID, "delivery-nil")
	}
	if payload.Event != nil {
		t.Errorf("event should be nil, got %+v", payload.Event)
	}
}

func TestNewWebhookTask_PreservesAllFields(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	event := &models.PREvent{
		Provider: models.VCSProviderGitLab,
		Type:     models.EventTypeIssueComment,
		Action:   models.PRActionCreated,
		Repo: models.RepoInfo{
			Owner:    "myorg",
			Name:     "myrepo",
			FullName: "myorg/myrepo",
			CloneURL: "https://gitlab.com/myorg/myrepo.git",
			HTMLURL:  "https://gitlab.com/myorg/myrepo",
		},
		PR: models.PRInfo{
			Number:  99,
			Title:   "MR Title",
			HeadSHA: "def456",
			HeadRef: "feature-branch",
			BaseRef: "main",
			State:   models.PRStateOpen,
			Draft:   true,
		},
		Comment: &models.Comment{
			ID:   789,
			Body: "lemuria plan",
			Author: models.UserInfo{
				Login: "user1",
				ID:    100,
			},
		},
		Sender: models.UserInfo{
			Login: "user1",
			ID:    100,
		},
		ReceivedAt: now,
	}

	task, err := NewWebhookTask("delivery-full", event)
	if err != nil {
		t.Fatalf("NewWebhookTask() error = %v", err)
	}

	var payload WebhookTaskPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}

	tests := []struct {
		name string
		got  interface{}
		want interface{}
	}{
		{"delivery ID", payload.DeliveryID, "delivery-full"},
		{"provider", string(payload.Event.Provider), string(models.VCSProviderGitLab)},
		{"event type", string(payload.Event.Type), string(models.EventTypeIssueComment)},
		{"action", string(payload.Event.Action), string(models.PRActionCreated)},
		{"repo owner", payload.Event.Repo.Owner, "myorg"},
		{"repo name", payload.Event.Repo.Name, "myrepo"},
		{"repo full name", payload.Event.Repo.FullName, "myorg/myrepo"},
		{"repo clone URL", payload.Event.Repo.CloneURL, "https://gitlab.com/myorg/myrepo.git"},
		{"PR number", payload.Event.PR.Number, 99},
		{"PR title", payload.Event.PR.Title, "MR Title"},
		{"PR head SHA", payload.Event.PR.HeadSHA, "def456"},
		{"PR head ref", payload.Event.PR.HeadRef, "feature-branch"},
		{"PR base ref", payload.Event.PR.BaseRef, "main"},
		{"PR draft", payload.Event.PR.Draft, true},
		{"comment body", payload.Event.Comment.Body, "lemuria plan"},
		{"comment author", payload.Event.Comment.Author.Login, "user1"},
		{"sender login", payload.Event.Sender.Login, "user1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("got %v, want %v", tt.got, tt.want)
			}
		})
	}
}

func TestNewWebhookTask_TaskType(t *testing.T) {
	if TypeWebhookProcess != "webhook:process" {
		t.Errorf("TypeWebhookProcess = %q, want %q", TypeWebhookProcess, "webhook:process")
	}
}

func TestWebhookTaskPayload_JSONRoundtrip(t *testing.T) {
	tests := []struct {
		name       string
		deliveryID string
		event      *models.PREvent
	}{
		{
			name:       "github PR opened",
			deliveryID: "gh-123",
			event: &models.PREvent{
				Provider: models.VCSProviderGitHub,
				Type:     models.EventTypePullRequest,
				Action:   models.PRActionOpened,
				Repo:     models.RepoInfo{FullName: "org/repo"},
				PR:       models.PRInfo{Number: 1},
			},
		},
		{
			name:       "gitlab comment",
			deliveryID: "gl-456",
			event: &models.PREvent{
				Provider: models.VCSProviderGitLab,
				Type:     models.EventTypeIssueComment,
				Action:   models.PRActionCreated,
				Repo:     models.RepoInfo{FullName: "group/project"},
				PR:       models.PRInfo{Number: 10},
				Comment:  &models.Comment{Body: "lemuria plan"},
			},
		},
		{
			name:       "PR closed",
			deliveryID: "close-789",
			event: &models.PREvent{
				Provider: models.VCSProviderGitHub,
				Type:     models.EventTypePullRequest,
				Action:   models.PRActionClosed,
				Repo:     models.RepoInfo{FullName: "org/repo"},
				PR:       models.PRInfo{Number: 5, Merged: true},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task, err := NewWebhookTask(tt.deliveryID, tt.event)
			if err != nil {
				t.Fatalf("NewWebhookTask() error = %v", err)
			}

			var payload WebhookTaskPayload
			if err := json.Unmarshal(task.Payload(), &payload); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}

			if payload.DeliveryID != tt.deliveryID {
				t.Errorf("delivery ID = %q, want %q", payload.DeliveryID, tt.deliveryID)
			}
			if payload.Event.Provider != tt.event.Provider {
				t.Errorf("provider = %q, want %q", payload.Event.Provider, tt.event.Provider)
			}
			if payload.Event.PR.Number != tt.event.PR.Number {
				t.Errorf("PR number = %d, want %d", payload.Event.PR.Number, tt.event.PR.Number)
			}
		})
	}
}
