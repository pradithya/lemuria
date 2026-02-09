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

// InvalidatePlanComments marks all existing plan comments on a MR as stale.
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
