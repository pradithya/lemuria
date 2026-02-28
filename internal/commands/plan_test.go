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
	"time"

	v1alpha1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"

	"github.com/org/lemuria/internal/argocd"
	"github.com/org/lemuria/internal/config"
	"github.com/org/lemuria/internal/models"
	"github.com/org/lemuria/pkg/diff"
)

// fakePlanArgoServer is a configurable fake ArgoCD server for plan tests.
type fakePlanArgoServer struct {
	// manifests to return for GET /manifests requests
	manifests []string
	// if true, GET /api/v1/applications/{name} returns 404 (new app)
	appNotFound bool
	// if true, POST /api/v1/applications fails with 500
	createFails bool
	// the app to return for GET requests (when appNotFound is false)
	app v1alpha1.Application
}

func (f *fakePlanArgoServer) start(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/manifests"):
			json.NewEncoder(w).Encode(map[string]any{"manifests": f.manifests}) //nolint:errcheck
		case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/api/v1/applications/"):
			if f.appNotFound {
				http.Error(w, `{"message":"not found"}`, http.StatusNotFound)
			} else {
				json.NewEncoder(w).Encode(f.app) //nolint:errcheck
			}
		case r.Method == "POST" && r.URL.Path == "/api/v1/applications":
			if f.createFails {
				http.Error(w, `{"message":"internal error"}`, http.StatusInternalServerError)
				return
			}
			var app v1alpha1.Application
			json.NewDecoder(r.Body).Decode(&app) //nolint:errcheck
			json.NewEncoder(w).Encode(app)       //nolint:errcheck
		case r.Method == "PUT" && strings.HasPrefix(r.URL.Path, "/api/v1/applications/"):
			// UpdateApplicationSpec — echo back body
			var spec v1alpha1.ApplicationSpec
			json.NewDecoder(r.Body).Decode(&spec) //nolint:errcheck
			json.NewEncoder(w).Encode(spec)       //nolint:errcheck
		case r.Method == "DELETE" && strings.HasPrefix(r.URL.Path, "/api/v1/applications/"):
			w.WriteHeader(http.StatusOK)
		default:
			t.Logf("unhandled request: %s %s", r.Method, r.URL.Path)
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
}

func newFakeArgoDiffServer(t *testing.T, manifests []string) *httptest.Server {
	t.Helper()
	return (&fakePlanArgoServer{
		manifests:   manifests,
		appNotFound: true,
	}).start(t)
}

func newFailingArgoDiffServer(t *testing.T) *httptest.Server {
	t.Helper()
	return (&fakePlanArgoServer{
		appNotFound: true,
		createFails: true,
	}).start(t)
}

func newArgoClient(t *testing.T, serverURL string) *argocd.Client {
	t.Helper()
	client, err := argocd.NewClient(config.ArgoCDConfig{
		ServerURL: serverURL,
		Token:     "test-token",
		Insecure:  true,
	})
	if err != nil {
		t.Fatalf("failed to create argocd client: %v", err)
	}
	return client
}

// sampleAppYAML returns a YAML string for a v1alpha1.Application.
func sampleAppYAML(name string) string {
	return fmt.Sprintf(`apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: %s
  namespace: argocd
spec:
  source:
    repoURL: https://github.com/org/repo
    path: manifests
    targetRevision: main
  destination:
    server: https://kubernetes.default.svc
    namespace: default
  project: default
`, name)
}

// sampleManifestJSON returns a JSON manifest for testing.
func sampleManifestJSON(kind, name string) string {
	m := map[string]any{
		"apiVersion": "v1",
		"kind":       kind,
		"metadata": map[string]any{
			"name":      name,
			"namespace": "default",
		},
	}
	data, _ := json.Marshal(m)
	return string(data)
}

func TestPlanApplication_NewApp_DiffSucceeds_ThenLock(t *testing.T) {
	manifests := []string{
		sampleManifestJSON("ConfigMap", "config"),
		sampleManifestJSON("Deployment", "web"),
	}
	argoServer := newFakeArgoDiffServer(t, manifests)
	defer argoServer.Close()

	lockMock := &mockLock{}
	vcs := &mockVCS{}

	exec := &Executor{
		vcs:    vcs,
		argocd: newArgoClient(t, argoServer.URL),
		lock:   lockMock,
		config: &config.Config{
			ArgoCD: config.ArgoCDConfig{
				TempAppTimeout: 5 * time.Second,
			},
		},
		renderer: diff.NewRenderer(),
	}

	app := models.Application{
		Name:       "new-app",
		ChangeType: models.ApplicationNew,
		SourceFile: "apps/new-app.yaml",
	}
	event := defaultEvent()

	headContents := map[string][]byte{
		"apps/new-app.yaml": []byte(sampleAppYAML("new-app")),
	}

	result := exec.planApplication(context.Background(), app, event, headContents, map[string][]byte{})

	// Verify diff succeeded
	if result.Error != nil {
		t.Fatalf("expected no error, got: %v", result.Error)
	}
	if len(result.Diffs) != 2 {
		t.Errorf("expected 2 diffs, got %d", len(result.Diffs))
	}

	// Verify lock was acquired (after diff success)
	if result.LockStatus != "Locked by this PR" {
		t.Errorf("LockStatus = %q, want 'Locked by this PR'", result.LockStatus)
	}

	// Verify plan was stored
	if len(lockMock.storedPlans) != 1 {
		t.Fatalf("expected 1 stored plan, got %d", len(lockMock.storedPlans))
	}
	if lockMock.storedPlans[0].Application != "new-app" {
		t.Errorf("stored plan app = %q, want 'new-app'", lockMock.storedPlans[0].Application)
	}
}

func TestPlanApplication_NewApp_DiffFails_NoLock(t *testing.T) {
	argoServer := newFailingArgoDiffServer(t)
	defer argoServer.Close()

	lockMock := &mockLock{}

	exec := &Executor{
		vcs:    &mockVCS{},
		argocd: newArgoClient(t, argoServer.URL),
		lock:   lockMock,
		config: &config.Config{
			ArgoCD: config.ArgoCDConfig{
				TempAppTimeout: 2 * time.Second,
			},
		},
		renderer: diff.NewRenderer(),
	}

	app := models.Application{
		Name:       "new-app",
		ChangeType: models.ApplicationNew,
		SourceFile: "apps/new-app.yaml",
	}
	event := defaultEvent()

	headContents := map[string][]byte{
		"apps/new-app.yaml": []byte(sampleAppYAML("new-app")),
	}

	result := exec.planApplication(context.Background(), app, event, headContents, map[string][]byte{})

	// Diff should fail
	if result.Error == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(result.Error.Error(), "failed to generate diff for new application") {
		t.Errorf("error = %q, want containing 'failed to generate diff for new application'", result.Error.Error())
	}

	// Lock should NOT have been acquired
	if result.LockStatus != "" {
		t.Errorf("LockStatus = %q, want empty (no lock acquired)", result.LockStatus)
	}

	// No plan should be stored
	if len(lockMock.storedPlans) != 0 {
		t.Errorf("expected 0 stored plans, got %d", len(lockMock.storedPlans))
	}
}

func TestPlanApplication_NewApp_NoSourceFile_NoLock(t *testing.T) {
	lockMock := &mockLock{}

	exec := &Executor{
		vcs:      &mockVCS{},
		lock:     lockMock,
		config:   &config.Config{},
		renderer: diff.NewRenderer(),
	}

	app := models.Application{
		Name:       "new-app",
		ChangeType: models.ApplicationNew,
		SourceFile: "", // no source file
	}
	event := defaultEvent()

	result := exec.planApplication(context.Background(), app, event, map[string][]byte{}, map[string][]byte{})

	// No lock should be acquired
	if result.LockStatus != "" {
		t.Errorf("LockStatus = %q, want empty", result.LockStatus)
	}

	// No plan stored
	if len(lockMock.storedPlans) != 0 {
		t.Errorf("expected 0 stored plans, got %d", len(lockMock.storedPlans))
	}
}

func TestPlanApplication_NewApp_SourceFileNotInContents_NoLock(t *testing.T) {
	lockMock := &mockLock{}

	exec := &Executor{
		vcs:      &mockVCS{},
		lock:     lockMock,
		config:   &config.Config{},
		renderer: diff.NewRenderer(),
	}

	app := models.Application{
		Name:       "new-app",
		ChangeType: models.ApplicationNew,
		SourceFile: "apps/new-app.yaml",
	}
	event := defaultEvent()

	// Head contents don't include the source file
	result := exec.planApplication(context.Background(), app, event, map[string][]byte{}, map[string][]byte{})

	if result.LockStatus != "" {
		t.Errorf("LockStatus = %q, want empty", result.LockStatus)
	}
	if len(lockMock.storedPlans) != 0 {
		t.Errorf("expected 0 stored plans, got %d", len(lockMock.storedPlans))
	}
}

func TestPlanApplication_NewApp_LockFails_AfterDiff(t *testing.T) {
	manifests := []string{
		sampleManifestJSON("ConfigMap", "config"),
	}
	argoServer := newFakeArgoDiffServer(t, manifests)
	defer argoServer.Close()

	lockMock := &mockLock{
		lockErr: fmt.Errorf("redis connection refused"),
	}

	exec := &Executor{
		vcs:    &mockVCS{},
		argocd: newArgoClient(t, argoServer.URL),
		lock:   lockMock,
		config: &config.Config{
			ArgoCD: config.ArgoCDConfig{
				TempAppTimeout: 5 * time.Second,
			},
		},
		renderer: diff.NewRenderer(),
	}

	app := models.Application{
		Name:       "new-app",
		ChangeType: models.ApplicationNew,
		SourceFile: "apps/new-app.yaml",
	}
	event := defaultEvent()

	headContents := map[string][]byte{
		"apps/new-app.yaml": []byte(sampleAppYAML("new-app")),
	}

	result := exec.planApplication(context.Background(), app, event, headContents, map[string][]byte{})

	// Should report lock error
	if result.Error == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(result.Error.Error(), "failed to acquire lock") {
		t.Errorf("error = %q, want containing 'failed to acquire lock'", result.Error.Error())
	}

	// Diffs should still be populated (diff succeeded before lock attempt)
	if len(result.Diffs) != 1 {
		t.Errorf("expected 1 diff, got %d", len(result.Diffs))
	}
}

func TestPlanApplication_NewApp_LockHeldByOther_AfterDiff(t *testing.T) {
	manifests := []string{
		sampleManifestJSON("ConfigMap", "config"),
	}
	argoServer := newFakeArgoDiffServer(t, manifests)
	defer argoServer.Close()

	lockMock := &mockLock{
		lockResult: &models.LockResult{
			Acquired: false,
			HeldBy: &models.Lock{
				PRNumber: 99,
				User:     "other-user",
			},
		},
	}

	exec := &Executor{
		vcs:    &mockVCS{},
		argocd: newArgoClient(t, argoServer.URL),
		lock:   lockMock,
		config: &config.Config{
			ArgoCD: config.ArgoCDConfig{
				TempAppTimeout: 5 * time.Second,
			},
		},
		renderer: diff.NewRenderer(),
	}

	app := models.Application{
		Name:       "new-app",
		ChangeType: models.ApplicationNew,
		SourceFile: "apps/new-app.yaml",
	}
	event := defaultEvent()

	headContents := map[string][]byte{
		"apps/new-app.yaml": []byte(sampleAppYAML("new-app")),
	}

	result := exec.planApplication(context.Background(), app, event, headContents, map[string][]byte{})

	// Diffs should still be populated
	if len(result.Diffs) != 1 {
		t.Errorf("expected 1 diff, got %d", len(result.Diffs))
	}

	// Lock status should indicate held by other PR
	if !strings.Contains(result.LockStatus, "PR #99") {
		t.Errorf("LockStatus = %q, want containing 'PR #99'", result.LockStatus)
	}

	// No plan stored since lock was not acquired
	if len(lockMock.storedPlans) != 0 {
		t.Errorf("expected 0 stored plans, got %d", len(lockMock.storedPlans))
	}
}

// ---------------------------------------------------------------------------
// Deleted application tests
// ---------------------------------------------------------------------------

func TestPlanApplication_DeletedApp_DiffSucceeds_ThenLock(t *testing.T) {
	manifests := []string{
		sampleManifestJSON("ConfigMap", "config"),
		sampleManifestJSON("Deployment", "web"),
	}

	// For deleted apps, the app exists in ArgoCD (GET returns it),
	// and temp apps are created for base branch manifests.
	fakeServer := &fakePlanArgoServer{
		manifests:   manifests,
		appNotFound: false,
		app: v1alpha1.Application{
			Spec: v1alpha1.ApplicationSpec{
				Source: &v1alpha1.ApplicationSource{
					RepoURL:        "https://github.com/org/repo",
					Path:           "manifests",
					TargetRevision: "main",
				},
				Destination: v1alpha1.ApplicationDestination{
					Server:    "https://kubernetes.default.svc",
					Namespace: "default",
				},
				Project: "default",
			},
		},
	}
	argoServer := fakeServer.start(t)
	defer argoServer.Close()

	lockMock := &mockLock{}

	exec := &Executor{
		vcs:    &mockVCS{},
		argocd: newArgoClient(t, argoServer.URL),
		lock:   lockMock,
		config: &config.Config{
			ArgoCD: config.ArgoCDConfig{
				TempAppTimeout: 5 * time.Second,
			},
		},
		renderer: diff.NewRenderer(),
	}

	app := models.Application{
		Name:       "deleted-app",
		ChangeType: models.ApplicationDeleted,
	}
	event := defaultEvent()

	result := exec.planApplication(context.Background(), app, event, map[string][]byte{}, map[string][]byte{})

	// Diff should succeed
	if result.Error != nil {
		t.Fatalf("expected no error, got: %v", result.Error)
	}
	if len(result.Diffs) == 0 {
		t.Error("expected diffs, got none")
	}

	// Lock should be acquired after diff
	if result.LockStatus != "Locked by this PR" {
		t.Errorf("LockStatus = %q, want 'Locked by this PR'", result.LockStatus)
	}

	// Plan should be stored
	if len(lockMock.storedPlans) != 1 {
		t.Fatalf("expected 1 stored plan, got %d", len(lockMock.storedPlans))
	}

	// Warning should be set
	if !strings.Contains(result.Warning, "removed") {
		t.Errorf("Warning = %q, want containing 'removed'", result.Warning)
	}
}

func TestPlanApplication_DeletedApp_DiffFails_NoLock(t *testing.T) {
	// Failing server — can't create temp apps
	fakeServer := &fakePlanArgoServer{
		appNotFound: false,
		createFails: true,
		app: v1alpha1.Application{
			Spec: v1alpha1.ApplicationSpec{
				Source: &v1alpha1.ApplicationSource{
					RepoURL:        "https://github.com/org/repo",
					Path:           "manifests",
					TargetRevision: "main",
				},
				Destination: v1alpha1.ApplicationDestination{
					Server:    "https://kubernetes.default.svc",
					Namespace: "default",
				},
				Project: "default",
			},
		},
	}
	argoServer := fakeServer.start(t)
	defer argoServer.Close()

	lockMock := &mockLock{}

	exec := &Executor{
		vcs:    &mockVCS{},
		argocd: newArgoClient(t, argoServer.URL),
		lock:   lockMock,
		config: &config.Config{
			ArgoCD: config.ArgoCDConfig{
				TempAppTimeout: 2 * time.Second,
			},
		},
		renderer: diff.NewRenderer(),
	}

	app := models.Application{
		Name:       "deleted-app",
		ChangeType: models.ApplicationDeleted,
	}
	event := defaultEvent()

	result := exec.planApplication(context.Background(), app, event, map[string][]byte{}, map[string][]byte{})

	// Diff should fail
	if result.Error == nil {
		t.Fatal("expected error, got nil")
	}

	// Lock should NOT have been acquired
	if result.LockStatus != "" {
		t.Errorf("LockStatus = %q, want empty", result.LockStatus)
	}

	// No plan stored
	if len(lockMock.storedPlans) != 0 {
		t.Errorf("expected 0 stored plans, got %d", len(lockMock.storedPlans))
	}
}

// ---------------------------------------------------------------------------
// Existing application tests
// ---------------------------------------------------------------------------

func TestPlanApplication_ExistingApp_DiffSucceeds_ThenLock(t *testing.T) {
	manifests := []string{
		sampleManifestJSON("ConfigMap", "config"),
	}

	fakeServer := &fakePlanArgoServer{
		manifests:   manifests,
		appNotFound: false,
		app: v1alpha1.Application{
			Spec: v1alpha1.ApplicationSpec{
				Source: &v1alpha1.ApplicationSource{
					RepoURL:        "https://github.com/org/repo",
					Path:           "manifests",
					TargetRevision: "main",
				},
				Destination: v1alpha1.ApplicationDestination{
					Server:    "https://kubernetes.default.svc",
					Namespace: "default",
				},
				Project: "default",
			},
		},
	}
	argoServer := fakeServer.start(t)
	defer argoServer.Close()

	lockMock := &mockLock{}

	exec := &Executor{
		vcs:    &mockVCS{},
		argocd: newArgoClient(t, argoServer.URL),
		lock:   lockMock,
		config: &config.Config{
			ArgoCD: config.ArgoCDConfig{
				TempAppTimeout: 5 * time.Second,
			},
		},
		renderer: diff.NewRenderer(),
	}

	app := models.Application{
		Name:       "existing-app",
		ChangeType: models.ApplicationExisting,
	}
	event := defaultEvent()

	result := exec.planApplication(context.Background(), app, event, map[string][]byte{}, map[string][]byte{})

	if result.Error != nil {
		t.Fatalf("expected no error, got: %v", result.Error)
	}

	// Lock should be acquired after diff
	if result.LockStatus != "Locked by this PR" {
		t.Errorf("LockStatus = %q, want 'Locked by this PR'", result.LockStatus)
	}

	// Plan should be stored
	if len(lockMock.storedPlans) != 1 {
		t.Fatalf("expected 1 stored plan, got %d", len(lockMock.storedPlans))
	}
}

func TestPlanApplication_ExistingApp_DiffFails_NoLock(t *testing.T) {
	// Server that fails on temp app creation
	fakeServer := &fakePlanArgoServer{
		appNotFound: false,
		createFails: true,
		app: v1alpha1.Application{
			Spec: v1alpha1.ApplicationSpec{
				Source: &v1alpha1.ApplicationSource{
					RepoURL:        "https://github.com/org/repo",
					Path:           "manifests",
					TargetRevision: "main",
				},
				Destination: v1alpha1.ApplicationDestination{
					Server:    "https://kubernetes.default.svc",
					Namespace: "default",
				},
				Project: "default",
			},
		},
	}
	argoServer := fakeServer.start(t)
	defer argoServer.Close()

	lockMock := &mockLock{}

	exec := &Executor{
		vcs:    &mockVCS{},
		argocd: newArgoClient(t, argoServer.URL),
		lock:   lockMock,
		config: &config.Config{
			ArgoCD: config.ArgoCDConfig{
				TempAppTimeout: 2 * time.Second,
			},
		},
		renderer: diff.NewRenderer(),
	}

	app := models.Application{
		Name:       "existing-app",
		ChangeType: models.ApplicationExisting,
	}
	event := defaultEvent()

	result := exec.planApplication(context.Background(), app, event, map[string][]byte{}, map[string][]byte{})

	// Diff should fail
	if result.Error == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(result.Error.Error(), "failed to generate diff") {
		t.Errorf("error = %q, want containing 'failed to generate diff'", result.Error.Error())
	}

	// Lock should NOT have been acquired
	if result.LockStatus != "" {
		t.Errorf("LockStatus = %q, want empty", result.LockStatus)
	}

	// No plan stored
	if len(lockMock.storedPlans) != 0 {
		t.Errorf("expected 0 stored plans, got %d", len(lockMock.storedPlans))
	}
}

func TestPlanApplication_ExistingApp_LockHeldByOther_AfterDiff(t *testing.T) {
	manifests := []string{
		sampleManifestJSON("ConfigMap", "config"),
	}

	fakeServer := &fakePlanArgoServer{
		manifests:   manifests,
		appNotFound: false,
		app: v1alpha1.Application{
			Spec: v1alpha1.ApplicationSpec{
				Source: &v1alpha1.ApplicationSource{
					RepoURL:        "https://github.com/org/repo",
					Path:           "manifests",
					TargetRevision: "main",
				},
				Destination: v1alpha1.ApplicationDestination{
					Server:    "https://kubernetes.default.svc",
					Namespace: "default",
				},
				Project: "default",
			},
		},
	}
	argoServer := fakeServer.start(t)
	defer argoServer.Close()

	lockMock := &mockLock{
		lockResult: &models.LockResult{
			Acquired: false,
			HeldBy: &models.Lock{
				PRNumber: 55,
				User:     "someone-else",
			},
		},
	}

	exec := &Executor{
		vcs:    &mockVCS{},
		argocd: newArgoClient(t, argoServer.URL),
		lock:   lockMock,
		config: &config.Config{
			ArgoCD: config.ArgoCDConfig{
				TempAppTimeout: 5 * time.Second,
			},
		},
		renderer: diff.NewRenderer(),
	}

	app := models.Application{
		Name:       "existing-app",
		ChangeType: models.ApplicationExisting,
	}
	event := defaultEvent()

	result := exec.planApplication(context.Background(), app, event, map[string][]byte{}, map[string][]byte{})

	// Diff should have succeeded (no error) — both temp apps return same manifests so diff is empty
	if result.Error != nil {
		t.Fatalf("expected no error, got: %v", result.Error)
	}

	// Lock should show held by other
	if !strings.Contains(result.LockStatus, "PR #55") {
		t.Errorf("LockStatus = %q, want containing 'PR #55'", result.LockStatus)
	}

	// No plan stored since lock was not acquired
	if len(lockMock.storedPlans) != 0 {
		t.Errorf("expected 0 stored plans, got %d", len(lockMock.storedPlans))
	}
}
