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
	"crypto/subtle"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"

	"fmt"

	"github.com/org/lemuria/internal/commands"
	"github.com/org/lemuria/internal/config"
	"github.com/org/lemuria/internal/models"
	"github.com/org/lemuria/internal/vcs"
	"github.com/org/lemuria/internal/queue"
)

// GitLabHandler processes GitLab webhook events.
type GitLabHandler struct {
	config      *config.Config
	vcsClient   vcs.Client
	cmdExecutor *commands.Executor
	queueClient *queue.Client
}

// NewGitLabHandler creates a new GitLab webhook handler.
// If queueClient is non-nil, events are enqueued for async processing instead of
// being handled in a fire-and-forget goroutine.
func NewGitLabHandler(cfg *config.Config, vcsClient vcs.Client, executor *commands.Executor, queueClient *queue.Client) *GitLabHandler {
	return &GitLabHandler{
		config:      cfg,
		vcsClient:   vcsClient,
		cmdExecutor: executor,
		queueClient: queueClient,
	}
}

// Handle processes incoming GitLab webhook requests.
func (h *GitLabHandler) Handle(w http.ResponseWriter, r *http.Request) {
	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		slog.Error("failed to read request body", "error", err)
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}

	// Validate webhook secret token
	token := r.Header.Get("X-Gitlab-Token")
	if !h.validateToken(token) {
		slog.Warn("invalid webhook token")
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}

	// Determine event type from header
	eventType := r.Header.Get("X-Gitlab-Event")

	slog.Info("received gitlab webhook",
		"event_type", eventType,
	)

	// Parse and handle the event
	event, err := ParseGitLabEvent(eventType, body)
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
		deliveryID := r.Header.Get("X-Gitlab-Event-UUID")
		if deliveryID == "" {
			deliveryID = fmt.Sprintf("gl-%s-%d-%d", event.Repo.FullName, event.PR.Number, event.ReceivedAt.UnixNano())
		}
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

// validateToken checks the X-Gitlab-Token header against the configured webhook secret.
func (h *GitLabHandler) validateToken(token string) bool {
	expected := h.config.GitLab.WebhookSecret
	if expected == "" {
		// No secret configured — reject all requests to prevent unauthenticated access.
		return false
	}
	return subtle.ConstantTimeCompare([]byte(token), []byte(expected)) == 1
}

// processEvent handles the webhook event based on its type.
func (h *GitLabHandler) processEvent(ctx context.Context, event *models.PREvent) {
	slog.Info("processing gitlab event",
		"type", event.Type,
		"action", event.Action,
		"repo", event.Repo.FullName,
		"mr", event.PR.Number,
	)

	// Handle MR close/merge - release all locks
	if event.ShouldUnlockAll() {
		h.handlePRClosed(ctx, event)
		return
	}

	// Handle autoplan on MR open or push
	if event.ShouldAutoplan() && h.config.Defaults.Autoplan {
		h.handleAutoplan(ctx, event)
		return
	}

	// Handle note/comment (commands)
	if event.Type == models.EventTypeIssueComment && event.Comment != nil {
		h.handleComment(ctx, event)
		return
	}
}

// handlePRClosed releases all locks held by the closed MR.
func (h *GitLabHandler) handlePRClosed(ctx context.Context, event *models.PREvent) {
	slog.Info("MR closed, releasing locks",
		"repo", event.Repo.FullName,
		"mr", event.PR.Number,
		"merged", event.PR.Merged,
	)

	if err := h.cmdExecutor.UnlockAll(ctx, event); err != nil {
		slog.Error("failed to release locks",
			"error", err,
			"repo", event.Repo.FullName,
			"mr", event.PR.Number,
		)
	}
}

// handleAutoplan runs plan for affected applications.
func (h *GitLabHandler) handleAutoplan(ctx context.Context, event *models.PREvent) {
	slog.Info("running autoplan",
		"repo", event.Repo.FullName,
		"mr", event.PR.Number,
	)

	if err := h.cmdExecutor.RunAutoplan(ctx, event); err != nil {
		slog.Error("autoplan failed",
			"error", err,
			"repo", event.Repo.FullName,
			"mr", event.PR.Number,
		)
	}
}

// handleComment parses and executes commands from MR comments.
func (h *GitLabHandler) handleComment(ctx context.Context, event *models.PREvent) {
	// Only handle new comments
	if event.Action != models.PRActionCreated {
		return
	}

	cmd, err := commands.Parse(event.Comment.Body)
	if err != nil {
		slog.Debug("not a lemuria command", "error", err)
		return
	}

	// Note webhooks may not include full MR details — fetch them if needed.
	if event.PR.HeadRef == "" || event.PR.BaseRef == "" {
		if err := h.enrichMRInfo(ctx, event); err != nil {
			slog.Error("failed to fetch MR details",
				"error", err,
				"repo", event.Repo.FullName,
				"mr", event.PR.Number,
			)
			return
		}
	}

	slog.Info("executing command",
		"command", cmd.Name,
		"repo", event.Repo.FullName,
		"mr", event.PR.Number,
		"user", event.Comment.Author.Login,
	)

	if err := h.cmdExecutor.Execute(ctx, cmd, event); err != nil {
		slog.Error("command execution failed",
			"error", err,
			"command", cmd.Name,
			"repo", event.Repo.FullName,
			"mr", event.PR.Number,
		)
	}
}

// enrichMRInfo fetches full MR details from the VCS provider and populates missing fields.
func (h *GitLabHandler) enrichMRInfo(ctx context.Context, event *models.PREvent) error {
	mr, err := h.vcsClient.GetPR(ctx, event.Repo.Owner, event.Repo.Name, event.PR.Number)
	if err != nil {
		return err
	}

	event.PR.HeadSHA = mr.HeadSHA
	event.PR.HeadRef = mr.HeadRef
	event.PR.BaseRef = mr.BaseRef
	event.PR.State = mr.State
	event.PR.Title = mr.Title
	event.PR.Draft = mr.Draft
	event.PR.Merged = mr.Merged

	slog.Debug("enriched MR info from API",
		"mr", event.PR.Number,
		"head_ref", event.PR.HeadRef,
		"base_ref", event.PR.BaseRef,
		"head_sha", event.PR.HeadSHA,
	)

	return nil
}
