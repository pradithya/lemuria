package e2e

import (
	"context"
	"fmt"
	"sync"

	"github.com/google/go-github/v60/github"

	"github.com/org/lemuria/internal/models"
)

// MockGitHubClient implements commands.GitHubClient for E2E tests.
type MockGitHubClient struct {
	mu sync.Mutex

	// Configurable return values
	ChangedFiles   []models.ChangedFile
	RepoConfigData []byte
	RepoConfigErr  error
	FileContents   map[string][]byte // key: "path@ref"
	PRApproved     bool
	PRMergeable    bool

	// Recorded interactions
	PostedComments []PostedComment
	Reactions      []Reaction
	MergeCalls     []MergeCall
	DeletedBranches []string
	InvalidatedPRs []InvalidatedPR

	// Internal counter for comment IDs
	nextCommentID int64
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

// NewMockGitHubClient creates a new mock GitHub client with sensible defaults.
func NewMockGitHubClient() *MockGitHubClient {
	return &MockGitHubClient{
		FileContents:  make(map[string][]byte),
		PRApproved:    true,
		PRMergeable:   true,
		nextCommentID: 1000,
	}
}

func (m *MockGitHubClient) GetChangedFiles(_ context.Context, _, _ string, _ int) ([]models.ChangedFile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ChangedFiles, nil
}

func (m *MockGitHubClient) GetRepoConfig(_ context.Context, _, _, _ string) ([]byte, error) {
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

func (m *MockGitHubClient) GetFileContent(_ context.Context, _, _, path, ref string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := path + "@" + ref
	content, ok := m.FileContents[key]
	if !ok {
		return nil, fmt.Errorf("file not found: %s at ref %s", path, ref)
	}
	return content, nil
}

func (m *MockGitHubClient) GetPR(_ context.Context, _, _ string, _ int) (*github.PullRequest, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return &github.PullRequest{
		State:     github.String("open"),
		Mergeable: github.Bool(m.PRMergeable),
	}, nil
}

func (m *MockGitHubClient) IsPRApproved(_ context.Context, _, _ string, _ int) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.PRApproved, nil
}

func (m *MockGitHubClient) PostComment(_ context.Context, owner, repo string, number int, body string, isPlan bool) (*github.IssueComment, error) {
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
	return &github.IssueComment{
		ID:   github.Int64(id),
		Body: github.String(body),
	}, nil
}

func (m *MockGitHubClient) AddReaction(_ context.Context, owner, repo string, commentID int64, reaction string) error {
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

func (m *MockGitHubClient) InvalidatePlanComments(_ context.Context, owner, repo string, number int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.InvalidatedPRs = append(m.InvalidatedPRs, InvalidatedPR{
		Owner:  owner,
		Repo:   repo,
		Number: number,
	})
	return nil
}

func (m *MockGitHubClient) MergePullRequest(_ context.Context, owner, repo string, number int, title, message, method string) error {
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

func (m *MockGitHubClient) DeleteBranch(_ context.Context, _, _, branch string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.DeletedBranches = append(m.DeletedBranches, branch)
	return nil
}

// GetPostedComments returns a copy of the posted comments for assertion.
func (m *MockGitHubClient) GetPostedComments() []PostedComment {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]PostedComment, len(m.PostedComments))
	copy(result, m.PostedComments)
	return result
}

// GetMergeCalls returns a copy of the merge calls for assertion.
func (m *MockGitHubClient) GetMergeCalls() []MergeCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]MergeCall, len(m.MergeCalls))
	copy(result, m.MergeCalls)
	return result
}

// Reset clears all recorded interactions.
func (m *MockGitHubClient) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.PostedComments = nil
	m.Reactions = nil
	m.MergeCalls = nil
	m.DeletedBranches = nil
	m.InvalidatedPRs = nil
}
