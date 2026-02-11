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
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/hibiken/asynq"

	"github.com/org/lemuria/internal/config"
	"github.com/org/lemuria/internal/models"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func makeTask(t *testing.T, deliveryID string, event *models.PREvent) *asynq.Task {
	t.Helper()
	payload, err := json.Marshal(WebhookTaskPayload{
		DeliveryID: deliveryID,
		Event:      event,
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return asynq.NewTask(TypeWebhookProcess, payload)
}

func TestProcessTask_MalformedPayload(t *testing.T) {
	h := NewWebhookHandler(
		&config.Config{},
		nil, nil, nil, nil,
		testLogger(),
	)

	task := asynq.NewTask(TypeWebhookProcess, []byte(`{invalid json`))
	err := h.ProcessTask(context.Background(), task)
	if err == nil {
		t.Fatal("expected error for malformed payload")
	}
	if !errors.Is(err, asynq.SkipRetry) {
		t.Errorf("error should wrap SkipRetry, got: %v", err)
	}
}

func TestProcessTask_NilEvent(t *testing.T) {
	h := NewWebhookHandler(
		&config.Config{},
		nil, nil, nil, nil,
		testLogger(),
	)

	payload, _ := json.Marshal(WebhookTaskPayload{
		DeliveryID: "test-1",
		Event:      nil,
	})
	task := asynq.NewTask(TypeWebhookProcess, payload)

	err := h.ProcessTask(context.Background(), task)
	if err == nil {
		t.Fatal("expected error for nil event")
	}
	if !errors.Is(err, asynq.SkipRetry) {
		t.Errorf("error should wrap SkipRetry, got: %v", err)
	}
}

func TestProcessTask_UnknownProvider(t *testing.T) {
	h := NewWebhookHandler(
		&config.Config{},
		nil, nil, nil, nil,
		testLogger(),
	)

	event := &models.PREvent{
		Provider: "bitbucket",
		Type:     models.EventTypePullRequest,
		Action:   models.PRActionOpened,
		Repo:     models.RepoInfo{FullName: "org/repo"},
		PR:       models.PRInfo{Number: 1},
	}

	task := makeTask(t, "test-1", event)
	err := h.ProcessTask(context.Background(), task)
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
	if !errors.Is(err, asynq.SkipRetry) {
		t.Errorf("error should wrap SkipRetry, got: %v", err)
	}
}

func TestProcessTask_NoExecutorConfigured(t *testing.T) {
	tests := []struct {
		name     string
		provider models.VCSProvider
	}{
		{"github without executor", models.VCSProviderGitHub},
		{"gitlab without executor", models.VCSProviderGitLab},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewWebhookHandler(
				&config.Config{},
				nil, nil, nil, nil,
				testLogger(),
			)

			event := &models.PREvent{
				Provider:   tt.provider,
				Type:       models.EventTypePullRequest,
				Action:     models.PRActionOpened,
				Repo:       models.RepoInfo{FullName: "org/repo"},
				PR:         models.PRInfo{Number: 1},
				ReceivedAt: time.Now(),
			}

			task := makeTask(t, "test-1", event)
			err := h.ProcessTask(context.Background(), task)
			if err == nil {
				t.Fatal("expected error when no executor configured")
			}
			if !errors.Is(err, asynq.SkipRetry) {
				t.Errorf("error should wrap SkipRetry, got: %v", err)
			}
		})
	}
}
