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
	"strings"
	"testing"

	"github.com/org/lemuria/internal/argocd"
	"github.com/org/lemuria/internal/config"
	"github.com/org/lemuria/internal/models"
)

func TestMatchAppName(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		appName string
		want    bool
	}{
		{"exact match", "my-app", "my-app", true},
		{"exact mismatch", "my-app", "other-app", false},
		{"wildcard match", "my-app-*", "my-app-staging", true},
		{"wildcard match prefix only", "prod-*", "prod-api", true},
		{"wildcard mismatch", "prod-*", "dev-api", false},
		{"wildcard empty suffix", "prod-*", "prod-", true},
		{"star only matches all", "*", "anything", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchAppName(tt.pattern, tt.appName); got != tt.want {
				t.Errorf("matchAppName(%q, %q) = %v, want %v", tt.pattern, tt.appName, got, tt.want)
			}
		})
	}
}

func TestPathContains(t *testing.T) {
	tests := []struct {
		name string
		dir  string
		file string
		want bool
	}{
		{"file in directory", "apps/my-app", "apps/my-app/deployment.yaml", true},
		{"file not in directory", "apps/other-app", "apps/my-app/deployment.yaml", false},
		{"empty directory matches all", "", "any/file.yaml", true},
		{"dot directory matches all", ".", "any/file.yaml", true},
		{"exact path not contained", "apps/my-app", "apps/my-app", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pathContains(tt.dir, tt.file); got != tt.want {
				t.Errorf("pathContains(%q, %q) = %v, want %v", tt.dir, tt.file, got, tt.want)
			}
		})
	}
}

func TestFilesToChangedFiles(t *testing.T) {
	paths := []string{"a.yaml", "b.yaml", "c.yaml"}
	files := filesToChangedFiles(paths)

	if len(files) != len(paths) {
		t.Errorf("filesToChangedFiles() returned %d files, want %d", len(files), len(paths))
		return
	}

	for i, f := range files {
		if f.Filename != paths[i] {
			t.Errorf("filesToChangedFiles()[%d].Filename = %q, want %q", i, f.Filename, paths[i])
		}
	}
}

func TestFormatPlanSummary(t *testing.T) {
	tests := []struct {
		name    string
		summary argocd.DiffSummary
		want    string
	}{
		{
			name:    "no changes",
			summary: argocd.DiffSummary{},
			want:    "No changes detected",
		},
		{
			name:    "creates only",
			summary: argocd.DiffSummary{Created: 3},
			want:    "3 to create",
		},
		{
			name:    "updates only",
			summary: argocd.DiffSummary{Updated: 1},
			want:    "1 to update",
		},
		{
			name:    "deletes only",
			summary: argocd.DiffSummary{Deleted: 2},
			want:    "2 to delete",
		},
		{
			name:    "all types",
			summary: argocd.DiffSummary{Created: 1, Updated: 2, Deleted: 3},
			want:    "1 to create, 2 to update, 3 to delete",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatPlanSummary(tt.summary); got != tt.want {
				t.Errorf("formatPlanSummary() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestToPlanDiffEntries(t *testing.T) {
	tests := []struct {
		name  string
		diffs []models.ManifestDiff
		want  int // expected count
	}{
		{
			name:  "empty diffs",
			diffs: nil,
			want:  0,
		},
		{
			name: "filters empty diffs",
			diffs: []models.ManifestDiff{
				{Resource: models.ResourceKey{Kind: "Deployment", Name: "app"}, Diff: "+added line", Action: models.DiffActionCreate},
				{Resource: models.ResourceKey{Kind: "Service", Name: "svc"}, Diff: "", Action: models.DiffActionNone},
				{Resource: models.ResourceKey{Kind: "ConfigMap", Name: "cm"}, Diff: "-removed", Action: models.DiffActionDelete},
			},
			want: 2,
		},
		{
			name: "all have diffs",
			diffs: []models.ManifestDiff{
				{Resource: models.ResourceKey{Kind: "Deployment", Name: "app"}, Diff: "+line", Action: models.DiffActionCreate},
				{Resource: models.ResourceKey{Kind: "Service", Name: "svc"}, Diff: "-line", Action: models.DiffActionDelete},
			},
			want: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entries := toPlanDiffEntries(tt.diffs)
			if len(entries) != tt.want {
				t.Errorf("toPlanDiffEntries() returned %d entries, want %d", len(entries), tt.want)
			}
		})
	}
}

func TestContainsAppByName(t *testing.T) {
	apps := []models.Application{
		{Name: "app-a"},
		{Name: "app-b"},
		{Name: "app-c"},
	}

	tests := []struct {
		name    string
		appName string
		want    bool
	}{
		{"found", "app-b", true},
		{"not found", "app-d", false},
		{"empty name", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := containsAppByName(apps, tt.appName); got != tt.want {
				t.Errorf("containsAppByName(%q) = %v, want %v", tt.appName, got, tt.want)
			}
		})
	}
}

func TestTitleCase(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"sync", "Sync"},
		{"rollback", "Rollback"},
		{"plan", "Plan"},
		{"", ""},
		{"HELP", "HELP"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := titleCase(tt.input); got != tt.want {
				t.Errorf("titleCase(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsUserAllowedForApp(t *testing.T) {
	tests := []struct {
		name       string
		repoConfig *config.RepoConfig
		appName    string
		user       string
		want       bool
	}{
		// (1) allowed_users unset => allow
		{
			name:       "nil repo config allows all users",
			repoConfig: nil,
			appName:    "my-app",
			user:       "anyone",
			want:       true,
		},
		{
			name:       "no sync requirements allows all users",
			repoConfig: &config.RepoConfig{},
			appName:    "my-app",
			user:       "anyone",
			want:       true,
		},
		{
			name: "matching requirement with empty allowed_users allows all",
			repoConfig: &config.RepoConfig{
				SyncRequirements: []config.SyncRequirement{
					{Name: "my-app", AllowedUsers: nil},
				},
			},
			appName: "my-app",
			user:    "anyone",
			want:    true,
		},
		{
			name: "no matching requirement allows all",
			repoConfig: &config.RepoConfig{
				SyncRequirements: []config.SyncRequirement{
					{Name: "other-app", AllowedUsers: []string{"alice"}},
				},
			},
			appName: "my-app",
			user:    "anyone",
			want:    true,
		},
		// (2) user in list => allow
		{
			name: "user in allowed list is allowed",
			repoConfig: &config.RepoConfig{
				SyncRequirements: []config.SyncRequirement{
					{Name: "my-app", AllowedUsers: []string{"alice", "bob"}},
				},
			},
			appName: "my-app",
			user:    "bob",
			want:    true,
		},
		// (3) user not in list => blocked
		{
			name: "user not in allowed list is blocked",
			repoConfig: &config.RepoConfig{
				SyncRequirements: []config.SyncRequirement{
					{Name: "my-app", AllowedUsers: []string{"alice", "bob"}},
				},
			},
			appName: "my-app",
			user:    "charlie",
			want:    false,
		},
		// Wildcard matching
		{
			name: "wildcard requirement allows user in list",
			repoConfig: &config.RepoConfig{
				SyncRequirements: []config.SyncRequirement{
					{Name: "*", AllowedUsers: []string{"admin"}},
				},
			},
			appName: "any-app",
			user:    "admin",
			want:    true,
		},
		{
			name: "wildcard requirement blocks user not in list",
			repoConfig: &config.RepoConfig{
				SyncRequirements: []config.SyncRequirement{
					{Name: "*", AllowedUsers: []string{"admin"}},
				},
			},
			appName: "any-app",
			user:    "hacker",
			want:    false,
		},
		// Most-specific match precedence: exact match overrides wildcard
		{
			name: "exact match takes priority over wildcard - user allowed by exact, blocked by wildcard",
			repoConfig: &config.RepoConfig{
				SyncRequirements: []config.SyncRequirement{
					{Name: "*", AllowedUsers: []string{"admin"}},
					{Name: "dev-app", AllowedUsers: []string{"dev-user"}},
				},
			},
			appName: "dev-app",
			user:    "dev-user",
			want:    true,
		},
		{
			name: "exact match takes priority over wildcard - user blocked by exact even if allowed by wildcard",
			repoConfig: &config.RepoConfig{
				SyncRequirements: []config.SyncRequirement{
					{Name: "*", AllowedUsers: []string{"admin"}},
					{Name: "prod-app", AllowedUsers: []string{"prod-deployer"}},
				},
			},
			appName: "prod-app",
			user:    "admin",
			want:    false,
		},
		// Prefix wildcard matching
		{
			name: "prefix wildcard allows user",
			repoConfig: &config.RepoConfig{
				SyncRequirements: []config.SyncRequirement{
					{Name: "prod-*", AllowedUsers: []string{"deployer"}},
				},
			},
			appName: "prod-api",
			user:    "deployer",
			want:    true,
		},
		{
			name: "prefix wildcard blocks user",
			repoConfig: &config.RepoConfig{
				SyncRequirements: []config.SyncRequirement{
					{Name: "prod-*", AllowedUsers: []string{"deployer"}},
				},
			},
			appName: "prod-api",
			user:    "junior-dev",
			want:    false,
		},
		// Exact match with empty allowed_users overrides wildcard restriction
		{
			name: "exact match with empty allowed_users overrides wildcard restriction",
			repoConfig: &config.RepoConfig{
				SyncRequirements: []config.SyncRequirement{
					{Name: "*", AllowedUsers: []string{"admin"}},
					{Name: "dev-app", AllowedUsers: nil},
				},
			},
			appName: "dev-app",
			user:    "anyone",
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exec := newSyncTestExecutor(nil, &config.Config{})
			got := exec.isUserAllowedForApp(tt.repoConfig, tt.appName, tt.user)
			if got != tt.want {
				t.Errorf("isUserAllowedForApp() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCheckUserAuthorizationForApps(t *testing.T) {
	tests := []struct {
		name           string
		repoConfigYAML string
		user           string
		appNames       []string
		cmdName        CommandType
		wantErr        bool
		wantErrContain string
	}{
		{
			name:     "no repo config allows all",
			user:     "anyone",
			appNames: []string{"app-a"},
			cmdName:  CommandSync,
			wantErr:  false,
		},
		{
			name:           "allowed_users unset allows all",
			repoConfigYAML: "version: 1\nsync_requirements:\n  - name: \"app-a\"\n    require_approval: false\n",
			user:           "anyone",
			appNames:       []string{"app-a"},
			cmdName:        CommandSync,
			wantErr:        false,
		},
		{
			name:           "user in list is allowed",
			repoConfigYAML: "version: 1\nsync_requirements:\n  - name: \"app-a\"\n    allowed_users: [\"alice\", \"bob\"]\n",
			user:           "alice",
			appNames:       []string{"app-a"},
			cmdName:        CommandSync,
			wantErr:        false,
		},
		{
			name:           "user not in list is blocked",
			repoConfigYAML: "version: 1\nsync_requirements:\n  - name: \"app-a\"\n    allowed_users: [\"alice\", \"bob\"]\n",
			user:           "charlie",
			appNames:       []string{"app-a"},
			cmdName:        CommandSync,
			wantErr:        true,
			wantErrContain: "not authorized",
		},
		{
			name:           "multi-app: user allowed for all apps",
			repoConfigYAML: "version: 1\nsync_requirements:\n  - name: \"*\"\n    allowed_users: [\"deployer\"]\n",
			user:           "deployer",
			appNames:       []string{"app-a", "app-b"},
			cmdName:        CommandSync,
			wantErr:        false,
		},
		{
			name:           "multi-app: user blocked on one app",
			repoConfigYAML: "version: 1\nsync_requirements:\n  - name: \"app-a\"\n    allowed_users: [\"alice\"]\n  - name: \"app-b\"\n    allowed_users: [\"bob\"]\n",
			user:           "alice",
			appNames:       []string{"app-a", "app-b"},
			cmdName:        CommandSync,
			wantErr:        true,
			wantErrContain: "app-b",
		},
		{
			name:           "exact match overrides wildcard - user restricted to specific app only",
			repoConfigYAML: "version: 1\nsync_requirements:\n  - name: \"*\"\n    allowed_users: [\"admin\"]\n  - name: \"dev-app\"\n    allowed_users: [\"dev-user\"]\n",
			user:           "dev-user",
			appNames:       []string{"dev-app"},
			cmdName:        CommandRollback,
			wantErr:        false,
		},
		{
			name:           "exact match overrides wildcard - user cannot touch specific app",
			repoConfigYAML: "version: 1\nsync_requirements:\n  - name: \"*\"\n    allowed_users: [\"admin\"]\n  - name: \"prod-app\"\n    allowed_users: [\"prod-deployer\"]\n",
			user:           "admin",
			appNames:       []string{"prod-app"},
			cmdName:        CommandSync,
			wantErr:        true,
			wantErrContain: "prod-app",
		},
		{
			name:     "nil comment allows all (autoplan scenario)",
			user:     "", // will set event.Comment = nil
			appNames: []string{"app-a"},
			cmdName:  CommandSync,
			wantErr:  false,
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
			}

			cfg := &config.Config{}
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

			if tt.user != "" {
				event.Comment = &models.Comment{
					Author: models.UserInfo{Login: tt.user},
				}
			}

			cmd := &Command{Name: tt.cmdName}
			err := exec.checkUserAuthorizationForApps(context.Background(), cmd, event, tt.appNames)

			if tt.wantErr {
				if err == nil {
					t.Errorf("checkUserAuthorizationForApps() expected error, got nil")
				} else if tt.wantErrContain != "" && !strings.Contains(err.Error(), tt.wantErrContain) {
					t.Errorf("checkUserAuthorizationForApps() error = %q, want containing %q", err.Error(), tt.wantErrContain)
				}
			} else {
				if err != nil {
					t.Errorf("checkUserAuthorizationForApps() unexpected error: %v", err)
				}
			}
		})
	}
}

func TestIsProtectedBranch(t *testing.T) {
	tests := []struct {
		name   string
		branch string
		want   bool
	}{
		{"main is protected", "main", true},
		{"master is protected", "master", true},
		{"develop is protected", "develop", true},
		{"development is protected", "development", true},
		{"release is not protected", "release", false},
		{"feature is not protected", "feature-branch", false},
		{"fix is not protected", "fix/my-bug", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsProtectedBranch(tt.branch); got != tt.want {
				t.Errorf("IsProtectedBranch(%q) = %v, want %v", tt.branch, got, tt.want)
			}
		})
	}
}
