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
	"fmt"
	"strings"
	"testing"

	v1alpha1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"

	"github.com/org/lemuria/internal/config"
	"github.com/org/lemuria/internal/models"
	"github.com/org/lemuria/internal/vcs"
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

func TestConvertV1alpha1App(t *testing.T) {
	tests := []struct {
		name        string
		app         *v1alpha1.Application
		wantRepoURL string
		wantSources int
	}{
		{
			name: "single source",
			app: &v1alpha1.Application{
				Spec: v1alpha1.ApplicationSpec{
					Source: &v1alpha1.ApplicationSource{
						RepoURL: "https://github.com/org/repo",
					},
				},
			},
			wantRepoURL: "https://github.com/org/repo",
			wantSources: 0,
		},
		{
			name: "multi source",
			app: &v1alpha1.Application{
				Spec: v1alpha1.ApplicationSpec{
					Sources: v1alpha1.ApplicationSources{
						{RepoURL: "https://github.com/org/repo"},
						{RepoURL: "https://charts.bitnami.com/bitnami"},
					},
				},
			},
			wantRepoURL: "",
			wantSources: 2,
		},
		{
			name: "nil source",
			app: &v1alpha1.Application{
				Spec: v1alpha1.ApplicationSpec{},
			},
			wantRepoURL: "",
			wantSources: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := convertV1alpha1App(tt.app)
			if got.RepoURL != tt.wantRepoURL {
				t.Errorf("RepoURL = %q, want %q", got.RepoURL, tt.wantRepoURL)
			}
			if len(got.Sources) != tt.wantSources {
				t.Errorf("Sources count = %d, want %d", len(got.Sources), tt.wantSources)
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

func TestBuildSyncRevisionOptions(t *testing.T) {
	tests := []struct {
		name          string
		app           *v1alpha1.Application
		repoURL       string
		revision      string
		wantRevisions []string
		wantPositions []int64
	}{
		{
			name: "single source returns nil (caller uses opts.Revision)",
			app: &v1alpha1.Application{
				Spec: v1alpha1.ApplicationSpec{
					Source: &v1alpha1.ApplicationSource{
						RepoURL: "https://github.com/org/repo",
					},
				},
			},
			repoURL:       "https://github.com/org/repo",
			revision:      "abc123",
			wantRevisions: nil,
			wantPositions: nil,
		},
		{
			name: "multi-source all matching PR repo",
			app: &v1alpha1.Application{
				Spec: v1alpha1.ApplicationSpec{
					Sources: v1alpha1.ApplicationSources{
						{RepoURL: "https://github.com/org/repo", Path: "manifests"},
						{RepoURL: "https://github.com/org/repo.git", Path: "values"},
					},
				},
			},
			repoURL:       "https://github.com/org/repo",
			revision:      "abc123",
			wantRevisions: []string{"abc123", "abc123"},
			wantPositions: []int64{1, 2},
		},
		{
			name: "multi-source mixed — only PR repo sources get revision",
			app: &v1alpha1.Application{
				Spec: v1alpha1.ApplicationSpec{
					Sources: v1alpha1.ApplicationSources{
						{RepoURL: "https://argoproj.github.io/argo-helm", Chart: "argo-cd"},
						{RepoURL: "https://github.com/org/repo", Path: "values"},
						{RepoURL: "https://charts.bitnami.com/bitnami", Chart: "redis"},
					},
				},
			},
			repoURL:       "https://github.com/org/repo",
			revision:      "def456",
			wantRevisions: []string{"def456"},
			wantPositions: []int64{2},
		},
		{
			name: "multi-source with no matching sources",
			app: &v1alpha1.Application{
				Spec: v1alpha1.ApplicationSpec{
					Sources: v1alpha1.ApplicationSources{
						{RepoURL: "https://argoproj.github.io/argo-helm", Chart: "argo-cd"},
						{RepoURL: "https://charts.bitnami.com/bitnami", Chart: "redis"},
					},
				},
			},
			repoURL:       "https://github.com/org/repo",
			revision:      "abc123",
			wantRevisions: nil,
			wantPositions: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			revisions, positions := buildSyncRevisionOptions(tt.app, tt.repoURL, tt.revision)
			if len(revisions) != len(tt.wantRevisions) {
				t.Fatalf("revisions count = %d, want %d", len(revisions), len(tt.wantRevisions))
			}
			for i, want := range tt.wantRevisions {
				if revisions[i] != want {
					t.Errorf("revisions[%d] = %q, want %q", i, revisions[i], want)
				}
			}
			if len(positions) != len(tt.wantPositions) {
				t.Fatalf("positions count = %d, want %d", len(positions), len(tt.wantPositions))
			}
			for i, want := range tt.wantPositions {
				if positions[i] != want {
					t.Errorf("positions[%d] = %d, want %d", i, positions[i], want)
				}
			}
		})
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
