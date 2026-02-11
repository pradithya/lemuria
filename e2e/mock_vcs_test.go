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

package e2e

import (
	"context"
	"fmt"
	"sync"

	"github.com/org/lemuria/internal/models"
)

// MockVCSClient implements vcs.Client for E2E tests.
type MockVCSClient struct {
	mu sync.Mutex

	// Configurable return values
	ChangedFiles   []models.ChangedFile
	RepoConfigData []byte
	RepoConfigErr  error
	FileContents   map[string][]byte // key: "path@ref"
	PRApproved     bool
	PRMergeable    bool
	PRHeadRef      string
	PRBaseRef      string
	PRHeadSHA      string

	// Recorded interactions
	PostedComments  []PostedComment
	UpdatedComments []UpdatedComment
	Reactions       []Reaction
	MergeCalls      []MergeCall
	DeletedBranches []string
	InvalidatedPRs  []InvalidatedPR

	// Internal counter for comment IDs
	nextCommentID int64
}

// UpdatedComment records a comment updated via UpdateComment.
type UpdatedComment struct {
	Owner     string
	Repo      string
	Number    int
	CommentID int64
	Body      string
}

// PostedComment records a comment posted via PostComment.
type PostedComment struct {
	Owner  string
	Repo   string
	Number int
	Body   string
	IsPlan bool
}

// Reaction records a reaction added via AddReaction.
type Reaction struct {
	Owner     string
	Repo      string
	CommentID int64
	Reaction  string
}

// MergeCall records a merge request via MergePullRequest.
type MergeCall struct {
	Owner   string
	Repo    string
	Number  int
	Title   string
	Message string
	Method  string
}

// InvalidatedPR records a call to InvalidatePlanComments.
type InvalidatedPR struct {
	Owner  string
	Repo   string
	Number int
}

// NewMockVCSClient creates a new mock VCS client with sensible defaults.
func NewMockVCSClient() *MockVCSClient {
	return &MockVCSClient{
		FileContents:  make(map[string][]byte),
		PRApproved:    true,
		PRMergeable:   true,
		nextCommentID: 1000,
	}
}

func (m *MockVCSClient) GetChangedFiles(_ context.Context, _, _ string, _ int) ([]models.ChangedFile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ChangedFiles, nil
}

func (m *MockVCSClient) GetRepoConfig(_ context.Context, _, _, _ string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.RepoConfigErr != nil {
		return nil, m.RepoConfigErr
	}
	if m.RepoConfigData == nil {
		return nil, fmt.Errorf(".lemuria.yaml not found")
	}
	return m.RepoConfigData, nil
}

func (m *MockVCSClient) GetFileContent(_ context.Context, _, _, path, ref string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := path + "@" + ref
	content, ok := m.FileContents[key]
	if !ok {
		return nil, fmt.Errorf("file not found: %s at ref %s", path, ref)
	}
	return content, nil
}

func (m *MockVCSClient) GetPR(_ context.Context, _, _ string, _ int) (*models.PullRequestDetail, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return &models.PullRequestDetail{
		State:     models.PRStateOpen,
		Mergeable: m.PRMergeable,
		HeadRef:   m.PRHeadRef,
		BaseRef:   m.PRBaseRef,
		HeadSHA:   m.PRHeadSHA,
	}, nil
}

func (m *MockVCSClient) IsPRApproved(_ context.Context, _, _ string, _ int) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.PRApproved, nil
}

func (m *MockVCSClient) PostComment(_ context.Context, owner, repo string, number int, body string, isPlan bool) (*models.CommentResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.PostedComments = append(m.PostedComments, PostedComment{
		Owner:  owner,
		Repo:   repo,
		Number: number,
		Body:   body,
		IsPlan: isPlan,
	})
	id := m.nextCommentID
	m.nextCommentID++
	return &models.CommentResult{
		ID: id,
	}, nil
}

func (m *MockVCSClient) UpdateComment(_ context.Context, owner, repo string, number int, commentID int64, body string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.UpdatedComments = append(m.UpdatedComments, UpdatedComment{
		Owner:     owner,
		Repo:      repo,
		Number:    number,
		CommentID: commentID,
		Body:      body,
	})
	return nil
}

func (m *MockVCSClient) AddReaction(_ context.Context, owner, repo string, commentID int64, reaction string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Reactions = append(m.Reactions, Reaction{
		Owner:     owner,
		Repo:      repo,
		CommentID: commentID,
		Reaction:  reaction,
	})
	return nil
}

func (m *MockVCSClient) InvalidatePlanComments(_ context.Context, owner, repo string, number int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.InvalidatedPRs = append(m.InvalidatedPRs, InvalidatedPR{
		Owner:  owner,
		Repo:   repo,
		Number: number,
	})
	return nil
}

func (m *MockVCSClient) MergePullRequest(_ context.Context, owner, repo string, number int, title, message, method string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.MergeCalls = append(m.MergeCalls, MergeCall{
		Owner:   owner,
		Repo:    repo,
		Number:  number,
		Title:   title,
		Message: message,
		Method:  method,
	})
	return nil
}

func (m *MockVCSClient) DeleteBranch(_ context.Context, _, _, branch string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.DeletedBranches = append(m.DeletedBranches, branch)
	return nil
}

// GetPostedComments returns a copy of the posted comments for assertion.
func (m *MockVCSClient) GetPostedComments() []PostedComment {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]PostedComment, len(m.PostedComments))
	copy(result, m.PostedComments)
	return result
}

// GetUpdatedComments returns a copy of the updated comments for assertion.
func (m *MockVCSClient) GetUpdatedComments() []UpdatedComment {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]UpdatedComment, len(m.UpdatedComments))
	copy(result, m.UpdatedComments)
	return result
}

// GetMergeCalls returns a copy of the merge calls for assertion.
func (m *MockVCSClient) GetMergeCalls() []MergeCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]MergeCall, len(m.MergeCalls))
	copy(result, m.MergeCalls)
	return result
}

// GetReactions returns a copy of the recorded reactions.
func (m *MockVCSClient) GetReactions() []Reaction {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]Reaction, len(m.Reactions))
	copy(result, m.Reactions)
	return result
}

// GetInvalidatedPRs returns a copy of the recorded invalidated PRs.
func (m *MockVCSClient) GetInvalidatedPRs() []InvalidatedPR {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]InvalidatedPR, len(m.InvalidatedPRs))
	copy(result, m.InvalidatedPRs)
	return result
}

// Reset clears all recorded interactions.
func (m *MockVCSClient) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.PostedComments = nil
	m.UpdatedComments = nil
	m.Reactions = nil
	m.MergeCalls = nil
	m.DeletedBranches = nil
	m.InvalidatedPRs = nil
}
