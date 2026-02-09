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

package models

import (
	"time"

	"github.com/org/lemuria/internal/config"
)

// VCSProvider identifies the version control system hosting the repository.
type VCSProvider string

const (
	VCSProviderGitHub VCSProvider = "github"
	VCSProviderGitLab VCSProvider = "gitlab"
)

// EventType represents the type of webhook event.
type EventType string

const (
	EventTypePullRequest       EventType = "pull_request"
	EventTypeIssueComment      EventType = "issue_comment"
	EventTypePullRequestReview EventType = "pull_request_review"
)

// PRAction represents a normalized webhook action.
type PRAction string

const (
	PRActionOpened      PRAction = "opened"
	PRActionSynchronize PRAction = "synchronize"
	PRActionClosed      PRAction = "closed"
	PRActionCreated     PRAction = "created"
	PRActionSubmitted   PRAction = "submitted"
)

// PRState represents the state of a pull request / merge request.
type PRState string

const (
	PRStateOpen   PRState = "open"
	PRStateClosed PRState = "closed"
)

// FileStatus represents the change status of a file in a PR.
type FileStatus string

const (
	FileStatusAdded    FileStatus = "added"
	FileStatusRemoved  FileStatus = "removed"
	FileStatusModified FileStatus = "modified"
	FileStatusRenamed  FileStatus = "renamed"
)

// PREvent represents a parsed pull request webhook event.
type PREvent struct {
	Provider   VCSProvider `json:"provider"`
	Type       EventType   `json:"type"`
	Action     PRAction    `json:"action"`
	Repo       RepoInfo    `json:"repo"`
	PR         PRInfo      `json:"pr"`
	Comment    *Comment    `json:"comment,omitempty"`
	Sender     UserInfo    `json:"sender"`
	ReceivedAt time.Time   `json:"receivedAt"`

	// RepoConfig is the parsed .lemuria.yaml for this PR's branch.
	// Populated lazily on first access; nil means not yet loaded or absent.
	RepoConfig *config.RepoConfig `json:"-"`
	// repoConfigLoaded tracks whether RepoConfig has been fetched (to distinguish nil config from not-yet-loaded).
	RepoConfigLoaded bool `json:"-"`
}

// RepoInfo contains repository information from the event.
type RepoInfo struct {
	Owner    string `json:"owner"`
	Name     string `json:"name"`
	FullName string `json:"fullName"`
	CloneURL string `json:"cloneURL"`
	HTMLURL  string `json:"htmlURL"`
}

// PRInfo contains pull request information from the event.
type PRInfo struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	State     PRState   `json:"state"`
	Draft     bool      `json:"draft"`
	Merged    bool      `json:"merged"`
	Mergeable *bool     `json:"mergeable,omitempty"`
	HeadSHA   string    `json:"headSHA"`
	HeadRef   string    `json:"headRef"`
	BaseRef   string    `json:"baseRef"`
	Author    UserInfo  `json:"author"`
	HTMLURL   string    `json:"htmlURL"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Comment contains PR comment information.
type Comment struct {
	ID        int64     `json:"id"`
	Body      string    `json:"body"`
	Author    UserInfo  `json:"author"`
	CreatedAt time.Time `json:"createdAt"`
}

// UserInfo contains user information.
type UserInfo struct {
	Login     string `json:"login"`
	ID        int64  `json:"id"`
	AvatarURL string `json:"avatarURL,omitempty"`
}

// ChangedFile represents a file changed in a PR.
type ChangedFile struct {
	Filename  string     `json:"filename"`
	Status    FileStatus `json:"status"` // added, removed, modified, renamed
	Additions int        `json:"additions"`
	Deletions int        `json:"deletions"`
	Changes   int        `json:"changes"`
	Patch     string     `json:"patch,omitempty"`
}

// IsPROpen returns true if the PR is in an open state.
func (e *PREvent) IsPROpen() bool {
	return e.PR.State == PRStateOpen && !e.PR.Merged
}

// IsPRMerged returns true if the PR has been merged.
func (e *PREvent) IsPRMerged() bool {
	return e.PR.Merged
}

// IsPRClosed returns true if the PR is closed (not merged).
func (e *PREvent) IsPRClosed() bool {
	return e.PR.State == PRStateClosed && !e.PR.Merged
}

// ShouldAutoplan returns true if this event should trigger autoplan.
func (e *PREvent) ShouldAutoplan() bool {
	if e.Type != EventTypePullRequest {
		return false
	}

	// Trigger on PR open or synchronize (new commits)
	return e.Action == PRActionOpened || e.Action == PRActionSynchronize
}

// ShouldUnlockAll returns true if this event should release all locks.
func (e *PREvent) ShouldUnlockAll() bool {
	if e.Type != EventTypePullRequest {
		return false
	}

	// Release locks on PR close or merge
	return e.Action == PRActionClosed
}

// PullRequestDetail holds provider-agnostic pull request details.
type PullRequestDetail struct {
	Number    int
	Title     string
	State     PRState
	Draft     bool
	Merged    bool
	Mergeable bool
	HeadSHA   string
	HeadRef   string
	BaseRef   string
}

// CommentResult holds the result of posting a comment.
type CommentResult struct {
	ID int64
}
