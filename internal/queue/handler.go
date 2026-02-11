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
	"fmt"
	"log/slog"

	"github.com/hibiken/asynq"

	"github.com/org/lemuria/internal/commands"
	"github.com/org/lemuria/internal/config"
	"github.com/org/lemuria/internal/github"
	"github.com/org/lemuria/internal/gitlab"
	"github.com/org/lemuria/internal/models"
)

// WebhookHandler processes webhook tasks dequeued from the task queue.
type WebhookHandler struct {
	config         *config.Config
	githubExecutor *commands.Executor
	gitlabExecutor *commands.Executor
	githubClient   *github.Client
	gitlabClient   *gitlab.Client
	logger         *slog.Logger
}

// NewWebhookHandler creates a new webhook task handler.
func NewWebhookHandler(
	cfg *config.Config,
	githubExecutor *commands.Executor,
	gitlabExecutor *commands.Executor,
	githubClient *github.Client,
	gitlabClient *gitlab.Client,
	logger *slog.Logger,
) *WebhookHandler {
	return &WebhookHandler{
		config:         cfg,
		githubExecutor: githubExecutor,
		gitlabExecutor: gitlabExecutor,
		githubClient:   githubClient,
		gitlabClient:   gitlabClient,
		logger:         logger,
	}
}

// ProcessTask handles a single webhook task from the queue.
func (h *WebhookHandler) ProcessTask(ctx context.Context, task *asynq.Task) error {
	var payload WebhookTaskPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return fmt.Errorf("unmarshaling task payload: %w: %w", err, asynq.SkipRetry)
	}

	event := payload.Event
	if event == nil {
		return fmt.Errorf("nil event in task payload: %w", asynq.SkipRetry)
	}

	h.logger.Info("processing webhook task",
		"delivery_id", payload.DeliveryID,
		"provider", event.Provider,
		"type", event.Type,
		"action", event.Action,
		"repo", event.Repo.FullName,
		"pr", event.PR.Number,
	)

	executor, err := h.executorFor(event.Provider)
	if err != nil {
		return err
	}

	return h.processEvent(ctx, executor, event)
}

// executorFor returns the appropriate executor for the given VCS provider.
func (h *WebhookHandler) executorFor(provider models.VCSProvider) (*commands.Executor, error) {
	switch provider {
	case models.VCSProviderGitHub:
		if h.githubExecutor == nil {
			return nil, fmt.Errorf("no GitHub executor configured: %w", asynq.SkipRetry)
		}
		return h.githubExecutor, nil
	case models.VCSProviderGitLab:
		if h.gitlabExecutor == nil {
			return nil, fmt.Errorf("no GitLab executor configured: %w", asynq.SkipRetry)
		}
		return h.gitlabExecutor, nil
	default:
		return nil, fmt.Errorf("unknown VCS provider %q: %w", provider, asynq.SkipRetry)
	}
}

// processEvent mirrors the webhook handler processEvent + handleComment logic.
func (h *WebhookHandler) processEvent(ctx context.Context, executor *commands.Executor, event *models.PREvent) error {
	// Handle PR close/merge — release all locks
	if event.ShouldUnlockAll() {
		if err := executor.UnlockAll(ctx, event); err != nil {
			return fmt.Errorf("unlocking all: %w", err)
		}
		return nil
	}

	// Handle autoplan on PR open or push
	if event.ShouldAutoplan() && h.config.Defaults.Autoplan {
		if err := executor.RunAutoplan(ctx, event); err != nil {
			return fmt.Errorf("autoplan: %w", err)
		}
		return nil
	}

	// Handle issue comment (commands)
	if event.Type == models.EventTypeIssueComment && event.Comment != nil {
		return h.handleComment(ctx, executor, event)
	}

	return nil
}

// handleComment parses and executes commands from PR comments.
func (h *WebhookHandler) handleComment(ctx context.Context, executor *commands.Executor, event *models.PREvent) error {
	if event.Action != models.PRActionCreated {
		return nil
	}

	cmd, err := commands.Parse(event.Comment.Body)
	if err != nil {
		// Not a lemuria command — nothing to do
		return nil
	}

	// Enrich PR info if branch details are missing
	if event.PR.HeadRef == "" || event.PR.BaseRef == "" {
		if err := h.enrichPRInfo(ctx, event); err != nil {
			return fmt.Errorf("enriching PR info: %w", err)
		}
	}

	h.logger.Info("executing command",
		"command", cmd.Name,
		"repo", event.Repo.FullName,
		"pr", event.PR.Number,
		"user", event.Comment.Author.Login,
	)

	if err := executor.Execute(ctx, cmd, event); err != nil {
		return fmt.Errorf("executing command %q: %w", cmd.Name, err)
	}
	return nil
}

// enrichPRInfo fetches full PR/MR details from the VCS provider.
func (h *WebhookHandler) enrichPRInfo(ctx context.Context, event *models.PREvent) error {
	switch event.Provider {
	case models.VCSProviderGitHub:
		if h.githubClient == nil {
			return fmt.Errorf("no GitHub client configured: %w", asynq.SkipRetry)
		}
		pr, err := h.githubClient.GetPRRaw(ctx, event.Repo.Owner, event.Repo.Name, event.PR.Number)
		if err != nil {
			return err
		}
		if pr.Head != nil {
			event.PR.HeadSHA = pr.Head.GetSHA()
			event.PR.HeadRef = pr.Head.GetRef()
		}
		if pr.Base != nil {
			event.PR.BaseRef = pr.Base.GetRef()
		}
		event.PR.State = models.PRState(pr.GetState())
		event.PR.Title = pr.GetTitle()
		event.PR.Draft = pr.GetDraft()
		event.PR.Merged = pr.GetMerged()

	case models.VCSProviderGitLab:
		if h.gitlabClient == nil {
			return fmt.Errorf("no GitLab client configured: %w", asynq.SkipRetry)
		}
		detail, err := h.gitlabClient.GetPR(ctx, event.Repo.Owner, event.Repo.Name, event.PR.Number)
		if err != nil {
			return err
		}
		event.PR.HeadSHA = detail.HeadSHA
		event.PR.HeadRef = detail.HeadRef
		event.PR.BaseRef = detail.BaseRef
		event.PR.State = detail.State
		event.PR.Title = detail.Title
		event.PR.Draft = detail.Draft
		event.PR.Merged = detail.Merged
	}

	return nil
}
