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
	"context"
	"fmt"
	"net/http"
	"os"

	"github.com/bradleyfalzon/ghinstallation/v2"
	"github.com/google/go-github/v60/github"

	"github.com/org/lemuria/internal/config"
	"github.com/org/lemuria/internal/models"
)

// Client wraps the GitHub API client with Lemuria-specific functionality.
type Client struct {
	client     *github.Client
	appID      int64
	privateKey []byte
}

// NewClient creates a new GitHub client using GitHub App authentication.
func NewClient(cfg config.GitHubConfig) (*Client, error) {
	// Load private key
	privateKey, err := loadPrivateKey(cfg.AppPrivateKey)
	if err != nil {
		return nil, fmt.Errorf("loading private key: %w", err)
	}

	// Create app transport
	appTransport, err := ghinstallation.NewAppsTransport(http.DefaultTransport, cfg.AppID, privateKey)
	if err != nil {
		return nil, fmt.Errorf("creating app transport: %w", err)
	}

	// Create client with app transport (for getting installation tokens)
	appClient := github.NewClient(&http.Client{Transport: appTransport})

	return &Client{
		client:     appClient,
		appID:      cfg.AppID,
		privateKey: privateKey,
	}, nil
}

// loadPrivateKey loads the private key from a file path or returns it directly if it's the key content.
func loadPrivateKey(pathOrKey string) ([]byte, error) {
	// Check if it's a file path
	if _, err := os.Stat(pathOrKey); err == nil {
		return os.ReadFile(pathOrKey)
	}

	// Otherwise, treat it as the key content itself
	return []byte(pathOrKey), nil
}

// GetInstallationClient returns a client authenticated for a specific installation.
func (c *Client) GetInstallationClient(ctx context.Context, owner string) (*github.Client, error) {
	// List installations to find the one for this owner
	installations, _, err := c.client.Apps.ListInstallations(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("listing installations: %w", err)
	}

	var installationID int64
	for _, inst := range installations {
		if inst.GetAccount().GetLogin() == owner {
			installationID = inst.GetID()
			break
		}
	}

	if installationID == 0 {
		return nil, fmt.Errorf("no installation found for owner %s", owner)
	}

	// Create installation transport
	itr, err := ghinstallation.New(http.DefaultTransport, c.appID, installationID, c.privateKey)
	if err != nil {
		return nil, fmt.Errorf("creating installation transport: %w", err)
	}

	return github.NewClient(&http.Client{Transport: itr}), nil
}

// GetRepoConfig fetches the .lemuria.yaml configuration from a repository.
func (c *Client) GetRepoConfig(ctx context.Context, owner, repo, ref string) ([]byte, error) {
	client, err := c.GetInstallationClient(ctx, owner)
	if err != nil {
		return nil, err
	}

	content, _, _, err := client.Repositories.GetContents(ctx, owner, repo, ".lemuria.yaml", &github.RepositoryContentGetOptions{
		Ref: ref,
	})
	if err != nil {
		return nil, fmt.Errorf("getting .lemuria.yaml: %w", err)
	}

	decoded, err := content.GetContent()
	if err != nil {
		return nil, fmt.Errorf("decoding content: %w", err)
	}

	return []byte(decoded), nil
}

// GetPR fetches pull request details and returns a provider-agnostic PullRequestDetail.
func (c *Client) GetPR(ctx context.Context, owner, repo string, number int) (*models.PullRequestDetail, error) {
	pr, err := c.GetPRRaw(ctx, owner, repo, number)
	if err != nil {
		return nil, err
	}

	return &models.PullRequestDetail{
		Number:    pr.GetNumber(),
		Title:     pr.GetTitle(),
		State:     models.PRState(pr.GetState()),
		Draft:     pr.GetDraft(),
		Merged:    pr.GetMerged(),
		Mergeable: pr.GetMergeable(),
		HeadSHA:   pr.GetHead().GetSHA(),
		HeadRef:   pr.GetHead().GetRef(),
		BaseRef:   pr.GetBase().GetRef(),
	}, nil
}

// GetPRRaw fetches the raw GitHub PullRequest object.
// Used by the webhook handler to enrich PR info with GitHub-specific fields.
func (c *Client) GetPRRaw(ctx context.Context, owner, repo string, number int) (*github.PullRequest, error) {
	client, err := c.GetInstallationClient(ctx, owner)
	if err != nil {
		return nil, err
	}

	pr, _, err := client.PullRequests.Get(ctx, owner, repo, number)
	if err != nil {
		return nil, fmt.Errorf("getting PR: %w", err)
	}

	return pr, nil
}

// IsPRApproved checks if the PR has been approved.
func (c *Client) IsPRApproved(ctx context.Context, owner, repo string, number int) (bool, error) {
	client, err := c.GetInstallationClient(ctx, owner)
	if err != nil {
		return false, err
	}

	reviews, _, err := client.PullRequests.ListReviews(ctx, owner, repo, number, nil)
	if err != nil {
		return false, fmt.Errorf("listing reviews: %w", err)
	}

	// Check for at least one approval without a subsequent request for changes
	approvals := make(map[string]bool)
	for _, review := range reviews {
		user := review.GetUser().GetLogin()
		switch review.GetState() {
		case "APPROVED":
			approvals[user] = true
		case "CHANGES_REQUESTED":
			approvals[user] = false
		}
	}

	for _, approved := range approvals {
		if approved {
			return true, nil
		}
	}

	return false, nil
}

// MergePullRequest merges a pull request using the specified method.
// Supported methods: "merge", "squash", "rebase".
func (c *Client) MergePullRequest(ctx context.Context, owner, repo string, number int, title, message, method string) error {
	client, err := c.GetInstallationClient(ctx, owner)
	if err != nil {
		return err
	}

	opts := &github.PullRequestOptions{
		MergeMethod: method,
	}

	// Use title as commit message for squash/merge
	commitMessage := message
	if commitMessage == "" {
		commitMessage = title
	}

	_, _, err = client.PullRequests.Merge(ctx, owner, repo, number, commitMessage, opts)
	if err != nil {
		return fmt.Errorf("merging PR #%d: %w", number, err)
	}

	return nil
}

// DeleteBranch deletes a branch from the repository.
func (c *Client) DeleteBranch(ctx context.Context, owner, repo, branch string) error {
	client, err := c.GetInstallationClient(ctx, owner)
	if err != nil {
		return err
	}

	_, err = client.Git.DeleteRef(ctx, owner, repo, "heads/"+branch)
	if err != nil {
		return fmt.Errorf("deleting branch %s: %w", branch, err)
	}

	return nil
}
