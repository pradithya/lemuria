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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/org/lemuria/internal/commands"
	"github.com/org/lemuria/internal/config"
	"github.com/org/lemuria/internal/models"
	"github.com/org/lemuria/internal/vcs"
)

// mockVCSClient implements vcs.Client for testing webhook handlers.
type mockVCSClient struct {
	getPRFunc       func(ctx context.Context, owner, repo string, number int) (*models.PullRequestDetail, error)
	postCommentFunc func(ctx context.Context, owner, repo string, number int, body string, isPlan bool) (*models.CommentResult, error)
}

func (m *mockVCSClient) GetChangedFiles(_ context.Context, _, _ string, _ int) ([]models.ChangedFile, error) {
	return nil, nil
}

func (m *mockVCSClient) GetRepoConfig(_ context.Context, _, _, _ string) ([]byte, error) {
	return nil, fmt.Errorf("not found")
}

func (m *mockVCSClient) GetFileContents(_ context.Context, _, _ string, _ []string, _ string) (map[string][]byte, error) {
	return nil, nil
}

func (m *mockVCSClient) GetPR(ctx context.Context, owner, repo string, number int) (*models.PullRequestDetail, error) {
	if m.getPRFunc != nil {
		return m.getPRFunc(ctx, owner, repo, number)
	}
	return &models.PullRequestDetail{
		Number:  number,
		Title:   "Test PR",
		State:   models.PRStateOpen,
		HeadSHA: "abc123",
		HeadRef: "feature",
		BaseRef: "main",
	}, nil
}

func (m *mockVCSClient) IsPRApproved(_ context.Context, _, _ string, _ int) (bool, error) {
	return false, nil
}

func (m *mockVCSClient) PostComment(ctx context.Context, owner, repo string, number int, body string, isPlan bool) (*models.CommentResult, error) {
	if m.postCommentFunc != nil {
		return m.postCommentFunc(ctx, owner, repo, number, body, isPlan)
	}
	return &models.CommentResult{ID: 1}, nil
}

func (m *mockVCSClient) UpdateComment(_ context.Context, _, _ string, _ int, _ int64, _ string) error {
	return nil
}

func (m *mockVCSClient) AddReaction(_ context.Context, _, _ string, _ int64, _ string) error {
	return nil
}

func (m *mockVCSClient) InvalidatePlanComments(_ context.Context, _, _ string, _ int) error {
	return nil
}

func (m *mockVCSClient) MergePullRequest(_ context.Context, _, _ string, _ int, _, _, _ string) error {
	return nil
}

func (m *mockVCSClient) DeleteBranch(_ context.Context, _, _, _ string) error {
	return nil
}

func (m *mockVCSClient) GetFilesByPattern(_ context.Context, _, _, _ string, _ []string, _ []string) (map[string][]byte, error) {
	return map[string][]byte{}, nil
}

func (m *mockVCSClient) MaxCommentSize() int {
	return 0
}

// Compile-time interface check.
var _ vcs.Client = (*mockVCSClient)(nil)

// mockLockManager implements lock.Manager for testing webhook handlers.
type mockLockManager struct {
	listByPRFunc func(ctx context.Context, repo string, prNumber int) ([]models.Lock, error)
	unlockFunc   func(ctx context.Context, application, repo string, prNumber int) error
}

func (m *mockLockManager) Lock(_ context.Context, _ models.LockRequest) (*models.LockResult, error) {
	return &models.LockResult{Acquired: true}, nil
}

func (m *mockLockManager) Unlock(ctx context.Context, application, repo string, prNumber int) error {
	if m.unlockFunc != nil {
		return m.unlockFunc(ctx, application, repo, prNumber)
	}
	return nil
}

func (m *mockLockManager) ForceUnlock(_ context.Context, _ string) error {
	return nil
}

func (m *mockLockManager) Get(_ context.Context, _ string) (*models.Lock, error) {
	return nil, nil
}

func (m *mockLockManager) ListByPR(ctx context.Context, repo string, prNumber int) ([]models.Lock, error) {
	if m.listByPRFunc != nil {
		return m.listByPRFunc(ctx, repo, prNumber)
	}
	return nil, nil
}

func (m *mockLockManager) ListAll(_ context.Context) ([]models.Lock, error) {
	return nil, nil
}

func (m *mockLockManager) StorePlan(_ context.Context, _ string, _ int, _, _, _ string, _ []models.PlanDiffEntry) error {
	return nil
}

func (m *mockLockManager) GetPlan(_ context.Context, _ string, _ int) (string, error) {
	return "", nil
}

func (m *mockLockManager) Ping(_ context.Context) error {
	return nil
}

func (m *mockLockManager) Close() error {
	return nil
}

func (m *mockLockManager) UpdateLock(_ context.Context, _ *models.Lock) error {
	return nil
}

func (m *mockLockManager) StoreAppSetAutoSync(_ context.Context, _, _ string, _ int, _ []byte) error {
	return nil
}

func (m *mockLockManager) GetAppSetAutoSync(_ context.Context, _, _ string, _ int) ([]byte, error) {
	return nil, nil
}

func (m *mockLockManager) DeleteAppSetAutoSync(_ context.Context, _, _ string, _ int) error {
	return nil
}

func (m *mockLockManager) StoreParentAutoSync(_ context.Context, _, _ string, _ int, _ []byte) error {
	return nil
}

func (m *mockLockManager) ListParentAutoSync(_ context.Context, _ string, _ int) (map[string][]byte, error) {
	return nil, nil
}

func (m *mockLockManager) DeleteParentAutoSync(_ context.Context, _, _ string, _ int) error {
	return nil
}

// newTestGitHubHandler creates a GitHubHandler with mocked dependencies.
func newTestGitHubHandler(cfg *config.Config, mockVCS *mockVCSClient, lockMgr *mockLockManager) *GitHubHandler {
	executor := commands.NewExecutor(mockVCS, nil, lockMgr, cfg)
	return &GitHubHandler{
		config:      cfg,
		vcsClient:   mockVCS,
		cmdExecutor: executor,
	}
}

// newTestGitLabHandler creates a GitLabHandler with mocked dependencies.
func newTestGitLabHandler(cfg *config.Config, mockVCS *mockVCSClient, lockMgr *mockLockManager) *GitLabHandler {
	executor := commands.NewExecutor(mockVCS, nil, lockMgr, cfg)
	return &GitLabHandler{
		config:      cfg,
		vcsClient:   mockVCS,
		cmdExecutor: executor,
	}
}

// --- GitHub handler: processEvent branching ---

func TestGitHubProcessEvent_PRClosed(t *testing.T) {
	tests := []struct {
		name    string
		event   *models.PREvent
		lockMgr *mockLockManager
	}{
		{
			name: "PR closed with no locks",
			event: &models.PREvent{
				Type:   models.EventTypePullRequest,
				Action: models.PRActionClosed,
				Repo:   models.RepoInfo{FullName: "org/repo", Owner: "org", Name: "repo"},
				PR:     models.PRInfo{Number: 1, State: models.PRStateClosed},
			},
			lockMgr: &mockLockManager{
				listByPRFunc: func(_ context.Context, _ string, _ int) ([]models.Lock, error) {
					return nil, nil
				},
			},
		},
		{
			name: "PR merged with deleted-type locks only",
			event: &models.PREvent{
				Type:   models.EventTypePullRequest,
				Action: models.PRActionClosed,
				Repo:   models.RepoInfo{FullName: "org/repo", Owner: "org", Name: "repo"},
				PR:     models.PRInfo{Number: 2, State: models.PRStateClosed, Merged: true},
			},
			lockMgr: &mockLockManager{
				listByPRFunc: func(_ context.Context, _ string, _ int) ([]models.Lock, error) {
					return []models.Lock{
						{Application: "app1", PRNumber: 2, Repo: "org/repo", ChangeType: models.ApplicationDeleted},
					}, nil
				},
			},
		},
		{
			name: "PR closed with lock listing error",
			event: &models.PREvent{
				Type:   models.EventTypePullRequest,
				Action: models.PRActionClosed,
				Repo:   models.RepoInfo{FullName: "org/repo"},
				PR:     models.PRInfo{Number: 3, State: models.PRStateClosed},
			},
			lockMgr: &mockLockManager{
				listByPRFunc: func(_ context.Context, _ string, _ int) ([]models.Lock, error) {
					return nil, fmt.Errorf("redis connection refused")
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			h := newTestGitHubHandler(cfg, &mockVCSClient{}, tt.lockMgr)
			// Should not panic
			h.processEvent(context.Background(), tt.event)
		})
	}
}

func TestGitLabProcessEvent_MRClosed(t *testing.T) {
	tests := []struct {
		name    string
		event   *models.PREvent
		lockMgr *mockLockManager
	}{
		{
			name: "MR closed with no locks",
			event: &models.PREvent{
				Type:   models.EventTypePullRequest,
				Action: models.PRActionClosed,
				Repo:   models.RepoInfo{FullName: "group/repo", Owner: "group", Name: "repo"},
				PR:     models.PRInfo{Number: 1, State: models.PRStateClosed},
			},
			lockMgr: &mockLockManager{
				listByPRFunc: func(_ context.Context, _ string, _ int) ([]models.Lock, error) {
					return nil, nil
				},
			},
		},
		{
			name: "MR merged with deleted-type locks only",
			event: &models.PREvent{
				Type:   models.EventTypePullRequest,
				Action: models.PRActionClosed,
				Repo:   models.RepoInfo{FullName: "group/repo", Owner: "group", Name: "repo"},
				PR:     models.PRInfo{Number: 2, State: models.PRStateClosed, Merged: true},
			},
			lockMgr: &mockLockManager{
				listByPRFunc: func(_ context.Context, _ string, _ int) ([]models.Lock, error) {
					return []models.Lock{
						{Application: "app1", PRNumber: 2, Repo: "group/repo", ChangeType: models.ApplicationDeleted},
					}, nil
				},
			},
		},
		{
			name: "MR closed with lock listing error",
			event: &models.PREvent{
				Type:   models.EventTypePullRequest,
				Action: models.PRActionClosed,
				Repo:   models.RepoInfo{FullName: "group/repo"},
				PR:     models.PRInfo{Number: 3, State: models.PRStateClosed},
			},
			lockMgr: &mockLockManager{
				listByPRFunc: func(_ context.Context, _ string, _ int) ([]models.Lock, error) {
					return nil, fmt.Errorf("redis timeout")
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			h := newTestGitLabHandler(cfg, &mockVCSClient{}, tt.lockMgr)
			h.processEvent(context.Background(), tt.event)
		})
	}
}

// --- handleComment tests (uses "help" command which doesn't need argocd) ---

func TestGitHubHandleComment(t *testing.T) {
	tests := []struct {
		name        string
		event       *models.PREvent
		getPRFunc   func(ctx context.Context, owner, repo string, number int) (*models.PullRequestDetail, error)
		wantHeadRef string
		wantEnrich  bool
	}{
		{
			name: "lemuria help command with PR info present",
			event: &models.PREvent{
				Type:   models.EventTypeIssueComment,
				Action: models.PRActionCreated,
				Repo: models.RepoInfo{
					FullName: "org/repo", Owner: "org", Name: "repo",
					HTMLURL: "https://github.com/org/repo",
				},
				PR: models.PRInfo{
					Number: 1, HeadRef: "feat", BaseRef: "main", HeadSHA: "sha1",
				},
				Comment: &models.Comment{
					ID: 100, Body: "lemuria help",
					Author: models.UserInfo{Login: "dev"},
				},
			},
			wantHeadRef: "feat",
		},
		{
			name: "lemuria help command enriches missing PR info",
			event: &models.PREvent{
				Type:   models.EventTypeIssueComment,
				Action: models.PRActionCreated,
				Repo: models.RepoInfo{
					FullName: "org/repo", Owner: "org", Name: "repo",
					HTMLURL: "https://github.com/org/repo",
				},
				PR: models.PRInfo{
					Number: 2, HeadRef: "", BaseRef: "",
				},
				Comment: &models.Comment{
					ID: 101, Body: "lemuria help",
					Author: models.UserInfo{Login: "dev"},
				},
			},
			getPRFunc: func(_ context.Context, _, _ string, _ int) (*models.PullRequestDetail, error) {
				return &models.PullRequestDetail{
					Number: 2, Title: "Test", State: models.PRStateOpen,
					HeadSHA: "enriched-sha", HeadRef: "enriched-branch", BaseRef: "main",
				}, nil
			},
			wantHeadRef: "enriched-branch",
			wantEnrich:  true,
		},
		{
			name: "enrichment failure prevents command execution",
			event: &models.PREvent{
				Type:   models.EventTypeIssueComment,
				Action: models.PRActionCreated,
				Repo: models.RepoInfo{
					FullName: "org/repo", Owner: "org", Name: "repo",
				},
				PR: models.PRInfo{
					Number: 3, HeadRef: "", BaseRef: "",
				},
				Comment: &models.Comment{
					ID: 102, Body: "lemuria help",
					Author: models.UserInfo{Login: "dev"},
				},
			},
			getPRFunc: func(_ context.Context, _, _ string, _ int) (*models.PullRequestDetail, error) {
				return nil, fmt.Errorf("API error: not found")
			},
			wantHeadRef: "", // enrichment failed
		},
		{
			name: "non-lemuria comment is ignored",
			event: &models.PREvent{
				Type:   models.EventTypeIssueComment,
				Action: models.PRActionCreated,
				Repo:   models.RepoInfo{FullName: "org/repo"},
				PR:     models.PRInfo{Number: 4, HeadRef: "feat", BaseRef: "main"},
				Comment: &models.Comment{
					ID: 103, Body: "looks good",
					Author: models.UserInfo{Login: "reviewer"},
				},
			},
			wantHeadRef: "feat",
		},
		{
			name: "edited comment action is ignored",
			event: &models.PREvent{
				Type:   models.EventTypeIssueComment,
				Action: models.PRAction("edited"),
				Repo:   models.RepoInfo{FullName: "org/repo"},
				PR:     models.PRInfo{Number: 5, HeadRef: "feat", BaseRef: "main"},
				Comment: &models.Comment{
					ID: 104, Body: "lemuria plan",
					Author: models.UserInfo{Login: "dev"},
				},
			},
			wantHeadRef: "feat",
		},
		{
			name: "deleted comment action is ignored",
			event: &models.PREvent{
				Type:   models.EventTypeIssueComment,
				Action: models.PRAction("deleted"),
				Repo:   models.RepoInfo{FullName: "org/repo"},
				PR:     models.PRInfo{Number: 6, HeadRef: "feat", BaseRef: "main"},
				Comment: &models.Comment{
					ID: 105, Body: "lemuria sync",
					Author: models.UserInfo{Login: "dev"},
				},
			},
			wantHeadRef: "feat",
		},
		{
			name: "lemuria unlock command with no locks",
			event: &models.PREvent{
				Type:   models.EventTypeIssueComment,
				Action: models.PRActionCreated,
				Repo: models.RepoInfo{
					FullName: "org/repo", Owner: "org", Name: "repo",
					HTMLURL: "https://github.com/org/repo",
				},
				PR: models.PRInfo{
					Number: 7, HeadRef: "feat", BaseRef: "main", HeadSHA: "sha7",
				},
				Comment: &models.Comment{
					ID: 106, Body: "lemuria unlock",
					Author: models.UserInfo{Login: "dev"},
				},
			},
			wantHeadRef: "feat",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			mockVCS := &mockVCSClient{getPRFunc: tt.getPRFunc}
			lockMgr := &mockLockManager{}
			h := newTestGitHubHandler(cfg, mockVCS, lockMgr)

			h.handleComment(context.Background(), tt.event)

			if tt.event.PR.HeadRef != tt.wantHeadRef {
				t.Errorf("PR.HeadRef = %q, want %q", tt.event.PR.HeadRef, tt.wantHeadRef)
			}
		})
	}
}

func TestGitLabHandleComment(t *testing.T) {
	tests := []struct {
		name        string
		event       *models.PREvent
		getPRFunc   func(ctx context.Context, owner, repo string, number int) (*models.PullRequestDetail, error)
		wantHeadRef string
	}{
		{
			name: "lemuria help command with MR info present",
			event: &models.PREvent{
				Type:   models.EventTypeIssueComment,
				Action: models.PRActionCreated,
				Repo: models.RepoInfo{
					FullName: "group/repo", Owner: "group", Name: "repo",
					HTMLURL: "https://gitlab.com/group/repo",
				},
				PR: models.PRInfo{
					Number: 1, HeadRef: "feat", BaseRef: "main", HeadSHA: "sha1",
				},
				Comment: &models.Comment{
					ID: 200, Body: "lemuria help",
					Author: models.UserInfo{Login: "dev"},
				},
			},
			wantHeadRef: "feat",
		},
		{
			name: "lemuria help command enriches missing MR info",
			event: &models.PREvent{
				Type:   models.EventTypeIssueComment,
				Action: models.PRActionCreated,
				Repo: models.RepoInfo{
					FullName: "group/repo", Owner: "group", Name: "repo",
					HTMLURL: "https://gitlab.com/group/repo",
				},
				PR: models.PRInfo{
					Number: 2, HeadRef: "", BaseRef: "",
				},
				Comment: &models.Comment{
					ID: 201, Body: "lemuria help",
					Author: models.UserInfo{Login: "dev"},
				},
			},
			getPRFunc: func(_ context.Context, _, _ string, _ int) (*models.PullRequestDetail, error) {
				return &models.PullRequestDetail{
					Number: 2, Title: "Test MR", State: models.PRStateOpen,
					HeadSHA: "gl-enriched-sha", HeadRef: "gl-enriched-branch", BaseRef: "main",
				}, nil
			},
			wantHeadRef: "gl-enriched-branch",
		},
		{
			name: "enrichment failure prevents command execution",
			event: &models.PREvent{
				Type:   models.EventTypeIssueComment,
				Action: models.PRActionCreated,
				Repo: models.RepoInfo{
					FullName: "group/repo", Owner: "group", Name: "repo",
				},
				PR: models.PRInfo{
					Number: 3, HeadRef: "", BaseRef: "",
				},
				Comment: &models.Comment{
					ID: 202, Body: "lemuria plan",
					Author: models.UserInfo{Login: "dev"},
				},
			},
			getPRFunc: func(_ context.Context, _, _ string, _ int) (*models.PullRequestDetail, error) {
				return nil, fmt.Errorf("API error: forbidden")
			},
			wantHeadRef: "",
		},
		{
			name: "non-lemuria note is ignored",
			event: &models.PREvent{
				Type:   models.EventTypeIssueComment,
				Action: models.PRActionCreated,
				Repo:   models.RepoInfo{FullName: "group/repo"},
				PR:     models.PRInfo{Number: 4, HeadRef: "feat", BaseRef: "main"},
				Comment: &models.Comment{
					ID: 203, Body: "LGTM",
					Author: models.UserInfo{Login: "reviewer"},
				},
			},
			wantHeadRef: "feat",
		},
		{
			name: "edited action is ignored",
			event: &models.PREvent{
				Type:   models.EventTypeIssueComment,
				Action: models.PRAction("edited"),
				Repo:   models.RepoInfo{FullName: "group/repo"},
				PR:     models.PRInfo{Number: 5, HeadRef: "feat", BaseRef: "main"},
				Comment: &models.Comment{
					ID: 204, Body: "lemuria plan",
					Author: models.UserInfo{Login: "dev"},
				},
			},
			wantHeadRef: "feat",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			mockVCS := &mockVCSClient{getPRFunc: tt.getPRFunc}
			lockMgr := &mockLockManager{}
			h := newTestGitLabHandler(cfg, mockVCS, lockMgr)

			h.handleComment(context.Background(), tt.event)

			if tt.event.PR.HeadRef != tt.wantHeadRef {
				t.Errorf("PR.HeadRef = %q, want %q", tt.event.PR.HeadRef, tt.wantHeadRef)
			}
		})
	}
}

// --- enrichPRInfo / enrichMRInfo direct tests ---

func TestGitHubEnrichPRInfo(t *testing.T) {
	tests := []struct {
		name      string
		getPRFunc func(ctx context.Context, owner, repo string, number int) (*models.PullRequestDetail, error)
		wantErr   bool
		wantSHA   string
		wantDraft bool
	}{
		{
			name: "successful enrichment populates all fields",
			getPRFunc: func(_ context.Context, _, _ string, _ int) (*models.PullRequestDetail, error) {
				return &models.PullRequestDetail{
					Number: 1, Title: "Enriched PR", State: models.PRStateOpen,
					Draft: true, Merged: false,
					HeadSHA: "enriched-sha-456", HeadRef: "feature-branch", BaseRef: "main",
				}, nil
			},
			wantSHA:   "enriched-sha-456",
			wantDraft: true,
		},
		{
			name: "API error returns error",
			getPRFunc: func(_ context.Context, _, _ string, _ int) (*models.PullRequestDetail, error) {
				return nil, fmt.Errorf("403 Forbidden")
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &GitHubHandler{vcsClient: &mockVCSClient{getPRFunc: tt.getPRFunc}}
			event := &models.PREvent{
				Repo: models.RepoInfo{Owner: "org", Name: "repo"},
				PR:   models.PRInfo{Number: 1},
			}
			err := h.enrichPRInfo(context.Background(), event)
			if (err != nil) != tt.wantErr {
				t.Errorf("enrichPRInfo() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if event.PR.HeadSHA != tt.wantSHA {
					t.Errorf("PR.HeadSHA = %q, want %q", event.PR.HeadSHA, tt.wantSHA)
				}
				if event.PR.Draft != tt.wantDraft {
					t.Errorf("PR.Draft = %v, want %v", event.PR.Draft, tt.wantDraft)
				}
			}
		})
	}
}

func TestGitLabEnrichMRInfo(t *testing.T) {
	tests := []struct {
		name      string
		getPRFunc func(ctx context.Context, owner, repo string, number int) (*models.PullRequestDetail, error)
		wantErr   bool
		wantSHA   string
		wantDraft bool
	}{
		{
			name: "successful enrichment populates all fields",
			getPRFunc: func(_ context.Context, _, _ string, _ int) (*models.PullRequestDetail, error) {
				return &models.PullRequestDetail{
					Number: 1, Title: "Enriched MR", State: models.PRStateOpen,
					Draft: true, Merged: false,
					HeadSHA: "gl-sha-789", HeadRef: "gl-feature", BaseRef: "main",
				}, nil
			},
			wantSHA:   "gl-sha-789",
			wantDraft: true,
		},
		{
			name: "API error returns error",
			getPRFunc: func(_ context.Context, _, _ string, _ int) (*models.PullRequestDetail, error) {
				return nil, fmt.Errorf("500 Internal Server Error")
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &GitLabHandler{vcsClient: &mockVCSClient{getPRFunc: tt.getPRFunc}}
			event := &models.PREvent{
				Repo: models.RepoInfo{Owner: "group", Name: "repo"},
				PR:   models.PRInfo{Number: 1},
			}
			err := h.enrichMRInfo(context.Background(), event)
			if (err != nil) != tt.wantErr {
				t.Errorf("enrichMRInfo() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if event.PR.HeadSHA != tt.wantSHA {
					t.Errorf("PR.HeadSHA = %q, want %q", event.PR.HeadSHA, tt.wantSHA)
				}
				if event.PR.Draft != tt.wantDraft {
					t.Errorf("PR.Draft = %v, want %v", event.PR.Draft, tt.wantDraft)
				}
			}
		})
	}
}

func TestGitHubHandlePRClosed(t *testing.T) {
	unlockCalled := false
	lockMgr := &mockLockManager{
		listByPRFunc: func(_ context.Context, _ string, _ int) ([]models.Lock, error) {
			return []models.Lock{
				{Application: "app1", PRNumber: 1, Repo: "org/repo", ChangeType: models.ApplicationDeleted},
			}, nil
		},
		unlockFunc: func(_ context.Context, app, _ string, _ int) error {
			if app == "app1" {
				unlockCalled = true
			}
			return nil
		},
	}

	cfg := &config.Config{}
	h := newTestGitHubHandler(cfg, &mockVCSClient{}, lockMgr)

	event := &models.PREvent{
		Type:   models.EventTypePullRequest,
		Action: models.PRActionClosed,
		Repo:   models.RepoInfo{FullName: "org/repo"},
		PR:     models.PRInfo{Number: 1, State: models.PRStateClosed, Merged: true},
	}

	h.handlePRClosed(context.Background(), event)

	if !unlockCalled {
		t.Error("expected Unlock to be called for app1")
	}
}

func TestGitLabHandlePRClosed(t *testing.T) {
	unlockCalled := false
	lockMgr := &mockLockManager{
		listByPRFunc: func(_ context.Context, _ string, _ int) ([]models.Lock, error) {
			return []models.Lock{
				{Application: "app1", PRNumber: 1, Repo: "group/repo", ChangeType: models.ApplicationDeleted},
			}, nil
		},
		unlockFunc: func(_ context.Context, app, _ string, _ int) error {
			if app == "app1" {
				unlockCalled = true
			}
			return nil
		},
	}

	cfg := &config.Config{}
	h := newTestGitLabHandler(cfg, &mockVCSClient{}, lockMgr)

	event := &models.PREvent{
		Type:   models.EventTypePullRequest,
		Action: models.PRActionClosed,
		Repo:   models.RepoInfo{FullName: "group/repo"},
		PR:     models.PRInfo{Number: 1, State: models.PRStateClosed, Merged: true},
	}

	h.handlePRClosed(context.Background(), event)

	if !unlockCalled {
		t.Error("expected Unlock to be called for app1")
	}
}

// --- processEvent: non-handled event types are no-ops ---

func TestGitHubProcessEvent_NoOp(t *testing.T) {
	tests := []struct {
		name  string
		event *models.PREvent
	}{
		{
			name: "pull_request_review is a no-op",
			event: &models.PREvent{
				Type:   models.EventTypePullRequestReview,
				Action: models.PRActionSubmitted,
				Repo:   models.RepoInfo{FullName: "org/repo"},
				PR:     models.PRInfo{Number: 1},
			},
		},
		{
			name: "PR opened with autoplan disabled is a no-op",
			event: &models.PREvent{
				Type:   models.EventTypePullRequest,
				Action: models.PRActionOpened,
				Repo:   models.RepoInfo{FullName: "org/repo"},
				PR:     models.PRInfo{Number: 2, State: models.PRStateOpen},
			},
		},
		{
			name: "issue_comment without comment field is a no-op",
			event: &models.PREvent{
				Type:   models.EventTypeIssueComment,
				Action: models.PRActionCreated,
				Repo:   models.RepoInfo{FullName: "org/repo"},
				PR:     models.PRInfo{Number: 3},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{} // autoplan off
			h := newTestGitHubHandler(cfg, &mockVCSClient{}, &mockLockManager{})
			// Should not panic or error
			h.processEvent(context.Background(), tt.event)
		})
	}
}

func TestGitLabProcessEvent_NoOp(t *testing.T) {
	tests := []struct {
		name  string
		event *models.PREvent
	}{
		{
			name: "pull_request_review is a no-op",
			event: &models.PREvent{
				Type:   models.EventTypePullRequestReview,
				Action: models.PRActionSubmitted,
				Repo:   models.RepoInfo{FullName: "group/repo"},
				PR:     models.PRInfo{Number: 1},
			},
		},
		{
			name: "MR opened with autoplan disabled is a no-op",
			event: &models.PREvent{
				Type:   models.EventTypePullRequest,
				Action: models.PRActionOpened,
				Repo:   models.RepoInfo{FullName: "group/repo"},
				PR:     models.PRInfo{Number: 2, State: models.PRStateOpen},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			h := newTestGitLabHandler(cfg, &mockVCSClient{}, &mockLockManager{})
			h.processEvent(context.Background(), tt.event)
		})
	}
}

// --- Bot comment filtering tests (via HTTP handler) ---

func TestGitHubHandleBotCommentIgnored(t *testing.T) {
	const secret = "gh-test-secret"

	botCommentPayload := fmt.Sprintf(`{
		"action": "created",
		"issue": {
			"number": 5,
			"pull_request": {"url": "https://api.github.com/repos/org/repo/pulls/5"}
		},
		"comment": {
			"id": 888,
			"body": "Plan output:\n%s\nSome details here",
			"user": {"login": "lemuria-bot", "id": 999},
			"created_at": "2024-01-15T10:30:00Z"
		},
		"repository": {
			"name": "repo",
			"full_name": "org/repo",
			"owner": {"login": "org"},
			"clone_url": "https://github.com/org/repo.git",
			"html_url": "https://github.com/org/repo"
		},
		"sender": {"login": "lemuria-bot", "id": 999}
	}`, models.CommentMarker)

	cfg := &config.Config{
		GitHub: config.GitHubConfig{WebhookSecret: secret},
	}
	h := &GitHubHandler{config: cfg, validator: NewValidator(secret)}

	body := []byte(botCommentPayload)
	sig := computeHMAC(body, secret)

	req := httptest.NewRequest(http.MethodPost, "/webhook/github", bytes.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", sig)
	req.Header.Set("X-GitHub-Event", "issue_comment")
	req.Header.Set("X-GitHub-Delivery", "test-bot-comment")

	rec := httptest.NewRecorder()
	h.Handle(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if got := resp["status"]; got != "bot comment ignored" {
		t.Errorf("status = %q, want %q", got, "bot comment ignored")
	}
}

func TestGitLabHandleBotCommentIgnored(t *testing.T) {
	const secret = "gl-test-secret"

	botNotePayload := fmt.Sprintf(`{
		"object_attributes": {
			"id": 777,
			"note": "Plan result:\n%s\nDetails follow",
			"noteable_type": "MergeRequest",
			"created_at": "2024-06-01 12:00:00 UTC"
		},
		"merge_request": {
			"iid": 5,
			"title": "Some MR",
			"state": "opened",
			"source_branch": "feature",
			"target_branch": "main",
			"last_commit": {"id": "def456"},
			"url": "https://gitlab.com/group/repo/-/merge_requests/5"
		},
		"project": {
			"name": "repo",
			"path_with_namespace": "group/repo",
			"web_url": "https://gitlab.com/group/repo",
			"git_http_url": "https://gitlab.com/group/repo.git"
		},
		"user": {"username": "lemuria-bot", "id": 999}
	}`, models.CommentMarker)

	cfg := &config.Config{
		GitLab: config.GitLabConfig{WebhookSecret: secret},
	}
	h := &GitLabHandler{config: cfg}

	req := httptest.NewRequest(http.MethodPost, "/webhook/gitlab", bytes.NewReader([]byte(botNotePayload)))
	req.Header.Set("X-Gitlab-Token", secret)
	req.Header.Set("X-Gitlab-Event", "Note Hook")

	rec := httptest.NewRecorder()
	h.Handle(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if got := resp["status"]; got != "bot comment ignored" {
		t.Errorf("status = %q, want %q", got, "bot comment ignored")
	}
}

// --- Concurrency test: handler returns before goroutine finishes ---

func TestGitHubHandleReturnsBeforeProcessing(t *testing.T) {
	const secret = "gh-test-secret"

	var mu sync.Mutex
	processCalled := false

	cfg := &config.Config{
		GitHub: config.GitHubConfig{WebhookSecret: secret},
	}

	lockMgr := &mockLockManager{
		listByPRFunc: func(_ context.Context, _ string, _ int) ([]models.Lock, error) {
			mu.Lock()
			processCalled = true
			mu.Unlock()
			return nil, nil
		},
	}

	mockVCS := &mockVCSClient{}
	executor := commands.NewExecutor(mockVCS, nil, lockMgr, cfg)
	h := &GitHubHandler{
		config:      cfg,
		vcsClient:   mockVCS,
		cmdExecutor: executor,
		validator:   NewValidator(secret),
	}

	body := []byte(`{
		"action": "closed",
		"number": 1,
		"pull_request": {
			"number": 1, "title": "T", "state": "closed", "draft": false, "merged": true,
			"head": {"sha": "a", "ref": "f"}, "base": {"ref": "m"},
			"user": {"login": "d", "id": 1}, "html_url": "https://github.com/o/r/pull/1"
		},
		"repository": {"name": "r", "full_name": "o/r", "owner": {"login": "o"},
			"clone_url": "https://github.com/o/r.git", "html_url": "https://github.com/o/r"},
		"sender": {"login": "d", "id": 1}
	}`)
	sig := computeHMAC(body, secret)

	req := httptest.NewRequest(http.MethodPost, "/webhook/github", bytes.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", sig)
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-GitHub-Delivery", "test-async")

	rec := httptest.NewRecorder()
	h.Handle(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	// Wait for the async goroutine
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if !processCalled {
		t.Error("processEvent goroutine was not triggered")
	}
}

func TestGitLabHandleReturnsBeforeProcessing(t *testing.T) {
	const secret = "gl-test-secret"

	var mu sync.Mutex
	processCalled := false

	cfg := &config.Config{
		GitLab: config.GitLabConfig{WebhookSecret: secret},
	}

	lockMgr := &mockLockManager{
		listByPRFunc: func(_ context.Context, _ string, _ int) ([]models.Lock, error) {
			mu.Lock()
			processCalled = true
			mu.Unlock()
			return nil, nil
		},
	}

	mockVCS := &mockVCSClient{}
	executor := commands.NewExecutor(mockVCS, nil, lockMgr, cfg)
	h := &GitLabHandler{
		config:      cfg,
		vcsClient:   mockVCS,
		cmdExecutor: executor,
	}

	body := []byte(`{
		"object_attributes": {
			"iid": 1, "title": "T", "state": "closed", "action": "close",
			"work_in_progress": false, "draft": false,
			"source_branch": "f", "target_branch": "m",
			"last_commit": {"id": "a"},
			"url": "https://gitlab.com/g/r/-/merge_requests/1"
		},
		"project": {
			"name": "r",
			"path_with_namespace": "g/r",
			"web_url": "https://gitlab.com/g/r",
			"http_url_to_repo": "https://gitlab.com/g/r.git"
		},
		"user": {"username": "d", "id": 1, "avatar_url": ""}
	}`)

	req := httptest.NewRequest(http.MethodPost, "/webhook/gitlab", bytes.NewReader(body))
	req.Header.Set("X-Gitlab-Token", secret)
	req.Header.Set("X-Gitlab-Event", "Merge Request Hook")

	rec := httptest.NewRecorder()
	h.Handle(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if !processCalled {
		t.Error("processEvent goroutine was not triggered")
	}
}

// --- GitLab Handle generating delivery ID when X-Gitlab-Event-UUID is missing ---

func TestGitLabHandleGeneratesDeliveryID(t *testing.T) {
	const secret = "gl-test-secret"

	cfg := &config.Config{
		GitLab: config.GitLabConfig{WebhookSecret: secret},
	}
	h := &GitLabHandler{config: cfg}

	body := []byte(`{
		"object_attributes": {
			"iid": 1, "title": "T", "state": "opened", "action": "open",
			"work_in_progress": false, "draft": false,
			"source_branch": "f", "target_branch": "m",
			"last_commit": {"id": "a"},
			"url": "https://gitlab.com/g/r/-/merge_requests/1"
		},
		"project": {
			"name": "r",
			"path_with_namespace": "g/r",
			"web_url": "https://gitlab.com/g/r",
			"http_url_to_repo": "https://gitlab.com/g/r.git"
		},
		"user": {"username": "d", "id": 1, "avatar_url": ""}
	}`)

	req := httptest.NewRequest(http.MethodPost, "/webhook/gitlab", bytes.NewReader(body))
	req.Header.Set("X-Gitlab-Token", secret)
	req.Header.Set("X-Gitlab-Event", "Merge Request Hook")
	// No X-Gitlab-Event-UUID

	rec := httptest.NewRecorder()
	h.Handle(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if got := resp["status"]; got != "accepted" {
		t.Errorf("status = %q, want %q", got, "accepted")
	}
}
