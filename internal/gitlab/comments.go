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
	"strings"

	gogitlab "gitlab.com/gitlab-org/api/client-go"

	"github.com/org/lemuria/internal/models"
)

const (
	// CommentMarker is used to identify Lemuria comments for updates.
	CommentMarker = "<!-- lemuria -->"
	// PlanCommentMarker identifies comments containing plan/diff results.
	PlanCommentMarker = "<!-- lemuria:plan -->"
	// StaleMarker indicates the comment is outdated.
	StaleMarker = "<!-- lemuria:stale -->"
	// StaleNotice is prepended to invalidated comments.
	StaleNotice = "> :warning: **This plan is outdated.** New changes have been pushed to this MR.\n\n"

	// MaxNoteBodySize is GitLab's maximum note body size in characters.
	MaxNoteBodySize = 1_000_000
)

// PostComment creates a new note on a merge request with appropriate markers
// and returns the resulting comment ID.
func (c *Client) PostComment(ctx context.Context, owner, repo string, number int, body string, isPlan bool) (*models.CommentResult, error) {
	markers := CommentMarker
	if isPlan {
		markers = CommentMarker + "\n" + PlanCommentMarker
	}
	markedBody := markers + "\n" + body

	note, _, err := c.client.Notes.CreateMergeRequestNote(projectPath(owner, repo), int64(number), &gogitlab.CreateMergeRequestNoteOptions{
		Body: gogitlab.Ptr(markedBody),
	}, gogitlab.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("creating MR note: %w", err)
	}

	return &models.CommentResult{
		ID: int64(note.ID),
	}, nil
}

// AddReaction adds an award emoji to a merge request note.
// Emoji names are mapped from GitHub convention to GitLab equivalents.
func (c *Client) AddReaction(ctx context.Context, owner, repo string, commentID int64, reaction string) error {
	// Map GitHub reaction names to GitLab award emoji names.
	emojiName := reaction
	switch reaction {
	case "+1":
		emojiName = "thumbsup"
	case "-1":
		emojiName = "thumbsdown"
	case "hooray", "tada":
		emojiName = "tada"
	case "laugh":
		emojiName = "laughing"
	}

	project := projectPath(owner, repo)

	// The VCS interface only provides the note ID, not the MR IID. We resolve
	// the MR IID by fetching the note via the project-level notes API endpoint,
	// which returns the noteable_iid field.
	type noteInfo struct {
		NoteableIID int `json:"noteable_iid"`
	}

	var info noteInfo
	req, err := c.client.NewRequest("GET", fmt.Sprintf("projects/%s/notes/%d", gogitlab.PathEscape(project), commentID), nil, nil)
	if err != nil {
		return fmt.Errorf("creating request to get note %d: %w", commentID, err)
	}

	_, err = c.client.Do(req, &info)
	if err != nil {
		return fmt.Errorf("getting note %d info: %w", commentID, err)
	}

	if info.NoteableIID == 0 {
		return fmt.Errorf("could not determine MR IID for note %d", commentID)
	}

	_, _, err = c.client.AwardEmoji.CreateMergeRequestAwardEmojiOnNote(project, int64(info.NoteableIID), commentID, &gogitlab.CreateAwardEmojiOptions{
		Name: emojiName,
	}, gogitlab.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("adding reaction %q to note %d: %w", emojiName, commentID, err)
	}

	return nil
}

// UpdateComment updates an existing merge request note (VCS interface method).
// The Lemuria marker is prepended to preserve comment identification.
func (c *Client) UpdateComment(ctx context.Context, owner, repo string, number int, commentID int64, body string) error {
	markedBody := CommentMarker + "\n" + body
	project := projectPath(owner, repo)
	_, _, err := c.client.Notes.UpdateMergeRequestNote(project, int64(number), commentID, &gogitlab.UpdateMergeRequestNoteOptions{
		Body: gogitlab.Ptr(markedBody),
	}, gogitlab.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("updating MR note %d: %w", commentID, err)
	}
	return nil
}

// MaxCommentSize returns a safe usable note body size.
// This is set below GitLab's absolute 1,000,000-char limit to provide buffer
// for internal markers prepended by PostComment.
func (c *Client) MaxCommentSize() int {
	return 990000
}

// InvalidatePlanComments marks all existing plan comments on a MR as stale
// and wraps the content in a collapsible <details> block.
func (c *Client) InvalidatePlanComments(ctx context.Context, owner, repo string, number int) error {
	project := projectPath(owner, repo)

	opts := &gogitlab.ListMergeRequestNotesOptions{
		ListOptions: gogitlab.ListOptions{PerPage: 100},
	}

	for {
		notes, resp, err := c.client.Notes.ListMergeRequestNotes(project, int64(number), opts, gogitlab.WithContext(ctx))
		if err != nil {
			return fmt.Errorf("listing MR notes: %w", err)
		}

		for _, note := range notes {
			body := note.Body
			// Only invalidate plan comments that aren't already stale
			if strings.Contains(body, PlanCommentMarker) && !strings.Contains(body, StaleMarker) {
				newBody := buildStaleBody(body)

				_, _, err := c.client.Notes.UpdateMergeRequestNote(project, int64(number), note.ID, &gogitlab.UpdateMergeRequestNoteOptions{
					Body: gogitlab.Ptr(newBody),
				}, gogitlab.WithContext(ctx))
				if err != nil {
					return fmt.Errorf("invalidating note %d: %w", note.ID, err)
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

// buildStaleBody wraps a plan comment body in a collapsible <details> block
// with a stale notice, so outdated plans are visually collapsed in the MR timeline.
func buildStaleBody(body string) string {
	// Add stale marker
	markerBody := strings.Replace(body, PlanCommentMarker, PlanCommentMarker+"\n"+StaleMarker, 1)

	// Find where the actual content starts (after markers)
	contentStart := strings.Index(markerBody, "\n## ")
	if contentStart == -1 {
		contentStart = strings.Index(markerBody, "\n#")
	}
	if contentStart == -1 {
		// No heading found; wrap entire body after markers
		return markerBody
	}

	markers := markerBody[:contentStart+1]
	content := markerBody[contentStart+1:]

	return markers + StaleNotice +
		"<details>\n<summary>Show outdated plan</summary>\n\n" +
		content +
		"\n</details>\n"
}
