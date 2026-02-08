package webhook

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/org/lemuria/internal/models"
)

// ParseGitLabEvent parses a GitLab webhook event into a PREvent.
func ParseGitLabEvent(eventType string, payload []byte) (*models.PREvent, error) {
	switch eventType {
	case "Merge Request Hook":
		return parseMergeRequestEvent(payload)
	case "Note Hook":
		return parseNoteEvent(payload)
	default:
		// Unhandled event type
		return nil, nil
	}
}

// parseMergeRequestEvent parses a GitLab Merge Request Hook event.
func parseMergeRequestEvent(payload []byte) (*models.PREvent, error) {
	var raw struct {
		ObjectAttributes struct {
			IID          int    `json:"iid"`
			Title        string `json:"title"`
			Description  string `json:"description"`
			State        string `json:"state"`
			Action       string `json:"action"`
			Draft        bool   `json:"draft"`
			WorkInProg   bool   `json:"work_in_progress"`
			SourceBranch string `json:"source_branch"`
			TargetBranch string `json:"target_branch"`
			LastCommit   struct {
				ID string `json:"id"`
			} `json:"last_commit"`
			URL       string `json:"url"`
			UpdatedAt string `json:"updated_at"`
		} `json:"object_attributes"`
		Project struct {
			Name              string `json:"name"`
			PathWithNamespace string `json:"path_with_namespace"`
			WebURL            string `json:"web_url"`
			GitHTTPURL        string `json:"git_http_url"`
			Namespace         string `json:"namespace"`
		} `json:"project"`
		User struct {
			Username  string `json:"username"`
			Name      string `json:"name"`
			AvatarURL string `json:"avatar_url"`
			ID        int64  `json:"id"`
		} `json:"user"`
	}

	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, fmt.Errorf("parsing merge_request event: %w", err)
	}

	// Map GitLab actions to normalized actions
	action := mapGitLabMRAction(raw.ObjectAttributes.Action)

	// Normalize state
	state := normalizeGitLabState(raw.ObjectAttributes.State)

	// Determine if the MR was merged
	merged := raw.ObjectAttributes.Action == "merge"

	// Extract owner and name from path_with_namespace (e.g., "group/subgroup/project")
	owner, name := splitPathWithNamespace(raw.ObjectAttributes.Action, raw.Project.PathWithNamespace, raw.Project.Name)

	updatedAt, _ := time.Parse("2006-01-02 15:04:05 UTC", raw.ObjectAttributes.UpdatedAt)

	return &models.PREvent{
		Provider: models.VCSProviderGitLab,
		Type:     models.EventTypePullRequest,
		Action:   action,
		Repo: models.RepoInfo{
			Owner:    owner,
			Name:     name,
			FullName: raw.Project.PathWithNamespace,
			CloneURL: raw.Project.GitHTTPURL,
			HTMLURL:  raw.Project.WebURL,
		},
		PR: models.PRInfo{
			Number:  raw.ObjectAttributes.IID,
			Title:   raw.ObjectAttributes.Title,
			Body:    raw.ObjectAttributes.Description,
			State:   state,
			Draft:   raw.ObjectAttributes.Draft || raw.ObjectAttributes.WorkInProg,
			Merged:  merged,
			HeadSHA: raw.ObjectAttributes.LastCommit.ID,
			HeadRef: raw.ObjectAttributes.SourceBranch,
			BaseRef: raw.ObjectAttributes.TargetBranch,
			Author: models.UserInfo{
				Login:     raw.User.Username,
				ID:        raw.User.ID,
				AvatarURL: raw.User.AvatarURL,
			},
			HTMLURL:   raw.ObjectAttributes.URL,
			UpdatedAt: updatedAt,
		},
		Sender: models.UserInfo{
			Login:     raw.User.Username,
			ID:        raw.User.ID,
			AvatarURL: raw.User.AvatarURL,
		},
		ReceivedAt: time.Now(),
	}, nil
}

// parseNoteEvent parses a GitLab Note Hook event (comment on MR).
func parseNoteEvent(payload []byte) (*models.PREvent, error) {
	var raw struct {
		ObjectAttributes struct {
			ID           int64  `json:"id"`
			Note         string `json:"note"`
			NoteableType string `json:"noteable_type"`
			CreatedAt    string `json:"created_at"`
		} `json:"object_attributes"`
		MergeRequest struct {
			IID          int    `json:"iid"`
			Title        string `json:"title"`
			Description  string `json:"description"`
			State        string `json:"state"`
			Draft        bool   `json:"draft"`
			WorkInProg   bool   `json:"work_in_progress"`
			SourceBranch string `json:"source_branch"`
			TargetBranch string `json:"target_branch"`
			LastCommit   struct {
				ID string `json:"id"`
			} `json:"last_commit"`
			URL string `json:"url"`
		} `json:"merge_request"`
		Project struct {
			Name              string `json:"name"`
			PathWithNamespace string `json:"path_with_namespace"`
			WebURL            string `json:"web_url"`
			GitHTTPURL        string `json:"git_http_url"`
			Namespace         string `json:"namespace"`
		} `json:"project"`
		User struct {
			Username  string `json:"username"`
			Name      string `json:"name"`
			AvatarURL string `json:"avatar_url"`
			ID        int64  `json:"id"`
		} `json:"user"`
	}

	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, fmt.Errorf("parsing note event: %w", err)
	}

	// Only handle notes on merge requests
	if raw.ObjectAttributes.NoteableType != "MergeRequest" {
		return nil, nil
	}

	// Extract owner and name from path_with_namespace
	owner, name := splitPathWithNamespace("", raw.Project.PathWithNamespace, raw.Project.Name)

	createdAt, _ := time.Parse("2006-01-02 15:04:05 UTC", raw.ObjectAttributes.CreatedAt)

	// Normalize state
	state := normalizeGitLabState(raw.MergeRequest.State)

	return &models.PREvent{
		Provider: models.VCSProviderGitLab,
		Type:     models.EventTypeIssueComment,
		Action:   "created",
		Repo: models.RepoInfo{
			Owner:    owner,
			Name:     name,
			FullName: raw.Project.PathWithNamespace,
			CloneURL: raw.Project.GitHTTPURL,
			HTMLURL:  raw.Project.WebURL,
		},
		PR: models.PRInfo{
			Number:  raw.MergeRequest.IID,
			Title:   raw.MergeRequest.Title,
			Body:    raw.MergeRequest.Description,
			State:   state,
			Draft:   raw.MergeRequest.Draft || raw.MergeRequest.WorkInProg,
			HeadSHA: raw.MergeRequest.LastCommit.ID,
			HeadRef: raw.MergeRequest.SourceBranch,
			BaseRef: raw.MergeRequest.TargetBranch,
			Author: models.UserInfo{
				Login:     raw.User.Username,
				ID:        raw.User.ID,
				AvatarURL: raw.User.AvatarURL,
			},
			HTMLURL: raw.MergeRequest.URL,
		},
		Comment: &models.Comment{
			ID:   raw.ObjectAttributes.ID,
			Body: raw.ObjectAttributes.Note,
			Author: models.UserInfo{
				Login:     raw.User.Username,
				ID:        raw.User.ID,
				AvatarURL: raw.User.AvatarURL,
			},
			CreatedAt: createdAt,
		},
		Sender: models.UserInfo{
			Login:     raw.User.Username,
			ID:        raw.User.ID,
			AvatarURL: raw.User.AvatarURL,
		},
		ReceivedAt: time.Now(),
	}, nil
}

// mapGitLabMRAction maps GitLab merge request actions to normalized actions
// that match the GitHub-style actions used by the rest of the codebase.
func mapGitLabMRAction(action string) string {
	switch action {
	case "open":
		return "opened"
	case "update":
		return "synchronize"
	case "close":
		return "closed"
	case "merge":
		return "closed"
	default:
		return action
	}
}

// normalizeGitLabState normalizes GitLab MR states to the canonical states
// used throughout the codebase ("open" or "closed").
func normalizeGitLabState(state string) string {
	switch state {
	case "opened":
		return "open"
	case "merged":
		return "closed"
	case "closed":
		return "closed"
	default:
		return state
	}
}

// splitPathWithNamespace extracts owner (namespace/group) and project name
// from GitLab's path_with_namespace (e.g., "group/subgroup/project").
// The project name portion is the last segment; everything before it is the owner.
func splitPathWithNamespace(_ string, pathWithNamespace, projectName string) (owner, name string) {
	name = projectName
	// path_with_namespace = "group/subgroup/project"
	// owner = "group/subgroup", name = "project"
	idx := strings.LastIndex(pathWithNamespace, "/")
	if idx >= 0 {
		owner = pathWithNamespace[:idx]
		name = pathWithNamespace[idx+1:]
	} else {
		owner = pathWithNamespace
	}
	return owner, name
}
