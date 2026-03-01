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
	"testing"

	v1alpha1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/org/lemuria/internal/argocd"
	"github.com/org/lemuria/internal/config"
	"github.com/org/lemuria/internal/models"
	"github.com/org/lemuria/pkg/diff"
)

func TestScanRepoForApplications(t *testing.T) {
	appYAML := `apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: my-app
spec:
  destination:
    server: https://kubernetes.default.svc
    namespace: default
  project: default
  source:
    repoURL: https://github.com/org/repo
    path: apps/my-app
    targetRevision: main
`

	appSetYAML := `apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
metadata:
  name: my-appset
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
		filesByPattern: map[string]map[string][]byte{
			"feature-branch": {
				"argocd/app.yaml":    []byte(appYAML),
				"argocd/appset.yaml": []byte(appSetYAML),
			},
			"main": {
				"argocd/app.yaml": []byte(appYAML),
			},
		},
	}

	exec := newTestExecutor(vcs, &mockLock{}, nil)

	event := &models.PREvent{
		Repo: models.RepoInfo{
			Owner: "org", Name: "repo", FullName: "org/repo",
		},
		PR: models.PRInfo{
			HeadRef: "feature-branch",
			BaseRef: "main",
		},
	}

	scanned, err := exec.scanRepoForApplications(context.Background(), event, []string{"argocd"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(scanned.HeadApps) != 1 {
		t.Errorf("expected 1 head app, got %d", len(scanned.HeadApps))
	}
	if len(scanned.HeadAppSets) != 1 {
		t.Errorf("expected 1 head appset, got %d", len(scanned.HeadAppSets))
	}
	if scanned.HeadAppSets[0].SourceFile != "argocd/appset.yaml" {
		t.Errorf("expected head appset source file = 'argocd/appset.yaml', got %q", scanned.HeadAppSets[0].SourceFile)
	}
	if len(scanned.BaseApps) != 1 {
		t.Errorf("expected 1 base app, got %d", len(scanned.BaseApps))
	}
	if len(scanned.BaseAppSets) != 0 {
		t.Errorf("expected 0 base appsets, got %d", len(scanned.BaseAppSets))
	}
}

func TestScanRepoForApplications_NoCRPaths(t *testing.T) {
	appYAML := `apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: my-app
spec:
  destination:
    server: https://kubernetes.default.svc
    namespace: default
  project: default
  source:
    repoURL: https://github.com/org/repo
    path: apps/my-app
    targetRevision: main
`

	vcs := &mockVCS{
		filesByPattern: map[string]map[string][]byte{
			"feature-branch": {
				"apps/app.yaml": []byte(appYAML),
			},
			"main": {},
		},
	}

	exec := newTestExecutor(vcs, &mockLock{}, nil)

	event := &models.PREvent{
		Repo: models.RepoInfo{Owner: "org", Name: "repo"},
		PR:   models.PRInfo{HeadRef: "feature-branch", BaseRef: "main"},
	}

	scanned, err := exec.scanRepoForApplications(context.Background(), event, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(scanned.HeadApps) != 1 {
		t.Errorf("expected 1 head app, got %d", len(scanned.HeadApps))
	}
}

func TestScanRepoForApplications_DeterministicOrder(t *testing.T) {
	appYAML1 := `apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: app-a
spec:
  project: default
  source:
    repoURL: https://github.com/org/repo
    path: apps/a
    targetRevision: main
  destination:
    server: https://kubernetes.default.svc
    namespace: default
`
	appYAML2 := `apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: app-b
spec:
  project: default
  source:
    repoURL: https://github.com/org/repo
    path: apps/b
    targetRevision: main
  destination:
    server: https://kubernetes.default.svc
    namespace: default
`

	vcs := &mockVCS{
		filesByPattern: map[string]map[string][]byte{
			"head": {
				"z-dir/app.yaml": []byte(appYAML2),
				"a-dir/app.yaml": []byte(appYAML1),
			},
			"base": {},
		},
	}

	exec := newTestExecutor(vcs, &mockLock{}, nil)
	event := &models.PREvent{
		Repo: models.RepoInfo{Owner: "org", Name: "repo"},
		PR:   models.PRInfo{HeadRef: "head", BaseRef: "base"},
	}

	// Run multiple times to verify deterministic order
	for i := 0; i < 5; i++ {
		scanned, err := exec.scanRepoForApplications(context.Background(), event, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(scanned.HeadApps) != 2 {
			t.Fatalf("expected 2 head apps, got %d", len(scanned.HeadApps))
		}
		// Sorted by file path: a-dir/app.yaml < z-dir/app.yaml
		if scanned.HeadApps[0].Name != "app-a" {
			t.Errorf("iteration %d: expected first app = 'app-a', got %q", i, scanned.HeadApps[0].Name)
		}
		if scanned.HeadApps[1].Name != "app-b" {
			t.Errorf("iteration %d: expected second app = 'app-b', got %q", i, scanned.HeadApps[1].Name)
		}
	}
}

// newTestArgoClient creates a test ArgoCD client pointing at a test HTTP server.
func newTestArgoClient(ts *httptest.Server) *argocd.Client {
	client, _ := argocd.NewClient(config.ArgoCDConfig{
		ServerURL: ts.URL,
		Token:     "test-token",
		Insecure:  true,
	})
	return client
}

// newTestExecutorWithArgo creates an Executor with a mock ArgoCD client for integration tests.
func newTestExecutorWithArgo(vcs *mockVCS, lock *mockLock, argoClient *argocd.Client) *Executor {
	return &Executor{
		vcs:      vcs,
		argocd:   argoClient,
		lock:     lock,
		config:   &config.Config{},
		renderer: diff.NewRenderer(),
	}
}

func TestDetectApplicationSetChangesFromScan_NewAppSet(t *testing.T) {
	// Mock ArgoCD server that responds to GenerateApplications
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/applicationsets/generate" {
			resp := struct {
				Applications []v1alpha1.Application `json:"applications"`
			}{
				Applications: []v1alpha1.Application{
					{ObjectMeta: metav1.ObjectMeta{Name: "dev-app"}},
					{ObjectMeta: metav1.ObjectMeta{Name: "staging-app"}},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	argoClient := newTestArgoClient(ts)
	exec := newTestExecutorWithArgo(&mockVCS{}, &mockLock{}, argoClient)

	appSet := v1alpha1.ApplicationSet{
		ObjectMeta: metav1.ObjectMeta{Name: "my-appset"},
	}

	scanned := &ScannedRepoApps{
		HeadAppSets: []argocd.ParsedAppSet{
			{AppSet: &appSet, SourceFile: "argocd/appset.yaml"},
		},
		BaseAppSets:    []argocd.ParsedAppSet{},
		HeadAppContent: map[string][]byte{},
		BaseAppContent: map[string][]byte{},
	}

	result, err := exec.detectApplicationSetChangesFromScan(context.Background(), scanned)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.NewApps) != 2 {
		t.Fatalf("expected 2 new apps, got %d", len(result.NewApps))
	}
	for _, app := range result.NewApps {
		if app.ChangeType != models.ApplicationNew {
			t.Errorf("app %s: expected ChangeType = %q, got %q", app.Name, models.ApplicationNew, app.ChangeType)
		}
		if app.ApplicationSetName != "my-appset" {
			t.Errorf("app %s: expected ApplicationSetName = 'my-appset', got %q", app.Name, app.ApplicationSetName)
		}
		if app.SourceFile != "argocd/appset.yaml" {
			t.Errorf("app %s: expected SourceFile = 'argocd/appset.yaml', got %q", app.Name, app.SourceFile)
		}
	}
}

func TestDetectApplicationSetChangesFromScan_DeletedAppSet(t *testing.T) {
	// Mock ArgoCD server: ListApplications for GetApplicationsByApplicationSet
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/applications" {
			resp := v1alpha1.ApplicationList{
				Items: []v1alpha1.Application{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:            "existing-app",
							OwnerReferences: []metav1.OwnerReference{{Kind: "ApplicationSet", Name: "old-appset"}},
						},
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	argoClient := newTestArgoClient(ts)
	exec := newTestExecutorWithArgo(&mockVCS{}, &mockLock{}, argoClient)

	appSet := v1alpha1.ApplicationSet{
		ObjectMeta: metav1.ObjectMeta{Name: "old-appset"},
	}

	scanned := &ScannedRepoApps{
		HeadAppSets: []argocd.ParsedAppSet{},
		BaseAppSets: []argocd.ParsedAppSet{
			{AppSet: &appSet, SourceFile: "argocd/old-appset.yaml"},
		},
		HeadAppContent: map[string][]byte{},
		BaseAppContent: map[string][]byte{},
	}

	result, err := exec.detectApplicationSetChangesFromScan(context.Background(), scanned)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.DeletedApps) != 1 {
		t.Fatalf("expected 1 deleted app, got %d", len(result.DeletedApps))
	}
	if result.DeletedApps[0].ChangeType != models.ApplicationDeleted {
		t.Errorf("expected ChangeType = %q, got %q", models.ApplicationDeleted, result.DeletedApps[0].ChangeType)
	}
	if result.DeletedApps[0].SourceFile != "argocd/old-appset.yaml" {
		t.Errorf("expected SourceFile = 'argocd/old-appset.yaml', got %q", result.DeletedApps[0].SourceFile)
	}
}

func TestDetectApplicationSetChangesFromScan_ModifiedAppSet(t *testing.T) {
	generateCallCount := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/applicationsets/generate" {
			generateCallCount++
			var apps []v1alpha1.Application
			if generateCallCount == 1 {
				// Head version: has new-app and common-app
				apps = []v1alpha1.Application{
					{ObjectMeta: metav1.ObjectMeta{Name: "new-app"}},
					{ObjectMeta: metav1.ObjectMeta{Name: "common-app"}},
				}
			} else {
				// Base version: has removed-app and common-app
				apps = []v1alpha1.Application{
					{ObjectMeta: metav1.ObjectMeta{Name: "removed-app"}},
					{ObjectMeta: metav1.ObjectMeta{Name: "common-app"}},
				}
			}
			resp := struct {
				Applications []v1alpha1.Application `json:"applications"`
			}{Applications: apps}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	argoClient := newTestArgoClient(ts)
	exec := newTestExecutorWithArgo(&mockVCS{}, &mockLock{}, argoClient)

	headAppSet := v1alpha1.ApplicationSet{ObjectMeta: metav1.ObjectMeta{Name: "shared-appset"}}
	baseAppSet := v1alpha1.ApplicationSet{ObjectMeta: metav1.ObjectMeta{Name: "shared-appset"}}

	scanned := &ScannedRepoApps{
		HeadAppSets: []argocd.ParsedAppSet{
			{AppSet: &headAppSet, SourceFile: "argocd/appset.yaml"},
		},
		BaseAppSets: []argocd.ParsedAppSet{
			{AppSet: &baseAppSet, SourceFile: "argocd/appset.yaml"},
		},
		HeadAppContent: map[string][]byte{},
		BaseAppContent: map[string][]byte{},
	}

	result, err := exec.detectApplicationSetChangesFromScan(context.Background(), scanned)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Modified) != 1 {
		t.Fatalf("expected 1 modified appset, got %d", len(result.Modified))
	}
	mod := result.Modified[0]
	if mod.Name != "shared-appset" {
		t.Errorf("expected modified appset name = 'shared-appset', got %q", mod.Name)
	}
	if mod.SourceFile != "argocd/appset.yaml" {
		t.Errorf("expected SourceFile = 'argocd/appset.yaml', got %q", mod.SourceFile)
	}
	if len(mod.NewApps) != 1 || mod.NewApps[0].Name != "new-app" {
		t.Errorf("expected 1 new app 'new-app', got %v", mod.NewApps)
	}
	if len(mod.RemovedApps) != 1 || mod.RemovedApps[0].Name != "removed-app" {
		t.Errorf("expected 1 removed app 'removed-app', got %v", mod.RemovedApps)
	}
	// Verify SourceFile propagation to generated apps
	for _, app := range mod.NewApps {
		if app.SourceFile != "argocd/appset.yaml" {
			t.Errorf("new app %s: expected SourceFile = 'argocd/appset.yaml', got %q", app.Name, app.SourceFile)
		}
	}
}

func TestDetectApplicationSetChangesFromScan_NoChanges(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/applicationsets/generate" {
			// Same apps for both head and base
			resp := struct {
				Applications []v1alpha1.Application `json:"applications"`
			}{
				Applications: []v1alpha1.Application{
					{ObjectMeta: metav1.ObjectMeta{Name: "app-1"}},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	argoClient := newTestArgoClient(ts)
	exec := newTestExecutorWithArgo(&mockVCS{}, &mockLock{}, argoClient)

	appSet := v1alpha1.ApplicationSet{ObjectMeta: metav1.ObjectMeta{Name: "stable-appset"}}

	scanned := &ScannedRepoApps{
		HeadAppSets: []argocd.ParsedAppSet{{AppSet: &appSet, SourceFile: "as.yaml"}},
		BaseAppSets: []argocd.ParsedAppSet{{AppSet: appSet.DeepCopy(), SourceFile: "as.yaml"}},
	}

	result, err := exec.detectApplicationSetChangesFromScan(context.Background(), scanned)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.NewApps) != 0 {
		t.Errorf("expected 0 new apps, got %d", len(result.NewApps))
	}
	if len(result.DeletedApps) != 0 {
		t.Errorf("expected 0 deleted apps, got %d", len(result.DeletedApps))
	}
	if len(result.Modified) != 0 {
		t.Errorf("expected 0 modified appsets, got %d", len(result.Modified))
	}
}

func TestDetectApplicationSetChangesFromScan_GenerateError(t *testing.T) {
	// ArgoCD returns error for GenerateApplications
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	argoClient := newTestArgoClient(ts)
	exec := newTestExecutorWithArgo(&mockVCS{}, &mockLock{}, argoClient)

	appSet := v1alpha1.ApplicationSet{ObjectMeta: metav1.ObjectMeta{Name: "fail-appset"}}

	scanned := &ScannedRepoApps{
		HeadAppSets: []argocd.ParsedAppSet{
			{AppSet: &appSet, SourceFile: "as.yaml"},
		},
		BaseAppSets: []argocd.ParsedAppSet{},
	}

	result, err := exec.detectApplicationSetChangesFromScan(context.Background(), scanned)
	if err != nil {
		t.Fatalf("unexpected error (should be logged, not returned): %v", err)
	}
	// Should have 0 new apps due to generate failure
	if len(result.NewApps) != 0 {
		t.Errorf("expected 0 new apps on generate error, got %d", len(result.NewApps))
	}
}

func TestDetectCrossRepoAffectedApps(t *testing.T) {
	existingApps := []models.Application{
		{Name: "cross-repo-app", RepoURL: "https://github.com/org/repo.git", Path: "deploy/app"},
		{Name: "already-detected", RepoURL: "https://github.com/org/repo.git", Path: "deploy/other"},
		{Name: "different-repo", RepoURL: "https://github.com/other-org/other-repo.git", Path: "deploy"},
		{Name: "no-path-match", RepoURL: "https://github.com/org/repo.git", Path: "unrelated/dir"},
	}

	alreadyDetected := map[string]bool{
		"already-detected": true,
	}
	filePaths := []string{"deploy/app/values.yaml", "deploy/app/templates/deployment.yaml"}

	affected := detectCrossRepoAffectedApps(
		"https://github.com/org/repo",
		filePaths,
		alreadyDetected,
		existingApps,
	)

	if len(affected) != 1 {
		t.Fatalf("expected 1 affected app, got %d", len(affected))
	}
	if affected[0].Name != "cross-repo-app" {
		t.Errorf("expected affected app = 'cross-repo-app', got %q", affected[0].Name)
	}
	if affected[0].ChangeType != models.ApplicationExisting {
		t.Errorf("expected ChangeType = %q, got %q", models.ApplicationExisting, affected[0].ChangeType)
	}
}

func TestDetectCrossRepoAffectedApps_EmptyExistingApps(t *testing.T) {
	affected := detectCrossRepoAffectedApps(
		"https://github.com/org/repo", []string{"file.yaml"}, nil, nil,
	)
	if len(affected) != 0 {
		t.Errorf("expected 0 affected apps with nil existingApps, got %d", len(affected))
	}
}

func TestDetectCrossRepoAffectedApps_NoApps(t *testing.T) {
	affected := detectCrossRepoAffectedApps(
		"https://github.com/org/repo", []string{"file.yaml"}, nil, []models.Application{},
	)
	if len(affected) != 0 {
		t.Errorf("expected 0 affected apps, got %d", len(affected))
	}
}

func TestVerifyNewAppsExist(t *testing.T) {
	existingByName := map[string]bool{
		"existing-new-app": true,
	}

	parsed := &argocd.ParsedApplications{
		New: []models.Application{
			{Name: "existing-new-app"},
			{Name: "truly-new-app"},
		},
	}

	verifyNewAppsExist(parsed, existingByName)
	// verifyNewAppsExist only logs, doesn't modify parsed — check no crash
	if len(parsed.New) != 2 {
		t.Errorf("expected 2 new apps unchanged, got %d", len(parsed.New))
	}
}

func TestVerifyNewAppsExist_EmptyList(t *testing.T) {
	parsed := &argocd.ParsedApplications{New: nil}
	verifyNewAppsExist(parsed, nil)
}

func TestVerifyDeletedAppsExist(t *testing.T) {
	existingByName := map[string]bool{
		"real-deleted-app": true,
	}

	parsed := &argocd.ParsedApplications{
		Deleted: []models.Application{
			{Name: "real-deleted-app"},
			{Name: "phantom-deleted-app"},
		},
	}

	verifyDeletedAppsExist(parsed, existingByName)
	// Should filter out phantom-deleted-app
	if len(parsed.Deleted) != 1 {
		t.Fatalf("expected 1 deleted app (filtered), got %d", len(parsed.Deleted))
	}
	if parsed.Deleted[0].Name != "real-deleted-app" {
		t.Errorf("expected 'real-deleted-app', got %q", parsed.Deleted[0].Name)
	}
}

func TestVerifyDeletedAppsExist_EmptyList(t *testing.T) {
	parsed := &argocd.ParsedApplications{Deleted: nil}
	verifyDeletedAppsExist(parsed, nil)
}

func TestVerifyDeletedAppsExist_NoneExist(t *testing.T) {
	existingByName := map[string]bool{}

	parsed := &argocd.ParsedApplications{
		Deleted: []models.Application{
			{Name: "phantom-app-1"},
			{Name: "phantom-app-2"},
		},
	}

	verifyDeletedAppsExist(parsed, existingByName)
	if len(parsed.Deleted) != 0 {
		t.Errorf("expected 0 deleted apps after filtering, got %d", len(parsed.Deleted))
	}
}

func TestScanRepoForApplications_VCSError(t *testing.T) {
	vcs := &mockVCS{
		filesByPatternErr: fmt.Errorf("archive download failed"),
	}

	exec := newTestExecutor(vcs, &mockLock{}, nil)
	event := &models.PREvent{
		Repo: models.RepoInfo{Owner: "org", Name: "repo"},
		PR:   models.PRInfo{HeadRef: "head", BaseRef: "base"},
	}

	_, err := exec.scanRepoForApplications(context.Background(), event, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestScanRepoForApplications_NonAppYAML(t *testing.T) {
	// A valid YAML file that is not an Application/ApplicationSet CR
	vcs := &mockVCS{
		filesByPattern: map[string]map[string][]byte{
			"head": {
				"values.yaml": []byte("replicas: 3\nimage: nginx:latest\n"),
			},
			"base": {},
		},
	}

	exec := newTestExecutor(vcs, &mockLock{}, nil)
	event := &models.PREvent{
		Repo: models.RepoInfo{Owner: "org", Name: "repo"},
		PR:   models.PRInfo{HeadRef: "head", BaseRef: "base"},
	}

	// Should not error — just find no apps
	scanned, err := exec.scanRepoForApplications(context.Background(), event, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(scanned.HeadApps) != 0 {
		t.Errorf("expected 0 apps from non-app YAML, got %d", len(scanned.HeadApps))
	}
	if len(scanned.HeadAppSets) != 0 {
		t.Errorf("expected 0 appsets from non-app YAML, got %d", len(scanned.HeadAppSets))
	}
}

func TestSortedKeys(t *testing.T) {
	tests := []struct {
		name string
		m    map[string][]byte
		want []string
	}{
		{"empty map", map[string][]byte{}, nil},
		{"single key", map[string][]byte{"a": {}}, []string{"a"}},
		{"sorted order", map[string][]byte{"c": {}, "a": {}, "b": {}}, []string{"a", "b", "c"}},
		{"paths", map[string][]byte{"z/file.yaml": {}, "a/file.yaml": {}}, []string{"a/file.yaml", "z/file.yaml"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sortedKeys(tt.m)
			if len(got) == 0 && len(tt.want) == 0 {
				return
			}
			if len(got) != len(tt.want) {
				t.Fatalf("len = %d, want %d", len(got), len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("index %d = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}
