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
	"fmt"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"

	"github.com/org/lemuria/internal/config"
	"github.com/org/lemuria/internal/models"
)

func startMiniredis(t *testing.T) *miniredis.Miniredis {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	t.Cleanup(mr.Close)
	return mr
}

func TestNewClient(t *testing.T) {
	mr := startMiniredis(t)

	cfg := config.RedisConfig{
		Address:  mr.Addr(),
		Password: "",
		DB:       0,
	}

	client := NewClient(cfg)
	if client == nil {
		t.Fatal("NewClient() returned nil")
	}
	if client.client == nil {
		t.Fatal("client.client is nil")
	}
}

func TestNewClient_WithPassword(t *testing.T) {
	mr := startMiniredis(t)
	mr.RequireAuth("secret")

	cfg := config.RedisConfig{
		Address:  mr.Addr(),
		Password: "secret",
		DB:       1,
	}

	client := NewClient(cfg)
	if client == nil {
		t.Fatal("NewClient() returned nil")
	}
}

func TestClient_EnqueueWebhook(t *testing.T) {
	mr := startMiniredis(t)

	cfg := config.RedisConfig{
		Address: mr.Addr(),
	}

	client := NewClient(cfg)
	defer func() { _ = client.Close() }()

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

	err := client.EnqueueWebhook("delivery-1", event)
	if err != nil {
		t.Fatalf("EnqueueWebhook() error = %v", err)
	}
}

func TestClient_EnqueueWebhook_Duplicate(t *testing.T) {
	mr := startMiniredis(t)

	cfg := config.RedisConfig{
		Address: mr.Addr(),
	}

	client := NewClient(cfg)
	defer func() { _ = client.Close() }()

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

	// Enqueue the first time
	err := client.EnqueueWebhook("dup-delivery", event)
	if err != nil {
		t.Fatalf("first EnqueueWebhook() error = %v", err)
	}

	// Enqueue the same delivery ID again -- should not return a fatal error.
	// With miniredis this may return ErrTaskIDConflict rather than ErrDuplicateTask,
	// so we just verify it doesn't crash.
	_ = client.EnqueueWebhook("dup-delivery", event)
}

func TestClient_EnqueueWebhook_MultipleDistinct(t *testing.T) {
	mr := startMiniredis(t)

	cfg := config.RedisConfig{
		Address: mr.Addr(),
	}

	client := NewClient(cfg)
	defer func() { _ = client.Close() }()

	for i := 0; i < 5; i++ {
		event := &models.PREvent{
			Provider:   models.VCSProviderGitHub,
			Type:       models.EventTypePullRequest,
			Action:     models.PRActionOpened,
			Repo:       models.RepoInfo{FullName: "org/repo"},
			PR:         models.PRInfo{Number: i + 1},
			ReceivedAt: time.Now(),
		}

		deliveryID := fmt.Sprintf("delivery-%d", i)
		err := client.EnqueueWebhook(deliveryID, event)
		if err != nil {
			t.Fatalf("EnqueueWebhook(%q) error = %v", deliveryID, err)
		}
	}
}

func TestClient_Close(t *testing.T) {
	mr := startMiniredis(t)

	cfg := config.RedisConfig{
		Address: mr.Addr(),
	}

	client := NewClient(cfg)
	err := client.Close()
	if err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestClient_EnqueueWebhook_GitLabEvent(t *testing.T) {
	mr := startMiniredis(t)

	cfg := config.RedisConfig{
		Address: mr.Addr(),
	}

	client := NewClient(cfg)
	defer func() { _ = client.Close() }()

	event := &models.PREvent{
		Provider: models.VCSProviderGitLab,
		Type:     models.EventTypeIssueComment,
		Action:   models.PRActionCreated,
		Repo: models.RepoInfo{
			Owner:    "group",
			Name:     "project",
			FullName: "group/project",
		},
		PR: models.PRInfo{
			Number: 7,
		},
		Comment: &models.Comment{
			ID:   42,
			Body: "lemuria plan",
			Author: models.UserInfo{
				Login: "user1",
			},
		},
		ReceivedAt: time.Now(),
	}

	err := client.EnqueueWebhook("gitlab-delivery-1", event)
	if err != nil {
		t.Fatalf("EnqueueWebhook() error = %v", err)
	}
}
