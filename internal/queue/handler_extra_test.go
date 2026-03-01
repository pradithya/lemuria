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

package queue

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hibiken/asynq"

	"github.com/org/lemuria/internal/commands"
	"github.com/org/lemuria/internal/config"
	"github.com/org/lemuria/internal/models"
	"github.com/org/lemuria/internal/vcs"
)

// --- Mock VCS Client ---

type mockVCSClient struct {
	getPRFunc            func(ctx context.Context, owner, repo string, number int) (*models.PullRequestDetail, error)
	getChangedFilesFunc  func(ctx context.Context, owner, repo string, number int) ([]models.ChangedFile, error)
	getRepoConfigFunc    func(ctx context.Context, owner, repo, ref string) ([]byte, error)
	getFileContentsFunc  func(ctx context.Context, owner, repo string, paths []string, ref string) (map[string][]byte, error)
	isPRApprovedFunc     func(ctx context.Context, owner, repo string, number int) (bool, error)
	postCommentFunc      func(ctx context.Context, owner, repo string, number int, body string, isPlan bool) (*models.CommentResult, error)
	updateCommentFunc    func(ctx context.Context, owner, repo string, number int, commentID int64, body string) error
	addReactionFunc      func(ctx context.Context, owner, repo string, commentID int64, reaction string) error
	invalidatePlanFunc   func(ctx context.Context, owner, repo string, number int) error
	mergePullRequestFunc func(ctx context.Context, owner, repo string, number int, title, message, method string) error
	deleteBranchFunc     func(ctx context.Context, owner, repo, branch string) error
}

func (m *mockVCSClient) GetPR(ctx context.Context, owner, repo string, number int) (*models.PullRequestDetail, error) {
	if m.getPRFunc != nil {
		return m.getPRFunc(ctx, owner, repo, number)
	}
	return &models.PullRequestDetail{
		Number:  number,
		HeadSHA: "abc123",
		HeadRef: "feature",
		BaseRef: "main",
		State:   models.PRStateOpen,
	}, nil
}

func (m *mockVCSClient) GetChangedFiles(ctx context.Context, owner, repo string, number int) ([]models.ChangedFile, error) {
	if m.getChangedFilesFunc != nil {
		return m.getChangedFilesFunc(ctx, owner, repo, number)
	}
	return nil, nil
}

func (m *mockVCSClient) GetRepoConfig(ctx context.Context, owner, repo, ref string) ([]byte, error) {
	if m.getRepoConfigFunc != nil {
		return m.getRepoConfigFunc(ctx, owner, repo, ref)
	}
	return nil, errors.New("not found")
}

func (m *mockVCSClient) GetFileContents(ctx context.Context, owner, repo string, paths []string, ref string) (map[string][]byte, error) {
	if m.getFileContentsFunc != nil {
		return m.getFileContentsFunc(ctx, owner, repo, paths, ref)
	}
	return nil, nil
}

func (m *mockVCSClient) IsPRApproved(ctx context.Context, owner, repo string, number int) (bool, error) {
	if m.isPRApprovedFunc != nil {
		return m.isPRApprovedFunc(ctx, owner, repo, number)
	}
	return true, nil
}

func (m *mockVCSClient) PostComment(ctx context.Context, owner, repo string, number int, body string, isPlan bool) (*models.CommentResult, error) {
	if m.postCommentFunc != nil {
		return m.postCommentFunc(ctx, owner, repo, number, body, isPlan)
	}
	return &models.CommentResult{ID: 1}, nil
}

func (m *mockVCSClient) UpdateComment(ctx context.Context, owner, repo string, number int, commentID int64, body string) error {
	if m.updateCommentFunc != nil {
		return m.updateCommentFunc(ctx, owner, repo, number, commentID, body)
	}
	return nil
}

func (m *mockVCSClient) AddReaction(ctx context.Context, owner, repo string, commentID int64, reaction string) error {
	if m.addReactionFunc != nil {
		return m.addReactionFunc(ctx, owner, repo, commentID, reaction)
	}
	return nil
}

func (m *mockVCSClient) InvalidatePlanComments(ctx context.Context, owner, repo string, number int) error {
	if m.invalidatePlanFunc != nil {
		return m.invalidatePlanFunc(ctx, owner, repo, number)
	}
	return nil
}

func (m *mockVCSClient) MergePullRequest(ctx context.Context, owner, repo string, number int, title, message, method string) error {
	if m.mergePullRequestFunc != nil {
		return m.mergePullRequestFunc(ctx, owner, repo, number, title, message, method)
	}
	return nil
}

func (m *mockVCSClient) DeleteBranch(ctx context.Context, owner, repo, branch string) error {
	if m.deleteBranchFunc != nil {
		return m.deleteBranchFunc(ctx, owner, repo, branch)
	}
	return nil
}

func (m *mockVCSClient) GetFilesByPattern(_ context.Context, _, _, _ string, _ []string, _ []string) (map[string][]byte, error) {
	return map[string][]byte{}, nil
}

func (m *mockVCSClient) MaxCommentSize() int {
	return 0
}

// --- Mock Lock Manager ---

type mockLockManager struct {
	listByPRFunc    func(ctx context.Context, repo string, prNumber int) ([]models.Lock, error)
	unlockFunc      func(ctx context.Context, application, repo string, prNumber int) error
	lockFunc        func(ctx context.Context, req models.LockRequest) (*models.LockResult, error)
	forceUnlockFunc func(ctx context.Context, application string) error
	getFunc         func(ctx context.Context, application string) (*models.Lock, error)
	listAllFunc     func(ctx context.Context) ([]models.Lock, error)
	storePlanFunc   func(ctx context.Context, application string, prNumber int, revision, sourceFile, planOutput string, diffs []models.PlanDiffEntry) error
	getPlanFunc     func(ctx context.Context, application string, prNumber int) (string, error)
}

func (m *mockLockManager) Lock(ctx context.Context, req models.LockRequest) (*models.LockResult, error) {
	if m.lockFunc != nil {
		return m.lockFunc(ctx, req)
	}
	return &models.LockResult{Acquired: true}, nil
}

func (m *mockLockManager) Unlock(ctx context.Context, application, repo string, prNumber int) error {
	if m.unlockFunc != nil {
		return m.unlockFunc(ctx, application, repo, prNumber)
	}
	return nil
}

func (m *mockLockManager) ForceUnlock(ctx context.Context, application string) error {
	if m.forceUnlockFunc != nil {
		return m.forceUnlockFunc(ctx, application)
	}
	return nil
}

func (m *mockLockManager) Get(ctx context.Context, application string) (*models.Lock, error) {
	if m.getFunc != nil {
		return m.getFunc(ctx, application)
	}
	return nil, nil
}

func (m *mockLockManager) ListByPR(ctx context.Context, repo string, prNumber int) ([]models.Lock, error) {
	if m.listByPRFunc != nil {
		return m.listByPRFunc(ctx, repo, prNumber)
	}
	return nil, nil
}

func (m *mockLockManager) ListAll(ctx context.Context) ([]models.Lock, error) {
	if m.listAllFunc != nil {
		return m.listAllFunc(ctx)
	}
	return nil, nil
}

func (m *mockLockManager) StorePlan(ctx context.Context, application string, prNumber int, revision, sourceFile, planOutput string, diffs []models.PlanDiffEntry) error {
	if m.storePlanFunc != nil {
		return m.storePlanFunc(ctx, application, prNumber, revision, sourceFile, planOutput, diffs)
	}
	return nil
}

func (m *mockLockManager) GetPlan(ctx context.Context, application string, prNumber int) (string, error) {
	if m.getPlanFunc != nil {
		return m.getPlanFunc(ctx, application, prNumber)
	}
	return "", nil
}

func (m *mockLockManager) Ping(_ context.Context) error {
	return nil
}

func (m *mockLockManager) Close() error {
	return nil
}

// Compile-time interface checks
var _ vcs.Client = (*mockVCSClient)(nil)

// --- Tests for processEvent ---

func TestProcessEvent_UnlockAll_Success(t *testing.T) {
	lockMgr := &mockLockManager{
		listByPRFunc: func(_ context.Context, _ string, _ int) ([]models.Lock, error) {
			return nil, nil // no locks to release
		},
	}
	mockVCS := &mockVCSClient{}

	cfg := &config.Config{}
	ghExec := commands.NewExecutor(mockVCS, nil, lockMgr, cfg)

	h := NewWebhookHandler(cfg, ghExec, nil, mockVCS, nil)

	event := &models.PREvent{
		Provider: models.VCSProviderGitHub,
		Type:     models.EventTypePullRequest,
		Action:   models.PRActionClosed,
		Repo: models.RepoInfo{
			Owner:    "org",
			Name:     "repo",
			FullName: "org/repo",
			HTMLURL:  "https://github.com/org/repo",
		},
		PR: models.PRInfo{
			Number:  1,
			BaseRef: "main",
			Merged:  true,
		},
		ReceivedAt: time.Now(),
	}

	task := makeTask(t, "unlock-all-success", event)
	err := h.ProcessTask(context.Background(), task)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProcessEvent_UnlockAll_Error(t *testing.T) {
	lockMgr := &mockLockManager{
		listByPRFunc: func(_ context.Context, _ string, _ int) ([]models.Lock, error) {
			return nil, errors.New("redis connection error")
		},
	}
	mockVCS := &mockVCSClient{}

	cfg := &config.Config{}
	ghExec := commands.NewExecutor(mockVCS, nil, lockMgr, cfg)

	h := NewWebhookHandler(cfg, ghExec, nil, mockVCS, nil)

	event := &models.PREvent{
		Provider: models.VCSProviderGitHub,
		Type:     models.EventTypePullRequest,
		Action:   models.PRActionClosed,
		Repo: models.RepoInfo{
			Owner:    "org",
			Name:     "repo",
			FullName: "org/repo",
			HTMLURL:  "https://github.com/org/repo",
		},
		PR: models.PRInfo{
			Number:  1,
			BaseRef: "main",
		},
		ReceivedAt: time.Now(),
	}

	task := makeTask(t, "unlock-all-error", event)
	err := h.ProcessTask(context.Background(), task)
	if err == nil {
		t.Fatal("expected error when ListByPR fails")
	}
}

func TestProcessEvent_Autoplan_Disabled(t *testing.T) {
	// When autoplan is disabled, PR open/sync events should be no-ops
	ghExec := commands.NewExecutor(nil, nil, nil, &config.Config{})

	h := NewWebhookHandler(
		&config.Config{Defaults: config.DefaultsConfig{Autoplan: false}},
		ghExec, nil, nil, nil,
	)

	tests := []struct {
		name   string
		action models.PRAction
	}{
		{"PR opened without autoplan", models.PRActionOpened},
		{"PR synchronize without autoplan", models.PRActionSynchronize},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := &models.PREvent{
				Provider:   models.VCSProviderGitHub,
				Type:       models.EventTypePullRequest,
				Action:     tt.action,
				Repo:       models.RepoInfo{FullName: "org/repo"},
				PR:         models.PRInfo{Number: 1},
				ReceivedAt: time.Now(),
			}

			task := makeTask(t, "autoplan-disabled-"+string(tt.action), event)
			err := h.ProcessTask(context.Background(), task)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// --- Tests for enrichPRInfo ---

func TestEnrichPRInfo_GitHub(t *testing.T) {
	getPRCalled := false
	mockVCS := &mockVCSClient{
		getPRFunc: func(_ context.Context, owner, repo string, number int) (*models.PullRequestDetail, error) {
			getPRCalled = true
			if owner != "org" || repo != "repo" || number != 42 {
				t.Errorf("unexpected args: owner=%s, repo=%s, number=%d", owner, repo, number)
			}
			return &models.PullRequestDetail{
				Number:  42,
				Title:   "My PR",
				State:   models.PRStateOpen,
				Draft:   true,
				Merged:  false,
				HeadSHA: "sha-999",
				HeadRef: "my-feature",
				BaseRef: "develop",
			}, nil
		},
	}

	h := NewWebhookHandler(&config.Config{}, nil, nil, mockVCS, nil)

	event := &models.PREvent{
		Provider: models.VCSProviderGitHub,
		Repo: models.RepoInfo{
			Owner: "org",
			Name:  "repo",
		},
		PR: models.PRInfo{
			Number: 42,
		},
	}

	err := h.enrichPRInfo(context.Background(), event)
	if err != nil {
		t.Fatalf("enrichPRInfo() error = %v", err)
	}

	if !getPRCalled {
		t.Fatal("expected GetPR to be called")
	}

	tests := []struct {
		name string
		got  interface{}
		want interface{}
	}{
		{"HeadSHA", event.PR.HeadSHA, "sha-999"},
		{"HeadRef", event.PR.HeadRef, "my-feature"},
		{"BaseRef", event.PR.BaseRef, "develop"},
		{"State", event.PR.State, models.PRStateOpen},
		{"Title", event.PR.Title, "My PR"},
		{"Draft", event.PR.Draft, true},
		{"Merged", event.PR.Merged, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("got %v, want %v", tt.got, tt.want)
			}
		})
	}
}

func TestEnrichPRInfo_GitLab(t *testing.T) {
	getPRCalled := false
	mockVCS := &mockVCSClient{
		getPRFunc: func(_ context.Context, _, _ string, _ int) (*models.PullRequestDetail, error) {
			getPRCalled = true
			return &models.PullRequestDetail{
				Number:  7,
				HeadSHA: "gl-sha",
				HeadRef: "gl-branch",
				BaseRef: "main",
				State:   models.PRStateOpen,
			}, nil
		},
	}

	h := NewWebhookHandler(&config.Config{}, nil, nil, nil, mockVCS)

	event := &models.PREvent{
		Provider: models.VCSProviderGitLab,
		Repo: models.RepoInfo{
			Owner: "group",
			Name:  "project",
		},
		PR: models.PRInfo{
			Number: 7,
		},
	}

	err := h.enrichPRInfo(context.Background(), event)
	if err != nil {
		t.Fatalf("enrichPRInfo() error = %v", err)
	}

	if !getPRCalled {
		t.Fatal("expected GetPR to be called for GitLab")
	}

	if event.PR.HeadSHA != "gl-sha" {
		t.Errorf("HeadSHA = %q, want %q", event.PR.HeadSHA, "gl-sha")
	}
	if event.PR.HeadRef != "gl-branch" {
		t.Errorf("HeadRef = %q, want %q", event.PR.HeadRef, "gl-branch")
	}
}

func TestEnrichPRInfo_NoVCSClient(t *testing.T) {
	tests := []struct {
		name     string
		provider models.VCSProvider
	}{
		{"github without VCS", models.VCSProviderGitHub},
		{"gitlab without VCS", models.VCSProviderGitLab},
		{"unknown provider", "bitbucket"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewWebhookHandler(&config.Config{}, nil, nil, nil, nil)

			event := &models.PREvent{
				Provider: tt.provider,
				Repo: models.RepoInfo{
					Owner: "org",
					Name:  "repo",
				},
				PR: models.PRInfo{
					Number: 1,
				},
			}

			err := h.enrichPRInfo(context.Background(), event)
			if err == nil {
				t.Fatal("expected error when no VCS client configured")
			}
			if !errors.Is(err, asynq.SkipRetry) {
				t.Errorf("error should wrap SkipRetry, got: %v", err)
			}
		})
	}
}

func TestEnrichPRInfo_GetPRError(t *testing.T) {
	mockVCS := &mockVCSClient{
		getPRFunc: func(_ context.Context, _, _ string, _ int) (*models.PullRequestDetail, error) {
			return nil, errors.New("network error")
		},
	}

	h := NewWebhookHandler(&config.Config{}, nil, nil, mockVCS, nil)

	event := &models.PREvent{
		Provider: models.VCSProviderGitHub,
		Repo: models.RepoInfo{
			Owner: "org",
			Name:  "repo",
		},
		PR: models.PRInfo{
			Number: 1,
		},
	}

	err := h.enrichPRInfo(context.Background(), event)
	if err == nil {
		t.Fatal("expected error when GetPR fails")
	}
}

// --- Tests for handleComment via ProcessTask ---

func TestHandleComment_ValidHelpCommand_WithBranches(t *testing.T) {
	// Help command only calls PostComment on the VCS, no ArgoCD needed
	postCommentCalled := false
	mockVCS := &mockVCSClient{
		postCommentFunc: func(_ context.Context, _, _ string, _ int, _ string, _ bool) (*models.CommentResult, error) {
			postCommentCalled = true
			return &models.CommentResult{ID: 1}, nil
		},
	}
	lockMgr := &mockLockManager{}

	cfg := &config.Config{}
	ghExec := commands.NewExecutor(mockVCS, nil, lockMgr, cfg)

	h := NewWebhookHandler(cfg, ghExec, nil, mockVCS, nil)

	event := &models.PREvent{
		Provider: models.VCSProviderGitHub,
		Type:     models.EventTypeIssueComment,
		Action:   models.PRActionCreated,
		Repo: models.RepoInfo{
			Owner:    "org",
			Name:     "repo",
			FullName: "org/repo",
			HTMLURL:  "https://github.com/org/repo",
		},
		PR: models.PRInfo{
			Number:  10,
			HeadRef: "feature",
			BaseRef: "main",
			HeadSHA: "abc123",
		},
		Comment: &models.Comment{
			ID:   100,
			Body: "lemuria help",
			Author: models.UserInfo{
				Login: "user1",
			},
		},
		ReceivedAt: time.Now(),
	}

	task := makeTask(t, "help-command", event)
	err := h.ProcessTask(context.Background(), task)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !postCommentCalled {
		t.Error("expected PostComment to be called for help command")
	}
}

func TestHandleComment_ValidCommand_NeedEnrichPR(t *testing.T) {
	getPRCalled := false
	postCommentCalled := false
	mockVCS := &mockVCSClient{
		getPRFunc: func(_ context.Context, _, _ string, _ int) (*models.PullRequestDetail, error) {
			getPRCalled = true
			return &models.PullRequestDetail{
				Number:  10,
				HeadSHA: "enriched-sha",
				HeadRef: "enriched-branch",
				BaseRef: "main",
				State:   models.PRStateOpen,
				Title:   "Test PR",
			}, nil
		},
		postCommentFunc: func(_ context.Context, _, _ string, _ int, _ string, _ bool) (*models.CommentResult, error) {
			postCommentCalled = true
			return &models.CommentResult{ID: 1}, nil
		},
	}
	lockMgr := &mockLockManager{}

	cfg := &config.Config{}
	ghExec := commands.NewExecutor(mockVCS, nil, lockMgr, cfg)

	h := NewWebhookHandler(cfg, ghExec, nil, mockVCS, nil)

	// Event with empty HeadRef/BaseRef triggers enrichPRInfo
	event := &models.PREvent{
		Provider: models.VCSProviderGitHub,
		Type:     models.EventTypeIssueComment,
		Action:   models.PRActionCreated,
		Repo: models.RepoInfo{
			Owner:    "org",
			Name:     "repo",
			FullName: "org/repo",
			HTMLURL:  "https://github.com/org/repo",
		},
		PR: models.PRInfo{
			Number:  10,
			HeadRef: "", // empty triggers enrichment
			BaseRef: "", // empty triggers enrichment
		},
		Comment: &models.Comment{
			ID:   100,
			Body: "lemuria help",
			Author: models.UserInfo{
				Login: "user1",
			},
		},
		ReceivedAt: time.Now(),
	}

	task := makeTask(t, "enrich-pr", event)
	err := h.ProcessTask(context.Background(), task)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !getPRCalled {
		t.Error("expected GetPR to be called for PR enrichment")
	}
	if !postCommentCalled {
		t.Error("expected PostComment to be called after enrichment")
	}

}

func TestHandleComment_EnrichPR_Error(t *testing.T) {
	mockVCS := &mockVCSClient{
		getPRFunc: func(_ context.Context, _, _ string, _ int) (*models.PullRequestDetail, error) {
			return nil, errors.New("API rate limit exceeded")
		},
	}
	lockMgr := &mockLockManager{}

	cfg := &config.Config{}
	ghExec := commands.NewExecutor(mockVCS, nil, lockMgr, cfg)

	h := NewWebhookHandler(cfg, ghExec, nil, mockVCS, nil)

	event := &models.PREvent{
		Provider: models.VCSProviderGitHub,
		Type:     models.EventTypeIssueComment,
		Action:   models.PRActionCreated,
		Repo: models.RepoInfo{
			Owner:    "org",
			Name:     "repo",
			FullName: "org/repo",
		},
		PR: models.PRInfo{
			Number:  10,
			HeadRef: "", // triggers enrichment
			BaseRef: "",
		},
		Comment: &models.Comment{
			ID:   100,
			Body: "lemuria plan",
			Author: models.UserInfo{
				Login: "user1",
			},
		},
		ReceivedAt: time.Now(),
	}

	task := makeTask(t, "enrich-pr-error", event)
	err := h.ProcessTask(context.Background(), task)
	if err == nil {
		t.Fatal("expected error when enrichPRInfo fails")
	}
}

func TestProcessEvent_GitLabProvider(t *testing.T) {
	lockMgr := &mockLockManager{
		listByPRFunc: func(_ context.Context, _ string, _ int) ([]models.Lock, error) {
			return nil, nil
		},
	}
	mockVCS := &mockVCSClient{}

	cfg := &config.Config{}
	glExec := commands.NewExecutor(mockVCS, nil, lockMgr, cfg)

	h := NewWebhookHandler(cfg, nil, glExec, nil, mockVCS)

	event := &models.PREvent{
		Provider: models.VCSProviderGitLab,
		Type:     models.EventTypePullRequest,
		Action:   models.PRActionClosed,
		Repo: models.RepoInfo{
			Owner:    "group",
			Name:     "project",
			FullName: "group/project",
			HTMLURL:  "https://gitlab.com/group/project",
		},
		PR: models.PRInfo{
			Number:  3,
			BaseRef: "main",
		},
		ReceivedAt: time.Now(),
	}

	task := makeTask(t, "gitlab-unlock", event)
	err := h.ProcessTask(context.Background(), task)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProcessEvent_IssueCommentNoComment(t *testing.T) {
	// issue_comment event with nil Comment field should be a no-op
	ghExec := commands.NewExecutor(nil, nil, nil, &config.Config{})

	h := NewWebhookHandler(
		&config.Config{},
		ghExec, nil, nil, nil,
	)

	event := &models.PREvent{
		Provider:   models.VCSProviderGitHub,
		Type:       models.EventTypeIssueComment,
		Action:     models.PRActionCreated,
		Repo:       models.RepoInfo{FullName: "org/repo"},
		PR:         models.PRInfo{Number: 1},
		Comment:    nil, // no comment
		ReceivedAt: time.Now(),
	}

	task := makeTask(t, "no-comment", event)
	err := h.ProcessTask(context.Background(), task)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHandleComment_GitLab_NeedEnrichPR(t *testing.T) {
	getPRCalled := false
	mockVCS := &mockVCSClient{
		getPRFunc: func(_ context.Context, _, _ string, _ int) (*models.PullRequestDetail, error) {
			getPRCalled = true
			return &models.PullRequestDetail{
				Number:  5,
				HeadSHA: "gl-sha",
				HeadRef: "mr-branch",
				BaseRef: "main",
				State:   models.PRStateOpen,
			}, nil
		},
		postCommentFunc: func(_ context.Context, _, _ string, _ int, _ string, _ bool) (*models.CommentResult, error) {
			return &models.CommentResult{ID: 1}, nil
		},
	}
	lockMgr := &mockLockManager{}

	cfg := &config.Config{}
	glExec := commands.NewExecutor(mockVCS, nil, lockMgr, cfg)

	h := NewWebhookHandler(cfg, nil, glExec, nil, mockVCS)

	event := &models.PREvent{
		Provider: models.VCSProviderGitLab,
		Type:     models.EventTypeIssueComment,
		Action:   models.PRActionCreated,
		Repo: models.RepoInfo{
			Owner:    "group",
			Name:     "project",
			FullName: "group/project",
			HTMLURL:  "https://gitlab.com/group/project",
		},
		PR: models.PRInfo{
			Number:  5,
			HeadRef: "", // triggers enrichment
			BaseRef: "",
		},
		Comment: &models.Comment{
			ID:   200,
			Body: "lemuria help",
			Author: models.UserInfo{
				Login: "user2",
			},
		},
		ReceivedAt: time.Now(),
	}

	task := makeTask(t, "gitlab-enrich", event)
	err := h.ProcessTask(context.Background(), task)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !getPRCalled {
		t.Error("expected GetPR to be called for GitLab enrichment")
	}
}

func TestHandleComment_EnrichPR_NoVCSClient(t *testing.T) {
	lockMgr := &mockLockManager{}
	cfg := &config.Config{}
	ghExec := commands.NewExecutor(nil, nil, lockMgr, cfg)

	// No VCS clients configured
	h := NewWebhookHandler(cfg, ghExec, nil, nil, nil)

	event := &models.PREvent{
		Provider: models.VCSProviderGitHub,
		Type:     models.EventTypeIssueComment,
		Action:   models.PRActionCreated,
		Repo: models.RepoInfo{
			Owner:    "org",
			Name:     "repo",
			FullName: "org/repo",
		},
		PR: models.PRInfo{
			Number:  1,
			HeadRef: "", // triggers enrichment
			BaseRef: "",
		},
		Comment: &models.Comment{
			ID:   100,
			Body: "lemuria help",
			Author: models.UserInfo{
				Login: "user1",
			},
		},
		ReceivedAt: time.Now(),
	}

	task := makeTask(t, "no-vcs-enrich", event)
	err := h.ProcessTask(context.Background(), task)
	if err == nil {
		t.Fatal("expected error when no VCS client for enrichment")
	}
	if !errors.Is(err, asynq.SkipRetry) {
		t.Errorf("error should wrap SkipRetry, got: %v", err)
	}
}

func TestHandleComment_PostCommentError(t *testing.T) {
	mockVCS := &mockVCSClient{
		postCommentFunc: func(_ context.Context, _, _ string, _ int, _ string, _ bool) (*models.CommentResult, error) {
			return nil, errors.New("GitHub API error")
		},
	}
	lockMgr := &mockLockManager{}

	cfg := &config.Config{}
	ghExec := commands.NewExecutor(mockVCS, nil, lockMgr, cfg)

	h := NewWebhookHandler(cfg, ghExec, nil, mockVCS, nil)

	event := &models.PREvent{
		Provider: models.VCSProviderGitHub,
		Type:     models.EventTypeIssueComment,
		Action:   models.PRActionCreated,
		Repo: models.RepoInfo{
			Owner:    "org",
			Name:     "repo",
			FullName: "org/repo",
			HTMLURL:  "https://github.com/org/repo",
		},
		PR: models.PRInfo{
			Number:  10,
			HeadRef: "feature",
			BaseRef: "main",
			HeadSHA: "abc123",
		},
		Comment: &models.Comment{
			ID:   100,
			Body: "lemuria help",
			Author: models.UserInfo{
				Login: "user1",
			},
		},
		ReceivedAt: time.Now(),
	}

	task := makeTask(t, "post-comment-error", event)
	err := h.ProcessTask(context.Background(), task)
	if err == nil {
		t.Fatal("expected error when PostComment fails")
	}
}
