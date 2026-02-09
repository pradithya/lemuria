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

package gitlab

import (
	"context"
	"fmt"

	gogitlab "gitlab.com/gitlab-org/api/client-go"

	"github.com/org/lemuria/internal/config"
	"github.com/org/lemuria/internal/models"
)

// Client wraps the GitLab API client with Lemuria-specific functionality.
type Client struct {
	client *gogitlab.Client
}

// NewClient creates a new GitLab client using personal/group access token authentication.
func NewClient(cfg config.GitLabConfig) (*Client, error) {
	baseURL := cfg.URL
	if baseURL == "" {
		baseURL = "https://gitlab.com"
	}

	client, err := gogitlab.NewClient(cfg.Token, gogitlab.WithBaseURL(baseURL+"/api/v4"))
	if err != nil {
		return nil, fmt.Errorf("creating GitLab client: %w", err)
	}

	return &Client{
		client: client,
	}, nil
}

// projectPath combines the owner (namespace/group) and repo (project name) into
// the full project path used by the GitLab API.
func projectPath(owner, repo string) string {
	return owner + "/" + repo
}

// GetPR fetches merge request details and returns a provider-agnostic PullRequestDetail.
func (c *Client) GetPR(ctx context.Context, owner, repo string, number int) (*models.PullRequestDetail, error) {
	mr, _, err := c.client.MergeRequests.GetMergeRequest(projectPath(owner, repo), int64(number), nil, gogitlab.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("getting MR !%d: %w", number, err)
	}

	rawState := mr.State
	merged := rawState == "merged"
	draft := mr.Draft
	mergeable := mr.DetailedMergeStatus == "mergeable"

	// Normalize GitLab state to canonical values used by the rest of the codebase.
	state := normalizeState(rawState)

	return &models.PullRequestDetail{
		Number:    int(mr.IID),
		Title:     mr.Title,
		State:     state,
		Draft:     draft,
		Merged:    merged,
		Mergeable: mergeable,
		HeadSHA:   mr.SHA,
		HeadRef:   mr.SourceBranch,
		BaseRef:   mr.TargetBranch,
	}, nil
}

// IsPRApproved checks if the merge request has been approved by at least one reviewer.
func (c *Client) IsPRApproved(ctx context.Context, owner, repo string, number int) (bool, error) {
	approvals, _, err := c.client.MergeRequestApprovals.GetConfiguration(projectPath(owner, repo), int64(number), gogitlab.WithContext(ctx))
	if err != nil {
		return false, fmt.Errorf("getting MR !%d approvals: %w", number, err)
	}

	return len(approvals.ApprovedBy) > 0, nil
}

// MergePullRequest merges a merge request. When method is "squash", the Squash
// option is set so GitLab squashes all commits into one before merging.
func (c *Client) MergePullRequest(ctx context.Context, owner, repo string, number int, title, message, method string) error {
	opts := &gogitlab.AcceptMergeRequestOptions{}

	// Prefer message over title for the commit message.
	var commitMessage string
	if message != "" {
		commitMessage = message
	} else if title != "" {
		commitMessage = title
	}

	if commitMessage != "" {
		opts.MergeCommitMessage = gogitlab.Ptr(commitMessage)
	}
	if method == "squash" {
		opts.Squash = gogitlab.Ptr(true)
		if commitMessage != "" {
			opts.SquashCommitMessage = gogitlab.Ptr(commitMessage)
		}
	}

	_, _, err := c.client.MergeRequests.AcceptMergeRequest(projectPath(owner, repo), int64(number), opts, gogitlab.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("merging MR !%d: %w", number, err)
	}

	return nil
}

// normalizeState normalizes GitLab MR states ("opened", "merged", "closed")
// to the canonical values ("open", "closed") used by the rest of the codebase.
func normalizeState(state string) models.PRState {
	switch state {
	case "opened":
		return models.PRStateOpen
	case "merged":
		return models.PRStateClosed
	default:
		return models.PRState(state)
	}
}

// DeleteBranch deletes a branch from the repository.
func (c *Client) DeleteBranch(ctx context.Context, owner, repo, branch string) error {
	_, err := c.client.Branches.DeleteBranch(projectPath(owner, repo), branch, gogitlab.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("deleting branch %s: %w", branch, err)
	}

	return nil
}
