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

package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/go-github/v60/github"

	"github.com/org/lemuria/internal/models"
)

const (
	// CommentMarker is used to identify Lemuria comments for updates.
	CommentMarker = "<!-- lemuria -->"
	// AppCommentMarker identifies comments for a specific application.
	AppCommentMarker = "<!-- lemuria:app:%s -->"
	// PlanCommentMarker identifies comments containing plan/diff results.
	PlanCommentMarker = "<!-- lemuria:plan -->"
	// StaleMarker indicates the comment is outdated.
	StaleMarker = "<!-- lemuria:stale -->"
	// StaleNotice is prepended to invalidated comments.
	StaleNotice = "> ⚠️ **This plan is outdated.** New changes have been pushed to this PR.\n\n"

	// MaxCommentBodySize is GitHub's maximum comment body size in characters.
	MaxCommentBodySize = 65536
)

// CreateComment posts a new comment on a PR.
func (c *Client) CreateComment(ctx context.Context, owner, repo string, number int, body string) (*github.IssueComment, error) {
	client, err := c.getInstallationClient(ctx, owner)
	if err != nil {
		return nil, err
	}

	comment, _, err := client.Issues.CreateComment(ctx, owner, repo, number, &github.IssueComment{
		Body: github.String(body),
	})
	if err != nil {
		return nil, fmt.Errorf("creating comment: %w", err)
	}

	return comment, nil
}

// editComment updates an existing comment (internal helper).
func (c *Client) editComment(ctx context.Context, owner, repo string, commentID int64, body string) (*github.IssueComment, error) {
	client, err := c.getInstallationClient(ctx, owner)
	if err != nil {
		return nil, err
	}

	comment, _, err := client.Issues.EditComment(ctx, owner, repo, commentID, &github.IssueComment{
		Body: github.String(body),
	})
	if err != nil {
		return nil, fmt.Errorf("updating comment: %w", err)
	}

	return comment, nil
}

// UpdateComment updates an existing comment (VCS interface method).
// The number parameter is ignored for GitHub since commentID is sufficient.
// The Lemuria marker is prepended to preserve comment identification.
func (c *Client) UpdateComment(ctx context.Context, owner, repo string, _ int, commentID int64, body string) error {
	markedBody := CommentMarker + "\n" + body
	_, err := c.editComment(ctx, owner, repo, commentID, markedBody)
	return err
}

// DeleteComment deletes a comment.
func (c *Client) DeleteComment(ctx context.Context, owner, repo string, commentID int64) error {
	client, err := c.getInstallationClient(ctx, owner)
	if err != nil {
		return err
	}

	_, err = client.Issues.DeleteComment(ctx, owner, repo, commentID)
	if err != nil {
		return fmt.Errorf("deleting comment: %w", err)
	}

	return nil
}

// FindLemuriComment finds an existing Lemuria comment on a PR.
func (c *Client) FindLemuriComment(ctx context.Context, owner, repo string, number int, appName string) (*github.IssueComment, error) {
	client, err := c.getInstallationClient(ctx, owner)
	if err != nil {
		return nil, err
	}

	marker := CommentMarker
	if appName != "" {
		marker = fmt.Sprintf(AppCommentMarker, appName)
	}

	opts := &github.IssueListCommentsOptions{
		ListOptions: github.ListOptions{PerPage: 100},
	}

	for {
		comments, resp, err := client.Issues.ListComments(ctx, owner, repo, number, opts)
		if err != nil {
			return nil, fmt.Errorf("listing comments: %w", err)
		}

		for _, comment := range comments {
			if strings.Contains(comment.GetBody(), marker) {
				return comment, nil
			}
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return nil, nil
}

// UpsertComment creates or updates a Lemuria comment on a PR.
func (c *Client) UpsertComment(ctx context.Context, owner, repo string, number int, appName, body string) (*github.IssueComment, error) {
	// Add marker to body
	marker := CommentMarker
	if appName != "" {
		marker = fmt.Sprintf(AppCommentMarker, appName)
	}
	markedBody := marker + "\n" + body

	// Find existing comment
	existing, err := c.FindLemuriComment(ctx, owner, repo, number, appName)
	if err != nil {
		return nil, err
	}

	if existing != nil {
		return c.editComment(ctx, owner, repo, existing.GetID(), markedBody)
	}

	return c.CreateComment(ctx, owner, repo, number, markedBody)
}

// AddReaction adds a reaction to a comment.
func (c *Client) AddReaction(ctx context.Context, owner, repo string, commentID int64, reaction string) error {
	client, err := c.getInstallationClient(ctx, owner)
	if err != nil {
		return err
	}

	_, _, err = client.Reactions.CreateIssueCommentReaction(ctx, owner, repo, commentID, reaction)
	if err != nil {
		return fmt.Errorf("adding reaction: %w", err)
	}

	return nil
}

// PostComment creates a new comment on a PR (never updates existing).
func (c *Client) PostComment(ctx context.Context, owner, repo string, number int, body string, isPlan bool) (*models.CommentResult, error) {
	// Add markers to body
	markers := CommentMarker
	if isPlan {
		markers = CommentMarker + "\n" + PlanCommentMarker
	}
	markedBody := markers + "\n" + body

	comment, err := c.CreateComment(ctx, owner, repo, number, markedBody)
	if err != nil {
		return nil, err
	}

	return &models.CommentResult{
		ID: comment.GetID(),
	}, nil
}

// MaxCommentSize returns a safe usable comment body size.
// This is set below GitHub's absolute 65536-char limit to provide buffer
// for internal markers prepended by PostComment.
func (c *Client) MaxCommentSize() int {
	return 60000
}

// InvalidatePlanComments marks all existing plan comments on a PR as stale
// and minimizes (hides) them using GitHub's GraphQL API.
func (c *Client) InvalidatePlanComments(ctx context.Context, owner, repo string, number int) error {
	client, err := c.getInstallationClient(ctx, owner)
	if err != nil {
		return err
	}

	opts := &github.IssueListCommentsOptions{
		ListOptions: github.ListOptions{PerPage: 100},
	}

	for {
		comments, resp, err := client.Issues.ListComments(ctx, owner, repo, number, opts)
		if err != nil {
			return fmt.Errorf("listing comments: %w", err)
		}

		for _, comment := range comments {
			body := comment.GetBody()
			// Only invalidate plan comments that aren't already stale
			if strings.Contains(body, PlanCommentMarker) && !strings.Contains(body, StaleMarker) {
				// Add stale marker and notice
				newBody := strings.Replace(body, PlanCommentMarker, PlanCommentMarker+"\n"+StaleMarker, 1)
				// Find the end of markers and insert stale notice
				markerEnd := strings.Index(newBody, "\n## ")
				if markerEnd == -1 {
					markerEnd = strings.Index(newBody, "\n#")
				}
				if markerEnd != -1 {
					newBody = newBody[:markerEnd+1] + StaleNotice + newBody[markerEnd+1:]
				}

				_, err := c.editComment(ctx, owner, repo, comment.GetID(), newBody)
				if err != nil {
					return fmt.Errorf("invalidating comment %d: %w", comment.GetID(), err)
				}

				// Minimize (hide) the comment so it collapses in the PR timeline
				if nodeID := comment.GetNodeID(); nodeID != "" {
					if err := c.minimizeComment(ctx, client, nodeID); err != nil {
						slog.Warn("failed to minimize comment",
							"comment_id", comment.GetID(),
							"error", err,
						)
					}
				}
			}
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return nil
}

// minimizeComment hides a comment using GitHub's GraphQL minimizeComment mutation.
// The comment is classified as OUTDATED so it collapses in the PR timeline.
func (c *Client) minimizeComment(ctx context.Context, client *github.Client, nodeID string) error {
	query := `mutation($id: ID!) {
		minimizeComment(input: {subjectId: $id, classifier: OUTDATED}) {
			minimizedComment {
				isMinimized
			}
		}
	}`

	payload := struct {
		Query     string         `json:"query"`
		Variables map[string]any `json:"variables"`
	}{
		Query: query,
		Variables: map[string]any{
			"id": nodeID,
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshaling GraphQL request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.github.com/graphql", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating GraphQL request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.BareDo(ctx, req)
	if err != nil {
		return fmt.Errorf("GraphQL minimizeComment: %w", err)
	}
	defer func() {
		err = resp.Body.Close()
		if err != nil {
			slog.Warn("failed to close GraphQL response body",
				"comment_node_id", nodeID,
				"error", err,
			)
		}
	}()

	return nil
}
