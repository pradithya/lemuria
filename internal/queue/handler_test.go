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
	"testing"
	"time"

	"github.com/hibiken/asynq"

	"github.com/org/lemuria/internal/commands"
	"github.com/org/lemuria/internal/config"
	"github.com/org/lemuria/internal/models"
)

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

func TestExecutorFor(t *testing.T) {
	ghExec := commands.NewExecutor(nil, nil, nil, &config.Config{})
	glExec := commands.NewExecutor(nil, nil, nil, &config.Config{})

	tests := []struct {
		name           string
		provider       models.VCSProvider
		githubExecutor *commands.Executor
		gitlabExecutor *commands.Executor
		wantErr        bool
		wantSkipRetry  bool
	}{
		{
			name:           "github with executor",
			provider:       models.VCSProviderGitHub,
			githubExecutor: ghExec,
			wantErr:        false,
		},
		{
			name:           "gitlab with executor",
			provider:       models.VCSProviderGitLab,
			gitlabExecutor: glExec,
			wantErr:        false,
		},
		{
			name:           "github without executor",
			provider:       models.VCSProviderGitHub,
			githubExecutor: nil,
			wantErr:        true,
			wantSkipRetry:  true,
		},
		{
			name:           "gitlab without executor",
			provider:       models.VCSProviderGitLab,
			gitlabExecutor: nil,
			wantErr:        true,
			wantSkipRetry:  true,
		},
		{
			name:          "unknown provider",
			provider:      "bitbucket",
			wantErr:       true,
			wantSkipRetry: true,
		},
		{
			name:          "empty provider",
			provider:      "",
			wantErr:       true,
			wantSkipRetry: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewWebhookHandler(
				&config.Config{},
				tt.githubExecutor, tt.gitlabExecutor,
				nil, nil,
			)

			executor, err := h.executorFor(tt.provider)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.wantSkipRetry && !errors.Is(err, asynq.SkipRetry) {
					t.Errorf("error should wrap SkipRetry, got: %v", err)
				}
				if executor != nil {
					t.Error("executor should be nil on error")
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if executor == nil {
					t.Fatal("executor should not be nil")
				}
			}
		})
	}
}

func TestExecutorFor_ReturnsCorrectExecutor(t *testing.T) {
	ghExec := commands.NewExecutor(nil, nil, nil, &config.Config{})
	glExec := commands.NewExecutor(nil, nil, nil, &config.Config{})

	h := NewWebhookHandler(
		&config.Config{},
		ghExec, glExec,
		nil, nil,
	)

	gotGH, err := h.executorFor(models.VCSProviderGitHub)
	if err != nil {
		t.Fatalf("executorFor(github): %v", err)
	}
	if gotGH != ghExec {
		t.Error("expected github executor instance")
	}

	gotGL, err := h.executorFor(models.VCSProviderGitLab)
	if err != nil {
		t.Fatalf("executorFor(gitlab): %v", err)
	}
	if gotGL != glExec {
		t.Error("expected gitlab executor instance")
	}
}

func TestProcessEvent_UnlockAll(t *testing.T) {
	// When event.ShouldUnlockAll() returns true and executor is nil,
	// processEvent should attempt to call executor.UnlockAll which will panic.
	// Instead we test via ProcessTask which goes through executorFor first.

	// A PR closed event with no executor should fail at executorFor
	h := NewWebhookHandler(
		&config.Config{},
		nil, nil, nil, nil,
	)

	event := &models.PREvent{
		Provider: models.VCSProviderGitHub,
		Type:     models.EventTypePullRequest,
		Action:   models.PRActionClosed,
		Repo:     models.RepoInfo{FullName: "org/repo"},
		PR:       models.PRInfo{Number: 1},
	}

	task := makeTask(t, "test-close", event)
	err := h.ProcessTask(context.Background(), task)
	if err == nil {
		t.Fatal("expected error for nil executor")
	}
}

func TestProcessEvent_IssueCommentNonCreated(t *testing.T) {
	// An issue_comment event with action != "created" should be a no-op.
	// We need an executor to pass executorFor, but processEvent should return nil
	// without calling anything on the executor.
	ghExec := commands.NewExecutor(nil, nil, nil, &config.Config{})

	h := NewWebhookHandler(
		&config.Config{},
		ghExec, nil, nil, nil,
	)

	// action=submitted is not "created", so handleComment returns nil early
	event := &models.PREvent{
		Provider: models.VCSProviderGitHub,
		Type:     models.EventTypeIssueComment,
		Action:   models.PRActionSubmitted, // not "created"
		Repo:     models.RepoInfo{FullName: "org/repo"},
		PR:       models.PRInfo{Number: 1},
		Comment:  &models.Comment{Body: "lemuria plan"},
	}

	task := makeTask(t, "test-non-created", event)
	err := h.ProcessTask(context.Background(), task)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProcessEvent_IssueCommentBotComment(t *testing.T) {
	// Comments containing the bot marker should be ignored
	ghExec := commands.NewExecutor(nil, nil, nil, &config.Config{})

	h := NewWebhookHandler(
		&config.Config{},
		ghExec, nil, nil, nil,
	)

	event := &models.PREvent{
		Provider: models.VCSProviderGitHub,
		Type:     models.EventTypeIssueComment,
		Action:   models.PRActionCreated,
		Repo:     models.RepoInfo{FullName: "org/repo"},
		PR:       models.PRInfo{Number: 1},
		Comment: &models.Comment{
			Body: models.CommentMarker + "\nsome bot comment",
			Author: models.UserInfo{
				Login: "lemuria[bot]",
			},
		},
	}

	task := makeTask(t, "test-bot-comment", event)
	err := h.ProcessTask(context.Background(), task)
	if err != nil {
		t.Fatalf("unexpected error for bot comment: %v", err)
	}
}

func TestProcessEvent_IssueCommentNotCommand(t *testing.T) {
	// A regular comment that is not a lemuria command should be silently ignored
	ghExec := commands.NewExecutor(nil, nil, nil, &config.Config{})

	h := NewWebhookHandler(
		&config.Config{},
		ghExec, nil, nil, nil,
	)

	event := &models.PREvent{
		Provider: models.VCSProviderGitHub,
		Type:     models.EventTypeIssueComment,
		Action:   models.PRActionCreated,
		Repo:     models.RepoInfo{FullName: "org/repo"},
		PR:       models.PRInfo{Number: 1},
		Comment: &models.Comment{
			Body:   "this is just a regular comment, not a command",
			Author: models.UserInfo{Login: "user1"},
		},
	}

	task := makeTask(t, "test-regular-comment", event)
	err := h.ProcessTask(context.Background(), task)
	if err != nil {
		t.Fatalf("unexpected error for non-command comment: %v", err)
	}
}

func TestProcessEvent_NoopEventTypes(t *testing.T) {
	// Events that don't match any condition should return nil
	ghExec := commands.NewExecutor(nil, nil, nil, &config.Config{})

	tests := []struct {
		name  string
		event *models.PREvent
	}{
		{
			name: "pull_request_review submitted",
			event: &models.PREvent{
				Provider: models.VCSProviderGitHub,
				Type:     models.EventTypePullRequestReview,
				Action:   models.PRActionSubmitted,
				Repo:     models.RepoInfo{FullName: "org/repo"},
				PR:       models.PRInfo{Number: 1},
			},
		},
		{
			name: "PR opened without autoplan",
			event: &models.PREvent{
				Provider: models.VCSProviderGitHub,
				Type:     models.EventTypePullRequest,
				Action:   models.PRActionOpened,
				Repo:     models.RepoInfo{FullName: "org/repo"},
				PR:       models.PRInfo{Number: 1},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewWebhookHandler(
				&config.Config{Defaults: config.DefaultsConfig{Autoplan: false}},
				ghExec, nil, nil, nil,
			)

			task := makeTask(t, "test-noop-"+tt.name, tt.event)
			err := h.ProcessTask(context.Background(), task)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestProcessTask_EmptyPayload(t *testing.T) {
	h := NewWebhookHandler(
		&config.Config{},
		nil, nil, nil, nil,
	)

	task := asynq.NewTask(TypeWebhookProcess, []byte(`{}`))
	err := h.ProcessTask(context.Background(), task)
	if err == nil {
		t.Fatal("expected error for empty payload (nil event)")
	}
	if !errors.Is(err, asynq.SkipRetry) {
		t.Errorf("error should wrap SkipRetry, got: %v", err)
	}
}

func TestProcessTask_EmptyBytesPayload(t *testing.T) {
	h := NewWebhookHandler(
		&config.Config{},
		nil, nil, nil, nil,
	)

	task := asynq.NewTask(TypeWebhookProcess, []byte(``))
	err := h.ProcessTask(context.Background(), task)
	if err == nil {
		t.Fatal("expected error for empty bytes payload")
	}
	if !errors.Is(err, asynq.SkipRetry) {
		t.Errorf("error should wrap SkipRetry, got: %v", err)
	}
}

func TestNewWebhookHandler(t *testing.T) {
	ghExec := commands.NewExecutor(nil, nil, nil, &config.Config{})
	glExec := commands.NewExecutor(nil, nil, nil, &config.Config{})
	cfg := &config.Config{
		Defaults: config.DefaultsConfig{Autoplan: true},
	}

	h := NewWebhookHandler(cfg, ghExec, glExec, nil, nil)
	if h == nil {
		t.Fatal("NewWebhookHandler() returned nil")
	}
	if h.config != cfg {
		t.Error("config not set correctly")
	}
	if h.githubExecutor != ghExec {
		t.Error("githubExecutor not set correctly")
	}
	if h.gitlabExecutor != glExec {
		t.Error("gitlabExecutor not set correctly")
	}
}

func TestNewWebhookHandler_AllNil(t *testing.T) {
	h := NewWebhookHandler(nil, nil, nil, nil, nil)
	if h == nil {
		t.Fatal("NewWebhookHandler() with nil args should still return non-nil handler")
	}
}
