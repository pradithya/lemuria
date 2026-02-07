package commands

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/google/go-github/v60/github"

	"github.com/org/lemuria/internal/config"
	"github.com/org/lemuria/internal/models"
)

// mockGitHubForSync is a minimal GitHubClient mock for sync requirement tests.
type mockGitHubForSync struct {
	repoConfigData []byte
	prApproved     bool
	prMergeable    bool
}

func (m *mockGitHubForSync) GetChangedFiles(context.Context, string, string, int) ([]models.ChangedFile, error) {
	return nil, nil
}

func (m *mockGitHubForSync) GetRepoConfig(context.Context, string, string, string) ([]byte, error) {
	if m.repoConfigData == nil {
		return nil, fmt.Errorf(".lemuria.yaml not found")
	}
	return m.repoConfigData, nil
}

func (m *mockGitHubForSync) GetFileContent(context.Context, string, string, string, string) ([]byte, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockGitHubForSync) GetPR(context.Context, string, string, int) (*github.PullRequest, error) {
	return &github.PullRequest{
		State:     github.String("open"),
		Mergeable: github.Bool(m.prMergeable),
	}, nil
}

func (m *mockGitHubForSync) IsPRApproved(context.Context, string, string, int) (bool, error) {
	return m.prApproved, nil
}

func (m *mockGitHubForSync) PostComment(context.Context, string, string, int, string, bool) (*github.IssueComment, error) {
	return &github.IssueComment{ID: github.Int64(1)}, nil
}

func (m *mockGitHubForSync) AddReaction(context.Context, string, string, int64, string) error {
	return nil
}

func (m *mockGitHubForSync) InvalidatePlanComments(context.Context, string, string, int) error {
	return nil
}

func (m *mockGitHubForSync) MergePullRequest(context.Context, string, string, int, string, string, string) error {
	return nil
}

func (m *mockGitHubForSync) DeleteBranch(context.Context, string, string, string) error {
	return nil
}

func newSyncTestExecutor(gh GitHubClient, cfg *config.Config) *Executor {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return &Executor{
		github: gh,
		config: cfg,
		logger: logger,
	}
}

func boolPtr(b bool) *bool {
	return &b
}

func TestAppRequiresApproval(t *testing.T) {
	tests := []struct {
		name         string
		serverRequire bool
		repoConfig   *config.RepoConfig
		appName      string
		want         bool
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

			mock := &mockGitHubForSync{
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
