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

func TestConvertToRenderResultsApplicationSetName(t *testing.T) {
	results := []appPlanResult{
		{
			Application:        "standalone-app",
			ApplicationSetName: "",
		},
		{
			Application:        "appset-dev",
			ApplicationSetName: "my-appset",
		},
		{
			Application:        "appset-staging",
			ApplicationSetName: "my-appset",
		},
	}

	rendered := convertToRenderResults(results)

	if len(rendered) != 3 {
		t.Fatalf("expected 3 results, got %d", len(rendered))
	}
	if rendered[0].ApplicationSetName != "" {
		t.Errorf("expected empty ApplicationSetName for standalone app, got %q", rendered[0].ApplicationSetName)
	}
	if rendered[1].ApplicationSetName != "my-appset" {
		t.Errorf("expected ApplicationSetName %q, got %q", "my-appset", rendered[1].ApplicationSetName)
	}
	if rendered[2].ApplicationSetName != "my-appset" {
		t.Errorf("expected ApplicationSetName %q, got %q", "my-appset", rendered[2].ApplicationSetName)
	}
}

func TestConvertToRenderResultsIsGeneratedApp(t *testing.T) {
	results := []appPlanResult{
		{
			Application:    "regular-new",
			ChangeType:     models.ApplicationNew,
			IsGeneratedApp: false,
		},
		{
			Application:        "generated-new",
			ApplicationSetName: "my-appset",
			ChangeType:         models.ApplicationNew,
			IsGeneratedApp:     true,
		},
		{
			Application:        "generated-deleted",
			ApplicationSetName: "my-appset",
			ChangeType:         models.ApplicationDeleted,
			IsGeneratedApp:     true,
		},
	}

	rendered := convertToRenderResults(results)

	if len(rendered) != 3 {
		t.Fatalf("expected 3 results, got %d", len(rendered))
	}
	if rendered[0].IsGeneratedApp {
		t.Error("expected regular-new to not be a generated app")
	}
	if !rendered[1].IsGeneratedApp {
		t.Error("expected generated-new to be a generated app")
	}
	if !rendered[2].IsGeneratedApp {
		t.Error("expected generated-deleted to be a generated app")
	}
	if rendered[1].ApplicationSetName != "my-appset" {
		t.Errorf("expected ApplicationSetName %q, got %q", "my-appset", rendered[1].ApplicationSetName)
	}
}

func TestIsAppAffected_ExactMappingTakesPrecedenceOverWildcard(t *testing.T) {
	e := &Executor{}

	tests := []struct {
		name     string
		app      models.Application
		repoURL  string
		files    []string
		config   *config.RepoConfig
		expected bool
	}{
		{
			name:    "exact mapping matches paths - affected",
			app:     models.Application{Name: "sealed-secrets"},
			repoURL: "https://github.com/org/repo",
			files:   []string{"bootstrap/sealed-secret/sealed-secrets-app.yaml"},
			config: &config.RepoConfig{
				Applications: []config.ApplicationMapping{
					{Name: "sealed-secrets", Paths: []string{"bootstrap/sealed-secret/**"}},
					{Name: "*", Paths: []string{"apps/**"}},
				},
			},
			expected: true,
		},
		{
			name:    "exact mapping exists but paths dont match - NOT affected even though wildcard would match",
			app:     models.Application{Name: "sealed-secrets"},
			repoURL: "https://github.com/org/repo",
			files:   []string{"apps/grafana/values.yaml", "apps/loki/values.yaml"},
			config: &config.RepoConfig{
				Applications: []config.ApplicationMapping{
					{Name: "sealed-secrets", Paths: []string{"bootstrap/sealed-secret/**"}},
					{Name: "*", Paths: []string{"apps/**"}},
				},
			},
			expected: false,
		},
		{
			name:    "no exact mapping - wildcard matches - affected",
			app:     models.Application{Name: "grafana"},
			repoURL: "https://github.com/org/repo",
			files:   []string{"apps/grafana/values.yaml"},
			config: &config.RepoConfig{
				Applications: []config.ApplicationMapping{
					{Name: "sealed-secrets", Paths: []string{"bootstrap/sealed-secret/**"}},
					{Name: "*", Paths: []string{"apps/**"}},
				},
			},
			expected: true,
		},
		{
			name:    "no exact mapping - wildcard paths dont match - NOT affected",
			app:     models.Application{Name: "grafana"},
			repoURL: "https://github.com/org/repo",
			files:   []string{"bootstrap/root-app.yaml"},
			config: &config.RepoConfig{
				Applications: []config.ApplicationMapping{
					{Name: "sealed-secrets", Paths: []string{"bootstrap/sealed-secret/**"}},
					{Name: "*", Paths: []string{"apps/**"}},
				},
			},
			expected: false,
		},
		{
			name:    "no config - falls through to repo URL matching",
			app:     models.Application{Name: "my-app", RepoURL: "https://github.com/org/repo", Path: "apps/my-app"},
			repoURL: "https://github.com/org/repo",
			files:   []string{"apps/my-app/deployment.yaml"},
			config:  nil,
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := e.isAppAffected(tt.app, tt.repoURL, tt.files, tt.config)
			if got != tt.expected {
				t.Errorf("isAppAffected() = %v, want %v", got, tt.expected)
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
