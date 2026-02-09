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
	"encoding/json"
	"fmt"
	"time"

	"github.com/org/lemuria/internal/models"
)

// ParseGitHubEvent parses a GitHub webhook event into a PREvent.
func ParseGitHubEvent(eventType string, payload []byte) (*models.PREvent, error) {
	switch eventType {
	case "pull_request":
		return parsePullRequestEvent(payload)
	case "issue_comment":
		return parseIssueCommentEvent(payload)
	case "pull_request_review":
		return parsePullRequestReviewEvent(payload)
	default:
		// Unhandled event type
		return nil, nil
	}
}

// parsePullRequestEvent parses a pull_request webhook event.
func parsePullRequestEvent(payload []byte) (*models.PREvent, error) {
	var raw struct {
		Action      string `json:"action"`
		Number      int    `json:"number"`
		PullRequest struct {
			Number    int    `json:"number"`
			Title     string `json:"title"`
			Body      string `json:"body"`
			State     string `json:"state"`
			Draft     bool   `json:"draft"`
			Merged    bool   `json:"merged"`
			Mergeable *bool  `json:"mergeable"`
			Head      struct {
				SHA string `json:"sha"`
				Ref string `json:"ref"`
			} `json:"head"`
			Base struct {
				Ref string `json:"ref"`
			} `json:"base"`
			User struct {
				Login     string `json:"login"`
				ID        int64  `json:"id"`
				AvatarURL string `json:"avatar_url"`
			} `json:"user"`
			HTMLURL   string `json:"html_url"`
			UpdatedAt string `json:"updated_at"`
		} `json:"pull_request"`
		Repository struct {
			Name     string `json:"name"`
			FullName string `json:"full_name"`
			Owner    struct {
				Login string `json:"login"`
			} `json:"owner"`
			CloneURL string `json:"clone_url"`
			HTMLURL  string `json:"html_url"`
		} `json:"repository"`
		Sender struct {
			Login     string `json:"login"`
			ID        int64  `json:"id"`
			AvatarURL string `json:"avatar_url"`
		} `json:"sender"`
	}

	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, fmt.Errorf("parsing pull_request event: %w", err)
	}

	updatedAt, _ := time.Parse(time.RFC3339, raw.PullRequest.UpdatedAt)

	return &models.PREvent{
		Provider: models.VCSProviderGitHub,
		Type:     models.EventTypePullRequest,
		Action:   models.PRAction(raw.Action),
		Repo: models.RepoInfo{
			Owner:    raw.Repository.Owner.Login,
			Name:     raw.Repository.Name,
			FullName: raw.Repository.FullName,
			CloneURL: raw.Repository.CloneURL,
			HTMLURL:  raw.Repository.HTMLURL,
		},
		PR: models.PRInfo{
			Number:    raw.PullRequest.Number,
			Title:     raw.PullRequest.Title,
			Body:      raw.PullRequest.Body,
			State:     models.PRState(raw.PullRequest.State),
			Draft:     raw.PullRequest.Draft,
			Merged:    raw.PullRequest.Merged,
			Mergeable: raw.PullRequest.Mergeable,
			HeadSHA:   raw.PullRequest.Head.SHA,
			HeadRef:   raw.PullRequest.Head.Ref,
			BaseRef:   raw.PullRequest.Base.Ref,
			Author: models.UserInfo{
				Login:     raw.PullRequest.User.Login,
				ID:        raw.PullRequest.User.ID,
				AvatarURL: raw.PullRequest.User.AvatarURL,
			},
			HTMLURL:   raw.PullRequest.HTMLURL,
			UpdatedAt: updatedAt,
		},
		Sender: models.UserInfo{
			Login:     raw.Sender.Login,
			ID:        raw.Sender.ID,
			AvatarURL: raw.Sender.AvatarURL,
		},
		ReceivedAt: time.Now(),
	}, nil
}

// parseIssueCommentEvent parses an issue_comment webhook event.
func parseIssueCommentEvent(payload []byte) (*models.PREvent, error) {
	var raw struct {
		Action string `json:"action"`
		Issue  struct {
			Number      int `json:"number"`
			PullRequest *struct {
				URL string `json:"url"`
			} `json:"pull_request"`
		} `json:"issue"`
		Comment struct {
			ID   int64  `json:"id"`
			Body string `json:"body"`
			User struct {
				Login     string `json:"login"`
				ID        int64  `json:"id"`
				AvatarURL string `json:"avatar_url"`
			} `json:"user"`
			CreatedAt string `json:"created_at"`
		} `json:"comment"`
		Repository struct {
			Name     string `json:"name"`
			FullName string `json:"full_name"`
			Owner    struct {
				Login string `json:"login"`
			} `json:"owner"`
			CloneURL string `json:"clone_url"`
			HTMLURL  string `json:"html_url"`
		} `json:"repository"`
		Sender struct {
			Login     string `json:"login"`
			ID        int64  `json:"id"`
			AvatarURL string `json:"avatar_url"`
		} `json:"sender"`
	}

	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, fmt.Errorf("parsing issue_comment event: %w", err)
	}

	// Only handle comments on PRs
	if raw.Issue.PullRequest == nil {
		return nil, nil
	}

	createdAt, _ := time.Parse(time.RFC3339, raw.Comment.CreatedAt)

	return &models.PREvent{
		Provider: models.VCSProviderGitHub,
		Type:     models.EventTypeIssueComment,
		Action:   models.PRAction(raw.Action),
		Repo: models.RepoInfo{
			Owner:    raw.Repository.Owner.Login,
			Name:     raw.Repository.Name,
			FullName: raw.Repository.FullName,
			CloneURL: raw.Repository.CloneURL,
			HTMLURL:  raw.Repository.HTMLURL,
		},
		PR: models.PRInfo{
			Number: raw.Issue.Number,
		},
		Comment: &models.Comment{
			ID:   raw.Comment.ID,
			Body: raw.Comment.Body,
			Author: models.UserInfo{
				Login:     raw.Comment.User.Login,
				ID:        raw.Comment.User.ID,
				AvatarURL: raw.Comment.User.AvatarURL,
			},
			CreatedAt: createdAt,
		},
		Sender: models.UserInfo{
			Login:     raw.Sender.Login,
			ID:        raw.Sender.ID,
			AvatarURL: raw.Sender.AvatarURL,
		},
		ReceivedAt: time.Now(),
	}, nil
}

// parsePullRequestReviewEvent parses a pull_request_review webhook event.
func parsePullRequestReviewEvent(payload []byte) (*models.PREvent, error) {
	var raw struct {
		Action string `json:"action"`
		Review struct {
			State string `json:"state"`
			User  struct {
				Login     string `json:"login"`
				ID        int64  `json:"id"`
				AvatarURL string `json:"avatar_url"`
			} `json:"user"`
		} `json:"review"`
		PullRequest struct {
			Number int    `json:"number"`
			Title  string `json:"title"`
			State  string `json:"state"`
			Draft  bool   `json:"draft"`
			Merged bool   `json:"merged"`
			Head   struct {
				SHA string `json:"sha"`
				Ref string `json:"ref"`
			} `json:"head"`
			Base struct {
				Ref string `json:"ref"`
			} `json:"base"`
			User struct {
				Login     string `json:"login"`
				ID        int64  `json:"id"`
				AvatarURL string `json:"avatar_url"`
			} `json:"user"`
			HTMLURL string `json:"html_url"`
		} `json:"pull_request"`
		Repository struct {
			Name     string `json:"name"`
			FullName string `json:"full_name"`
			Owner    struct {
				Login string `json:"login"`
			} `json:"owner"`
			CloneURL string `json:"clone_url"`
			HTMLURL  string `json:"html_url"`
		} `json:"repository"`
		Sender struct {
			Login     string `json:"login"`
			ID        int64  `json:"id"`
			AvatarURL string `json:"avatar_url"`
		} `json:"sender"`
	}

	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, fmt.Errorf("parsing pull_request_review event: %w", err)
	}

	return &models.PREvent{
		Provider: models.VCSProviderGitHub,
		Type:     models.EventTypePullRequestReview,
		Action:   models.PRAction(raw.Action),
		Repo: models.RepoInfo{
			Owner:    raw.Repository.Owner.Login,
			Name:     raw.Repository.Name,
			FullName: raw.Repository.FullName,
			CloneURL: raw.Repository.CloneURL,
			HTMLURL:  raw.Repository.HTMLURL,
		},
		PR: models.PRInfo{
			Number:  raw.PullRequest.Number,
			Title:   raw.PullRequest.Title,
			State:   models.PRState(raw.PullRequest.State),
			Draft:   raw.PullRequest.Draft,
			Merged:  raw.PullRequest.Merged,
			HeadSHA: raw.PullRequest.Head.SHA,
			HeadRef: raw.PullRequest.Head.Ref,
			BaseRef: raw.PullRequest.Base.Ref,
			Author: models.UserInfo{
				Login:     raw.PullRequest.User.Login,
				ID:        raw.PullRequest.User.ID,
				AvatarURL: raw.PullRequest.User.AvatarURL,
			},
			HTMLURL: raw.PullRequest.HTMLURL,
		},
		Sender: models.UserInfo{
			Login:     raw.Sender.Login,
			ID:        raw.Sender.ID,
			AvatarURL: raw.Sender.AvatarURL,
		},
		ReceivedAt: time.Now(),
	}, nil
}
