package github

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/go-github/v60/github"
)

const (
	// CommentMarker is used to identify Lemuria comments for updates.
	CommentMarker = "<!-- lemuria -->"
	// AppCommentMarker identifies comments for a specific application.
	AppCommentMarker = "<!-- lemuria:app:%s -->"
)

// CreateComment posts a new comment on a PR.
func (c *Client) CreateComment(ctx context.Context, owner, repo string, number int, body string) (*github.IssueComment, error) {
	client, err := c.GetInstallationClient(ctx, owner)
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

// UpdateComment updates an existing comment.
func (c *Client) UpdateComment(ctx context.Context, owner, repo string, commentID int64, body string) (*github.IssueComment, error) {
	client, err := c.GetInstallationClient(ctx, owner)
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

// DeleteComment deletes a comment.
func (c *Client) DeleteComment(ctx context.Context, owner, repo string, commentID int64) error {
	client, err := c.GetInstallationClient(ctx, owner)
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
	client, err := c.GetInstallationClient(ctx, owner)
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
		return c.UpdateComment(ctx, owner, repo, existing.GetID(), markedBody)
	}

	return c.CreateComment(ctx, owner, repo, number, markedBody)
}

// AddReaction adds a reaction to a comment.
func (c *Client) AddReaction(ctx context.Context, owner, repo string, commentID int64, reaction string) error {
	client, err := c.GetInstallationClient(ctx, owner)
	if err != nil {
		return err
	}

	_, _, err = client.Reactions.CreateIssueCommentReaction(ctx, owner, repo, commentID, reaction)
	if err != nil {
		return fmt.Errorf("adding reaction: %w", err)
	}

	return nil
}
