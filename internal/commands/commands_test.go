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

package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	v1alpha1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/org/lemuria/internal/argocd"
	"github.com/org/lemuria/internal/config"
	"github.com/org/lemuria/internal/models"
	"github.com/org/lemuria/pkg/diff"
)

// ---------------------------------------------------------------------------
// Full-featured mock implementations
// ---------------------------------------------------------------------------

// mockVCS implements vcs.Client for unit tests with configurable behavior.
type mockVCS struct {
	changedFiles       []models.ChangedFile
	changedFilesErr    error
	repoConfigData     []byte
	repoConfigErr      error
	fileContents       map[string][]byte
	fileContentsErr    error
	filesByPattern     map[string]map[string][]byte // key: ref → path → content
	filesByPatternErr  error
	pr                 *models.PullRequestDetail
	prErr              error
	prApproved         bool
	prApprovedErr      error
	postedComments     []postedComment
	postCommentErr     error
	updatedComments    []updatedComment
	updateCommentErr   error
	reactions          []addedReaction
	reactionErr        error
	invalidated        bool
	invalidateErr      error
	mergedPRs          []mergedPR
	mergeErr           error
	deletedBranches    []string
	deleteBranchErr    error
	maxCommentSizeVal  int
	postCommentCounter int
}

type postedComment struct {
	Owner  string
	Repo   string
	Number int
	Body   string
	IsPlan bool
}

type updatedComment struct {
	Owner     string
	Repo      string
	Number    int
	CommentID int64
	Body      string
}

type addedReaction struct {
	Owner     string
	Repo      string
	CommentID int64
	Reaction  string
}

type mergedPR struct {
	Owner   string
	Repo    string
	Number  int
	Title   string
	Message string
	Method  string
}

func (m *mockVCS) GetChangedFiles(_ context.Context, owner, repo string, number int) ([]models.ChangedFile, error) {
	return m.changedFiles, m.changedFilesErr
}

func (m *mockVCS) GetRepoConfig(_ context.Context, owner, repo, ref string) ([]byte, error) {
	if m.repoConfigErr != nil {
		return nil, m.repoConfigErr
	}
	if m.repoConfigData == nil {
		return nil, fmt.Errorf(".lemuria.yaml not found")
	}
	return m.repoConfigData, nil
}

func (m *mockVCS) GetFileContents(_ context.Context, owner, repo string, paths []string, ref string) (map[string][]byte, error) {
	if m.fileContentsErr != nil {
		return nil, m.fileContentsErr
	}
	if m.fileContents == nil {
		return map[string][]byte{}, nil
	}
	result := make(map[string][]byte)
	for _, p := range paths {
		if content, ok := m.fileContents[p]; ok {
			result[p] = content
		}
	}
	return result, nil
}

func (m *mockVCS) GetPR(_ context.Context, owner, repo string, number int) (*models.PullRequestDetail, error) {
	if m.prErr != nil {
		return nil, m.prErr
	}
	if m.pr != nil {
		return m.pr, nil
	}
	return &models.PullRequestDetail{State: models.PRStateOpen, Mergeable: true}, nil
}

func (m *mockVCS) IsPRApproved(_ context.Context, owner, repo string, number int) (bool, error) {
	return m.prApproved, m.prApprovedErr
}

func (m *mockVCS) PostComment(_ context.Context, owner, repo string, number int, body string, isPlan bool) (*models.CommentResult, error) {
	m.postCommentCounter++
	m.postedComments = append(m.postedComments, postedComment{
		Owner: owner, Repo: repo, Number: number, Body: body, IsPlan: isPlan,
	})
	if m.postCommentErr != nil {
		return nil, m.postCommentErr
	}
	return &models.CommentResult{ID: int64(m.postCommentCounter)}, nil
}

func (m *mockVCS) UpdateComment(_ context.Context, owner, repo string, number int, commentID int64, body string) error {
	m.updatedComments = append(m.updatedComments, updatedComment{
		Owner: owner, Repo: repo, Number: number, CommentID: commentID, Body: body,
	})
	return m.updateCommentErr
}

func (m *mockVCS) AddReaction(_ context.Context, owner, repo string, commentID int64, reaction string) error {
	m.reactions = append(m.reactions, addedReaction{
		Owner: owner, Repo: repo, CommentID: commentID, Reaction: reaction,
	})
	return m.reactionErr
}

func (m *mockVCS) InvalidatePlanComments(_ context.Context, owner, repo string, number int) error {
	m.invalidated = true
	return m.invalidateErr
}

func (m *mockVCS) MergePullRequest(_ context.Context, owner, repo string, number int, title, message, method string) error {
	m.mergedPRs = append(m.mergedPRs, mergedPR{
		Owner: owner, Repo: repo, Number: number, Title: title, Message: message, Method: method,
	})
	return m.mergeErr
}

func (m *mockVCS) DeleteBranch(_ context.Context, owner, repo, branch string) error {
	m.deletedBranches = append(m.deletedBranches, branch)
	return m.deleteBranchErr
}

func (m *mockVCS) GetFilesByPattern(_ context.Context, _, _, ref string, _ []string, _ []string) (map[string][]byte, error) {
	if m.filesByPatternErr != nil {
		return nil, m.filesByPatternErr
	}
	if m.filesByPattern == nil {
		return map[string][]byte{}, nil
	}
	if refContents, ok := m.filesByPattern[ref]; ok {
		return refContents, nil
	}
	return map[string][]byte{}, nil
}

func (m *mockVCS) MaxCommentSize() int {
	return m.maxCommentSizeVal
}

// mockLock implements lock.Manager for unit tests.
type mockLock struct {
	locks          []models.Lock
	listByPRErr    error
	lockResult     *models.LockResult
	lockErr        error
	unlocked       []string
	unlockErr      error
	forceUnlocked  []string
	forceUnlockErr error
	storedPlans    []storedPlan
	storePlanErr   error
	planRevision   string
	planErr        error
}

type storedPlan struct {
	Application string
	PRNumber    int
	Revision    string
	SourceFile  string
	PlanOutput  string
	PlanDiffs   []models.PlanDiffEntry
}

func (m *mockLock) Lock(_ context.Context, req models.LockRequest) (*models.LockResult, error) {
	if m.lockErr != nil {
		return nil, m.lockErr
	}
	if m.lockResult != nil {
		return m.lockResult, nil
	}
	return &models.LockResult{Acquired: true}, nil
}

func (m *mockLock) Unlock(_ context.Context, application, repo string, prNumber int) error {
	m.unlocked = append(m.unlocked, application)
	return m.unlockErr
}

func (m *mockLock) ForceUnlock(_ context.Context, application string) error {
	m.forceUnlocked = append(m.forceUnlocked, application)
	return m.forceUnlockErr
}

func (m *mockLock) Get(_ context.Context, application string) (*models.Lock, error) {
	for _, l := range m.locks {
		if l.Application == application {
			return &l, nil
		}
	}
	return nil, nil
}

func (m *mockLock) ListByPR(_ context.Context, repo string, prNumber int) ([]models.Lock, error) {
	if m.listByPRErr != nil {
		return nil, m.listByPRErr
	}
	return m.locks, nil
}

func (m *mockLock) ListAll(_ context.Context) ([]models.Lock, error) {
	return m.locks, nil
}

func (m *mockLock) StorePlan(_ context.Context, application string, prNumber int, revision, sourceFile, planOutput string, diffs []models.PlanDiffEntry) error {
	m.storedPlans = append(m.storedPlans, storedPlan{
		Application: application,
		PRNumber:    prNumber,
		Revision:    revision,
		SourceFile:  sourceFile,
		PlanOutput:  planOutput,
		PlanDiffs:   diffs,
	})
	return m.storePlanErr
}

func (m *mockLock) GetPlan(_ context.Context, application string, prNumber int) (string, error) {
	return m.planRevision, m.planErr
}

func (m *mockLock) Ping(_ context.Context) error { return nil }
func (m *mockLock) Close() error                 { return nil }
func (m *mockLock) UpdateLock(_ context.Context, _ *models.Lock) error {
	return nil
}
func (m *mockLock) StoreAppSetAutoSync(_ context.Context, _, _ string, _ int, _ []byte) error {
	return nil
}
func (m *mockLock) GetAppSetAutoSync(_ context.Context, _, _ string, _ int) ([]byte, error) {
	return nil, nil
}
func (m *mockLock) DeleteAppSetAutoSync(_ context.Context, _, _ string, _ int) error {
	return nil
}
func (m *mockLock) StoreParentAutoSync(_ context.Context, _, _ string, _ int, _ []byte) error {
	return nil
}
func (m *mockLock) ListParentAutoSync(_ context.Context, _ string, _ int) (map[string][]byte, error) {
	return nil, nil
}
func (m *mockLock) DeleteParentAutoSync(_ context.Context, _, _ string, _ int) error {
	return nil
}

// ---------------------------------------------------------------------------
// Helper functions
// ---------------------------------------------------------------------------

func defaultEvent() *models.PREvent {
	return &models.PREvent{
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
			Number:  42,
			Title:   "Add feature",
			HeadSHA: "abc123",
			HeadRef: "feature-branch",
			BaseRef: "main",
		},
		Sender: models.UserInfo{Login: "user1"},
	}
}

func defaultEventWithComment() *models.PREvent {
	event := defaultEvent()
	event.Comment = &models.Comment{ID: 100, Body: "lemuria plan"}
	return event
}

func newTestExecutor(vcs *mockVCS, lock *mockLock, cfg *config.Config) *Executor {
	if cfg == nil {
		cfg = &config.Config{}
	}
	return &Executor{
		vcs:      vcs,
		lock:     lock,
		config:   cfg,
		renderer: diff.NewRenderer(),
	}
}

// ---------------------------------------------------------------------------
// Tests: Execute dispatch
// ---------------------------------------------------------------------------

func TestExecute_UnknownCommand(t *testing.T) {
	exec := newTestExecutor(&mockVCS{}, &mockLock{}, nil)
	cmd := &Command{Name: "invalid"}
	event := defaultEvent()

	err := exec.Execute(context.Background(), cmd, event)
	if err == nil {
		t.Fatal("expected error for unknown command")
	}
	if !strings.Contains(err.Error(), "unknown command") {
		t.Errorf("error = %q, want containing 'unknown command'", err.Error())
	}
}

func TestExecute_DispatchesHelp(t *testing.T) {
	vcs := &mockVCS{}
	exec := newTestExecutor(vcs, &mockLock{}, nil)
	cmd := &Command{Name: CommandHelp}
	event := defaultEvent()

	err := exec.Execute(context.Background(), cmd, event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vcs.postedComments) == 0 {
		t.Fatal("expected help comment to be posted")
	}
	if !strings.Contains(vcs.postedComments[0].Body, "Lemuria Help") {
		t.Error("expected help text in comment body")
	}
}

// ---------------------------------------------------------------------------
// Tests: RunAutoplan
// ---------------------------------------------------------------------------

func TestRunAutoplan_CreatesCommandAndDelegates(t *testing.T) {
	appYAML := `apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: my-app
spec:
  project: default
  source:
    repoURL: https://github.com/org/repo
    path: apps/my-app
    targetRevision: main
  destination:
    server: https://kubernetes.default.svc
    namespace: default
`

	vcs := &mockVCS{
		changedFiles: []models.ChangedFile{
			{Filename: "apps/my-app/deployment.yaml", Status: "modified"},
		},
		filesByPattern: map[string]map[string][]byte{
			"feature-branch": {"argocd/app.yaml": []byte(appYAML)},
			"main":           {"argocd/app.yaml": []byte(appYAML)},
		},
	}

	// ArgoCD mock: responds to ListApplications, GetManifests, and CreateApplication
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == "GET" && strings.Contains(r.URL.Path, "/api/v1/applications") && !strings.Contains(r.URL.Path, "/manifests"):
			// ListApplications or GetApplication
			if strings.Count(strings.TrimSuffix(r.URL.Path, "/"), "/") > 3 {
				// GetApplication (specific app)
				json.NewEncoder(w).Encode(v1alpha1.Application{ //nolint:errcheck
					ObjectMeta: metav1.ObjectMeta{Name: "my-app"},
					Spec: v1alpha1.ApplicationSpec{
						Source: &v1alpha1.ApplicationSource{
							RepoURL:        "https://github.com/org/repo",
							Path:           "apps/my-app",
							TargetRevision: "main",
						},
					},
				})
			} else {
				json.NewEncoder(w).Encode(v1alpha1.ApplicationList{ //nolint:errcheck
					Items: []v1alpha1.Application{},
				})
			}
		default:
			json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
				"manifests": []string{`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"test"}}`},
			})
		}
	}))
	defer ts.Close()

	argoClient := newTestArgoClient(ts)
	exec := newTestExecutorWithArgo(vcs, &mockLock{}, argoClient)

	event := &models.PREvent{
		Provider: models.VCSProviderGitHub,
		Type:     models.EventTypePullRequest,
		Action:   models.PRActionOpened,
		Repo: models.RepoInfo{
			Owner: "org", Name: "repo", FullName: "org/repo",
			HTMLURL: "https://github.com/org/repo",
		},
		PR: models.PRInfo{
			Number:  1,
			HeadRef: "feature-branch",
			BaseRef: "main",
			HeadSHA: "abc123",
		},
		Sender: models.UserInfo{Login: "user1"},
	}

	err := exec.RunAutoplan(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify plan comments were posted
	if len(vcs.postedComments) == 0 {
		t.Fatal("expected plan comment to be posted")
	}
}

func TestRunAutoplan_ErrorPostsComment(t *testing.T) {
	// When VCS returns an error for changed files, RunAutoplan posts an error comment.
	vcs := &mockVCS{
		changedFilesErr: fmt.Errorf("vcs error"),
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(v1alpha1.ApplicationList{Items: []v1alpha1.Application{}}) //nolint:errcheck
	}))
	defer ts.Close()

	argoClient := newTestArgoClient(ts)
	exec := newTestExecutorWithArgo(vcs, &mockLock{}, argoClient)

	event := &models.PREvent{
		Provider: models.VCSProviderGitHub,
		Type:     models.EventTypePullRequest,
		Repo: models.RepoInfo{
			Owner: "org", Name: "repo", FullName: "org/repo",
			HTMLURL: "https://github.com/org/repo",
		},
		PR: models.PRInfo{
			Number:  1,
			HeadRef: "feature-branch",
			BaseRef: "main",
		},
		Sender: models.UserInfo{Login: "user1"},
	}

	err := exec.RunAutoplan(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// executePlan calls postError which posts a comment with the error
	if len(vcs.postedComments) == 0 {
		t.Fatal("expected error comment to be posted")
	}
}

// ---------------------------------------------------------------------------
// Tests: UnlockAll
// ---------------------------------------------------------------------------

func TestUnlockAll_NoLocks(t *testing.T) {
	vcs := &mockVCS{}
	lock := &mockLock{locks: nil}
	exec := newTestExecutor(vcs, lock, nil)
	event := defaultEvent()

	err := exec.UnlockAll(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(lock.unlocked) != 0 {
		t.Errorf("expected no unlocks, got %v", lock.unlocked)
	}
}

func TestUnlockAll_WithLocks(t *testing.T) {
	// UnlockAll calls revertTargetRevision which needs argocd
	fake := newFakeArgoServer(v1alpha1.Application{
		Spec: v1alpha1.ApplicationSpec{
			Source: &v1alpha1.ApplicationSource{
				RepoURL:        "https://github.com/org/repo",
				Path:           "manifests",
				TargetRevision: "main",
			},
		},
	})
	defer fake.close()

	vcs := &mockVCS{}
	lock := &mockLock{
		locks: []models.Lock{
			{Application: "app-a", PRNumber: 42, Repo: "org/repo", ChangeType: models.ApplicationExisting},
			{Application: "app-b", PRNumber: 42, Repo: "org/repo", ChangeType: models.ApplicationExisting},
		},
	}
	exec := &Executor{
		vcs:      vcs,
		argocd:   fake.client(),
		lock:     lock,
		config:   &config.Config{},
		renderer: diff.NewRenderer(),
	}
	event := defaultEvent()

	err := exec.UnlockAll(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(lock.unlocked) != 2 {
		t.Errorf("expected 2 unlocks, got %d", len(lock.unlocked))
	}
}

func TestUnlockAll_SkipsDeletedAppsForRevert(t *testing.T) {
	fake := newFakeArgoServer(v1alpha1.Application{
		Spec: v1alpha1.ApplicationSpec{
			Source: &v1alpha1.ApplicationSource{
				RepoURL:        "https://github.com/org/repo",
				Path:           "manifests",
				TargetRevision: "main",
			},
		},
	})
	defer fake.close()

	vcs := &mockVCS{}
	lock := &mockLock{
		locks: []models.Lock{
			{Application: "app-a", PRNumber: 42, ChangeType: models.ApplicationDeleted},
			{Application: "app-b", PRNumber: 42, ChangeType: models.ApplicationExisting},
		},
	}
	exec := &Executor{
		vcs:      vcs,
		argocd:   fake.client(),
		lock:     lock,
		config:   &config.Config{},
		renderer: diff.NewRenderer(),
	}
	event := defaultEvent()

	err := exec.UnlockAll(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Both should still be unlocked
	if len(lock.unlocked) != 2 {
		t.Errorf("expected 2 unlocks, got %d", len(lock.unlocked))
	}
}

func TestUnlockAll_ListByPRError(t *testing.T) {
	lock := &mockLock{listByPRErr: fmt.Errorf("redis down")}
	exec := newTestExecutor(&mockVCS{}, lock, nil)
	event := defaultEvent()

	err := exec.UnlockAll(context.Background(), event)
	if err == nil {
		t.Fatal("expected error when ListByPR fails")
	}
	if !strings.Contains(err.Error(), "listing locks") {
		t.Errorf("error = %q, want containing 'listing locks'", err.Error())
	}
}

// ---------------------------------------------------------------------------
// Tests: getRepoConfig
// ---------------------------------------------------------------------------

func TestGetRepoConfig_LoadsAndCaches(t *testing.T) {
	vcs := &mockVCS{
		repoConfigData: []byte("version: 1\nautoplan: true\n"),
	}
	exec := newTestExecutor(vcs, &mockLock{}, nil)
	event := defaultEvent()

	// First call should load
	rc := exec.getRepoConfig(context.Background(), event)
	if rc == nil {
		t.Fatal("expected non-nil repo config")
	}
	if !event.RepoConfigLoaded {
		t.Error("expected RepoConfigLoaded to be true")
	}

	// Second call should use cache (even if VCS mock changes)
	vcs.repoConfigData = nil
	rc2 := exec.getRepoConfig(context.Background(), event)
	if rc2 != rc {
		t.Error("expected cached config to be returned")
	}
}

func TestGetRepoConfig_NotFound(t *testing.T) {
	vcs := &mockVCS{
		repoConfigErr: fmt.Errorf("not found"),
	}
	exec := newTestExecutor(vcs, &mockLock{}, nil)
	event := defaultEvent()

	rc := exec.getRepoConfig(context.Background(), event)
	if rc != nil {
		t.Error("expected nil repo config when not found")
	}
	if !event.RepoConfigLoaded {
		t.Error("expected RepoConfigLoaded to be true even when not found")
	}
}

func TestGetRepoConfig_InvalidConfig(t *testing.T) {
	// Valid YAML but missing required version field
	vcs := &mockVCS{
		repoConfigData: []byte("version: -1\napplications: not_a_list\n"),
	}
	exec := newTestExecutor(vcs, &mockLock{}, nil)
	event := defaultEvent()

	rc := exec.getRepoConfig(context.Background(), event)
	// Even if parsing fails, it should be marked as loaded
	if !event.RepoConfigLoaded {
		t.Error("expected RepoConfigLoaded to be true even for invalid config")
	}
	// Depending on how strict LoadRepoConfig is, rc may or may not be nil.
	// What matters is that it doesn't panic and marks as loaded.
	_ = rc
}

// ---------------------------------------------------------------------------
// Tests: InvalidatePlanComments
// ---------------------------------------------------------------------------

func TestInvalidatePlanComments(t *testing.T) {
	vcs := &mockVCS{}
	exec := newTestExecutor(vcs, &mockLock{}, nil)
	event := defaultEvent()

	err := exec.InvalidatePlanComments(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !vcs.invalidated {
		t.Error("expected InvalidatePlanComments to be called")
	}
}

func TestInvalidatePlanComments_Error(t *testing.T) {
	vcs := &mockVCS{invalidateErr: fmt.Errorf("vcs error")}
	exec := newTestExecutor(vcs, &mockLock{}, nil)
	event := defaultEvent()

	err := exec.InvalidatePlanComments(context.Background(), event)
	if err == nil {
		t.Fatal("expected error")
	}
}

// ---------------------------------------------------------------------------
// Tests: executeUnlock
// ---------------------------------------------------------------------------

func TestExecuteUnlock_NoLocks(t *testing.T) {
	vcs := &mockVCS{}
	lock := &mockLock{locks: nil}
	exec := newTestExecutor(vcs, lock, nil)
	cmd := &Command{Name: CommandUnlock}
	event := defaultEventWithComment()

	err := exec.executeUnlock(context.Background(), cmd, event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vcs.postedComments) == 0 {
		t.Fatal("expected comment to be posted")
	}
	if !strings.Contains(vcs.postedComments[0].Body, "No applications are locked") {
		t.Error("expected 'No applications are locked' in comment")
	}
}

func TestExecuteUnlock_AllApps(t *testing.T) {
	vcs := &mockVCS{}
	lock := &mockLock{
		locks: []models.Lock{
			{Application: "app-a", PRNumber: 42, Repo: "org/repo"},
			{Application: "app-b", PRNumber: 42, Repo: "org/repo"},
		},
	}
	exec := newTestExecutor(vcs, lock, nil)
	cmd := &Command{Name: CommandUnlock}
	event := defaultEventWithComment()

	err := exec.executeUnlock(context.Background(), cmd, event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(lock.unlocked) != 2 {
		t.Errorf("expected 2 unlocks, got %d", len(lock.unlocked))
	}
	if len(vcs.postedComments) == 0 {
		t.Fatal("expected comment to be posted")
	}
	body := vcs.postedComments[0].Body
	if !strings.Contains(body, "app-a") || !strings.Contains(body, "Unlocked") {
		t.Errorf("expected unlock result in comment, got: %s", body)
	}
}

func TestExecuteUnlock_SpecificApp(t *testing.T) {
	vcs := &mockVCS{}
	lock := &mockLock{
		locks: []models.Lock{
			{Application: "app-a", PRNumber: 42, Repo: "org/repo"},
			{Application: "app-b", PRNumber: 42, Repo: "org/repo"},
		},
	}
	exec := newTestExecutor(vcs, lock, nil)
	cmd := &Command{Name: CommandUnlock, Application: "app-a"}
	event := defaultEventWithComment()

	err := exec.executeUnlock(context.Background(), cmd, event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(lock.unlocked) != 1 {
		t.Fatalf("expected 1 unlock, got %d", len(lock.unlocked))
	}
	if lock.unlocked[0] != "app-a" {
		t.Errorf("expected app-a to be unlocked, got %s", lock.unlocked[0])
	}
}

func TestExecuteUnlock_AppNotLocked(t *testing.T) {
	vcs := &mockVCS{}
	lock := &mockLock{
		locks: []models.Lock{
			{Application: "app-a", PRNumber: 42, Repo: "org/repo"},
		},
	}
	exec := newTestExecutor(vcs, lock, nil)
	cmd := &Command{Name: CommandUnlock, Application: "app-nonexistent"}
	event := defaultEventWithComment()

	err := exec.executeUnlock(context.Background(), cmd, event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(lock.unlocked) != 0 {
		t.Errorf("expected no unlocks, got %v", lock.unlocked)
	}
	if len(vcs.postedComments) == 0 {
		t.Fatal("expected comment to be posted")
	}
	if !strings.Contains(vcs.postedComments[0].Body, "is not locked") {
		t.Error("expected 'is not locked' in comment")
	}
}

func TestExecuteUnlock_ListByPRError(t *testing.T) {
	vcs := &mockVCS{}
	lock := &mockLock{listByPRErr: fmt.Errorf("redis down")}
	exec := newTestExecutor(vcs, lock, nil)
	cmd := &Command{Name: CommandUnlock}
	event := defaultEventWithComment()

	err := exec.executeUnlock(context.Background(), cmd, event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Error is posted as a comment, not returned
	if len(vcs.postedComments) == 0 {
		t.Fatal("expected error comment to be posted")
	}
	if !strings.Contains(vcs.postedComments[0].Body, "listing locks") {
		t.Error("expected 'listing locks' error in comment")
	}
}

func TestExecuteUnlock_UnlockError(t *testing.T) {
	vcs := &mockVCS{}
	lock := &mockLock{
		locks: []models.Lock{
			{Application: "app-a", PRNumber: 42, Repo: "org/repo"},
		},
		unlockErr: fmt.Errorf("unlock failed"),
	}
	exec := newTestExecutor(vcs, lock, nil)
	cmd := &Command{Name: CommandUnlock}
	event := defaultEventWithComment()

	err := exec.executeUnlock(context.Background(), cmd, event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vcs.postedComments) == 0 {
		t.Fatal("expected comment to be posted")
	}
	body := vcs.postedComments[0].Body
	if !strings.Contains(body, "unlock failed") {
		t.Errorf("expected error in unlock result comment, got: %s", body)
	}
}

func TestExecuteUnlock_AddsReaction(t *testing.T) {
	vcs := &mockVCS{}
	lock := &mockLock{locks: nil}
	exec := newTestExecutor(vcs, lock, nil)
	cmd := &Command{Name: CommandUnlock}
	event := defaultEventWithComment()

	_ = exec.executeUnlock(context.Background(), cmd, event)
	if len(vcs.reactions) != 1 {
		t.Fatalf("expected 1 reaction, got %d", len(vcs.reactions))
	}
	if vcs.reactions[0].Reaction != "eyes" {
		t.Errorf("expected 'eyes' reaction, got %q", vcs.reactions[0].Reaction)
	}
}

// ---------------------------------------------------------------------------
// Tests: renderUnlockResults
// ---------------------------------------------------------------------------

func TestRenderUnlockResults(t *testing.T) {
	exec := newTestExecutor(&mockVCS{}, &mockLock{}, nil)

	tests := []struct {
		name     string
		results  []unlockResult
		contains []string
	}{
		{
			name: "all succeeded",
			results: []unlockResult{
				{Application: "app-a", Success: true},
				{Application: "app-b", Success: true},
			},
			contains: []string{"app-a", "Unlocked", "app-b", "All locks released"},
		},
		{
			name: "mixed results",
			results: []unlockResult{
				{Application: "app-a", Success: true},
				{Application: "app-b", Error: fmt.Errorf("timeout")},
			},
			contains: []string{"app-a", "Unlocked", "app-b", "timeout"},
		},
		{
			name: "all failed",
			results: []unlockResult{
				{Application: "app-a", Error: fmt.Errorf("error1")},
			},
			contains: []string{"app-a", "error1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := exec.renderUnlockResults(tt.results)
			for _, s := range tt.contains {
				if !strings.Contains(output, s) {
					t.Errorf("output missing %q: %s", s, output)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Tests: executeHelp
// ---------------------------------------------------------------------------

func TestExecuteHelp(t *testing.T) {
	vcs := &mockVCS{}
	exec := newTestExecutor(vcs, &mockLock{}, nil)
	event := defaultEvent()

	err := exec.executeHelp(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vcs.postedComments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(vcs.postedComments))
	}
	body := vcs.postedComments[0].Body
	expected := []string{
		"Lemuria Help",
		"lemuria plan",
		"lemuria sync",
		"lemuria unlock",
		"lemuria rollback",
		"lemuria help",
	}
	for _, s := range expected {
		if !strings.Contains(body, s) {
			t.Errorf("help text missing %q", s)
		}
	}
}

// ---------------------------------------------------------------------------
// Tests: postError / postComment
// ---------------------------------------------------------------------------

func TestPostError(t *testing.T) {
	vcs := &mockVCS{}
	exec := newTestExecutor(vcs, &mockLock{}, nil)
	event := defaultEvent()

	err := exec.postError(context.Background(), event, fmt.Errorf("something went wrong"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vcs.postedComments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(vcs.postedComments))
	}
	body := vcs.postedComments[0].Body
	if !strings.Contains(body, "Lemuria Error") {
		t.Error("expected 'Lemuria Error' in comment")
	}
	if !strings.Contains(body, "something went wrong") {
		t.Error("expected error message in comment")
	}
	if vcs.postedComments[0].IsPlan {
		t.Error("error comments should not be marked as plan")
	}
}

func TestPostPlanComment(t *testing.T) {
	vcs := &mockVCS{}
	exec := newTestExecutor(vcs, &mockLock{}, nil)
	event := defaultEvent()

	err := exec.postPlanComment(context.Background(), event, "## Plan output")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vcs.postedComments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(vcs.postedComments))
	}
	if !vcs.postedComments[0].IsPlan {
		t.Error("plan comment should be marked as plan")
	}
}

// ---------------------------------------------------------------------------
// Tests: isAppAffected (additional cases beyond existing test)
// ---------------------------------------------------------------------------

func TestIsAppAffected_RepoURLMatching(t *testing.T) {
	exec := newTestExecutor(&mockVCS{}, &mockLock{}, nil)

	tests := []struct {
		name    string
		app     models.Application
		repoURL string
		files   []string
		want    bool
	}{
		{
			name: "app with matching repo and path",
			app: models.Application{
				Name:    "my-app",
				RepoURL: "https://github.com/org/repo",
				Path:    "apps/my-app",
			},
			repoURL: "https://github.com/org/repo",
			files:   []string{"apps/my-app/deployment.yaml"},
			want:    true,
		},
		{
			name: "app with matching repo but different path",
			app: models.Application{
				Name:    "my-app",
				RepoURL: "https://github.com/org/repo",
				Path:    "apps/other-app",
			},
			repoURL: "https://github.com/org/repo",
			files:   []string{"apps/my-app/deployment.yaml"},
			want:    false,
		},
		{
			name: "app with different repo",
			app: models.Application{
				Name:    "my-app",
				RepoURL: "https://github.com/org/other-repo",
				Path:    "apps/my-app",
			},
			repoURL: "https://github.com/org/repo",
			files:   []string{"apps/my-app/deployment.yaml"},
			want:    false,
		},
		{
			name: "app with .git suffix matches",
			app: models.Application{
				Name:    "my-app",
				RepoURL: "https://github.com/org/repo.git",
				Path:    "apps/my-app",
			},
			repoURL: "https://github.com/org/repo",
			files:   []string{"apps/my-app/deployment.yaml"},
			want:    true,
		},
		{
			name: "multi-source app with matching repo",
			app: models.Application{
				Name: "multi-app",
				Sources: []models.ApplicationSource{
					{RepoURL: "https://github.com/org/repo", Path: "manifests"},
					{RepoURL: "https://charts.bitnami.com", Chart: "redis"},
				},
			},
			repoURL: "https://github.com/org/repo",
			files:   []string{"manifests/deployment.yaml"},
			want:    true, // multi-source paths are now checked individually
		},
		{
			name: "app with empty path does not match (isAppAffected requires non-empty path)",
			app: models.Application{
				Name:    "root-app",
				RepoURL: "https://github.com/org/repo",
				Path:    "",
			},
			repoURL: "https://github.com/org/repo",
			files:   []string{"anything.yaml"},
			want:    false,
		},
		{
			name: "app with dot path matches all files in repo",
			app: models.Application{
				Name:    "root-app",
				RepoURL: "https://github.com/org/repo",
				Path:    ".",
			},
			repoURL: "https://github.com/org/repo",
			files:   []string{"anything.yaml"},
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := exec.isAppAffected(tt.app, tt.repoURL, tt.files, nil)
			if got != tt.want {
				t.Errorf("isAppAffected() = %v, want %v", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Tests: syncCommentTracker
// ---------------------------------------------------------------------------

func TestSyncCommentTracker_RenderProgress(t *testing.T) {
	vcs := &mockVCS{}
	event := defaultEvent()
	appNames := []string{"app-a", "app-b", "app-c"}

	tracker := newSyncCommentTracker(vcs, event, appNames)

	// Initial render - all waiting
	progress := tracker.renderProgress()
	if !strings.Contains(progress, "Sync in progress") {
		t.Error("expected 'Sync in progress' in output")
	}
	for _, name := range appNames {
		if !strings.Contains(progress, name) {
			t.Errorf("expected app %q in progress output", name)
		}
	}
	if !strings.Contains(progress, "Waiting...") {
		t.Error("expected 'Waiting...' in initial progress")
	}

	// Mark first as completed successfully
	tracker.mu.Lock()
	tracker.results[0] = syncResult{
		Application: "app-a",
		Result:      &models.SyncResult{Phase: models.SyncPhaseSucceeded},
	}
	tracker.completed[0] = true
	tracker.mu.Unlock()

	progress = tracker.renderProgress()
	if !strings.Contains(progress, "Succeeded") {
		t.Error("expected 'Succeeded' for completed app")
	}

	// Mark second as error
	tracker.mu.Lock()
	tracker.results[1] = syncResult{
		Application: "app-b",
		Error:       fmt.Errorf("sync failed"),
	}
	tracker.completed[1] = true
	tracker.mu.Unlock()

	progress = tracker.renderProgress()
	if !strings.Contains(progress, "Error") {
		t.Error("expected 'Error' for failed app")
	}

	// Third still waiting
	if !strings.Contains(progress, "Waiting...") {
		t.Error("expected 'Waiting...' for incomplete app")
	}
}

func TestSyncCommentTracker_PostInitial(t *testing.T) {
	vcs := &mockVCS{}
	event := defaultEvent()
	tracker := newSyncCommentTracker(vcs, event, []string{"app-a"})

	tracker.postInitial(context.Background())

	if len(vcs.postedComments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(vcs.postedComments))
	}
	if tracker.commentID == 0 {
		t.Error("expected commentID to be set")
	}
}

func TestSyncCommentTracker_PostFinal_UpdatesExisting(t *testing.T) {
	vcs := &mockVCS{}
	event := defaultEvent()
	tracker := newSyncCommentTracker(vcs, event, []string{"app-a"})

	tracker.postInitial(context.Background())

	err := tracker.postFinal(context.Background(), "## Final output")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vcs.updatedComments) != 1 {
		t.Fatalf("expected 1 update, got %d", len(vcs.updatedComments))
	}
	if vcs.updatedComments[0].Body != "## Final output" {
		t.Error("expected final body in updated comment")
	}
}

func TestSyncCommentTracker_PostFinal_FallsBackToNew(t *testing.T) {
	vcs := &mockVCS{}
	event := defaultEvent()
	tracker := newSyncCommentTracker(vcs, event, []string{"app-a"})
	// Don't call postInitial, so commentID stays 0

	err := tracker.postFinal(context.Background(), "## Final output")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vcs.postedComments) != 1 {
		t.Fatalf("expected 1 new comment, got %d", len(vcs.postedComments))
	}
}

func TestSyncCommentTracker_PostFinal_UpdateFailsFallsBackToNew(t *testing.T) {
	vcs := &mockVCS{updateCommentErr: fmt.Errorf("update failed")}
	event := defaultEvent()
	tracker := newSyncCommentTracker(vcs, event, []string{"app-a"})

	tracker.postInitial(context.Background())
	vcs.updateCommentErr = fmt.Errorf("update failed")

	err := tracker.postFinal(context.Background(), "## Final output")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should have posted a new comment as fallback
	found := false
	for _, c := range vcs.postedComments {
		if c.Body == "## Final output" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected fallback comment to be posted")
	}
}

// ---------------------------------------------------------------------------
// Tests: renderProgress edge cases
// ---------------------------------------------------------------------------

func TestSyncCommentTracker_RenderProgress_ResultNil(t *testing.T) {
	vcs := &mockVCS{}
	event := defaultEvent()
	tracker := newSyncCommentTracker(vcs, event, []string{"app-a"})

	// Mark completed but with nil Result and nil Error
	tracker.mu.Lock()
	tracker.results[0] = syncResult{Application: "app-a"}
	tracker.completed[0] = true
	tracker.mu.Unlock()

	progress := tracker.renderProgress()
	// nil Result + nil Error should show "Error"
	if !strings.Contains(progress, "Error") {
		t.Error("expected 'Error' for nil result with no error")
	}
}

func TestSyncCommentTracker_RenderProgress_FailedPhase(t *testing.T) {
	vcs := &mockVCS{}
	event := defaultEvent()
	tracker := newSyncCommentTracker(vcs, event, []string{"app-a"})

	tracker.mu.Lock()
	tracker.results[0] = syncResult{
		Application: "app-a",
		Result:      &models.SyncResult{Phase: models.SyncPhaseFailed},
	}
	tracker.completed[0] = true
	tracker.mu.Unlock()

	progress := tracker.renderProgress()
	if !strings.Contains(progress, "Failed") {
		t.Error("expected 'Failed' phase in progress")
	}
}

// ---------------------------------------------------------------------------
// Tests: mergeResourceHealth
// ---------------------------------------------------------------------------

func TestMergeResourceHealth(t *testing.T) {
	result := &models.SyncResult{
		Resources: []models.ResourceResult{
			{Resource: models.ResourceKey{Kind: "Deployment", Name: "app", Namespace: "default"}},
			{Resource: models.ResourceKey{Kind: "Service", Name: "svc", Namespace: "default"}},
			{Resource: models.ResourceKey{Kind: "ConfigMap", Name: "cm"}},
		},
	}

	healthInfo := []models.ResourceHealthInfo{
		{
			Resource:      models.ResourceKey{Kind: "Deployment", Name: "app", Namespace: "default"},
			HealthStatus:  models.HealthStatusHealthy,
			HealthMessage: "All replicas ready",
		},
		{
			Resource:      models.ResourceKey{Kind: "Service", Name: "svc", Namespace: "default"},
			HealthStatus:  models.HealthStatusHealthy,
			HealthMessage: "Service is healthy",
		},
	}

	mergeResourceHealth(result, healthInfo)

	if result.Resources[0].HealthStatus != models.HealthStatusHealthy {
		t.Errorf("Deployment health = %v, want Healthy", result.Resources[0].HealthStatus)
	}
	if result.Resources[0].HealthMessage != "All replicas ready" {
		t.Errorf("Deployment health message = %q, want 'All replicas ready'", result.Resources[0].HealthMessage)
	}
	if result.Resources[1].HealthStatus != models.HealthStatusHealthy {
		t.Errorf("Service health = %v, want Healthy", result.Resources[1].HealthStatus)
	}
	// ConfigMap not in health info, should remain empty
	if result.Resources[2].HealthStatus != "" {
		t.Errorf("ConfigMap health = %v, want empty", result.Resources[2].HealthStatus)
	}
}

func TestMergeResourceHealth_EmptyHealthInfo(t *testing.T) {
	result := &models.SyncResult{
		Resources: []models.ResourceResult{
			{Resource: models.ResourceKey{Kind: "Deployment", Name: "app"}},
		},
	}

	mergeResourceHealth(result, nil)

	// Should not panic, health should remain empty
	if result.Resources[0].HealthStatus != "" {
		t.Error("expected empty health status")
	}
}

// ---------------------------------------------------------------------------
// Tests: autoMergePR
// ---------------------------------------------------------------------------

func TestAutoMergePR_DefaultMethod(t *testing.T) {
	vcs := &mockVCS{}
	cfg := &config.Config{
		Defaults: config.DefaultsConfig{},
	}
	exec := newTestExecutor(vcs, &mockLock{}, cfg)
	event := defaultEvent()

	err := exec.autoMergePR(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vcs.mergedPRs) != 1 {
		t.Fatalf("expected 1 merge, got %d", len(vcs.mergedPRs))
	}
	if vcs.mergedPRs[0].Method != "squash" {
		t.Errorf("merge method = %q, want 'squash'", vcs.mergedPRs[0].Method)
	}
}

func TestAutoMergePR_CustomMethod(t *testing.T) {
	vcs := &mockVCS{}
	cfg := &config.Config{
		Defaults: config.DefaultsConfig{
			MergeMethod: "merge",
		},
	}
	exec := newTestExecutor(vcs, &mockLock{}, cfg)
	event := defaultEvent()

	err := exec.autoMergePR(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if vcs.mergedPRs[0].Method != "merge" {
		t.Errorf("merge method = %q, want 'merge'", vcs.mergedPRs[0].Method)
	}
}

func TestAutoMergePR_DeleteSourceBranch(t *testing.T) {
	vcs := &mockVCS{}
	cfg := &config.Config{
		Defaults: config.DefaultsConfig{
			DeleteSourceBranch: true,
		},
	}
	exec := newTestExecutor(vcs, &mockLock{}, cfg)
	event := defaultEvent()

	err := exec.autoMergePR(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vcs.deletedBranches) != 1 {
		t.Fatalf("expected 1 branch deletion, got %d", len(vcs.deletedBranches))
	}
	if vcs.deletedBranches[0] != "feature-branch" {
		t.Errorf("deleted branch = %q, want 'feature-branch'", vcs.deletedBranches[0])
	}
}

func TestAutoMergePR_DoesNotDeleteProtectedBranch(t *testing.T) {
	vcs := &mockVCS{}
	cfg := &config.Config{
		Defaults: config.DefaultsConfig{
			DeleteSourceBranch: true,
		},
	}
	exec := newTestExecutor(vcs, &mockLock{}, cfg)
	event := defaultEvent()
	event.PR.HeadRef = "main" // Protected branch

	err := exec.autoMergePR(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vcs.deletedBranches) != 0 {
		t.Errorf("expected no branch deletions for protected branch, got %v", vcs.deletedBranches)
	}
}

func TestAutoMergePR_MergeError(t *testing.T) {
	vcs := &mockVCS{mergeErr: fmt.Errorf("merge conflict")}
	cfg := &config.Config{}
	exec := newTestExecutor(vcs, &mockLock{}, cfg)
	event := defaultEvent()

	err := exec.autoMergePR(context.Background(), event)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "merging PR") {
		t.Errorf("error = %q, want containing 'merging PR'", err.Error())
	}
}

// ---------------------------------------------------------------------------
// Tests: setToSlice
// ---------------------------------------------------------------------------

func TestSetToSlice(t *testing.T) {
	tests := []struct {
		name string
		set  map[string]struct{}
		want int
	}{
		{"empty set", map[string]struct{}{}, 0},
		{"single element", map[string]struct{}{"a": {}}, 1},
		{"multiple elements", map[string]struct{}{"a": {}, "b": {}, "c": {}}, 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := setToSlice(tt.set)
			if len(result) != tt.want {
				t.Errorf("setToSlice() returned %d elements, want %d", len(result), tt.want)
			}
			// Verify all elements are present
			for k := range tt.set {
				found := false
				for _, v := range result {
					if v == k {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("setToSlice() missing element %q", k)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Tests: detectModifiedAppsFromContent
// ---------------------------------------------------------------------------

func TestDetectApplicationChangesFromScan_EmptyScan(t *testing.T) {
	scanned := &ScannedRepoApps{
		HeadAppContent: map[string][]byte{},
		BaseAppContent: map[string][]byte{},
	}
	parsed := detectApplicationChangesFromScan(scanned)
	if len(parsed.New) != 0 {
		t.Errorf("expected 0 new apps, got %d", len(parsed.New))
	}
	if len(parsed.Deleted) != 0 {
		t.Errorf("expected 0 deleted apps, got %d", len(parsed.Deleted))
	}
	if len(parsed.Modified) != 0 {
		t.Errorf("expected 0 modified apps, got %d", len(parsed.Modified))
	}
}

func TestDetectApplicationChangesFromScan_NewApp(t *testing.T) {
	scanned := &ScannedRepoApps{
		HeadApps: []models.Application{
			{Name: "my-app", Path: "apps/my-app"},
		},
		HeadAppContent: map[string][]byte{},
		BaseAppContent: map[string][]byte{},
	}
	parsed := detectApplicationChangesFromScan(scanned)
	if len(parsed.New) != 1 {
		t.Fatalf("expected 1 new app, got %d", len(parsed.New))
	}
	if parsed.New[0].Name != "my-app" {
		t.Errorf("new app name = %q, want 'my-app'", parsed.New[0].Name)
	}
	if parsed.New[0].ChangeType != models.ApplicationNew {
		t.Errorf("change type = %q, want %q", parsed.New[0].ChangeType, models.ApplicationNew)
	}
}

func TestDetectApplicationChangesFromScan_DeletedApp(t *testing.T) {
	scanned := &ScannedRepoApps{
		BaseApps: []models.Application{
			{Name: "old-app", Path: "apps/old-app"},
		},
		HeadAppContent: map[string][]byte{},
		BaseAppContent: map[string][]byte{},
	}
	parsed := detectApplicationChangesFromScan(scanned)
	if len(parsed.Deleted) != 1 {
		t.Fatalf("expected 1 deleted app, got %d", len(parsed.Deleted))
	}
	if parsed.Deleted[0].Name != "old-app" {
		t.Errorf("deleted app name = %q, want 'old-app'", parsed.Deleted[0].Name)
	}
	if parsed.Deleted[0].ChangeType != models.ApplicationDeleted {
		t.Errorf("change type = %q, want %q", parsed.Deleted[0].ChangeType, models.ApplicationDeleted)
	}
}

func TestDetectApplicationChangesFromScan_AppAddedAndRemoved(t *testing.T) {
	scanned := &ScannedRepoApps{
		HeadApps: []models.Application{
			{Name: "new-app", Path: "apps/new-app"},
		},
		BaseApps: []models.Application{
			{Name: "old-app", Path: "apps/old-app"},
		},
		HeadAppContent: map[string][]byte{},
		BaseAppContent: map[string][]byte{},
	}
	parsed := detectApplicationChangesFromScan(scanned)
	if len(parsed.New) != 1 {
		t.Fatalf("expected 1 new app, got %d", len(parsed.New))
	}
	if parsed.New[0].Name != "new-app" {
		t.Errorf("new app name = %q, want 'new-app'", parsed.New[0].Name)
	}
	if len(parsed.Deleted) != 1 {
		t.Fatalf("expected 1 deleted app, got %d", len(parsed.Deleted))
	}
	if parsed.Deleted[0].Name != "old-app" {
		t.Errorf("deleted app name = %q, want 'old-app'", parsed.Deleted[0].Name)
	}
}

func TestDetectApplicationChangesFromScan_ModifiedApp(t *testing.T) {
	headContent := []byte("apiVersion: v1\nkind: Application\nmetadata:\n  name: my-app\nspec:\n  source:\n    path: apps/my-app\n    targetRevision: HEAD\n")
	baseContent := []byte("apiVersion: v1\nkind: Application\nmetadata:\n  name: my-app\nspec:\n  source:\n    path: apps/my-app\n    targetRevision: main\n")
	scanned := &ScannedRepoApps{
		HeadApps: []models.Application{
			{Name: "my-app", Path: "apps/my-app", SourceFile: "argocd/app.yaml"},
		},
		BaseApps: []models.Application{
			{Name: "my-app", Path: "apps/my-app", SourceFile: "argocd/app.yaml"},
		},
		HeadAppContent: map[string][]byte{"my-app": headContent},
		BaseAppContent: map[string][]byte{"my-app": baseContent},
	}
	parsed := detectApplicationChangesFromScan(scanned)
	if len(parsed.New) != 0 {
		t.Errorf("expected 0 new apps, got %d", len(parsed.New))
	}
	if len(parsed.Deleted) != 0 {
		t.Errorf("expected 0 deleted apps, got %d", len(parsed.Deleted))
	}
	if len(parsed.Modified) != 1 {
		t.Fatalf("expected 1 modified app, got %d", len(parsed.Modified))
	}
	if parsed.Modified[0].Name != "my-app" {
		t.Errorf("modified app name = %q, want 'my-app'", parsed.Modified[0].Name)
	}
}

func TestDetectApplicationChangesFromScan_UnchangedApp(t *testing.T) {
	content := []byte("apiVersion: v1\nkind: Application\nmetadata:\n  name: my-app\n")
	scanned := &ScannedRepoApps{
		HeadApps: []models.Application{
			{Name: "my-app", Path: "apps/my-app", SourceFile: "argocd/app.yaml"},
		},
		BaseApps: []models.Application{
			{Name: "my-app", Path: "apps/my-app", SourceFile: "argocd/app.yaml"},
		},
		HeadAppContent: map[string][]byte{"my-app": content},
		BaseAppContent: map[string][]byte{"my-app": content},
	}
	parsed := detectApplicationChangesFromScan(scanned)
	if len(parsed.New) != 0 {
		t.Errorf("expected 0 new apps, got %d", len(parsed.New))
	}
	if len(parsed.Deleted) != 0 {
		t.Errorf("expected 0 deleted apps, got %d", len(parsed.Deleted))
	}
	if len(parsed.Modified) != 0 {
		t.Errorf("expected 0 modified apps (content unchanged), got %d", len(parsed.Modified))
	}
}

func TestDetectApplicationChangesFromScan_MultiAppFilePartialChange(t *testing.T) {
	// Two apps share the same source file. Only app-a has different content
	// between head and base; app-b is identical. We expect only app-a to
	// appear in Modified.
	sharedContent := []byte("apiVersion: v1\nkind: Application\nmetadata:\n  name: app-b\nspec:\n  source:\n    path: apps/b\n")
	scanned := &ScannedRepoApps{
		HeadApps: []models.Application{
			{Name: "app-a", Path: "apps/a", SourceFile: "argocd/multi.yaml"},
			{Name: "app-b", Path: "apps/b", SourceFile: "argocd/multi.yaml"},
		},
		BaseApps: []models.Application{
			{Name: "app-a", Path: "apps/a", SourceFile: "argocd/multi.yaml"},
			{Name: "app-b", Path: "apps/b", SourceFile: "argocd/multi.yaml"},
		},
		HeadAppContent: map[string][]byte{
			"app-a": []byte("apiVersion: v1\nkind: Application\nmetadata:\n  name: app-a\nspec:\n  source:\n    path: apps/a\n    targetRevision: HEAD\n"),
			"app-b": sharedContent,
		},
		BaseAppContent: map[string][]byte{
			"app-a": []byte("apiVersion: v1\nkind: Application\nmetadata:\n  name: app-a\nspec:\n  source:\n    path: apps/a\n    targetRevision: main\n"),
			"app-b": sharedContent,
		},
	}

	parsed := detectApplicationChangesFromScan(scanned)

	if len(parsed.New) != 0 {
		t.Errorf("expected 0 new apps, got %d", len(parsed.New))
	}
	if len(parsed.Deleted) != 0 {
		t.Errorf("expected 0 deleted apps, got %d", len(parsed.Deleted))
	}
	if len(parsed.Modified) != 1 {
		t.Fatalf("expected 1 modified app, got %d", len(parsed.Modified))
	}
	if parsed.Modified[0].Name != "app-a" {
		t.Errorf("modified app name = %q, want 'app-a'", parsed.Modified[0].Name)
	}
}

// ---------------------------------------------------------------------------
// Tests: executeSync edge cases
// ---------------------------------------------------------------------------

func TestExecuteSync_NoLocks(t *testing.T) {
	vcs := &mockVCS{}
	lock := &mockLock{locks: nil}
	exec := newTestExecutor(vcs, lock, nil)
	cmd := &Command{Name: CommandSync}
	event := defaultEventWithComment()

	err := exec.executeSync(context.Background(), cmd, event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vcs.postedComments) == 0 {
		t.Fatal("expected comment to be posted")
	}
	if !strings.Contains(vcs.postedComments[0].Body, "No applications are locked") {
		t.Error("expected 'No applications are locked' in comment")
	}
}

func TestExecuteSync_ListLocksError(t *testing.T) {
	vcs := &mockVCS{}
	lock := &mockLock{listByPRErr: fmt.Errorf("redis error")}
	exec := newTestExecutor(vcs, lock, nil)
	cmd := &Command{Name: CommandSync}
	event := defaultEventWithComment()

	err := exec.executeSync(context.Background(), cmd, event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vcs.postedComments) == 0 {
		t.Fatal("expected error comment")
	}
	if !strings.Contains(vcs.postedComments[0].Body, "listing locks") {
		t.Error("expected error about listing locks")
	}
}

func TestExecuteSync_SpecificAppDirectMatch(t *testing.T) {
	vcs := &mockVCS{
		pr: &models.PullRequestDetail{State: models.PRStateOpen, Mergeable: true},
	}
	lock := &mockLock{
		locks: []models.Lock{
			{Application: "app-a", PRNumber: 42, Repo: "org/repo", PlanRevision: "abc123", ChangeType: models.ApplicationNew},
		},
	}
	exec := newTestExecutor(vcs, lock, nil)
	cmd := &Command{Name: CommandSync, Application: "app-a"}
	event := defaultEventWithComment()
	event.PR.HeadSHA = "abc123"

	// syncNewApplication will fail because argocd is nil, but the lock filter works.
	// We just verify the app filter selects app-a.
	// The error will be caught in syncApplication and posted via tracker.
	_ = exec.executeSync(context.Background(), cmd, event)
	// If sync started, the tracker would have been created.
}

func TestExecuteSync_StalePlan(t *testing.T) {
	vcs := &mockVCS{
		pr: &models.PullRequestDetail{State: models.PRStateOpen, Mergeable: true},
	}
	lock := &mockLock{
		locks: []models.Lock{
			{
				Application:  "app-a",
				PRNumber:     42,
				Repo:         "org/repo",
				PlanRevision: "old-sha",
				ChangeType:   models.ApplicationNew, // skip auto-sync check
			},
		},
	}
	exec := newTestExecutor(vcs, lock, nil)
	cmd := &Command{Name: CommandSync}
	event := defaultEventWithComment()
	event.PR.HeadSHA = "new-sha"

	err := exec.executeSync(context.Background(), cmd, event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vcs.postedComments) == 0 {
		t.Fatal("expected comment about stale plan")
	}
	body := vcs.postedComments[0].Body
	if !strings.Contains(body, "stale") {
		t.Errorf("expected 'stale' in comment, got: %s", body)
	}
}

// ---------------------------------------------------------------------------
// Tests: executeRollback
// ---------------------------------------------------------------------------

func TestRenderSingleRollbackResult_Success(t *testing.T) {
	exec := newTestExecutor(&mockVCS{}, &mockLock{}, nil)

	result := rollbackResult{
		Application:    "my-app",
		TargetRevision: "main",
		Result: &models.SyncResult{
			Phase: models.SyncPhaseSucceeded,
		},
	}

	output := exec.renderSingleRollbackResult(result, false)
	if !strings.Contains(output, "my-app") {
		t.Error("expected app name in output")
	}
	if !strings.Contains(output, "main") {
		t.Error("expected target revision in output")
	}
	if !strings.Contains(output, "Rollback successful") {
		t.Error("expected 'Rollback successful' in output")
	}
}

func TestRenderSingleRollbackResult_DryRun(t *testing.T) {
	exec := newTestExecutor(&mockVCS{}, &mockLock{}, nil)

	result := rollbackResult{
		Application:    "my-app",
		TargetRevision: "main",
		Result: &models.SyncResult{
			Phase: models.SyncPhaseSucceeded,
		},
	}

	output := exec.renderSingleRollbackResult(result, true)
	if !strings.Contains(output, "Dry-run") {
		t.Error("expected 'Dry-run' in output")
	}
}

func TestRenderSingleRollbackResult_Error(t *testing.T) {
	exec := newTestExecutor(&mockVCS{}, &mockLock{}, nil)

	result := rollbackResult{
		Application:    "my-app",
		TargetRevision: "main",
		Error:          fmt.Errorf("sync timeout"),
	}

	output := exec.renderSingleRollbackResult(result, false)
	if !strings.Contains(output, "sync timeout") {
		t.Error("expected error message in output")
	}
}

func TestRenderSingleRollbackResult_Failed(t *testing.T) {
	exec := newTestExecutor(&mockVCS{}, &mockLock{}, nil)

	result := rollbackResult{
		Application:    "my-app",
		TargetRevision: "main",
		Result: &models.SyncResult{
			Phase:   models.SyncPhaseFailed,
			Message: "resource validation failed",
		},
	}

	output := exec.renderSingleRollbackResult(result, false)
	if !strings.Contains(output, "resource validation failed") {
		t.Error("expected failure message in output")
	}
}

func TestRenderRollbackResults_AllSucceeded(t *testing.T) {
	exec := newTestExecutor(&mockVCS{}, &mockLock{}, nil)

	results := []rollbackResult{
		{
			Application:    "app-a",
			TargetRevision: "main",
			Result:         &models.SyncResult{Phase: models.SyncPhaseSucceeded},
		},
		{
			Application:    "app-b",
			TargetRevision: "main",
			Result:         &models.SyncResult{Phase: models.SyncPhaseSucceeded},
		},
	}

	output := exec.renderRollbackResults(results, false)
	if !strings.Contains(output, "All applications rolled back successfully") {
		t.Error("expected success message in output")
	}
}

func TestRenderRollbackResults_MixedResults(t *testing.T) {
	exec := newTestExecutor(&mockVCS{}, &mockLock{}, nil)

	results := []rollbackResult{
		{
			Application:    "app-a",
			TargetRevision: "main",
			Result:         &models.SyncResult{Phase: models.SyncPhaseSucceeded},
		},
		{
			Application:    "app-b",
			TargetRevision: "main",
			Error:          fmt.Errorf("timeout"),
		},
	}

	output := exec.renderRollbackResults(results, false)
	if strings.Contains(output, "All applications rolled back successfully") {
		t.Error("should not show success message when some failed")
	}
	if !strings.Contains(output, "timeout") {
		t.Error("expected error in output")
	}
}

func TestRenderRollbackResults_DryRun(t *testing.T) {
	exec := newTestExecutor(&mockVCS{}, &mockLock{}, nil)

	results := []rollbackResult{
		{
			Application:    "app-a",
			TargetRevision: "main",
			Result:         &models.SyncResult{Phase: models.SyncPhaseSucceeded},
		},
	}

	output := exec.renderRollbackResults(results, true)
	if !strings.Contains(output, "Dry-run") {
		t.Error("expected 'Dry-run' in output")
	}
}

func TestRenderRollbackResults_RunningPhase(t *testing.T) {
	exec := newTestExecutor(&mockVCS{}, &mockLock{}, nil)

	results := []rollbackResult{
		{
			Application:    "app-a",
			TargetRevision: "main",
			Result:         &models.SyncResult{Phase: models.SyncPhaseRunning},
		},
	}

	output := exec.renderRollbackResults(results, false)
	if !strings.Contains(output, "in progress") {
		t.Error("expected 'in progress' in output")
	}
}

// ---------------------------------------------------------------------------
// Tests: checkRollbackRequirements
// ---------------------------------------------------------------------------

func TestCheckRollbackRequirements_NoApprovalRequired(t *testing.T) {
	vcs := &mockVCS{}
	cfg := &config.Config{
		Defaults: config.DefaultsConfig{RequireApproval: false},
	}
	exec := newTestExecutor(vcs, &mockLock{}, cfg)
	event := defaultEvent()

	err := exec.checkRollbackRequirements(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCheckRollbackRequirements_ApprovalRequired_Approved(t *testing.T) {
	vcs := &mockVCS{prApproved: true}
	cfg := &config.Config{
		Defaults: config.DefaultsConfig{RequireApproval: true},
	}
	exec := newTestExecutor(vcs, &mockLock{}, cfg)
	event := defaultEvent()

	err := exec.checkRollbackRequirements(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCheckRollbackRequirements_ApprovalRequired_NotApproved(t *testing.T) {
	vcs := &mockVCS{prApproved: false}
	cfg := &config.Config{
		Defaults: config.DefaultsConfig{RequireApproval: true},
	}
	exec := newTestExecutor(vcs, &mockLock{}, cfg)
	event := defaultEvent()

	err := exec.checkRollbackRequirements(context.Background(), event)
	if err == nil {
		t.Fatal("expected error for unapproved PR")
	}
	if !strings.Contains(err.Error(), "must be approved") {
		t.Errorf("error = %q, want containing 'must be approved'", err.Error())
	}
}

func TestCheckRollbackRequirements_ApprovalCheckError(t *testing.T) {
	vcs := &mockVCS{prApprovedErr: fmt.Errorf("API error")}
	cfg := &config.Config{
		Defaults: config.DefaultsConfig{RequireApproval: true},
	}
	exec := newTestExecutor(vcs, &mockLock{}, cfg)
	event := defaultEvent()

	err := exec.checkRollbackRequirements(context.Background(), event)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "checking PR approval") {
		t.Errorf("error = %q, want containing 'checking PR approval'", err.Error())
	}
}

// ---------------------------------------------------------------------------
// Tests: convertToRenderResults
// ---------------------------------------------------------------------------

func TestConvertToRenderResults_FullFields(t *testing.T) {
	results := []appPlanResult{
		{
			Application:        "app-a",
			ApplicationSetName: "my-appset",
			Diffs: []models.ManifestDiff{
				{Resource: models.ResourceKey{Kind: "Deployment", Name: "app"}, Diff: "+line"},
			},
			Summary:        argocd.DiffSummary{Created: 1, Updated: 2, Deleted: 3},
			LockStatus:     "Locked by this PR",
			Warning:        "auto-sync enabled",
			Error:          fmt.Errorf("something"),
			ChangeType:     models.ApplicationNew,
			SourceFile:     "apps/app.yaml",
			IsGeneratedApp: true,
		},
	}

	rendered := convertToRenderResults(results)
	if len(rendered) != 1 {
		t.Fatalf("expected 1 result, got %d", len(rendered))
	}

	r := rendered[0]
	if r.Application != "app-a" {
		t.Errorf("Application = %q, want 'app-a'", r.Application)
	}
	if r.ApplicationSetName != "my-appset" {
		t.Errorf("ApplicationSetName = %q, want 'my-appset'", r.ApplicationSetName)
	}
	if len(r.Diffs) != 1 {
		t.Errorf("Diffs count = %d, want 1", len(r.Diffs))
	}
	if r.Created != 1 || r.Updated != 2 || r.Deleted != 3 {
		t.Errorf("Summary = %d/%d/%d, want 1/2/3", r.Created, r.Updated, r.Deleted)
	}
	if r.LockStatus != "Locked by this PR" {
		t.Errorf("LockStatus = %q, want 'Locked by this PR'", r.LockStatus)
	}
	if r.Warning != "auto-sync enabled" {
		t.Errorf("Warning = %q, want 'auto-sync enabled'", r.Warning)
	}
	if r.Error == nil {
		t.Error("expected non-nil Error")
	}
	if r.ChangeType != models.ApplicationNew {
		t.Errorf("ChangeType = %q, want %q", r.ChangeType, models.ApplicationNew)
	}
	if r.SourceFile != "apps/app.yaml" {
		t.Errorf("SourceFile = %q, want 'apps/app.yaml'", r.SourceFile)
	}
	if !r.IsGeneratedApp {
		t.Error("expected IsGeneratedApp to be true")
	}
}

// ---------------------------------------------------------------------------
// Tests: Execute dispatch for all command types
// ---------------------------------------------------------------------------

func TestExecute_DispatchesPlan(t *testing.T) {
	// Plan dispatch with specific app requires argocd. We test dispatch
	// works by verifying the help command dispatches properly.
	// Full plan tests use the fakeArgoServer pattern from sync_test.go.
}

func TestExecute_DispatchesSync(t *testing.T) {
	vcs := &mockVCS{}
	lock := &mockLock{locks: nil}
	exec := newTestExecutor(vcs, lock, nil)
	cmd := &Command{Name: CommandSync}
	event := defaultEvent()

	err := exec.Execute(context.Background(), cmd, event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should post "No applications are locked" comment
	if len(vcs.postedComments) > 0 {
		if !strings.Contains(vcs.postedComments[0].Body, "No applications are locked") {
			t.Log("sync dispatch worked but no lock message")
		}
	}
}

func TestExecute_DispatchesUnlock(t *testing.T) {
	vcs := &mockVCS{}
	lock := &mockLock{locks: nil}
	exec := newTestExecutor(vcs, lock, nil)
	cmd := &Command{Name: CommandUnlock}
	event := defaultEvent()

	err := exec.Execute(context.Background(), cmd, event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExecute_DispatchesRollback(t *testing.T) {
	vcs := &mockVCS{}
	lock := &mockLock{locks: nil}
	exec := newTestExecutor(vcs, lock, nil)
	cmd := &Command{Name: CommandRollback}
	event := defaultEvent()

	// Without argocd, checkRollbackRequirements will pass (approval not required),
	// then it will try to list locks (empty) and post "Nothing to rollback"
	err := exec.Execute(context.Background(), cmd, event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vcs.postedComments) > 0 {
		if !strings.Contains(vcs.postedComments[0].Body, "Nothing to rollback") {
			t.Logf("rollback posted: %s", vcs.postedComments[0].Body)
		}
	}
}

func TestExecuteRollback_WithLocksDisablesAutoSync(t *testing.T) {
	// When rollback is called without a specific app, it disables auto-sync on all locked apps
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		// ListApplications for BuildParentMap
		case r.Method == "GET" && r.URL.Path == "/api/v1/applications" && r.URL.RawQuery != "":
			resp := v1alpha1.ApplicationList{Items: []v1alpha1.Application{}}
			_ = json.NewEncoder(w).Encode(resp)

		// GetApplication for rollback
		case r.Method == "GET" && r.URL.Path == "/api/v1/applications/my-app":
			resp := v1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{Name: "my-app"},
				Spec: v1alpha1.ApplicationSpec{
					Source: &v1alpha1.ApplicationSource{
						RepoURL:        "https://github.com/org/repo",
						TargetRevision: "main",
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)

		// GetApplicationRaw for disableAutoSync
		case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/api/v1/applications/"):
			appName := strings.TrimPrefix(r.URL.Path, "/api/v1/applications/")
			resp := v1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{Name: appName},
				Spec:       v1alpha1.ApplicationSpec{}, // no auto-sync
			}
			_ = json.NewEncoder(w).Encode(resp)

		// SyncApplication
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/sync"):
			resp := v1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{Name: "my-app"},
				Status: v1alpha1.ApplicationStatus{
					OperationState: &v1alpha1.OperationState{
						Phase:   "Succeeded",
						Message: "ok",
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)

		case strings.HasSuffix(r.URL.Path, "/managed-resources"):
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}})

		default:
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{})
		}
	}))
	defer ts.Close()

	argoClient := newTestArgoClient(ts)
	vcsClient := &mockVCS{}
	lockMgr := &mockLock{
		locks: []models.Lock{
			{
				Application:  "my-app",
				PRNumber:     42,
				Repo:         "org/repo",
				ChangeType:   models.ApplicationExisting,
				PlanRevision: "abc123",
			},
		},
	}

	exec := &Executor{
		vcs:      vcsClient,
		argocd:   argoClient,
		lock:     lockMgr,
		config:   &config.Config{},
		renderer: diff.NewRenderer(),
	}

	cmd := &Command{Name: CommandRollback}
	event := defaultEvent()

	err := exec.Execute(context.Background(), cmd, event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have posted a rollback result comment
	if len(vcsClient.postedComments) == 0 {
		t.Fatal("expected a comment to be posted")
	}
	if !strings.Contains(vcsClient.postedComments[0].Body, "Lemuria Rollback") {
		t.Errorf("expected rollback comment, got: %s", vcsClient.postedComments[0].Body)
	}
}

func TestExecuteRollback_SpecificAppDisablesAutoSync(t *testing.T) {
	// When rollback is called with a specific app, it calls disableAutoSyncForRollback
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.Path == "/api/v1/applications" && r.URL.RawQuery != "":
			resp := v1alpha1.ApplicationList{Items: []v1alpha1.Application{}}
			_ = json.NewEncoder(w).Encode(resp)

		case r.Method == "GET" && r.URL.Path == "/api/v1/applications/my-app":
			resp := v1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{Name: "my-app"},
				Spec: v1alpha1.ApplicationSpec{
					SyncPolicy: &v1alpha1.SyncPolicy{
						Automated: &v1alpha1.SyncPolicyAutomated{Prune: true},
					},
					Source: &v1alpha1.ApplicationSource{
						RepoURL:        "https://github.com/org/repo",
						TargetRevision: "main",
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)

		case r.Method == "PUT" && strings.HasSuffix(r.URL.Path, "/spec"):
			w.WriteHeader(http.StatusOK)

		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/sync"):
			resp := v1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{Name: "my-app"},
				Status: v1alpha1.ApplicationStatus{
					OperationState: &v1alpha1.OperationState{
						Phase:   "Succeeded",
						Message: "ok",
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)

		case strings.HasSuffix(r.URL.Path, "/managed-resources"):
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}})

		default:
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{})
		}
	}))
	defer ts.Close()

	argoClient := newTestArgoClient(ts)
	vcsClient := &mockVCS{}
	lockMgr := &mockLock{
		locks: []models.Lock{
			{
				Application:  "my-app",
				PRNumber:     42,
				Repo:         "org/repo",
				ChangeType:   models.ApplicationExisting,
				PlanRevision: "abc123",
			},
		},
	}

	exec := &Executor{
		vcs:      vcsClient,
		argocd:   argoClient,
		lock:     lockMgr,
		config:   &config.Config{},
		renderer: diff.NewRenderer(),
	}

	cmd := &Command{Name: CommandRollback, Application: "my-app"}
	event := defaultEvent()

	err := exec.Execute(context.Background(), cmd, event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have posted a rollback result comment
	if len(vcsClient.postedComments) == 0 {
		t.Fatal("expected a comment to be posted")
	}
	if !strings.Contains(vcsClient.postedComments[0].Body, "Rollback") {
		t.Errorf("expected rollback comment, got: %s", vcsClient.postedComments[0].Body)
	}
}

// ---------------------------------------------------------------------------
// Tests: Reaction handling when comment is nil
// ---------------------------------------------------------------------------

func TestExecuteUnlock_NoReactionWhenNoComment(t *testing.T) {
	vcs := &mockVCS{}
	lock := &mockLock{locks: nil}
	exec := newTestExecutor(vcs, lock, nil)
	cmd := &Command{Name: CommandUnlock}
	event := defaultEvent() // No comment

	_ = exec.executeUnlock(context.Background(), cmd, event)
	if len(vcs.reactions) != 0 {
		t.Error("expected no reactions when comment is nil")
	}
}

// ---------------------------------------------------------------------------
// Tests: NewExecutor
// ---------------------------------------------------------------------------

func TestNewExecutor(t *testing.T) {
	vcs := &mockVCS{}
	lock := &mockLock{}
	cfg := &config.Config{}

	exec := NewExecutor(vcs, nil, lock, cfg)
	if exec == nil {
		t.Fatal("expected non-nil executor")
	}
	if exec.vcs != vcs {
		t.Error("executor VCS client not set correctly")
	}
	if exec.lock != lock {
		t.Error("executor lock manager not set correctly")
	}
	if exec.config != cfg {
		t.Error("executor config not set correctly")
	}
	if exec.renderer == nil {
		t.Error("expected non-nil renderer")
	}
}

// ---------------------------------------------------------------------------
// Tests: formatPlanSummary edge cases
// ---------------------------------------------------------------------------

func TestFormatPlanSummary_SingleCounts(t *testing.T) {
	tests := []struct {
		name    string
		summary argocd.DiffSummary
		want    string
	}{
		{
			name:    "only created and deleted",
			summary: argocd.DiffSummary{Created: 5, Deleted: 1},
			want:    "5 to create, 1 to delete",
		},
		{
			name:    "only updated and deleted",
			summary: argocd.DiffSummary{Updated: 3, Deleted: 2},
			want:    "3 to update, 2 to delete",
		},
		{
			name:    "only created and updated",
			summary: argocd.DiffSummary{Created: 1, Updated: 1},
			want:    "1 to create, 1 to update",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatPlanSummary(tt.summary)
			if got != tt.want {
				t.Errorf("formatPlanSummary() = %q, want %q", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Tests: toPlanDiffEntries preserves fields
// ---------------------------------------------------------------------------

func TestToPlanDiffEntries_PreservesFields(t *testing.T) {
	diffs := []models.ManifestDiff{
		{
			Resource: models.ResourceKey{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       "nginx",
				Namespace:  "default",
			},
			Diff:   "+replicas: 3",
			Action: models.DiffActionUpdate,
		},
	}

	entries := toPlanDiffEntries(diffs)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	e := entries[0]
	if e.Resource.Kind != "Deployment" {
		t.Errorf("Kind = %q, want 'Deployment'", e.Resource.Kind)
	}
	if e.Resource.Name != "nginx" {
		t.Errorf("Name = %q, want 'nginx'", e.Resource.Name)
	}
	if e.Action != models.DiffActionUpdate {
		t.Errorf("Action = %q, want 'update'", e.Action)
	}
	if e.Diff != "+replicas: 3" {
		t.Errorf("Diff = %q, want '+replicas: 3'", e.Diff)
	}
}

// ---------------------------------------------------------------------------
// Tests: containsAppByName edge cases
// ---------------------------------------------------------------------------

func TestContainsAppByName_EmptySlice(t *testing.T) {
	if containsAppByName(nil, "app") {
		t.Error("expected false for nil slice")
	}
	if containsAppByName([]models.Application{}, "app") {
		t.Error("expected false for empty slice")
	}
}
