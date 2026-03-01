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
	"net/http"
	"net/http/httptest"
	"testing"

	v1alpha1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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
		{"wildcard trailing", "my-app-*", "my-app-staging", true},
		{"wildcard prefix only", "prod-*", "prod-api", true},
		{"wildcard mismatch", "prod-*", "dev-api", false},
		{"wildcard empty suffix", "prod-*", "prod-", true},
		{"star only matches all", "*", "anything", true},
		{"wildcard leading", "*-prod", "my-app-prod", true},
		{"wildcard leading mismatch", "*-prod", "my-app-dev", false},
		{"wildcard mid-string", "my-*-app", "my-cool-app", true},
		{"wildcard mid-string mismatch", "my-*-app", "my-cool-svc", false},
		{"regex match", "/^app-[0-9]+$/", "app-123", true},
		{"regex mismatch", "/^app-[0-9]+$/", "app-abc", false},
		{"regex partial", "/prod/", "my-prod-app", true},
		{"regex suffix", "/-prod$/", "my-app-prod", true},
		{"regex suffix mismatch", "/-prod$/", "my-app-dev", false},
		{"invalid regex returns false", "/[invalid/", "anything", false},
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
		{"prefix collision rejected", "apps/my-app", "apps/my-app-other/deployment.yaml", false},
		{"nested file matches", "apps/my-app", "apps/my-app/sub/dir/file.yaml", true},
		{"trailing slash in dir", "apps/my-app/", "apps/my-app/deployment.yaml", true},
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
			name:     "no config - falls through to repo URL matching",
			app:      models.Application{Name: "my-app", RepoURL: "https://github.com/org/repo", Path: "apps/my-app"},
			repoURL:  "https://github.com/org/repo",
			files:    []string{"apps/my-app/deployment.yaml"},
			config:   nil,
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
		name              string
		branch            string
		protectedBranches []string
		want              bool
	}{
		{"main is protected with defaults", "main", nil, true},
		{"master is protected with defaults", "master", nil, true},
		{"develop is protected with defaults", "develop", nil, true},
		{"development is protected with defaults", "development", nil, true},
		{"release is not protected with defaults", "release", nil, false},
		{"feature is not protected with defaults", "feature-branch", nil, false},
		{"fix is not protected with defaults", "fix/my-bug", nil, false},
		{"custom protected branch", "release", []string{"main", "release"}, true},
		{"not in custom list", "master", []string{"main", "release"}, false},
		{"empty list falls back to defaults", "main", []string{}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsProtectedBranch(tt.branch, tt.protectedBranches); got != tt.want {
				t.Errorf("IsProtectedBranch(%q, %v) = %v, want %v", tt.branch, tt.protectedBranches, got, tt.want)
			}
		})
	}
}

func TestIsAppAffected(t *testing.T) {
	exec := newTestExecutor(&mockVCS{}, &mockLock{}, nil)

	tests := []struct {
		name       string
		app        models.Application
		repoURL    string
		files      []string
		repoConfig *config.RepoConfig
		want       bool
	}{
		{
			name:    "app path matches changed file",
			app:     models.Application{Name: "my-app", Path: "apps/my-app", RepoURL: "https://github.com/org/repo"},
			repoURL: "https://github.com/org/repo",
			files:   []string{"apps/my-app/deployment.yaml"},
			want:    true,
		},
		{
			name:    "app path does not match",
			app:     models.Application{Name: "my-app", Path: "apps/my-app", RepoURL: "https://github.com/org/repo"},
			repoURL: "https://github.com/org/repo",
			files:   []string{"apps/other-app/deployment.yaml"},
			want:    false,
		},
		{
			name:    "different repo URL",
			app:     models.Application{Name: "my-app", Path: "apps/my-app", RepoURL: "https://github.com/other-org/other-repo"},
			repoURL: "https://github.com/org/repo",
			files:   []string{"apps/my-app/deployment.yaml"},
			want:    false,
		},
		{
			name: "multi-source app path matches",
			app: models.Application{
				Name: "multi-app",
				Sources: []models.ApplicationSource{
					{RepoURL: "https://github.com/org/repo", Path: "apps/frontend"},
					{RepoURL: "https://github.com/org/repo", Path: "apps/backend"},
				},
			},
			repoURL: "https://github.com/org/repo",
			files:   []string{"apps/backend/deployment.yaml"},
			want:    true,
		},
		{
			name: "multi-source app path does not match",
			app: models.Application{
				Name: "multi-app",
				Sources: []models.ApplicationSource{
					{RepoURL: "https://github.com/org/repo", Path: "apps/frontend"},
					{RepoURL: "https://github.com/org/repo", Path: "apps/backend"},
				},
			},
			repoURL: "https://github.com/org/repo",
			files:   []string{"apps/other/deployment.yaml"},
			want:    false,
		},
		{
			name: "multi-source only one source references this repo",
			app: models.Application{
				Name: "multi-app",
				Sources: []models.ApplicationSource{
					{RepoURL: "https://github.com/other-org/charts", Path: "charts/app"},
					{RepoURL: "https://github.com/org/repo", Path: "deploy/app"},
				},
			},
			repoURL: "https://github.com/org/repo",
			files:   []string{"deploy/app/values.yaml"},
			want:    true,
		},
		{
			name: "multi-source helm valueFile relative path matches",
			app: models.Application{
				Name: "helm-app",
				Sources: []models.ApplicationSource{
					{RepoURL: "https://github.com/org/charts", Path: "charts/myapp", Chart: "myapp"},
					{
						RepoURL: "https://github.com/org/repo",
						Path:    "deploy/envs/prod",
						Helm:    &models.HelmSource{ValueFiles: []string{"values-prod.yaml"}},
					},
				},
			},
			repoURL: "https://github.com/org/repo",
			files:   []string{"deploy/envs/prod/values-prod.yaml"},
			want:    true,
		},
		{
			name: "multi-source helm valueFile absolute path matches",
			app: models.Application{
				Name: "helm-app",
				Sources: []models.ApplicationSource{
					{RepoURL: "https://github.com/org/charts", Path: "charts/myapp"},
					{
						RepoURL: "https://github.com/org/repo",
						Path:    "deploy/envs/prod",
						Helm:    &models.HelmSource{ValueFiles: []string{"/common/values.yaml"}},
					},
				},
			},
			repoURL: "https://github.com/org/repo",
			files:   []string{"common/values.yaml"},
			want:    true,
		},
		{
			name: "multi-source helm $ref with no matching ref source does not match",
			app: models.Application{
				Name: "helm-app",
				Sources: []models.ApplicationSource{
					{
						RepoURL: "https://github.com/org/repo",
						Chart:   "mychart",
						Helm:    &models.HelmSource{ValueFiles: []string{"$values/common.yaml"}},
					},
				},
			},
			repoURL: "https://github.com/org/repo",
			files:   []string{"values/common.yaml"},
			want:    false,
		},
		{
			name: "multi-source chart-only source with no path",
			app: models.Application{
				Name: "chart-only-app",
				Sources: []models.ApplicationSource{
					{RepoURL: "https://github.com/org/repo", Chart: "mychart"},
				},
			},
			repoURL: "https://github.com/org/repo",
			files:   []string{"charts/mychart/values.yaml"},
			want:    false,
		},
		{
			name:    "repo config path mapping overrides",
			app:     models.Application{Name: "my-app", Path: "apps/my-app", RepoURL: "https://github.com/org/repo"},
			repoURL: "https://github.com/org/repo",
			files:   []string{"custom/path/values.yaml"},
			repoConfig: &config.RepoConfig{
				Applications: []config.ApplicationMapping{
					{Name: "my-app", Paths: []string{"custom/path/**"}},
				},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := exec.isAppAffected(tt.app, tt.repoURL, tt.files, tt.repoConfig)
			if got != tt.want {
				t.Errorf("isAppAffected() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFindAffectedApplications_BasicFlow(t *testing.T) {
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
			"feature-branch": {
				"argocd/app.yaml": []byte(appYAML),
			},
			"main": {
				"argocd/app.yaml": []byte(appYAML),
			},
		},
	}

	// ArgoCD mock: ListApplications for cross-repo (returns empty)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := v1alpha1.ApplicationList{Items: []v1alpha1.Application{}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	argoClient := newTestArgoClient(ts)
	exec := newTestExecutorWithArgo(vcs, &mockLock{}, argoClient)

	event := &models.PREvent{
		Repo: models.RepoInfo{
			Owner: "org", Name: "repo", FullName: "org/repo",
			HTMLURL: "https://github.com/org/repo",
		},
		PR: models.PRInfo{
			Number:  1,
			HeadRef: "feature-branch",
			BaseRef: "main",
		},
	}

	affected, err := exec.findAffectedApplications(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(affected) != 1 {
		t.Fatalf("expected 1 affected app, got %d", len(affected))
	}
	if affected[0].Name != "my-app" {
		t.Errorf("expected app name = 'my-app', got %q", affected[0].Name)
	}
	if affected[0].ChangeType != models.ApplicationExisting {
		t.Errorf("expected ChangeType = %q, got %q", models.ApplicationExisting, affected[0].ChangeType)
	}
}

func TestFindAffectedApplications_NewAndDeletedApps(t *testing.T) {
	newAppYAML := `apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: new-app
spec:
  project: default
  source:
    repoURL: https://github.com/org/repo
    path: apps/new-app
    targetRevision: main
  destination:
    server: https://kubernetes.default.svc
    namespace: default
`
	deletedAppYAML := `apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: deleted-app
spec:
  project: default
  source:
    repoURL: https://github.com/org/repo
    path: apps/deleted-app
    targetRevision: main
  destination:
    server: https://kubernetes.default.svc
    namespace: default
`

	vcs := &mockVCS{
		changedFiles: []models.ChangedFile{
			{Filename: "argocd/new-app.yaml", Status: "added"},
			{Filename: "argocd/deleted-app.yaml", Status: "removed"},
		},
		filesByPattern: map[string]map[string][]byte{
			"feature-branch": {
				"argocd/new-app.yaml": []byte(newAppYAML),
			},
			"main": {
				"argocd/deleted-app.yaml": []byte(deletedAppYAML),
			},
		},
	}

	// ArgoCD mock: ListApplications returns the deleted app (so it passes verify)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := v1alpha1.ApplicationList{
			Items: []v1alpha1.Application{
				{ObjectMeta: metav1.ObjectMeta{Name: "deleted-app"}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	argoClient := newTestArgoClient(ts)
	exec := newTestExecutorWithArgo(vcs, &mockLock{}, argoClient)

	event := &models.PREvent{
		Repo: models.RepoInfo{
			Owner: "org", Name: "repo", FullName: "org/repo",
			HTMLURL: "https://github.com/org/repo",
		},
		PR: models.PRInfo{
			Number:  1,
			HeadRef: "feature-branch",
			BaseRef: "main",
		},
	}

	affected, err := exec.findAffectedApplications(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have new-app and deleted-app
	newFound := false
	deletedFound := false
	for _, app := range affected {
		if app.Name == "new-app" && app.ChangeType == models.ApplicationNew {
			newFound = true
		}
		if app.Name == "deleted-app" && app.ChangeType == models.ApplicationDeleted {
			deletedFound = true
		}
	}
	if !newFound {
		t.Error("expected 'new-app' with ChangeType=New in affected apps")
	}
	if !deletedFound {
		t.Error("expected 'deleted-app' with ChangeType=Deleted in affected apps")
	}
}

func TestFindAffectedApplications_ModifiedAppCR(t *testing.T) {
	headYAML := `apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: my-app
spec:
  project: default
  source:
    repoURL: https://github.com/org/repo
    path: apps/my-app
    targetRevision: HEAD
  destination:
    server: https://kubernetes.default.svc
    namespace: default
`
	baseYAML := `apiVersion: argoproj.io/v1alpha1
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
			{Filename: "argocd/app.yaml", Status: "modified"},
		},
		filesByPattern: map[string]map[string][]byte{
			"feature-branch": {
				"argocd/app.yaml": []byte(headYAML),
			},
			"main": {
				"argocd/app.yaml": []byte(baseYAML),
			},
		},
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := v1alpha1.ApplicationList{Items: []v1alpha1.Application{}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	argoClient := newTestArgoClient(ts)
	exec := newTestExecutorWithArgo(vcs, &mockLock{}, argoClient)

	event := &models.PREvent{
		Repo: models.RepoInfo{
			Owner: "org", Name: "repo", FullName: "org/repo",
			HTMLURL: "https://github.com/org/repo",
		},
		PR: models.PRInfo{
			Number:  1,
			HeadRef: "feature-branch",
			BaseRef: "main",
		},
	}

	affected, err := exec.findAffectedApplications(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(affected) != 1 {
		t.Fatalf("expected 1 affected app, got %d", len(affected))
	}
	if affected[0].Name != "my-app" {
		t.Errorf("expected app name = 'my-app', got %q", affected[0].Name)
	}
	if affected[0].SourceFile != "argocd/app.yaml" {
		t.Errorf("expected SourceFile = 'argocd/app.yaml', got %q", affected[0].SourceFile)
	}
}

func TestFindAffectedApplications_WithRepoConfig(t *testing.T) {
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
			{Filename: "custom/values.yaml", Status: "modified"},
		},
		repoConfigData: []byte(`version: 1
applications:
  - name: my-app
    paths:
      - "custom/**"
cr_paths:
  - "argocd"
`),
		filesByPattern: map[string]map[string][]byte{
			"feature-branch": {
				"argocd/app.yaml": []byte(appYAML),
			},
			"main": {
				"argocd/app.yaml": []byte(appYAML),
			},
		},
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := v1alpha1.ApplicationList{Items: []v1alpha1.Application{}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	argoClient := newTestArgoClient(ts)
	exec := newTestExecutorWithArgo(vcs, &mockLock{}, argoClient)

	event := &models.PREvent{
		Repo: models.RepoInfo{
			Owner: "org", Name: "repo", FullName: "org/repo",
			HTMLURL: "https://github.com/org/repo",
		},
		PR: models.PRInfo{
			Number:  1,
			HeadRef: "feature-branch",
			BaseRef: "main",
		},
	}

	affected, err := exec.findAffectedApplications(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(affected) != 1 {
		t.Fatalf("expected 1 affected app via repo config paths, got %d", len(affected))
	}
	if affected[0].Name != "my-app" {
		t.Errorf("expected app name = 'my-app', got %q", affected[0].Name)
	}
}

func TestFindAffectedApplications_ModifiedAppAlreadyAffected(t *testing.T) {
	// App is affected by path changes AND its CR was modified — should propagate SourceFile
	headYAML := `apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: my-app
spec:
  project: default
  source:
    repoURL: https://github.com/org/repo
    path: apps/my-app
    targetRevision: HEAD
  destination:
    server: https://kubernetes.default.svc
    namespace: default
`
	baseYAML := `apiVersion: argoproj.io/v1alpha1
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
			{Filename: "argocd/app.yaml", Status: "modified"},
		},
		filesByPattern: map[string]map[string][]byte{
			"feature-branch": {
				"argocd/app.yaml": []byte(headYAML),
			},
			"main": {
				"argocd/app.yaml": []byte(baseYAML),
			},
		},
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := v1alpha1.ApplicationList{Items: []v1alpha1.Application{}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	argoClient := newTestArgoClient(ts)
	exec := newTestExecutorWithArgo(vcs, &mockLock{}, argoClient)

	event := &models.PREvent{
		Repo: models.RepoInfo{
			Owner: "org", Name: "repo", FullName: "org/repo",
			HTMLURL: "https://github.com/org/repo",
		},
		PR: models.PRInfo{
			Number:  1,
			HeadRef: "feature-branch",
			BaseRef: "main",
		},
	}

	affected, err := exec.findAffectedApplications(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(affected) != 1 {
		t.Fatalf("expected 1 affected app (deduplicated), got %d", len(affected))
	}
	if affected[0].Name != "my-app" {
		t.Errorf("expected app name = 'my-app', got %q", affected[0].Name)
	}
	// SourceFile should be propagated from the modified app detection
	if affected[0].SourceFile != "argocd/app.yaml" {
		t.Errorf("expected SourceFile = 'argocd/app.yaml', got %q", affected[0].SourceFile)
	}
}

func TestFindAffectedApplications_AppSetNewAndDeleted(t *testing.T) {
	// A new ApplicationSet in head that doesn't exist in base
	headAppSetYAML := `apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
metadata:
  name: new-appset
spec:
  generators:
    - list:
        elements:
          - cluster: dev
  template:
    metadata:
      name: '{{cluster}}-app'
    spec:
      project: default
      source:
        repoURL: https://github.com/org/repo
        path: apps
        targetRevision: main
      destination:
        server: https://kubernetes.default.svc
        namespace: default
`

	vcs := &mockVCS{
		changedFiles: []models.ChangedFile{
			{Filename: "argocd/appset.yaml", Status: "added"},
		},
		filesByPattern: map[string]map[string][]byte{
			"feature-branch": {
				"argocd/appset.yaml": []byte(headAppSetYAML),
			},
			"main": {},
		},
	}

	// ArgoCD: GenerateApplications returns generated apps, ListApplications returns empty
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/applicationsets/generate" {
			resp := struct {
				Applications []v1alpha1.Application `json:"applications"`
			}{
				Applications: []v1alpha1.Application{
					{ObjectMeta: metav1.ObjectMeta{Name: "dev-app"}},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		// ListApplications
		resp := v1alpha1.ApplicationList{Items: []v1alpha1.Application{}}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	argoClient := newTestArgoClient(ts)
	exec := newTestExecutorWithArgo(vcs, &mockLock{}, argoClient)

	event := &models.PREvent{
		Repo: models.RepoInfo{
			Owner: "org", Name: "repo", FullName: "org/repo",
			HTMLURL: "https://github.com/org/repo",
		},
		PR: models.PRInfo{
			Number:  1,
			HeadRef: "feature-branch",
			BaseRef: "main",
		},
	}

	affected, err := exec.findAffectedApplications(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should detect the generated app from the new AppSet
	found := false
	for _, app := range affected {
		if app.Name == "dev-app" {
			found = true
			if app.ChangeType != models.ApplicationNew {
				t.Errorf("expected ChangeType = %q, got %q", models.ApplicationNew, app.ChangeType)
			}
			if !app.IsGeneratedApp {
				t.Error("expected IsGeneratedApp = true")
			}
		}
	}
	if !found {
		t.Error("expected 'dev-app' from new appset in affected list")
	}
}

func TestFindAffectedApplications_UnchangedAppCRNotIncluded(t *testing.T) {
	// An app CR that exists in both branches with identical content should NOT be
	// included as affected if no source files changed.
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
			{Filename: "docs/readme.md", Status: "modified"},
		},
		filesByPattern: map[string]map[string][]byte{
			"feature-branch": {
				"argocd/app.yaml": []byte(appYAML),
			},
			"main": {
				"argocd/app.yaml": []byte(appYAML),
			},
		},
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := v1alpha1.ApplicationList{Items: []v1alpha1.Application{}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	argoClient := newTestArgoClient(ts)
	exec := newTestExecutorWithArgo(vcs, &mockLock{}, argoClient)

	event := &models.PREvent{
		Repo: models.RepoInfo{
			Owner: "org", Name: "repo", FullName: "org/repo",
			HTMLURL: "https://github.com/org/repo",
		},
		PR: models.PRInfo{
			Number:  1,
			HeadRef: "feature-branch",
			BaseRef: "main",
		},
	}

	affected, err := exec.findAffectedApplications(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(affected) != 0 {
		t.Errorf("expected 0 affected apps (unchanged CR, unrelated file changes), got %d", len(affected))
		for _, a := range affected {
			t.Logf("  affected: %s (change_type=%s)", a.Name, a.ChangeType)
		}
	}
}

func TestFindAffectedApplications_NewAppWithMatchingSourcePath(t *testing.T) {
	// A new Application CR whose source path matches changed manifest files
	// should get ChangeType=ApplicationNew, not ApplicationExisting.
	// This is the bug scenario from PR #21: new apps detected via path matching
	// in Step 3 were prematurely set to ApplicationExisting, causing Step 4
	// to skip them (already detected) and leaving them incorrectly classified.
	newAppYAML := `apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: new-app
spec:
  project: default
  source:
    repoURL: https://github.com/org/repo
    path: apps/new-app
    targetRevision: main
  destination:
    server: https://kubernetes.default.svc
    namespace: default
`

	vcs := &mockVCS{
		changedFiles: []models.ChangedFile{
			{Filename: "argocd/new-app.yaml", Status: "added"},
			{Filename: "apps/new-app/deployment.yaml", Status: "added"},
			{Filename: "apps/new-app/service.yaml", Status: "added"},
		},
		filesByPattern: map[string]map[string][]byte{
			"feature-branch": {
				"argocd/new-app.yaml": []byte(newAppYAML),
			},
			"main": {},
		},
	}

	// ArgoCD mock: ListApplications returns empty (app doesn't exist yet)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := v1alpha1.ApplicationList{Items: []v1alpha1.Application{}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	argoClient := newTestArgoClient(ts)
	exec := newTestExecutorWithArgo(vcs, &mockLock{}, argoClient)

	event := &models.PREvent{
		Repo: models.RepoInfo{
			Owner: "org", Name: "repo", FullName: "org/repo",
			HTMLURL: "https://github.com/org/repo",
		},
		PR: models.PRInfo{
			Number:  1,
			HeadRef: "feature-branch",
			BaseRef: "main",
		},
	}

	affected, err := exec.findAffectedApplications(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(affected) != 1 {
		t.Fatalf("expected 1 affected app, got %d", len(affected))
	}
	if affected[0].Name != "new-app" {
		t.Errorf("expected app name = 'new-app', got %q", affected[0].Name)
	}
	if affected[0].ChangeType != models.ApplicationNew {
		t.Errorf("expected ChangeType = %q, got %q", models.ApplicationNew, affected[0].ChangeType)
	}
}

func TestResolveValueFilePath(t *testing.T) {
	tests := []struct {
		name       string
		sourcePath string
		valueFile  string
		want       string
	}{
		{
			name:       "absolute path strips leading slash",
			sourcePath: "charts/app",
			valueFile:  "/values.yaml",
			want:       "values.yaml",
		},
		{
			name:       "absolute path with subdirectory",
			sourcePath: "charts/app",
			valueFile:  "/common/values.yaml",
			want:       "common/values.yaml",
		},
		{
			name:       "relative path joined with source path",
			sourcePath: "charts/app",
			valueFile:  "values-prod.yaml",
			want:       "charts/app/values-prod.yaml",
		},
		{
			name:       "relative path with parent directory",
			sourcePath: "charts/app",
			valueFile:  "../common/values.yaml",
			want:       "charts/common/values.yaml",
		},
		{
			name:       "empty source path",
			sourcePath: "",
			valueFile:  "values.yaml",
			want:       "values.yaml",
		},
		{
			name:       "dot source path",
			sourcePath: ".",
			valueFile:  "values.yaml",
			want:       "values.yaml",
		},
		{
			name:       "nested relative path",
			sourcePath: "deploy/envs/prod",
			valueFile:  "overrides/values.yaml",
			want:       "deploy/envs/prod/overrides/values.yaml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveValueFilePath(tt.sourcePath, tt.valueFile)
			if got != tt.want {
				t.Errorf("resolveValueFilePath(%q, %q) = %q, want %q", tt.sourcePath, tt.valueFile, got, tt.want)
			}
		})
	}
}

func TestFileMatchesAppSources(t *testing.T) {
	tests := []struct {
		name    string
		app     models.Application
		repoURL string
		file    string
		want    bool
	}{
		{
			name:    "single-source path match",
			app:     models.Application{Path: "apps/my-app", RepoURL: "https://github.com/org/repo"},
			repoURL: "https://github.com/org/repo",
			file:    "apps/my-app/deployment.yaml",
			want:    true,
		},
		{
			name:    "single-source path no match",
			app:     models.Application{Path: "apps/my-app", RepoURL: "https://github.com/org/repo"},
			repoURL: "https://github.com/org/repo",
			file:    "apps/other/deployment.yaml",
			want:    false,
		},
		{
			name:    "single-source empty path",
			app:     models.Application{Path: "", RepoURL: "https://github.com/org/repo"},
			repoURL: "https://github.com/org/repo",
			file:    "anything.yaml",
			want:    false,
		},
		{
			name: "multi-source path match first source",
			app: models.Application{
				Sources: []models.ApplicationSource{
					{RepoURL: "https://github.com/org/repo", Path: "apps/frontend"},
					{RepoURL: "https://github.com/org/repo", Path: "apps/backend"},
				},
			},
			repoURL: "https://github.com/org/repo",
			file:    "apps/frontend/index.html",
			want:    true,
		},
		{
			name: "multi-source different repo not matched",
			app: models.Application{
				Sources: []models.ApplicationSource{
					{RepoURL: "https://github.com/other/repo", Path: "charts/app"},
					{RepoURL: "https://github.com/org/repo", Path: "deploy"},
				},
			},
			repoURL: "https://github.com/org/repo",
			file:    "charts/app/Chart.yaml",
			want:    false,
		},
		{
			name: "multi-source helm valueFile relative",
			app: models.Application{
				Sources: []models.ApplicationSource{
					{
						RepoURL: "https://github.com/org/repo",
						Path:    "deploy/prod",
						Helm:    &models.HelmSource{ValueFiles: []string{"values.yaml", "values-override.yaml"}},
					},
				},
			},
			repoURL: "https://github.com/org/repo",
			file:    "deploy/prod/values-override.yaml",
			want:    true,
		},
		{
			name: "multi-source helm valueFile absolute",
			app: models.Application{
				Sources: []models.ApplicationSource{
					{
						RepoURL: "https://github.com/org/repo",
						Path:    "deploy/prod",
						Helm:    &models.HelmSource{ValueFiles: []string{"/shared/values.yaml"}},
					},
				},
			},
			repoURL: "https://github.com/org/repo",
			file:    "shared/values.yaml",
			want:    true,
		},
		{
			name: "multi-source helm $ref resolved to matching repo",
			app: models.Application{
				Sources: []models.ApplicationSource{
					{
						RepoURL:        "https://github.com/org/repo",
						TargetRevision: "main",
						Ref:            "values",
					},
					{
						RepoURL:        "https://helm.example.io",
						Chart:          "mychart",
						TargetRevision: "1.0.0",
						Helm:           &models.HelmSource{ValueFiles: []string{"$values/apps/myapp/values.yaml"}},
					},
				},
			},
			repoURL: "https://github.com/org/repo",
			file:    "apps/myapp/values.yaml",
			want:    true,
		},
		{
			name: "multi-source helm $ref resolved to different repo",
			app: models.Application{
				Sources: []models.ApplicationSource{
					{
						RepoURL:        "https://github.com/other-org/other-repo",
						TargetRevision: "main",
						Ref:            "values",
					},
					{
						RepoURL:        "https://helm.example.io",
						Chart:          "mychart",
						TargetRevision: "1.0.0",
						Helm:           &models.HelmSource{ValueFiles: []string{"$values/apps/myapp/values.yaml"}},
					},
				},
			},
			repoURL: "https://github.com/org/repo",
			file:    "apps/myapp/values.yaml",
			want:    false,
		},
		{
			name: "multi-source helm $ref with no matching ref source",
			app: models.Application{
				Sources: []models.ApplicationSource{
					{
						RepoURL:        "https://github.com/org/repo",
						TargetRevision: "main",
						// No Ref field set
					},
					{
						RepoURL:        "https://helm.example.io",
						Chart:          "mychart",
						TargetRevision: "1.0.0",
						Helm:           &models.HelmSource{ValueFiles: []string{"$values/common.yaml"}},
					},
				},
			},
			repoURL: "https://github.com/org/repo",
			file:    "common.yaml",
			want:    false,
		},
		{
			name: "harbor-like: ref-only source + helm chart with $ref valueFile",
			app: models.Application{
				Sources: []models.ApplicationSource{
					{
						RepoURL:        "https://github.com/org/argocd-gitops.git",
						TargetRevision: "main",
						Ref:            "values",
					},
					{
						RepoURL:        "https://helm.goharbor.io",
						Chart:          "harbor",
						TargetRevision: "1.16.1",
						Helm:           &models.HelmSource{ValueFiles: []string{"$values/apps/harbor/values.yaml"}},
					},
					{
						RepoURL:        "https://github.com/org/argocd-gitops.git",
						TargetRevision: "main",
						Path:           "apps/harbor/manifests",
					},
				},
			},
			repoURL: "https://github.com/org/argocd-gitops",
			file:    "apps/harbor/values.yaml",
			want:    true,
		},
		{
			name: "multi-source helm $ref bare ref name without path",
			app: models.Application{
				Sources: []models.ApplicationSource{
					{
						RepoURL: "https://github.com/org/repo",
						Ref:     "values",
					},
					{
						RepoURL: "https://helm.example.io",
						Chart:   "mychart",
						Helm:    &models.HelmSource{ValueFiles: []string{"$values"}},
					},
				},
			},
			repoURL: "https://github.com/org/repo",
			file:    "values",
			want:    false,
		},
		{
			name: "multi-source helm valueFile with parent dir",
			app: models.Application{
				Sources: []models.ApplicationSource{
					{
						RepoURL: "https://github.com/org/repo",
						Path:    "deploy/envs/prod",
						Helm:    &models.HelmSource{ValueFiles: []string{"../../common/values.yaml"}},
					},
				},
			},
			repoURL: "https://github.com/org/repo",
			file:    "deploy/common/values.yaml",
			want:    true,
		},
		{
			name: "multi-source chart-only no path no match",
			app: models.Application{
				Sources: []models.ApplicationSource{
					{RepoURL: "https://github.com/org/repo", Chart: "mychart"},
				},
			},
			repoURL: "https://github.com/org/repo",
			file:    "anything.yaml",
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fileMatchesAppSources(tt.app, tt.repoURL, tt.file)
			if got != tt.want {
				t.Errorf("fileMatchesAppSources() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMatchByRepoConfig(t *testing.T) {
	e := &Executor{}

	tests := []struct {
		name       string
		app        models.Application
		files      []string
		repoConfig *config.RepoConfig
		wantNil    bool // true if matchByRepoConfig should return nil (not handled)
		wantResult bool // only checked when wantNil is false
	}{
		{
			name:       "nil config returns nil",
			app:        models.Application{Name: "my-app"},
			files:      []string{"apps/my-app/deploy.yaml"},
			repoConfig: nil,
			wantNil:    true,
		},
		{
			name:  "exact mapping matches - returns true",
			app:   models.Application{Name: "my-app"},
			files: []string{"apps/my-app/deploy.yaml"},
			repoConfig: &config.RepoConfig{
				Applications: []config.ApplicationMapping{
					{Name: "my-app", Paths: []string{"apps/my-app/**"}},
				},
			},
			wantResult: true,
		},
		{
			name:  "exact mapping exists but paths dont match - returns false",
			app:   models.Application{Name: "my-app"},
			files: []string{"other/file.yaml"},
			repoConfig: &config.RepoConfig{
				Applications: []config.ApplicationMapping{
					{Name: "my-app", Paths: []string{"apps/my-app/**"}},
				},
			},
			wantResult: false,
		},
		{
			name:  "wildcard matches when no exact mapping exists",
			app:   models.Application{Name: "grafana"},
			files: []string{"apps/grafana/values.yaml"},
			repoConfig: &config.RepoConfig{
				Applications: []config.ApplicationMapping{
					{Name: "*", Paths: []string{"apps/**"}},
				},
			},
			wantResult: true,
		},
		{
			name:  "no matching mapping returns nil",
			app:   models.Application{Name: "unknown-app"},
			files: []string{"apps/grafana/values.yaml"},
			repoConfig: &config.RepoConfig{
				Applications: []config.ApplicationMapping{
					{Name: "my-app", Paths: []string{"apps/my-app/**"}},
				},
			},
			wantNil: true,
		},
		{
			name:  "exact mapping takes precedence - blocks wildcard",
			app:   models.Application{Name: "sealed-secrets"},
			files: []string{"apps/grafana/values.yaml"},
			repoConfig: &config.RepoConfig{
				Applications: []config.ApplicationMapping{
					{Name: "sealed-secrets", Paths: []string{"bootstrap/sealed-secret/**"}},
					{Name: "*", Paths: []string{"apps/**"}},
				},
			},
			wantResult: false, // exact mapping exists, paths don't match, wildcard blocked
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := e.matchByRepoConfig(tt.app, tt.files, tt.repoConfig)
			if tt.wantNil {
				if got != nil {
					t.Errorf("matchByRepoConfig() = %v, want nil", *got)
				}
				return
			}
			if got == nil {
				t.Fatalf("matchByRepoConfig() = nil, want %v", tt.wantResult)
			}
			if *got != tt.wantResult {
				t.Errorf("matchByRepoConfig() = %v, want %v", *got, tt.wantResult)
			}
		})
	}
}

func TestAppReferencesRepo(t *testing.T) {
	e := &Executor{}

	tests := []struct {
		name    string
		app     models.Application
		repoURL string
		want    bool
	}{
		{
			name:    "matching repo URL",
			app:     models.Application{Name: "my-app", RepoURL: "https://github.com/org/repo"},
			repoURL: "https://github.com/org/repo",
			want:    true,
		},
		{
			name:    "matching with .git suffix",
			app:     models.Application{Name: "my-app", RepoURL: "https://github.com/org/repo.git"},
			repoURL: "https://github.com/org/repo",
			want:    true,
		},
		{
			name:    "non-matching repo",
			app:     models.Application{Name: "my-app", RepoURL: "https://github.com/org/other-repo"},
			repoURL: "https://github.com/org/repo",
			want:    false,
		},
		{
			name:    "no repo URL on app",
			app:     models.Application{Name: "my-app"},
			repoURL: "https://github.com/org/repo",
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := e.appReferencesRepo(tt.app, tt.repoURL); got != tt.want {
				t.Errorf("appReferencesRepo() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExpandAppSetMappings_NilConfig(t *testing.T) {
	e := &Executor{}
	alreadyDetected := make(map[string]bool)

	result := e.expandAppSetMappings(context.Background(), nil, []string{"file.yaml"}, alreadyDetected)
	if len(result) != 0 {
		t.Errorf("expected no results for nil config, got %d", len(result))
	}
}

func TestExpandAppSetMappings_NoAppSetMappings(t *testing.T) {
	e := &Executor{}
	alreadyDetected := make(map[string]bool)
	repoConfig := &config.RepoConfig{
		Applications: []config.ApplicationMapping{
			{Name: "my-app", Paths: []string{"apps/**"}},
		},
	}

	result := e.expandAppSetMappings(context.Background(), repoConfig, []string{"apps/my-app/deploy.yaml"}, alreadyDetected)
	if len(result) != 0 {
		t.Errorf("expected no results when no appset mappings, got %d", len(result))
	}
}

func TestExpandAppSetMappings_NoMatchingPaths(t *testing.T) {
	e := newTestExecutor(&mockVCS{}, &mockLock{}, nil)
	alreadyDetected := make(map[string]bool)
	repoConfig := &config.RepoConfig{
		Applications: []config.ApplicationMapping{
			{Name: "my-appset", ApplicationSet: "my-appset", Paths: []string{"apps/**"}},
		},
	}

	// Changed files don't match the mapping paths
	result := e.expandAppSetMappings(context.Background(), repoConfig, []string{"other/file.yaml"}, alreadyDetected)
	if len(result) != 0 {
		t.Errorf("expected no results when paths don't match, got %d", len(result))
	}
}

func TestExpandAppSetMappings_SkipsNonAppSetMappings(t *testing.T) {
	e := newTestExecutor(&mockVCS{}, &mockLock{}, nil)
	alreadyDetected := make(map[string]bool)
	repoConfig := &config.RepoConfig{
		Applications: []config.ApplicationMapping{
			{Name: "my-app", Paths: []string{"apps/**"}}, // No ApplicationSet field
		},
	}

	result := e.expandAppSetMappings(context.Background(), repoConfig, []string{"apps/values.yaml"}, alreadyDetected)
	if len(result) != 0 {
		t.Errorf("expected no results for non-appset mappings, got %d", len(result))
	}
}

func TestExpandAppSetMappings_PositiveMatch(t *testing.T) {
	// Mock ArgoCD server that returns an ApplicationSet and its generated apps
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v1/applicationsets/my-appset":
			// GetApplicationSet
			resp := v1alpha1.ApplicationSet{
				ObjectMeta: metav1.ObjectMeta{Name: "my-appset"},
			}
			_ = json.NewEncoder(w).Encode(resp)
		case r.URL.Path == "/api/v1/applications" && r.URL.Query().Get("selector") != "":
			// ListApplicationsWithSelector — returns apps belonging to the appset
			resp := v1alpha1.ApplicationList{
				Items: []v1alpha1.Application{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "app-dev",
							OwnerReferences: []metav1.OwnerReference{
								{Kind: "ApplicationSet", Name: "my-appset"},
							},
						},
						Spec: v1alpha1.ApplicationSpec{
							Source: &v1alpha1.ApplicationSource{
								RepoURL: "https://github.com/org/repo",
								Path:    "apps/dev",
							},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "app-staging",
							OwnerReferences: []metav1.OwnerReference{
								{Kind: "ApplicationSet", Name: "my-appset"},
							},
						},
						Spec: v1alpha1.ApplicationSpec{
							Source: &v1alpha1.ApplicationSource{
								RepoURL: "https://github.com/org/repo",
								Path:    "apps/staging",
							},
						},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	argoClient := newTestArgoClient(ts)
	e := newTestExecutorWithArgo(&mockVCS{}, &mockLock{}, argoClient)
	alreadyDetected := make(map[string]bool)

	repoConfig := &config.RepoConfig{
		Applications: []config.ApplicationMapping{
			{Name: "my-appset", ApplicationSet: "my-appset", Paths: []string{"apps/**"}},
		},
	}

	result := e.expandAppSetMappings(context.Background(), repoConfig, []string{"apps/dev/values.yaml"}, alreadyDetected)

	if len(result) != 2 {
		t.Fatalf("expected 2 expanded apps, got %d", len(result))
	}
	for _, app := range result {
		if app.ChangeType != models.ApplicationExisting {
			t.Errorf("app %s ChangeType = %q, want %q", app.Name, app.ChangeType, models.ApplicationExisting)
		}
	}
	if !alreadyDetected["app-dev"] || !alreadyDetected["app-staging"] {
		t.Error("expected both apps to be marked in alreadyDetected")
	}
}

func TestExpandAppSetMappings_SkipsDuplicates(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v1/applicationsets/my-appset":
			resp := v1alpha1.ApplicationSet{
				ObjectMeta: metav1.ObjectMeta{Name: "my-appset"},
			}
			_ = json.NewEncoder(w).Encode(resp)
		case r.URL.Path == "/api/v1/applications" && r.URL.Query().Get("selector") != "":
			resp := v1alpha1.ApplicationList{
				Items: []v1alpha1.Application{
					{
						ObjectMeta: metav1.ObjectMeta{Name: "app-dev"},
						Spec: v1alpha1.ApplicationSpec{
							Source: &v1alpha1.ApplicationSource{
								RepoURL: "https://github.com/org/repo",
								Path:    "apps/dev",
							},
						},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	argoClient := newTestArgoClient(ts)
	e := newTestExecutorWithArgo(&mockVCS{}, &mockLock{}, argoClient)

	// Pre-populate alreadyDetected — app-dev should be skipped
	alreadyDetected := map[string]bool{"app-dev": true}
	repoConfig := &config.RepoConfig{
		Applications: []config.ApplicationMapping{
			{Name: "my-appset", ApplicationSet: "my-appset", Paths: []string{"apps/**"}},
		},
	}

	result := e.expandAppSetMappings(context.Background(), repoConfig, []string{"apps/dev/values.yaml"}, alreadyDetected)

	if len(result) != 0 {
		t.Errorf("expected 0 apps (all duplicates), got %d", len(result))
	}
}

func TestUpdateOrAppendAffected(t *testing.T) {
	t.Run("appends new app", func(t *testing.T) {
		affected := []models.Application{
			{Name: "existing-app", ChangeType: models.ApplicationExisting},
		}
		detected := map[string]bool{"existing-app": true}
		newApp := models.Application{Name: "new-app", ChangeType: models.ApplicationNew, SourceFile: "apps/new.yaml"}

		result := updateOrAppendAffected(affected, newApp, detected)

		if len(result) != 2 {
			t.Fatalf("expected 2 apps, got %d", len(result))
		}
		if result[1].Name != "new-app" {
			t.Errorf("expected new-app, got %s", result[1].Name)
		}
		if !detected["new-app"] {
			t.Error("expected new-app to be marked as detected")
		}
	})

	t.Run("updates existing app", func(t *testing.T) {
		affected := []models.Application{
			{Name: "my-app", ChangeType: models.ApplicationExisting},
		}
		detected := map[string]bool{"my-app": true}
		update := models.Application{Name: "my-app", ChangeType: models.ApplicationNew, SourceFile: "apps/my-app.yaml"}

		result := updateOrAppendAffected(affected, update, detected)

		if len(result) != 1 {
			t.Fatalf("expected 1 app, got %d", len(result))
		}
		if result[0].ChangeType != models.ApplicationNew {
			t.Errorf("expected ChangeType %q, got %q", models.ApplicationNew, result[0].ChangeType)
		}
		if result[0].SourceFile != "apps/my-app.yaml" {
			t.Errorf("expected SourceFile %q, got %q", "apps/my-app.yaml", result[0].SourceFile)
		}
	})

	t.Run("preserves existing source file", func(t *testing.T) {
		affected := []models.Application{
			{Name: "my-app", ChangeType: models.ApplicationExisting, SourceFile: "original.yaml"},
		}
		detected := map[string]bool{"my-app": true}
		update := models.Application{Name: "my-app", ChangeType: models.ApplicationNew, SourceFile: "new.yaml"}

		result := updateOrAppendAffected(affected, update, detected)

		if result[0].SourceFile != "original.yaml" {
			t.Errorf("expected SourceFile to be preserved as %q, got %q", "original.yaml", result[0].SourceFile)
		}
	})
}
