package models

import "time"

// EventType represents the type of GitHub webhook event.
type EventType string

const (
	EventTypePullRequest       EventType = "pull_request"
	EventTypeIssueComment      EventType = "issue_comment"
	EventTypePullRequestReview EventType = "pull_request_review"
)

// PREvent represents a parsed pull request webhook event.
type PREvent struct {
	Type       EventType `json:"type"`
	Action     string    `json:"action"`
	Repo       RepoInfo  `json:"repo"`
	PR         PRInfo    `json:"pr"`
	Comment    *Comment  `json:"comment,omitempty"`
	Sender     UserInfo  `json:"sender"`
	ReceivedAt time.Time `json:"receivedAt"`
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
	State     string    `json:"state"`
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
	Filename  string `json:"filename"`
	Status    string `json:"status"` // added, removed, modified, renamed
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Changes   int    `json:"changes"`
	Patch     string `json:"patch,omitempty"`
}

// IsPROpen returns true if the PR is in an open state.
func (e *PREvent) IsPROpen() bool {
	return e.PR.State == "open" && !e.PR.Merged
}

// IsPRMerged returns true if the PR has been merged.
func (e *PREvent) IsPRMerged() bool {
	return e.PR.Merged
}

// IsPRClosed returns true if the PR is closed (not merged).
func (e *PREvent) IsPRClosed() bool {
	return e.PR.State == "closed" && !e.PR.Merged
}

// ShouldAutoplan returns true if this event should trigger autoplan.
func (e *PREvent) ShouldAutoplan() bool {
	if e.Type != EventTypePullRequest {
		return false
	}

	// Trigger on PR open or synchronize (new commits)
	return e.Action == "opened" || e.Action == "synchronize"
}

// ShouldUnlockAll returns true if this event should release all locks.
func (e *PREvent) ShouldUnlockAll() bool {
	if e.Type != EventTypePullRequest {
		return false
	}

	// Release locks on PR close or merge
	return e.Action == "closed"
}
