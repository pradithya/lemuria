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

package webhook

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"

	"github.com/org/lemuria/internal/commands"
	"github.com/org/lemuria/internal/config"
	"github.com/org/lemuria/internal/github"
	"github.com/org/lemuria/internal/models"
	"github.com/org/lemuria/internal/queue"
)

// GitHubHandler processes GitHub webhook events.
type GitHubHandler struct {
	config       *config.Config
	githubClient *github.Client
	cmdExecutor  *commands.Executor
	validator    *Validator
	queueClient  *queue.Client
}

// NewGitHubHandler creates a new GitHub webhook handler.
// If queueClient is non-nil, events are enqueued for async processing instead of
// being handled in a fire-and-forget goroutine.
func NewGitHubHandler(cfg *config.Config, gh *github.Client, executor *commands.Executor, queueClient *queue.Client) *GitHubHandler {
	return &GitHubHandler{
		config:       cfg,
		githubClient: gh,
		cmdExecutor:  executor,
		validator:    NewValidator(cfg.GitHub.WebhookSecret),
		queueClient:  queueClient,
	}
}

// Handle processes incoming GitHub webhook requests.
func (h *GitHubHandler) Handle(w http.ResponseWriter, r *http.Request) {
	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		slog.Error("failed to read request body", "error", err)
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}

	// Validate webhook signature
	signature := r.Header.Get("X-Hub-Signature-256")
	if !h.validator.Validate(body, signature) {
		slog.Warn("invalid webhook signature")
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}

	// Determine event type
	eventType := r.Header.Get("X-GitHub-Event")
	deliveryID := r.Header.Get("X-GitHub-Delivery")

	slog.Info("received webhook",
		"event_type", eventType,
		"delivery_id", deliveryID,
	)

	// Parse and handle the event
	event, err := ParseGitHubEvent(eventType, body)
	if err != nil {
		slog.Error("failed to parse event", "error", err)
		http.Error(w, "failed to parse event", http.StatusBadRequest)
		return
	}

	if event == nil {
		// Event type not handled
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(map[string]string{"status": "ignored"}); err != nil {
			slog.Warn("failed to encode response", "error", err)
		}
		return
	}

	// Check if repo is allowed
	if !h.config.IsRepoAllowed(event.Repo.FullName) {
		slog.Info("repository not in allowlist", "repo", event.Repo.FullName)
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(map[string]string{"status": "repo not allowed"}); err != nil {
			slog.Warn("failed to encode response", "error", err)
		}
		return
	}

	// Process the event asynchronously
	if h.queueClient != nil {
		if err := h.queueClient.EnqueueWebhook(deliveryID, event); err != nil {
			slog.Error("failed to enqueue webhook", "error", err, "delivery_id", deliveryID)
			go h.processEvent(context.Background(), event) // fallback
		}
	} else {
		go h.processEvent(context.Background(), event)
	}

	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "accepted"}); err != nil {
		slog.Warn("failed to encode response", "error", err)
	}
}

// processEvent handles the webhook event based on its type.
func (h *GitHubHandler) processEvent(ctx context.Context, event *models.PREvent) {
	slog.Info("processing event",
		"type", event.Type,
		"action", event.Action,
		"repo", event.Repo.FullName,
		"pr", event.PR.Number,
	)

	// Handle PR close/merge - release all locks
	if event.ShouldUnlockAll() {
		h.handlePRClosed(ctx, event)
		return
	}

	// Handle autoplan on PR open or push
	if event.ShouldAutoplan() && h.config.Defaults.Autoplan {
		h.handleAutoplan(ctx, event)
		return
	}

	// Handle issue comment (commands)
	if event.Type == models.EventTypeIssueComment && event.Comment != nil {
		h.handleComment(ctx, event)
		return
	}
}

// handlePRClosed releases all locks held by the closed PR.
func (h *GitHubHandler) handlePRClosed(ctx context.Context, event *models.PREvent) {
	slog.Info("PR closed, releasing locks",
		"repo", event.Repo.FullName,
		"pr", event.PR.Number,
		"merged", event.PR.Merged,
	)

	if err := h.cmdExecutor.UnlockAll(ctx, event); err != nil {
		slog.Error("failed to release locks",
			"error", err,
			"repo", event.Repo.FullName,
			"pr", event.PR.Number,
		)
	}
}

// handleAutoplan runs plan for affected applications.
func (h *GitHubHandler) handleAutoplan(ctx context.Context, event *models.PREvent) {
	slog.Info("running autoplan",
		"repo", event.Repo.FullName,
		"pr", event.PR.Number,
	)

	if err := h.cmdExecutor.RunAutoplan(ctx, event); err != nil {
		slog.Error("autoplan failed",
			"error", err,
			"repo", event.Repo.FullName,
			"pr", event.PR.Number,
		)
	}
}

// handleComment parses and executes commands from PR comments.
func (h *GitHubHandler) handleComment(ctx context.Context, event *models.PREvent) {
	// Only handle new comments
	if event.Action != models.PRActionCreated {
		return
	}

	cmd, err := commands.Parse(event.Comment.Body)
	if err != nil {
		slog.Debug("not a lemuria command", "error", err)
		return
	}

	// issue_comment webhooks only include the PR number — branch info, HEAD SHA,
	// etc. are missing. Fetch the full PR details before executing commands.
	if event.PR.HeadRef == "" || event.PR.BaseRef == "" {
		if err := h.enrichPRInfo(ctx, event); err != nil {
			slog.Error("failed to fetch PR details",
				"error", err,
				"repo", event.Repo.FullName,
				"pr", event.PR.Number,
			)
			return
		}
	}

	slog.Info("executing command",
		"command", cmd.Name,
		"repo", event.Repo.FullName,
		"pr", event.PR.Number,
		"user", event.Comment.Author.Login,
	)

	if err := h.cmdExecutor.Execute(ctx, cmd, event); err != nil {
		slog.Error("command execution failed",
			"error", err,
			"command", cmd.Name,
			"repo", event.Repo.FullName,
			"pr", event.PR.Number,
		)
	}
}

// enrichPRInfo fetches full PR details from GitHub and populates missing fields.
func (h *GitHubHandler) enrichPRInfo(ctx context.Context, event *models.PREvent) error {
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

	slog.Debug("enriched PR info from API",
		"pr", event.PR.Number,
		"head_ref", event.PR.HeadRef,
		"base_ref", event.PR.BaseRef,
		"head_sha", event.PR.HeadSHA,
	)

	return nil
}
