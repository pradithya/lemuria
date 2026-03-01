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
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	v1alpha1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"

	"github.com/org/lemuria/internal/argocd"
	"github.com/org/lemuria/internal/config"
	"github.com/org/lemuria/internal/models"
	"github.com/org/lemuria/internal/vcs"
	"github.com/org/lemuria/pkg/diff"
)

// mockVCSForSync is a minimal vcs.Client mock for sync requirement tests.
type mockVCSForSync struct {
	repoConfigData []byte
	prApproved     bool
	prMergeable    bool
}

func (m *mockVCSForSync) GetChangedFiles(context.Context, string, string, int) ([]models.ChangedFile, error) {
	return nil, nil
}

func (m *mockVCSForSync) GetRepoConfig(context.Context, string, string, string) ([]byte, error) {
	if m.repoConfigData == nil {
		return nil, fmt.Errorf(".lemuria.yaml not found")
	}
	return m.repoConfigData, nil
}

func (m *mockVCSForSync) GetFileContents(context.Context, string, string, []string, string) (map[string][]byte, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockVCSForSync) GetPR(context.Context, string, string, int) (*models.PullRequestDetail, error) {
	return &models.PullRequestDetail{
		State:     models.PRStateOpen,
		Mergeable: m.prMergeable,
	}, nil
}

func (m *mockVCSForSync) IsPRApproved(context.Context, string, string, int) (bool, error) {
	return m.prApproved, nil
}

func (m *mockVCSForSync) PostComment(context.Context, string, string, int, string, bool) (*models.CommentResult, error) {
	return &models.CommentResult{ID: 1}, nil
}

func (m *mockVCSForSync) UpdateComment(context.Context, string, string, int, int64, string) error {
	return nil
}

func (m *mockVCSForSync) AddReaction(context.Context, string, string, int64, string) error {
	return nil
}

func (m *mockVCSForSync) InvalidatePlanComments(context.Context, string, string, int) error {
	return nil
}

func (m *mockVCSForSync) MergePullRequest(context.Context, string, string, int, string, string, string) error {
	return nil
}

func (m *mockVCSForSync) DeleteBranch(context.Context, string, string, string) error {
	return nil
}

func (m *mockVCSForSync) GetFilesByPattern(context.Context, string, string, string, []string, []string) (map[string][]byte, error) {
	return map[string][]byte{}, nil
}

func (m *mockVCSForSync) MaxCommentSize() int {
	return 0
}

// mockLockManagerForSync is a minimal lock.Manager mock for sync tests.
type mockLockManagerForSync struct{}

func (m *mockLockManagerForSync) Lock(context.Context, models.LockRequest) (*models.LockResult, error) {
	return nil, nil
}
func (m *mockLockManagerForSync) Unlock(context.Context, string, string, int) error { return nil }
func (m *mockLockManagerForSync) ForceUnlock(context.Context, string) error         { return nil }
func (m *mockLockManagerForSync) Get(context.Context, string) (*models.Lock, error) {
	return nil, nil
}
func (m *mockLockManagerForSync) ListByPR(context.Context, string, int) ([]models.Lock, error) {
	return nil, nil
}
func (m *mockLockManagerForSync) ListAll(context.Context) ([]models.Lock, error) { return nil, nil }
func (m *mockLockManagerForSync) StorePlan(context.Context, string, int, string, string, string, []models.PlanDiffEntry) error {
	return nil
}
func (m *mockLockManagerForSync) GetPlan(context.Context, string, int) (string, error) {
	return "", nil
}
func (m *mockLockManagerForSync) Ping(context.Context) error { return nil }
func (m *mockLockManagerForSync) Close() error               { return nil }
func (m *mockLockManagerForSync) UpdateLock(_ context.Context, _ *models.Lock) error {
	return nil
}
func (m *mockLockManagerForSync) StoreAppSetAutoSync(_ context.Context, _, _ string, _ int, _ []byte) error {
	return nil
}
func (m *mockLockManagerForSync) GetAppSetAutoSync(_ context.Context, _, _ string, _ int) ([]byte, error) {
	return nil, nil
}
func (m *mockLockManagerForSync) DeleteAppSetAutoSync(_ context.Context, _, _ string, _ int) error {
	return nil
}
func (m *mockLockManagerForSync) StoreParentAutoSync(_ context.Context, _, _ string, _ int, _ []byte) error {
	return nil
}
func (m *mockLockManagerForSync) ListParentAutoSync(_ context.Context, _ string, _ int) (map[string][]byte, error) {
	return nil, nil
}
func (m *mockLockManagerForSync) DeleteParentAutoSync(_ context.Context, _, _ string, _ int) error {
	return nil
}

func newSyncTestExecutor(vcsClient vcs.Client, cfg *config.Config) *Executor {
	return &Executor{
		vcs:    vcsClient,
		lock:   &mockLockManagerForSync{},
		config: cfg,
	}
}

func boolPtr(b bool) *bool {
	return &b
}

func TestAppRequiresApproval(t *testing.T) {
	tests := []struct {
		name          string
		serverRequire bool
		repoConfig    *config.RepoConfig
		appName       string
		want          bool
	}{
		// --- Server defaults only (no repo config) ---
		{
			name:          "server requires approval, no repo config",
			serverRequire: true,
			repoConfig:    nil,
			appName:       "my-app",
			want:          true,
		},
		{
			name:          "server does not require approval, no repo config",
			serverRequire: false,
			repoConfig:    nil,
			appName:       "my-app",
			want:          false,
		},

		// --- Repo config top-level override ---
		{
			name:          "server true, repo config overrides to false",
			serverRequire: true,
			repoConfig:    &config.RepoConfig{RequireApproval: boolPtr(false)},
			appName:       "my-app",
			want:          false,
		},
		{
			name:          "server false, repo config overrides to true",
			serverRequire: false,
			repoConfig:    &config.RepoConfig{RequireApproval: boolPtr(true)},
			appName:       "my-app",
			want:          true,
		},
		{
			name:          "server true, repo config nil (not set) keeps server default",
			serverRequire: true,
			repoConfig:    &config.RepoConfig{},
			appName:       "my-app",
			want:          true,
		},

		// --- sync_requirements wildcard override ---
		{
			name:          "repo true, sync_requirements wildcard overrides to false",
			serverRequire: true,
			repoConfig: &config.RepoConfig{
				RequireApproval: boolPtr(true),
				SyncRequirements: []config.SyncRequirement{
					{Name: "*", RequireApproval: false},
				},
			},
			appName: "my-app",
			want:    false,
		},
		{
			name:          "repo false, sync_requirements wildcard overrides to true",
			serverRequire: false,
			repoConfig: &config.RepoConfig{
				RequireApproval: boolPtr(false),
				SyncRequirements: []config.SyncRequirement{
					{Name: "*", RequireApproval: true},
				},
			},
			appName: "my-app",
			want:    true,
		},

		// --- Exact match takes priority over wildcard ---
		{
			name:          "exact match true overrides wildcard false",
			serverRequire: false,
			repoConfig: &config.RepoConfig{
				SyncRequirements: []config.SyncRequirement{
					{Name: "*", RequireApproval: false},
					{Name: "prod-app", RequireApproval: true},
				},
			},
			appName: "prod-app",
			want:    true,
		},
		{
			name:          "exact match false overrides wildcard true",
			serverRequire: true,
			repoConfig: &config.RepoConfig{
				SyncRequirements: []config.SyncRequirement{
					{Name: "*", RequireApproval: true},
					{Name: "dev-app", RequireApproval: false},
				},
			},
			appName: "dev-app",
			want:    false,
		},
		{
			name:          "non-matching app uses wildcard, not exact for different app",
			serverRequire: false,
			repoConfig: &config.RepoConfig{
				SyncRequirements: []config.SyncRequirement{
					{Name: "*", RequireApproval: false},
					{Name: "prod-app", RequireApproval: true},
				},
			},
			appName: "staging-app",
			want:    false,
		},

		// --- Prefix wildcard matching ---
		{
			name:          "prefix wildcard matches app",
			serverRequire: false,
			repoConfig: &config.RepoConfig{
				SyncRequirements: []config.SyncRequirement{
					{Name: "prod-*", RequireApproval: true},
				},
			},
			appName: "prod-api",
			want:    true,
		},
		{
			name:          "prefix wildcard does not match, falls to server default",
			serverRequire: false,
			repoConfig: &config.RepoConfig{
				SyncRequirements: []config.SyncRequirement{
					{Name: "prod-*", RequireApproval: true},
				},
			},
			appName: "dev-api",
			want:    false,
		},

		// --- No matching sync requirement falls back to repo top-level ---
		{
			name:          "no matching sync_requirement, uses repo top-level true",
			serverRequire: false,
			repoConfig: &config.RepoConfig{
				RequireApproval: boolPtr(true),
				SyncRequirements: []config.SyncRequirement{
					{Name: "other-app", RequireApproval: false},
				},
			},
			appName: "my-app",
			want:    true,
		},
		{
			name:          "no matching sync_requirement, uses repo top-level false",
			serverRequire: true,
			repoConfig: &config.RepoConfig{
				RequireApproval: boolPtr(false),
				SyncRequirements: []config.SyncRequirement{
					{Name: "other-app", RequireApproval: true},
				},
			},
			appName: "my-app",
			want:    false,
		},

		// --- Empty sync_requirements uses repo top-level ---
		{
			name:          "empty sync_requirements, uses repo top-level",
			serverRequire: true,
			repoConfig: &config.RepoConfig{
				RequireApproval:  boolPtr(false),
				SyncRequirements: []config.SyncRequirement{},
			},
			appName: "my-app",
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Defaults: config.DefaultsConfig{
					RequireApproval: tt.serverRequire,
				},
			}
			exec := newSyncTestExecutor(nil, cfg)

			got := exec.appRequiresApproval(tt.repoConfig, tt.appName)
			if got != tt.want {
				t.Errorf("appRequiresApproval() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsApprovalRequired(t *testing.T) {
	tests := []struct {
		name          string
		serverRequire bool
		repoConfig    *config.RepoConfig
		locks         []models.Lock
		want          bool
	}{
		{
			name:          "empty locks never requires approval",
			serverRequire: true,
			locks:         nil,
			want:          false,
		},
		{
			name:          "all apps exempt via wildcard",
			serverRequire: true,
			repoConfig: &config.RepoConfig{
				RequireApproval: boolPtr(true),
				SyncRequirements: []config.SyncRequirement{
					{Name: "*", RequireApproval: false},
				},
			},
			locks: []models.Lock{
				{Application: "app-a"},
				{Application: "app-b"},
			},
			want: false,
		},
		{
			name:          "one of multiple apps requires approval",
			serverRequire: false,
			repoConfig: &config.RepoConfig{
				SyncRequirements: []config.SyncRequirement{
					{Name: "prod-app", RequireApproval: true},
				},
			},
			locks: []models.Lock{
				{Application: "dev-app"},
				{Application: "prod-app"},
			},
			want: true,
		},
		{
			name:          "no app requires approval with server default false",
			serverRequire: false,
			locks: []models.Lock{
				{Application: "dev-app"},
			},
			want: false,
		},
		{
			name:          "all apps require approval with server default true",
			serverRequire: true,
			locks: []models.Lock{
				{Application: "app-a"},
				{Application: "app-b"},
			},
			want: true,
		},
		{
			name:          "mixed: exact match exempts one app but wildcard requires others",
			serverRequire: false,
			repoConfig: &config.RepoConfig{
				SyncRequirements: []config.SyncRequirement{
					{Name: "*", RequireApproval: true},
					{Name: "dev-app", RequireApproval: false},
				},
			},
			locks: []models.Lock{
				{Application: "dev-app"},
				{Application: "prod-app"},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Defaults: config.DefaultsConfig{
					RequireApproval: tt.serverRequire,
				},
			}
			exec := newSyncTestExecutor(nil, cfg)

			got := exec.isApprovalRequired(tt.repoConfig, tt.locks)
			if got != tt.want {
				t.Errorf("isApprovalRequired() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAppSourcesFromRepo(t *testing.T) {
	tests := []struct {
		name    string
		app     models.Application
		repoURL string
		want    bool
	}{
		{
			name: "single source matches PR repo",
			app: models.Application{
				Name:    "my-app",
				RepoURL: "https://github.com/org/repo",
			},
			repoURL: "https://github.com/org/repo",
			want:    true,
		},
		{
			name: "single source matches PR repo with .git suffix",
			app: models.Application{
				Name:    "my-app",
				RepoURL: "https://github.com/org/repo.git",
			},
			repoURL: "https://github.com/org/repo",
			want:    true,
		},
		{
			name: "single source external Helm chart",
			app: models.Application{
				Name:    "helm-app",
				RepoURL: "https://argoproj.github.io/argo-helm",
			},
			repoURL: "https://github.com/org/repo",
			want:    false,
		},
		{
			name: "multi-source with one matching PR repo",
			app: models.Application{
				Name: "multi-app",
				Sources: []models.ApplicationSource{
					{RepoURL: "https://github.com/org/repo", Path: "manifests"},
					{RepoURL: "https://argoproj.github.io/argo-helm", Chart: "argo-cd"},
				},
			},
			repoURL: "https://github.com/org/repo",
			want:    true,
		},
		{
			name: "multi-source with no matching PR repo",
			app: models.Application{
				Name: "external-multi",
				Sources: []models.ApplicationSource{
					{RepoURL: "https://charts.bitnami.com/bitnami", Chart: "redis"},
					{RepoURL: "https://argoproj.github.io/argo-helm", Chart: "argo-cd"},
				},
			},
			repoURL: "https://github.com/org/repo",
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := appSourcesFromRepo(tt.app, tt.repoURL)
			if got != tt.want {
				t.Errorf("appSourcesFromRepo() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCheckSyncRequirements(t *testing.T) {
	tests := []struct {
		name           string
		serverRequire  bool
		repoConfigYAML string
		prApproved     bool
		prMergeable    bool
		locks          []models.Lock
		wantErr        string
	}{
		{
			name:          "server requires, no repo config, PR not approved",
			serverRequire: true,
			prApproved:    false,
			prMergeable:   true,
			locks:         []models.Lock{{Application: "my-app"}},
			wantErr:       "PR must be approved before sync",
		},
		{
			name:          "server requires, no repo config, PR approved and mergeable",
			serverRequire: true,
			prApproved:    true,
			prMergeable:   true,
			locks:         []models.Lock{{Application: "my-app"}},
			wantErr:       "",
		},
		{
			name:           "repo sync_requirements wildcard overrides server, PR not approved passes",
			serverRequire:  true,
			repoConfigYAML: "version: 1\nrequire_approval: true\nsync_requirements:\n  - name: \"*\"\n    require_approval: false\n",
			prApproved:     false,
			prMergeable:    true,
			locks:          []models.Lock{{Application: "my-app"}},
			wantErr:        "",
		},
		{
			name:           "repo sync_requirements requires approval for specific app",
			serverRequire:  false,
			repoConfigYAML: "version: 1\nsync_requirements:\n  - name: \"prod-*\"\n    require_approval: true\n",
			prApproved:     false,
			prMergeable:    true,
			locks:          []models.Lock{{Application: "prod-api"}},
			wantErr:        "PR must be approved before sync",
		},
		{
			name:           "repo sync_requirements does not match app, server allows",
			serverRequire:  false,
			repoConfigYAML: "version: 1\nsync_requirements:\n  - name: \"prod-*\"\n    require_approval: true\n",
			prApproved:     false,
			prMergeable:    true,
			locks:          []models.Lock{{Application: "dev-api"}},
			wantErr:        "",
		},
		{
			name:        "PR not mergeable",
			prApproved:  true,
			prMergeable: false,
			locks:       []models.Lock{{Application: "my-app"}},
			wantErr:     "PR has merge conflicts",
		},
		{
			name:           "multiple apps, one requires approval, PR approved",
			serverRequire:  false,
			repoConfigYAML: "version: 1\nsync_requirements:\n  - name: \"prod-app\"\n    require_approval: true\n",
			prApproved:     true,
			prMergeable:    true,
			locks: []models.Lock{
				{Application: "dev-app"},
				{Application: "prod-app"},
			},
			wantErr: "",
		},
		{
			name:           "multiple apps, one requires approval, PR not approved",
			serverRequire:  false,
			repoConfigYAML: "version: 1\nsync_requirements:\n  - name: \"prod-app\"\n    require_approval: true\n",
			prApproved:     false,
			prMergeable:    true,
			locks: []models.Lock{
				{Application: "dev-app"},
				{Application: "prod-app"},
			},
			wantErr: "PR must be approved before sync",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var repoConfigData []byte
			if tt.repoConfigYAML != "" {
				repoConfigData = []byte(tt.repoConfigYAML)
			}

			mock := &mockVCSForSync{
				repoConfigData: repoConfigData,
				prApproved:     tt.prApproved,
				prMergeable:    tt.prMergeable,
			}

			cfg := &config.Config{
				Defaults: config.DefaultsConfig{
					RequireApproval: tt.serverRequire,
				},
			}

			exec := newSyncTestExecutor(mock, cfg)
			event := &models.PREvent{
				Repo: models.RepoInfo{
					Owner:    "org",
					Name:     "repo",
					FullName: "org/repo",
				},
				PR: models.PRInfo{
					Number:  1,
					HeadRef: "feature-branch",
				},
			}

			err := exec.checkSyncRequirements(context.Background(), event, tt.locks)

			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("checkSyncRequirements() unexpected error: %v", err)
				}
			} else {
				if err == nil {
					t.Errorf("checkSyncRequirements() expected error containing %q, got nil", tt.wantErr)
				} else if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("checkSyncRequirements() error = %q, want containing %q", err.Error(), tt.wantErr)
				}
			}
		})
	}
}

// mockLockManagerForSyncTracking tracks unlock calls for testing.
type mockLockManagerForSyncTracking struct {
	mockLockManagerForSync
	unlocked []string
}

func (m *mockLockManagerForSyncTracking) Unlock(_ context.Context, application, _ string, _ int) error {
	m.unlocked = append(m.unlocked, application)
	return nil
}

func TestSyncDeletedApplication(t *testing.T) {
	lockMgr := &mockLockManagerForSyncTracking{}
	exec := &Executor{
		lock:   lockMgr,
		config: &config.Config{},
	}

	lock := models.Lock{
		Application: "deleted-app",
		PRNumber:    1,
		Repo:        "org/repo",
		ChangeType:  models.ApplicationDeleted,
		PlanOutput:  "test plan output",
	}
	cmd := &Command{Name: CommandSync}
	event := &models.PREvent{
		Repo: models.RepoInfo{
			Owner:    "org",
			Name:     "repo",
			FullName: "org/repo",
		},
		PR: models.PRInfo{
			Number:  1,
			HeadSHA: "abc123",
		},
	}

	result := exec.syncDeletedApplication(context.Background(), lock, cmd, event)

	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if result.Result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Result.Phase != models.SyncPhaseSucceeded {
		t.Errorf("phase = %v, want %v", result.Result.Phase, models.SyncPhaseSucceeded)
	}
	if result.Application != "deleted-app" {
		t.Errorf("application = %q, want %q", result.Application, "deleted-app")
	}
	if result.PlanOutput != "test plan output" {
		t.Errorf("plan output = %q, want %q", result.PlanOutput, "test plan output")
	}
	if len(lockMgr.unlocked) != 0 {
		t.Errorf("expected no unlock (deferred to auto-merge), got %v", lockMgr.unlocked)
	}
}

func TestSyncDeletedApplicationDryRun(t *testing.T) {
	lockMgr := &mockLockManagerForSyncTracking{}
	exec := &Executor{
		lock:   lockMgr,
		config: &config.Config{},
	}

	lock := models.Lock{
		Application: "deleted-app",
		PRNumber:    1,
		Repo:        "org/repo",
		ChangeType:  models.ApplicationDeleted,
	}
	cmd := &Command{Name: CommandSync, DryRun: true}
	event := &models.PREvent{
		Repo: models.RepoInfo{FullName: "org/repo"},
		PR:   models.PRInfo{Number: 1},
	}

	result := exec.syncDeletedApplication(context.Background(), lock, cmd, event)

	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if len(lockMgr.unlocked) != 0 {
		t.Errorf("expected no unlock in dry-run, got %v", lockMgr.unlocked)
	}
}

func TestSyncNewApplicationNoSourceFile(t *testing.T) {
	exec := &Executor{
		config: &config.Config{},
	}

	lock := models.Lock{
		Application: "new-app",
		ChangeType:  models.ApplicationNew,
		// SourceFile intentionally empty
	}
	cmd := &Command{Name: CommandSync}
	event := &models.PREvent{
		PR: models.PRInfo{HeadSHA: "abc123"},
	}

	result := exec.syncNewApplication(context.Background(), lock, cmd, event, map[string][]byte{})

	if result.Error == nil {
		t.Fatal("expected error for missing source file")
	}
	if !strings.Contains(result.Error.Error(), "no source file") {
		t.Errorf("error = %q, want containing 'no source file'", result.Error.Error())
	}
}

func TestSyncNewApplicationMissingContent(t *testing.T) {
	exec := &Executor{
		config: &config.Config{},
	}

	lock := models.Lock{
		Application: "new-app",
		ChangeType:  models.ApplicationNew,
		SourceFile:  "apps/new-app.yaml",
	}
	cmd := &Command{Name: CommandSync}
	event := &models.PREvent{
		PR: models.PRInfo{HeadSHA: "abc123"},
	}

	result := exec.syncNewApplication(context.Background(), lock, cmd, event, map[string][]byte{})

	if result.Error == nil {
		t.Fatal("expected error for missing content")
	}
	if !strings.Contains(result.Error.Error(), "not found in pre-fetched") {
		t.Errorf("error = %q, want containing 'not found in pre-fetched'", result.Error.Error())
	}
}

func TestRewriteTargetRevision(t *testing.T) {
	tests := []struct {
		name        string
		app         *v1alpha1.Application
		repoURL     string
		revision    string
		wantSource  string   // expected targetRevision on Source
		wantSources []string // expected targetRevisions on Sources
	}{
		{
			name: "single source matching PR repo",
			app: &v1alpha1.Application{
				Spec: v1alpha1.ApplicationSpec{
					Source: &v1alpha1.ApplicationSource{
						RepoURL:        "https://github.com/org/repo",
						TargetRevision: "main",
					},
				},
			},
			repoURL:    "https://github.com/org/repo",
			revision:   "abc123",
			wantSource: "abc123",
		},
		{
			name: "single source not matching PR repo",
			app: &v1alpha1.Application{
				Spec: v1alpha1.ApplicationSpec{
					Source: &v1alpha1.ApplicationSource{
						RepoURL:        "https://charts.helm.sh/stable",
						TargetRevision: "1.0.0",
					},
				},
			},
			repoURL:    "https://github.com/org/repo",
			revision:   "abc123",
			wantSource: "1.0.0",
		},
		{
			name: "multi-source all matching PR repo",
			app: &v1alpha1.Application{
				Spec: v1alpha1.ApplicationSpec{
					Sources: v1alpha1.ApplicationSources{
						{RepoURL: "https://github.com/org/repo", TargetRevision: "main"},
						{RepoURL: "https://github.com/org/repo.git", TargetRevision: "main"},
					},
				},
			},
			repoURL:     "https://github.com/org/repo",
			revision:    "abc123",
			wantSources: []string{"abc123", "abc123"},
		},
		{
			name: "multi-source mixed — PR repo and external helm",
			app: &v1alpha1.Application{
				Spec: v1alpha1.ApplicationSpec{
					Sources: v1alpha1.ApplicationSources{
						{RepoURL: "https://github.com/org/repo", TargetRevision: "main"},
						{RepoURL: "https://argoproj.github.io/argo-helm", TargetRevision: "5.46.0"},
					},
				},
			},
			repoURL:     "https://github.com/org/repo",
			revision:    "abc123",
			wantSources: []string{"abc123", "5.46.0"},
		},
		{
			name: "nil source with multi-source",
			app: &v1alpha1.Application{
				Spec: v1alpha1.ApplicationSpec{
					Sources: v1alpha1.ApplicationSources{
						{RepoURL: "https://github.com/org/repo", TargetRevision: "main"},
					},
				},
			},
			repoURL:     "https://github.com/org/repo",
			revision:    "abc123",
			wantSources: []string{"abc123"},
		},
		{
			name: "repo URL with .git suffix matches",
			app: &v1alpha1.Application{
				Spec: v1alpha1.ApplicationSpec{
					Source: &v1alpha1.ApplicationSource{
						RepoURL:        "https://github.com/org/repo.git",
						TargetRevision: "main",
					},
				},
			},
			repoURL:    "https://github.com/org/repo",
			revision:   "abc123",
			wantSource: "abc123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rewriteTargetRevision(tt.app, tt.repoURL, tt.revision)

			if tt.app.Spec.Source != nil {
				if tt.app.Spec.Source.TargetRevision != tt.wantSource {
					t.Errorf("Source.TargetRevision = %q, want %q", tt.app.Spec.Source.TargetRevision, tt.wantSource)
				}
			}
			if len(tt.wantSources) > 0 {
				if len(tt.app.Spec.Sources) != len(tt.wantSources) {
					t.Fatalf("Sources count = %d, want %d", len(tt.app.Spec.Sources), len(tt.wantSources))
				}
				for i, want := range tt.wantSources {
					if tt.app.Spec.Sources[i].TargetRevision != want {
						t.Errorf("Sources[%d].TargetRevision = %q, want %q", i, tt.app.Spec.Sources[i].TargetRevision, want)
					}
				}
			}
		})
	}
}

// fakeArgoServer creates an httptest server that mocks the ArgoCD API
// for syncApplication tests. It tracks calls to key endpoints.
type fakeArgoServer struct {
	server *httptest.Server

	// Application returned by GET /api/v1/applications/{name}
	app v1alpha1.Application

	// Tracking
	specUpdateCount atomic.Int32
	specUpdateBody  atomic.Value // stores last v1alpha1.ApplicationSpec as JSON
	syncCallCount   atomic.Int32
	syncRequestBody atomic.Value // stores last sync request body as map
}

func newFakeArgoServer(app v1alpha1.Application) *fakeArgoServer {
	f := &fakeArgoServer{app: app}
	mux := http.NewServeMux()

	// GET /api/v1/applications/{name} - GetApplication / GetApplicationRaw / GetResourceHealth
	mux.HandleFunc("GET /api/v1/applications/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(f.app) //nolint:errcheck
	})

	// PUT /api/v1/applications/{name}/spec - UpdateApplicationSpec
	mux.HandleFunc("PUT /api/v1/applications/", func(w http.ResponseWriter, r *http.Request) {
		f.specUpdateCount.Add(1)
		body, _ := io.ReadAll(r.Body)
		f.specUpdateBody.Store(string(body))
		w.Header().Set("Content-Type", "application/json")
		w.Write(body) //nolint:errcheck
	})

	// POST /api/v1/applications/{name}/sync - SyncApplication
	mux.HandleFunc("POST /api/v1/applications/", func(w http.ResponseWriter, r *http.Request) {
		f.syncCallCount.Add(1)
		body, _ := io.ReadAll(r.Body)
		f.syncRequestBody.Store(string(body))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(f.app) //nolint:errcheck
	})

	// GET /api/v1/stream/applications - Watch (for waitForSyncComplete)
	// Returns a Running event (to clear the stale event filter from pre-opened
	// watch), then a Succeeded + Healthy event, then closes.
	mux.HandleFunc("GET /api/v1/stream/applications", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		encoder := json.NewEncoder(w)
		// 1. Running event — indicates the new sync has started.
		encoder.Encode(map[string]any{ //nolint:errcheck
			"result": map[string]any{
				"type": "MODIFIED",
				"application": map[string]any{
					"status": map[string]any{
						"health": map[string]any{"status": "Progressing"},
						"operationState": map[string]any{
							"phase":   "Running",
							"message": "syncing",
						},
					},
				},
			},
		})
		// 2. Succeeded + Healthy event — sync completed.
		encoder.Encode(map[string]any{ //nolint:errcheck
			"result": map[string]any{
				"type": "MODIFIED",
				"application": map[string]any{
					"status": map[string]any{
						"health": map[string]any{"status": "Healthy"},
						"operationState": map[string]any{
							"phase":   "Succeeded",
							"message": "sync completed",
							"syncResult": map[string]any{
								"revision":  "abc123",
								"resources": []any{},
							},
						},
					},
				},
			},
		})
	})

	f.server = httptest.NewServer(mux)
	return f
}

func (f *fakeArgoServer) close() {
	f.server.Close()
}

func (f *fakeArgoServer) client() *argocd.Client {
	client, _ := argocd.NewClient(config.ArgoCDConfig{
		ServerURL: f.server.URL,
		Token:     "test-token",
		Insecure:  true,
	})
	return client
}

func (f *fakeArgoServer) lastSyncRequest() map[string]any {
	raw, ok := f.syncRequestBody.Load().(string)
	if !ok || raw == "" {
		return nil
	}
	var m map[string]any
	json.Unmarshal([]byte(raw), &m) //nolint:errcheck
	return m
}

func (f *fakeArgoServer) lastSpecUpdate() *v1alpha1.ApplicationSpec {
	raw, ok := f.specUpdateBody.Load().(string)
	if !ok || raw == "" {
		return nil
	}
	var spec v1alpha1.ApplicationSpec
	json.Unmarshal([]byte(raw), &spec) //nolint:errcheck
	return &spec
}

func TestSyncApplicationMultiSource(t *testing.T) {
	tests := []struct {
		name                string
		app                 v1alpha1.Application
		dryRun              bool
		wantSpecUpdate      bool
		wantTargetRevisions []string // expected targetRevisions in spec update (nil if no update expected)
		wantSyncRevision    bool     // true if sync request should have "revision" field
		wantTargetRewritten bool
	}{
		{
			name: "multi-source app rewrites targetRevision and updates spec",
			app: v1alpha1.Application{
				Spec: v1alpha1.ApplicationSpec{
					Sources: v1alpha1.ApplicationSources{
						{RepoURL: "https://github.com/org/repo", Path: "manifests", TargetRevision: "main"},
						{RepoURL: "https://argoproj.github.io/argo-helm", Chart: "argo-cd", TargetRevision: "5.51.0"},
					},
				},
			},
			wantSpecUpdate:      true,
			wantTargetRevisions: []string{"abc123", "5.51.0"},
			wantSyncRevision:    false, // no revision override for multi-source
			wantTargetRewritten: true,
		},
		{
			name: "multi-source app dry-run does not update spec",
			app: v1alpha1.Application{
				Spec: v1alpha1.ApplicationSpec{
					Sources: v1alpha1.ApplicationSources{
						{RepoURL: "https://github.com/org/repo", Path: "manifests", TargetRevision: "main"},
						{RepoURL: "https://argoproj.github.io/argo-helm", Chart: "argo-cd", TargetRevision: "5.51.0"},
					},
				},
			},
			dryRun:              true,
			wantSpecUpdate:      false,
			wantSyncRevision:    false,
			wantTargetRewritten: false,
		},
		{
			name: "single-source app uses revision override",
			app: v1alpha1.Application{
				Spec: v1alpha1.ApplicationSpec{
					Source: &v1alpha1.ApplicationSource{
						RepoURL:        "https://github.com/org/repo",
						Path:           "manifests",
						TargetRevision: "main",
					},
				},
			},
			wantSpecUpdate:      false,
			wantSyncRevision:    true, // single-source uses opts.Revision
			wantTargetRewritten: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := newFakeArgoServer(tt.app)
			defer fake.close()

			exec := &Executor{
				vcs:      &mockVCSForSync{prMergeable: true},
				argocd:   fake.client(),
				lock:     &mockLockManagerForSync{},
				config:   &config.Config{ArgoCD: config.ArgoCDConfig{SyncTimeout: 30 * time.Second}},
				renderer: diff.NewRenderer(),
			}

			lock := models.Lock{
				Application:  "test-app",
				PlanRevision: "abc123",
			}
			cmd := &Command{Name: CommandSync, DryRun: tt.dryRun}
			event := &models.PREvent{
				Repo: models.RepoInfo{
					Owner:    "org",
					Name:     "repo",
					FullName: "org/repo",
					HTMLURL:  "https://github.com/org/repo",
				},
				PR: models.PRInfo{
					Number:  1,
					HeadSHA: "abc123",
					HeadRef: "feature-branch",
				},
			}

			result := exec.syncApplication(context.Background(), lock, cmd, event, map[string][]byte{})

			if result.Error != nil {
				t.Fatalf("unexpected error: %v", result.Error)
			}

			// Check spec update calls
			specUpdates := int(fake.specUpdateCount.Load())
			if tt.wantSpecUpdate && specUpdates == 0 {
				t.Error("expected UpdateApplicationSpec to be called, but it was not")
			}
			if !tt.wantSpecUpdate && specUpdates > 0 {
				t.Errorf("expected no UpdateApplicationSpec call, got %d", specUpdates)
			}

			// Check targetRevisions in spec update
			if tt.wantTargetRevisions != nil {
				spec := fake.lastSpecUpdate()
				if spec == nil {
					t.Fatal("expected spec update body, got nil")
				}
				if len(spec.Sources) != len(tt.wantTargetRevisions) {
					t.Fatalf("spec sources count = %d, want %d", len(spec.Sources), len(tt.wantTargetRevisions))
				}
				for i, want := range tt.wantTargetRevisions {
					if spec.Sources[i].TargetRevision != want {
						t.Errorf("spec.Sources[%d].TargetRevision = %q, want %q", i, spec.Sources[i].TargetRevision, want)
					}
				}
			}

			// Check sync request
			syncReq := fake.lastSyncRequest()
			if syncReq != nil {
				_, hasRevision := syncReq["revision"]
				if tt.wantSyncRevision && !hasRevision {
					t.Error("expected sync request to have 'revision' field, but it was missing")
				}
				if !tt.wantSyncRevision && hasRevision {
					t.Errorf("expected sync request without 'revision' field, got %v", syncReq["revision"])
				}
				_, hasRevisions := syncReq["revisions"]
				if hasRevisions {
					t.Error("expected sync request without 'revisions' field (multi-source override), but it was present")
				}
			}

			// Check TargetRevRewritten flag
			if result.TargetRevRewritten != tt.wantTargetRewritten {
				t.Errorf("TargetRevRewritten = %v, want %v", result.TargetRevRewritten, tt.wantTargetRewritten)
			}
		})
	}
}

func TestExecuteSyncParallel(t *testing.T) {
	// Prove that sync executes concurrently: each fake sync blocks until all
	// 3 goroutines have started. If execution were sequential, the test would
	// deadlock (barrier never reached).
	const numApps = 3
	var started sync.WaitGroup
	started.Add(numApps)
	barrier := make(chan struct{})

	app := v1alpha1.Application{
		Spec: v1alpha1.ApplicationSpec{
			Source: &v1alpha1.ApplicationSource{
				RepoURL:        "https://github.com/org/repo",
				Path:           "manifests",
				TargetRevision: "main",
			},
		},
	}

	// Create a blocking fake server: sync endpoint waits for all goroutines
	fake := newFakeArgoServer(app)
	defer fake.close()

	origHandler := fake.server.Config.Handler
	blockingMux := http.NewServeMux()
	blockingMux.HandleFunc("POST /api/v1/applications/", func(w http.ResponseWriter, r *http.Request) {
		started.Done() // signal this goroutine has started
		<-barrier      // wait for all to start
		origHandler.ServeHTTP(w, r)
	})
	// Proxy all other requests to original handler
	blockingMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		origHandler.ServeHTTP(w, r)
	})
	fake.server.Config.Handler = blockingMux

	lockMgr := &mockLockManagerForSyncWithList{
		locks: []models.Lock{
			{Application: "app-1", PlanRevision: "abc123"},
			{Application: "app-2", PlanRevision: "abc123"},
			{Application: "app-3", PlanRevision: "abc123"},
		},
	}

	mock := &mockVCSForSync{prMergeable: true, prApproved: true}
	exec := &Executor{
		vcs:      mock,
		argocd:   fake.client(),
		lock:     lockMgr,
		config:   &config.Config{ArgoCD: config.ArgoCDConfig{SyncTimeout: 30 * time.Second}},
		renderer: diff.NewRenderer(),
	}

	event := &models.PREvent{
		Repo: models.RepoInfo{
			Owner:    "org",
			Name:     "repo",
			FullName: "org/repo",
			HTMLURL:  "https://github.com/org/repo",
		},
		PR: models.PRInfo{
			Number:  1,
			HeadSHA: "abc123",
			HeadRef: "feature-branch",
		},
		Comment: &models.Comment{ID: 100},
	}

	// Release the barrier once all goroutines have started
	go func() {
		started.Wait()
		close(barrier)
	}()

	err := exec.executeSync(context.Background(), &Command{Name: CommandSync}, event)
	if err != nil {
		t.Fatalf("executeSync() unexpected error: %v", err)
	}

	// All 3 applications should have been synced
	syncCalls := int(fake.syncCallCount.Load())
	if syncCalls != 3 {
		t.Errorf("expected 3 sync calls, got %d", syncCalls)
	}
}

func TestExecuteSyncSkipNoChanges(t *testing.T) {
	app := v1alpha1.Application{
		Spec: v1alpha1.ApplicationSpec{
			Source: &v1alpha1.ApplicationSource{
				RepoURL:        "https://github.com/org/repo",
				Path:           "manifests",
				TargetRevision: "main",
			},
		},
	}
	fake := newFakeArgoServer(app)
	defer fake.close()

	lockMgr := &mockLockManagerForSyncWithList{
		locks: []models.Lock{
			{Application: "app-changed", PlanRevision: "abc123", PlanOutput: "some diff output", PlanDiffs: []models.PlanDiffEntry{{Diff: "some diff"}}},
			{Application: "app-no-changes", PlanRevision: "abc123", PlanOutput: "No changes detected"},
		},
	}

	mock := &mockVCSForSync{prMergeable: true, prApproved: true}
	exec := &Executor{
		vcs:    mock,
		argocd: fake.client(),
		lock:   lockMgr,
		config: &config.Config{
			ArgoCD:   config.ArgoCDConfig{SyncTimeout: 30 * time.Second},
			Defaults: config.DefaultsConfig{SkipNoChanges: true},
		},
		renderer: diff.NewRenderer(),
	}

	event := &models.PREvent{
		Repo: models.RepoInfo{
			Owner:    "org",
			Name:     "repo",
			FullName: "org/repo",
			HTMLURL:  "https://github.com/org/repo",
		},
		PR: models.PRInfo{
			Number:  1,
			HeadSHA: "abc123",
			HeadRef: "feature-branch",
		},
		Comment: &models.Comment{ID: 100},
	}

	err := exec.executeSync(context.Background(), &Command{Name: CommandSync}, event)
	if err != nil {
		t.Fatalf("executeSync() unexpected error: %v", err)
	}

	// Only app-changed should have been synced, app-no-changes should be skipped
	syncCalls := int(fake.syncCallCount.Load())
	if syncCalls != 1 {
		t.Errorf("expected 1 sync call (skipping no-changes app), got %d", syncCalls)
	}
}

func TestExecuteSyncNoSkipWhenDisabled(t *testing.T) {
	app := v1alpha1.Application{
		Spec: v1alpha1.ApplicationSpec{
			Source: &v1alpha1.ApplicationSource{
				RepoURL:        "https://github.com/org/repo",
				Path:           "manifests",
				TargetRevision: "main",
			},
		},
	}
	fake := newFakeArgoServer(app)
	defer fake.close()

	lockMgr := &mockLockManagerForSyncWithList{
		locks: []models.Lock{
			{Application: "app-changed", PlanRevision: "abc123", PlanOutput: "some diff"},
			{Application: "app-no-changes", PlanRevision: "abc123", PlanOutput: "No changes detected"},
		},
	}

	mock := &mockVCSForSync{prMergeable: true, prApproved: true}
	exec := &Executor{
		vcs:    mock,
		argocd: fake.client(),
		lock:   lockMgr,
		config: &config.Config{
			ArgoCD:   config.ArgoCDConfig{SyncTimeout: 30 * time.Second},
			Defaults: config.DefaultsConfig{SkipNoChanges: false},
		},
		renderer: diff.NewRenderer(),
	}

	event := &models.PREvent{
		Repo: models.RepoInfo{
			Owner:    "org",
			Name:     "repo",
			FullName: "org/repo",
			HTMLURL:  "https://github.com/org/repo",
		},
		PR: models.PRInfo{
			Number:  1,
			HeadSHA: "abc123",
			HeadRef: "feature-branch",
		},
		Comment: &models.Comment{ID: 100},
	}

	err := exec.executeSync(context.Background(), &Command{Name: CommandSync}, event)
	if err != nil {
		t.Fatalf("executeSync() unexpected error: %v", err)
	}

	// Both apps should be synced when skip_no_changes is disabled
	syncCalls := int(fake.syncCallCount.Load())
	if syncCalls != 2 {
		t.Errorf("expected 2 sync calls, got %d", syncCalls)
	}
}

func TestExecuteSyncSkipNoChangesRepoConfigOverride(t *testing.T) {
	app := v1alpha1.Application{
		Spec: v1alpha1.ApplicationSpec{
			Source: &v1alpha1.ApplicationSource{
				RepoURL:        "https://github.com/org/repo",
				Path:           "manifests",
				TargetRevision: "main",
			},
		},
	}
	fake := newFakeArgoServer(app)
	defer fake.close()

	lockMgr := &mockLockManagerForSyncWithList{
		locks: []models.Lock{
			{Application: "app-changed", PlanRevision: "abc123", PlanOutput: "some diff", PlanDiffs: []models.PlanDiffEntry{{Diff: "some diff"}}},
			{Application: "app-no-changes", PlanRevision: "abc123", PlanOutput: "No changes detected"},
		},
	}

	// Server default: skip_no_changes=false, but repo config overrides to true
	mock := &mockVCSForSync{
		prMergeable:    true,
		prApproved:     true,
		repoConfigData: []byte("version: 1\nskip_no_changes: true\n"),
	}
	exec := &Executor{
		vcs:    mock,
		argocd: fake.client(),
		lock:   lockMgr,
		config: &config.Config{
			ArgoCD:   config.ArgoCDConfig{SyncTimeout: 30 * time.Second},
			Defaults: config.DefaultsConfig{SkipNoChanges: false},
		},
		renderer: diff.NewRenderer(),
	}

	event := &models.PREvent{
		Repo: models.RepoInfo{
			Owner:    "org",
			Name:     "repo",
			FullName: "org/repo",
			HTMLURL:  "https://github.com/org/repo",
		},
		PR: models.PRInfo{
			Number:  1,
			HeadSHA: "abc123",
			HeadRef: "feature-branch",
		},
		Comment: &models.Comment{ID: 100},
	}

	err := exec.executeSync(context.Background(), &Command{Name: CommandSync}, event)
	if err != nil {
		t.Fatalf("executeSync() unexpected error: %v", err)
	}

	// Only app-changed should be synced (repo config enables skip)
	syncCalls := int(fake.syncCallCount.Load())
	if syncCalls != 1 {
		t.Errorf("expected 1 sync call (repo config skip_no_changes=true), got %d", syncCalls)
	}
}

func TestExecuteSyncSkipNoChangesEmptyOutput(t *testing.T) {
	// When diff succeeds with zero diffs, planApplication stores PlanOutput=""
	// and PlanDiffs=nil. Verify that skip_no_changes handles this case.
	app := v1alpha1.Application{
		Spec: v1alpha1.ApplicationSpec{
			Source: &v1alpha1.ApplicationSource{
				RepoURL:        "https://github.com/org/repo",
				Path:           "manifests",
				TargetRevision: "main",
			},
		},
	}
	fake := newFakeArgoServer(app)
	defer fake.close()

	lockMgr := &mockLockManagerForSyncWithList{
		locks: []models.Lock{
			{Application: "app-changed", PlanRevision: "abc123", PlanOutput: "some diff", PlanDiffs: []models.PlanDiffEntry{{Diff: "some diff"}}},
			{Application: "app-no-changes-empty", PlanRevision: "abc123", PlanOutput: "", PlanDiffs: nil},
		},
	}

	mock := &mockVCSForSync{prMergeable: true, prApproved: true}
	exec := &Executor{
		vcs:    mock,
		argocd: fake.client(),
		lock:   lockMgr,
		config: &config.Config{
			ArgoCD:   config.ArgoCDConfig{SyncTimeout: 30 * time.Second},
			Defaults: config.DefaultsConfig{SkipNoChanges: true},
		},
		renderer: diff.NewRenderer(),
	}

	event := &models.PREvent{
		Repo: models.RepoInfo{
			Owner:    "org",
			Name:     "repo",
			FullName: "org/repo",
			HTMLURL:  "https://github.com/org/repo",
		},
		PR: models.PRInfo{
			Number:  1,
			HeadSHA: "abc123",
			HeadRef: "feature-branch",
		},
		Comment: &models.Comment{ID: 100},
	}

	err := exec.executeSync(context.Background(), &Command{Name: CommandSync}, event)
	if err != nil {
		t.Fatalf("executeSync() unexpected error: %v", err)
	}

	// Only the app with changes should be synced; the empty-output app should be skipped
	syncCalls := int(fake.syncCallCount.Load())
	if syncCalls != 1 {
		t.Errorf("expected 1 sync call (skipping empty-output no-changes app), got %d", syncCalls)
	}
}

// mockLockManagerForSyncWithList extends mockLockManagerForSync to return preset locks.
type mockLockManagerForSyncWithList struct {
	mockLockManagerForSync
	locks []models.Lock
}

func (m *mockLockManagerForSyncWithList) ListByPR(_ context.Context, _ string, _ int) ([]models.Lock, error) {
	return m.locks, nil
}

func TestSyncApplicationMultiSourceWithSourceFile(t *testing.T) {
	// When SourceFile is set AND app is multi-source AND fromPRRepo,
	// the parsed spec should have rewritten targetRevisions and
	// UpdateApplicationSpec should be called exactly once.
	fake := newFakeArgoServer(v1alpha1.Application{
		Spec: v1alpha1.ApplicationSpec{
			Sources: v1alpha1.ApplicationSources{
				{RepoURL: "https://github.com/org/repo", Path: "manifests", TargetRevision: "main"},
				{RepoURL: "https://argoproj.github.io/argo-helm", Chart: "argo-cd", TargetRevision: "5.51.0"},
			},
		},
	})
	defer fake.close()

	// Minimal valid Application YAML for the source file
	sourceFileContent := `apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: test-app
spec:
  sources:
    - repoURL: https://github.com/org/repo
      path: manifests
      targetRevision: main
    - repoURL: https://argoproj.github.io/argo-helm
      chart: argo-cd
      targetRevision: 5.51.0
  destination:
    server: https://kubernetes.default.svc
    namespace: default
  project: default
`

	exec := &Executor{
		vcs:      &mockVCSForSync{prMergeable: true},
		argocd:   fake.client(),
		lock:     &mockLockManagerForSync{},
		config:   &config.Config{ArgoCD: config.ArgoCDConfig{SyncTimeout: 30 * time.Second}},
		renderer: diff.NewRenderer(),
	}

	lock := models.Lock{
		Application:  "test-app",
		PlanRevision: "abc123",
		SourceFile:   "apps/test-app.yaml",
	}
	cmd := &Command{Name: CommandSync}
	event := &models.PREvent{
		Repo: models.RepoInfo{
			Owner:    "org",
			Name:     "repo",
			FullName: "org/repo",
			HTMLURL:  "https://github.com/org/repo",
		},
		PR: models.PRInfo{
			Number:  1,
			HeadSHA: "abc123",
			HeadRef: "feature-branch",
		},
	}

	headContents := map[string][]byte{
		"apps/test-app.yaml": []byte(sourceFileContent),
	}

	result := exec.syncApplication(context.Background(), lock, cmd, event, headContents)

	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}

	// Should call UpdateApplicationSpec exactly once (with rewritten targetRevisions)
	specUpdates := int(fake.specUpdateCount.Load())
	if specUpdates != 1 {
		t.Errorf("expected exactly 1 UpdateApplicationSpec call, got %d", specUpdates)
	}

	// Check that the spec update has rewritten targetRevisions
	spec := fake.lastSpecUpdate()
	if spec == nil {
		t.Fatal("expected spec update body, got nil")
	}
	if len(spec.Sources) != 2 {
		t.Fatalf("spec sources count = %d, want 2", len(spec.Sources))
	}
	// Git source should have PR SHA
	if spec.Sources[0].TargetRevision != "abc123" {
		t.Errorf("spec.Sources[0].TargetRevision = %q, want %q", spec.Sources[0].TargetRevision, "abc123")
	}
	// Helm source should keep original version
	if spec.Sources[1].TargetRevision != "5.51.0" {
		t.Errorf("spec.Sources[1].TargetRevision = %q, want %q", spec.Sources[1].TargetRevision, "5.51.0")
	}

	if !result.TargetRevRewritten {
		t.Error("expected TargetRevRewritten to be true")
	}
}

func TestSyncApplicationStripsAutoSyncFromSourceFile(t *testing.T) {
	// When the PR source file defines syncPolicy.automated, the sync flow
	// must strip it before calling UpdateApplicationSpec. Otherwise, the
	// spec update re-enables auto-sync that was intentionally disabled by
	// disableAutoSyncForLocks, and ArgoCD rejects the subsequent sync with
	// "Cannot sync to X: auto-sync currently set to main".
	tests := []struct {
		name           string
		appSpec        v1alpha1.ApplicationSpec // live app spec (already has auto-sync disabled)
		sourceFileYAML string                   // PR source file content
	}{
		{
			name: "single-source app with auto-sync in source file",
			appSpec: v1alpha1.ApplicationSpec{
				Source: &v1alpha1.ApplicationSource{
					RepoURL:        "https://github.com/org/repo",
					Path:           "manifests",
					TargetRevision: "main",
				},
				SyncPolicy: &v1alpha1.SyncPolicy{
					SyncOptions: v1alpha1.SyncOptions{"CreateNamespace=true"},
					// Automated is nil — already disabled by disableAutoSyncForLocks
				},
			},
			sourceFileYAML: `apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: test-app
spec:
  source:
    repoURL: https://github.com/org/repo
    path: manifests
    targetRevision: main
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
    syncOptions:
      - CreateNamespace=true
  destination:
    server: https://kubernetes.default.svc
    namespace: default
  project: default
`,
		},
		{
			name: "multi-source app with auto-sync in source file",
			appSpec: v1alpha1.ApplicationSpec{
				Sources: v1alpha1.ApplicationSources{
					{RepoURL: "https://github.com/org/repo", Path: "manifests", TargetRevision: "main"},
				},
				SyncPolicy: &v1alpha1.SyncPolicy{
					SyncOptions: v1alpha1.SyncOptions{"CreateNamespace=true"},
				},
			},
			sourceFileYAML: `apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: test-app
spec:
  sources:
    - repoURL: https://github.com/org/repo
      path: manifests
      targetRevision: main
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
    syncOptions:
      - CreateNamespace=true
  destination:
    server: https://kubernetes.default.svc
    namespace: default
  project: default
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := newFakeArgoServer(v1alpha1.Application{
				Spec: tt.appSpec,
			})
			defer fake.close()

			exec := &Executor{
				vcs:      &mockVCSForSync{prMergeable: true},
				argocd:   fake.client(),
				lock:     &mockLockManagerForSync{},
				config:   &config.Config{ArgoCD: config.ArgoCDConfig{SyncTimeout: 30 * time.Second}},
				renderer: diff.NewRenderer(),
			}

			lock := models.Lock{
				Application:  "test-app",
				PlanRevision: "abc123",
				SourceFile:   "apps/test-app.yaml",
			}
			cmd := &Command{Name: CommandSync}
			event := &models.PREvent{
				Repo: models.RepoInfo{
					Owner:    "org",
					Name:     "repo",
					FullName: "org/repo",
					HTMLURL:  "https://github.com/org/repo",
				},
				PR: models.PRInfo{
					Number:  1,
					HeadSHA: "abc123",
					HeadRef: "feature-branch",
				},
			}

			headContents := map[string][]byte{
				"apps/test-app.yaml": []byte(tt.sourceFileYAML),
			}

			result := exec.syncApplication(context.Background(), lock, cmd, event, headContents)

			if result.Error != nil {
				t.Fatalf("unexpected error: %v", result.Error)
			}

			// Verify UpdateApplicationSpec was called
			specUpdates := int(fake.specUpdateCount.Load())
			if specUpdates == 0 {
				t.Fatal("expected UpdateApplicationSpec to be called")
			}

			// Verify the spec update does NOT contain syncPolicy.automated
			spec := fake.lastSpecUpdate()
			if spec == nil {
				t.Fatal("expected spec update body, got nil")
			}
			if spec.SyncPolicy != nil && spec.SyncPolicy.Automated != nil {
				t.Error("spec update must not contain syncPolicy.automated — " +
					"it would re-enable auto-sync that was intentionally disabled")
			}
		})
	}
}

func TestFilterSyncLocks(t *testing.T) {
	t.Run("direct match", func(t *testing.T) {
		locks := []models.Lock{
			{Application: "app-a"},
			{Application: "app-b"},
			{Application: "app-c"},
		}
		// Create a minimal fake ArgoCD server — we only need the client
		fake := newFakeArgoServer(v1alpha1.Application{})
		defer fake.close()
		e := &Executor{argocd: fake.client()}

		filtered, err := e.filterSyncLocks(context.Background(), locks, "app-b")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(filtered) != 1 {
			t.Fatalf("expected 1 lock, got %d", len(filtered))
		}
		if filtered[0].Application != "app-b" {
			t.Errorf("expected app-b, got %s", filtered[0].Application)
		}
	})

	t.Run("no match returns empty", func(t *testing.T) {
		locks := []models.Lock{
			{Application: "app-a"},
		}
		fake := newFakeArgoServer(v1alpha1.Application{})
		defer fake.close()
		e := &Executor{argocd: fake.client()}

		filtered, err := e.filterSyncLocks(context.Background(), locks, "nonexistent")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(filtered) != 0 {
			t.Errorf("expected 0 locks, got %d", len(filtered))
		}
	})
}
