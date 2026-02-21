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

package argocd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	v1alpha1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/org/lemuria/internal/config"
	"github.com/org/lemuria/internal/models"
)

// sampleApp creates a v1alpha1.Application for testing.
func sampleApp(name, repoURL, path, revision string) *v1alpha1.Application {
	return &v1alpha1.Application{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "argoproj.io/v1alpha1",
			Kind:       "Application",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "argocd",
		},
		Spec: v1alpha1.ApplicationSpec{
			Source: &v1alpha1.ApplicationSource{
				RepoURL:        repoURL,
				Path:           path,
				TargetRevision: revision,
			},
			Destination: v1alpha1.ApplicationDestination{
				Server:    "https://kubernetes.default.svc",
				Namespace: "default",
			},
			Project: "default",
		},
	}
}

// manifestJSON returns a JSON manifest string for a given kind/name/namespace.
func manifestJSON(kind, name, namespace string, extra map[string]any) string {
	m := map[string]any{
		"apiVersion": "v1",
		"kind":       kind,
		"metadata": map[string]any{
			"name":      name,
			"namespace": namespace,
		},
	}
	for k, v := range extra {
		m[k] = v
	}
	data, _ := json.Marshal(m)
	return string(data)
}

// diffTestServer creates an httptest.Server that handles the ArgoCD API endpoints
// needed for diff operations. It returns the server and a cleanup function.
//
// handlers is a map of "METHOD /path" -> handler function. Common endpoints
// are pre-registered but can be overridden.
func diffTestServer(t *testing.T, handlers map[string]http.HandlerFunc) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Method + " " + r.URL.Path
		if h, ok := handlers[key]; ok {
			h(w, r)
			return
		}
		// Also check with prefix matching for parameterized paths
		for pattern, h := range handlers {
			parts := strings.SplitN(pattern, " ", 2)
			if len(parts) == 2 && r.Method == parts[0] && strings.HasPrefix(r.URL.Path, parts[1]) {
				h(w, r)
				return
			}
		}
		t.Logf("unhandled request: %s %s", r.Method, r.URL.Path)
		http.Error(w, "not found", http.StatusNotFound)
	}))
}

func TestNewClient(t *testing.T) {
	t.Run("creates client with valid config", func(t *testing.T) {
		cfg := config.ArgoCDConfig{
			ServerURL: "https://argocd.example.com/",
			Token:     "test-token-123",
			Insecure:  false,
		}
		client, err := NewClient(cfg)
		if err != nil {
			t.Fatalf("NewClient() error = %v", err)
		}
		if client == nil {
			t.Fatal("expected non-nil client")
		}
		// Verify trailing slash is trimmed
		if client.baseURL != "https://argocd.example.com" {
			t.Errorf("baseURL = %q, want trailing slash trimmed", client.baseURL)
		}
		if client.token != "test-token-123" {
			t.Errorf("token = %q, want %q", client.token, "test-token-123")
		}
	})

	t.Run("creates client with insecure config", func(t *testing.T) {
		cfg := config.ArgoCDConfig{
			ServerURL: "https://argocd.example.com",
			Token:     "token",
			Insecure:  true,
		}
		client, err := NewClient(cfg)
		if err != nil {
			t.Fatalf("NewClient() error = %v", err)
		}
		if client == nil {
			t.Fatal("expected non-nil client")
		}
	})
}

func TestDiffNewApp(t *testing.T) {
	cm1 := manifestJSON("ConfigMap", "config", "default", map[string]any{"data": map[string]any{"key": "val"}})
	deploy1 := manifestJSON("Deployment", "web", "default", map[string]any{"spec": map[string]any{"replicas": 3}})

	t.Run("creates temp app and diffs against empty", func(t *testing.T) {
		var createdApp string
		var deletedApp string

		ts := diffTestServer(t, map[string]http.HandlerFunc{
			// CreateApplication (CreateNewApp checks if app exists first via GetApplicationRaw)
			"GET /api/v1/applications/": func(w http.ResponseWriter, r *http.Request) {
				// GetApplicationRaw for the new app - return 404 since it doesn't exist
				appName := strings.TrimPrefix(r.URL.Path, "/api/v1/applications/")
				if strings.Contains(appName, "/") {
					// It's a sub-resource like /manifests
					if strings.HasSuffix(r.URL.Path, "/manifests") {
						// GetManifests for the temp app
						w.WriteHeader(http.StatusOK)
						resp := manifestResponse{
							Manifests: []string{cm1, deploy1},
						}
						_ = json.NewEncoder(w).Encode(resp)
						return
					}
				}
				// Return 404 for the app itself (new app doesn't exist)
				http.Error(w, `{"message":"not found"}`, http.StatusNotFound)
			},
			"POST /api/v1/applications": func(w http.ResponseWriter, r *http.Request) {
				var app v1alpha1.Application
				_ = json.NewDecoder(r.Body).Decode(&app)
				createdApp = app.Name
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(app)
			},
			"DELETE /api/v1/applications/": func(w http.ResponseWriter, r *http.Request) {
				deletedApp = strings.TrimPrefix(r.URL.Path, "/api/v1/applications/")
				w.WriteHeader(http.StatusOK)
			},
		})
		defer ts.Close()

		client := newTestClient(ts)
		appSpec := sampleApp("new-app", "https://github.com/org/repo", "manifests", "main")

		diffs, err := client.DiffNewApp(context.Background(), appSpec, DiffOptions{
			TargetBranch: "feature/add-app",
			PRNumber:     1,
			PRRepo:       "https://github.com/org/repo",
			Timeout:      5 * time.Second,
		})
		if err != nil {
			t.Fatalf("DiffNewApp() error = %v", err)
		}

		if len(diffs) != 2 {
			t.Fatalf("expected 2 diffs, got %d", len(diffs))
		}

		for _, d := range diffs {
			if d.Action != models.DiffActionCreate {
				t.Errorf("expected Create action for %s, got %s", d.Resource.String(), d.Action)
			}
		}

		if createdApp == "" {
			t.Error("expected temp app to be created")
		}
		if deletedApp == "" {
			t.Error("expected temp app to be deleted (cleanup)")
		}
	})

	t.Run("returns error when temp app creation fails", func(t *testing.T) {
		ts := diffTestServer(t, map[string]http.HandlerFunc{
			"GET /api/v1/applications/": func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, `{"message":"not found"}`, http.StatusNotFound)
			},
			"POST /api/v1/applications": func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, `{"message":"quota exceeded"}`, http.StatusForbidden)
			},
		})
		defer ts.Close()

		client := newTestClient(ts)
		appSpec := sampleApp("new-app", "https://github.com/org/repo", "manifests", "main")

		_, err := client.DiffNewApp(context.Background(), appSpec, DiffOptions{
			TargetBranch: "feature/add-app",
			PRNumber:     1,
			PRRepo:       "https://github.com/org/repo",
			Timeout:      5 * time.Second,
		})
		if err == nil {
			t.Fatal("expected error from DiffNewApp")
		}
		if !strings.Contains(err.Error(), "creating temp app") {
			t.Errorf("expected error about creating temp app, got: %v", err)
		}
	})

	t.Run("returns error when manifest wait fails", func(t *testing.T) {
		ts := diffTestServer(t, map[string]http.HandlerFunc{
			"GET /api/v1/applications/": func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/manifests") {
					http.Error(w, `{"message":"rendering in progress"}`, http.StatusInternalServerError)
					return
				}
				http.Error(w, `{"message":"not found"}`, http.StatusNotFound)
			},
			"POST /api/v1/applications": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{}`))
			},
			"DELETE /api/v1/applications/": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			},
		})
		defer ts.Close()

		client := newTestClient(ts)
		appSpec := sampleApp("new-app", "https://github.com/org/repo", "manifests", "main")

		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		_, err := client.DiffNewApp(ctx, appSpec, DiffOptions{
			TargetBranch: "feature/add-app",
			PRNumber:     1,
			PRRepo:       "https://github.com/org/repo",
			Timeout:      1 * time.Second,
		})
		if err == nil {
			t.Fatal("expected error from DiffNewApp")
		}
		if !strings.Contains(err.Error(), "waiting for manifests") {
			t.Errorf("expected error about waiting for manifests, got: %v", err)
		}
	})
}

func TestDiffDeletedApp(t *testing.T) {
	cm1 := manifestJSON("ConfigMap", "config", "default", map[string]any{"data": map[string]any{"key": "val"}})

	t.Run("branch mode - diffs base branch against empty", func(t *testing.T) {
		app := sampleApp("delete-me", "https://github.com/org/repo", "manifests", "main")

		ts := diffTestServer(t, map[string]http.HandlerFunc{
			"GET /api/v1/applications/": func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/manifests") {
					resp := manifestResponse{Manifests: []string{cm1}}
					_ = json.NewEncoder(w).Encode(resp)
					return
				}
				// GetApplicationRaw
				_ = json.NewEncoder(w).Encode(app)
			},
			"POST /api/v1/applications": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{}`))
			},
			"DELETE /api/v1/applications/": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			},
		})
		defer ts.Close()

		client := newTestClient(ts)
		diffs, err := client.DiffDeletedApp(context.Background(), "delete-me", DiffOptions{
			Mode:         DiffModeBranch,
			BaseBranch:   "main",
			TargetBranch: "feature/remove-app",
			PRNumber:     2,
			PRRepo:       "https://github.com/org/repo",
			Timeout:      5 * time.Second,
		})
		if err != nil {
			t.Fatalf("DiffDeletedApp() error = %v", err)
		}

		if len(diffs) != 1 {
			t.Fatalf("expected 1 diff, got %d", len(diffs))
		}
		if diffs[0].Action != models.DiffActionDelete {
			t.Errorf("expected Delete action, got %s", diffs[0].Action)
		}
	})

	t.Run("branch mode - error when base branch missing", func(t *testing.T) {
		ts := diffTestServer(t, map[string]http.HandlerFunc{})
		defer ts.Close()

		client := newTestClient(ts)
		_, err := client.DiffDeletedApp(context.Background(), "delete-me", DiffOptions{
			Mode:     DiffModeBranch,
			PRNumber: 2,
			PRRepo:   "https://github.com/org/repo",
			Timeout:  5 * time.Second,
		})
		if err == nil {
			t.Fatal("expected error when BaseBranch is empty")
		}
		if !strings.Contains(err.Error(), "base branch is required") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("live mode - diffs live resources against empty", func(t *testing.T) {
		ts := diffTestServer(t, map[string]http.HandlerFunc{
			"GET /api/v1/applications/": func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/managed-resources") {
					resp := struct {
						Items []ManagedResource `json:"items"`
					}{
						Items: []ManagedResource{
							{
								Kind:                "ConfigMap",
								Name:                "config",
								Namespace:           "default",
								NormalizedLiveState: cm1,
							},
						},
					}
					_ = json.NewEncoder(w).Encode(resp)
					return
				}
			},
		})
		defer ts.Close()

		client := newTestClient(ts)
		diffs, err := client.DiffDeletedApp(context.Background(), "delete-me", DiffOptions{
			Mode:    DiffModeLive,
			Timeout: 5 * time.Second,
		})
		if err != nil {
			t.Fatalf("DiffDeletedApp(live) error = %v", err)
		}

		if len(diffs) != 1 {
			t.Fatalf("expected 1 diff, got %d", len(diffs))
		}
		if diffs[0].Action != models.DiffActionDelete {
			t.Errorf("expected Delete action, got %s", diffs[0].Action)
		}
	})

	t.Run("live mode - error when managed resources fail", func(t *testing.T) {
		ts := diffTestServer(t, map[string]http.HandlerFunc{
			"GET /api/v1/applications/": func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, `{"message":"not found"}`, http.StatusNotFound)
			},
		})
		defer ts.Close()

		client := newTestClient(ts)
		_, err := client.DiffDeletedApp(context.Background(), "delete-me", DiffOptions{
			Mode:    DiffModeLive,
			Timeout: 5 * time.Second,
		})
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("both mode - 3-way diff for deleted app", func(t *testing.T) {
		app := sampleApp("delete-me", "https://github.com/org/repo", "manifests", "main")

		ts := diffTestServer(t, map[string]http.HandlerFunc{
			"GET /api/v1/applications/": func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/managed-resources") {
					resp := struct {
						Items []ManagedResource `json:"items"`
					}{
						Items: []ManagedResource{
							{
								Kind:                "ConfigMap",
								Name:                "config",
								Namespace:           "default",
								NormalizedLiveState: cm1,
							},
						},
					}
					_ = json.NewEncoder(w).Encode(resp)
					return
				}
				if strings.HasSuffix(r.URL.Path, "/manifests") {
					resp := manifestResponse{Manifests: []string{cm1}}
					_ = json.NewEncoder(w).Encode(resp)
					return
				}
				// GetApplicationRaw
				_ = json.NewEncoder(w).Encode(app)
			},
			"POST /api/v1/applications": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{}`))
			},
			"DELETE /api/v1/applications/": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			},
		})
		defer ts.Close()

		client := newTestClient(ts)
		diffs, err := client.DiffDeletedApp(context.Background(), "delete-me", DiffOptions{
			Mode:         DiffModeBoth,
			BaseBranch:   "main",
			TargetBranch: "feature/remove",
			PRNumber:     3,
			PRRepo:       "https://github.com/org/repo",
			Timeout:      5 * time.Second,
		})
		if err != nil {
			t.Fatalf("DiffDeletedApp(both) error = %v", err)
		}

		if len(diffs) != 1 {
			t.Fatalf("expected 1 diff, got %d", len(diffs))
		}
	})

	t.Run("both mode - error when base branch missing", func(t *testing.T) {
		ts := diffTestServer(t, map[string]http.HandlerFunc{
			"GET /api/v1/applications/": func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/managed-resources") {
					resp := struct {
						Items []ManagedResource `json:"items"`
					}{Items: []ManagedResource{}}
					_ = json.NewEncoder(w).Encode(resp)
					return
				}
			},
		})
		defer ts.Close()

		client := newTestClient(ts)
		_, err := client.DiffDeletedApp(context.Background(), "delete-me", DiffOptions{
			Mode:    DiffModeBoth,
			Timeout: 5 * time.Second,
		})
		if err == nil {
			t.Fatal("expected error when BaseBranch is empty for both mode")
		}
	})

	t.Run("default mode uses branch mode", func(t *testing.T) {
		app := sampleApp("delete-me", "https://github.com/org/repo", "manifests", "main")

		ts := diffTestServer(t, map[string]http.HandlerFunc{
			"GET /api/v1/applications/": func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/manifests") {
					resp := manifestResponse{Manifests: []string{cm1}}
					_ = json.NewEncoder(w).Encode(resp)
					return
				}
				_ = json.NewEncoder(w).Encode(app)
			},
			"POST /api/v1/applications": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{}`))
			},
			"DELETE /api/v1/applications/": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			},
		})
		defer ts.Close()

		client := newTestClient(ts)
		diffs, err := client.DiffDeletedApp(context.Background(), "delete-me", DiffOptions{
			BaseBranch:   "main",
			TargetBranch: "feature/remove",
			PRNumber:     4,
			PRRepo:       "https://github.com/org/repo",
			Timeout:      5 * time.Second,
		})
		if err != nil {
			t.Fatalf("DiffDeletedApp(default) error = %v", err)
		}
		if len(diffs) == 0 {
			t.Error("expected at least 1 diff")
		}
	})
}

func TestGetApplicationDiff(t *testing.T) {
	cmBase := manifestJSON("ConfigMap", "config", "default", map[string]any{"data": map[string]any{"key": "base-val"}})
	cmTarget := manifestJSON("ConfigMap", "config", "default", map[string]any{"data": map[string]any{"key": "target-val"}})

	// setupServer creates a test server that handles both base and target temp apps.
	setupServer := func(t *testing.T) *httptest.Server {
		t.Helper()
		app := sampleApp("my-app", "https://github.com/org/repo", "manifests", "main")
		callCount := 0

		return diffTestServer(t, map[string]http.HandlerFunc{
			"GET /api/v1/applications/": func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/managed-resources") {
					resp := struct {
						Items []ManagedResource `json:"items"`
					}{
						Items: []ManagedResource{
							{
								Kind:                "ConfigMap",
								Name:                "config",
								Namespace:           "default",
								NormalizedLiveState: cmBase,
							},
						},
					}
					_ = json.NewEncoder(w).Encode(resp)
					return
				}
				if strings.HasSuffix(r.URL.Path, "/manifests") {
					callCount++
					// First manifests call returns base, second returns target
					manifest := cmBase
					if callCount > 1 {
						manifest = cmTarget
					}
					resp := manifestResponse{Manifests: []string{manifest}}
					_ = json.NewEncoder(w).Encode(resp)
					return
				}
				// GetApplicationRaw
				_ = json.NewEncoder(w).Encode(app)
			},
			"POST /api/v1/applications": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{}`))
			},
			"DELETE /api/v1/applications/": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			},
		})
	}

	t.Run("branch mode - compares base and target branches", func(t *testing.T) {
		ts := setupServer(t)
		defer ts.Close()

		client := newTestClient(ts)
		diffs, err := client.GetApplicationDiff(context.Background(), "my-app", DiffOptions{
			Mode:         DiffModeBranch,
			BaseBranch:   "main",
			TargetBranch: "feature/update",
			PRNumber:     10,
			PRRepo:       "https://github.com/org/repo",
			Timeout:      5 * time.Second,
		})
		if err != nil {
			t.Fatalf("GetApplicationDiff(branch) error = %v", err)
		}

		if len(diffs) != 1 {
			t.Fatalf("expected 1 diff, got %d", len(diffs))
		}
		if diffs[0].Action != models.DiffActionUpdate {
			t.Errorf("expected Update action, got %s", diffs[0].Action)
		}
	})

	t.Run("branch mode - error when base branch missing", func(t *testing.T) {
		ts := setupServer(t)
		defer ts.Close()

		client := newTestClient(ts)
		_, err := client.GetApplicationDiff(context.Background(), "my-app", DiffOptions{
			Mode:         DiffModeBranch,
			TargetBranch: "feature/update",
			PRNumber:     10,
			PRRepo:       "https://github.com/org/repo",
		})
		if err == nil {
			t.Fatal("expected error when BaseBranch is empty")
		}
		if !strings.Contains(err.Error(), "base branch is required") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("branch mode - error when target branch missing", func(t *testing.T) {
		ts := setupServer(t)
		defer ts.Close()

		client := newTestClient(ts)
		_, err := client.GetApplicationDiff(context.Background(), "my-app", DiffOptions{
			Mode:       DiffModeBranch,
			BaseBranch: "main",
			PRNumber:   10,
			PRRepo:     "https://github.com/org/repo",
		})
		if err == nil {
			t.Fatal("expected error when TargetBranch is empty")
		}
		if !strings.Contains(err.Error(), "target branch is required") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("live mode - compares live state against target", func(t *testing.T) {
		ts := setupServer(t)
		defer ts.Close()

		client := newTestClient(ts)
		diffs, err := client.GetApplicationDiff(context.Background(), "my-app", DiffOptions{
			Mode:         DiffModeLive,
			TargetBranch: "feature/update",
			PRNumber:     10,
			PRRepo:       "https://github.com/org/repo",
			Timeout:      5 * time.Second,
		})
		if err != nil {
			t.Fatalf("GetApplicationDiff(live) error = %v", err)
		}

		// At least should not error out; number of diffs depends on manifests
		_ = diffs
	})

	t.Run("live mode - error when target branch missing", func(t *testing.T) {
		ts := setupServer(t)
		defer ts.Close()

		client := newTestClient(ts)
		_, err := client.GetApplicationDiff(context.Background(), "my-app", DiffOptions{
			Mode:   DiffModeLive,
			PRRepo: "https://github.com/org/repo",
		})
		if err == nil {
			t.Fatal("expected error when TargetBranch is empty")
		}
		if !strings.Contains(err.Error(), "target branch is required") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("live mode - error when managed resources fail", func(t *testing.T) {
		ts := diffTestServer(t, map[string]http.HandlerFunc{
			"GET /api/v1/applications/": func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, `{"message":"not found"}`, http.StatusNotFound)
			},
		})
		defer ts.Close()

		client := newTestClient(ts)
		_, err := client.GetApplicationDiff(context.Background(), "my-app", DiffOptions{
			Mode:         DiffModeLive,
			TargetBranch: "feature/update",
			PRNumber:     10,
			PRRepo:       "https://github.com/org/repo",
		})
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("both mode - 3-way diff", func(t *testing.T) {
		ts := setupServer(t)
		defer ts.Close()

		client := newTestClient(ts)
		diffs, err := client.GetApplicationDiff(context.Background(), "my-app", DiffOptions{
			Mode:         DiffModeBoth,
			BaseBranch:   "main",
			TargetBranch: "feature/update",
			PRNumber:     10,
			PRRepo:       "https://github.com/org/repo",
			Timeout:      5 * time.Second,
		})
		if err != nil {
			t.Fatalf("GetApplicationDiff(both) error = %v", err)
		}

		_ = diffs // success is sufficient
	})

	t.Run("both mode - error when base branch missing", func(t *testing.T) {
		ts := setupServer(t)
		defer ts.Close()

		client := newTestClient(ts)
		_, err := client.GetApplicationDiff(context.Background(), "my-app", DiffOptions{
			Mode:         DiffModeBoth,
			TargetBranch: "feature/update",
			PRNumber:     10,
			PRRepo:       "https://github.com/org/repo",
		})
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("both mode - error when target branch missing", func(t *testing.T) {
		ts := setupServer(t)
		defer ts.Close()

		client := newTestClient(ts)
		_, err := client.GetApplicationDiff(context.Background(), "my-app", DiffOptions{
			Mode:       DiffModeBoth,
			BaseBranch: "main",
			PRNumber:   10,
			PRRepo:     "https://github.com/org/repo",
		})
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("default mode uses branch mode", func(t *testing.T) {
		ts := setupServer(t)
		defer ts.Close()

		client := newTestClient(ts)
		diffs, err := client.GetApplicationDiff(context.Background(), "my-app", DiffOptions{
			BaseBranch:   "main",
			TargetBranch: "feature/update",
			PRNumber:     10,
			PRRepo:       "https://github.com/org/repo",
			Timeout:      5 * time.Second,
		})
		if err != nil {
			t.Fatalf("GetApplicationDiff(default) error = %v", err)
		}
		_ = diffs
	})

	t.Run("uses default timeout when zero", func(t *testing.T) {
		ts := setupServer(t)
		defer ts.Close()

		client := newTestClient(ts)
		// Just verify it doesn't panic with zero timeout
		_, err := client.GetApplicationDiff(context.Background(), "my-app", DiffOptions{
			Mode:         DiffModeBranch,
			BaseBranch:   "main",
			TargetBranch: "feature/update",
			PRNumber:     10,
			PRRepo:       "https://github.com/org/repo",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("branch mode - error when base temp app creation fails", func(t *testing.T) {
		ts := diffTestServer(t, map[string]http.HandlerFunc{
			"GET /api/v1/applications/": func(w http.ResponseWriter, r *http.Request) {
				// GetApplicationRaw - return error
				http.Error(w, `{"message":"server error"}`, http.StatusInternalServerError)
			},
		})
		defer ts.Close()

		client := newTestClient(ts)
		_, err := client.GetApplicationDiff(context.Background(), "my-app", DiffOptions{
			Mode:         DiffModeBranch,
			BaseBranch:   "main",
			TargetBranch: "feature/update",
			PRNumber:     10,
			PRRepo:       "https://github.com/org/repo",
		})
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "creating base temp app") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("branch mode - error when target temp app creation fails", func(t *testing.T) {
		app := sampleApp("my-app", "https://github.com/org/repo", "manifests", "main")
		createCount := 0

		ts := diffTestServer(t, map[string]http.HandlerFunc{
			"GET /api/v1/applications/": func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(app)
			},
			"POST /api/v1/applications": func(w http.ResponseWriter, r *http.Request) {
				createCount++
				if createCount > 1 {
					// Fail the second create (target temp app)
					http.Error(w, `{"message":"quota exceeded"}`, http.StatusForbidden)
					return
				}
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{}`))
			},
			"DELETE /api/v1/applications/": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			},
		})
		defer ts.Close()

		client := newTestClient(ts)
		_, err := client.GetApplicationDiff(context.Background(), "my-app", DiffOptions{
			Mode:         DiffModeBranch,
			BaseBranch:   "main",
			TargetBranch: "feature/update",
			PRNumber:     10,
			PRRepo:       "https://github.com/org/repo",
		})
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "creating target temp app") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("branch mode - error when base manifests wait fails", func(t *testing.T) {
		app := sampleApp("my-app", "https://github.com/org/repo", "manifests", "main")

		ts := diffTestServer(t, map[string]http.HandlerFunc{
			"GET /api/v1/applications/": func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/manifests") {
					http.Error(w, `{"message":"rendering failed"}`, http.StatusInternalServerError)
					return
				}
				_ = json.NewEncoder(w).Encode(app)
			},
			"POST /api/v1/applications": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{}`))
			},
			"DELETE /api/v1/applications/": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			},
		})
		defer ts.Close()

		client := newTestClient(ts)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		_, err := client.GetApplicationDiff(ctx, "my-app", DiffOptions{
			Mode:         DiffModeBranch,
			BaseBranch:   "main",
			TargetBranch: "feature/update",
			PRNumber:     10,
			PRRepo:       "https://github.com/org/repo",
			Timeout:      1 * time.Second,
		})
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "waiting for base manifests") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("branch mode with app spec overrides", func(t *testing.T) {
		baseSpec := sampleApp("my-app", "https://github.com/org/repo", "manifests", "main")
		headSpec := sampleApp("my-app", "https://github.com/org/repo", "manifests", "main")

		ts := diffTestServer(t, map[string]http.HandlerFunc{
			"GET /api/v1/applications/": func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/manifests") {
					resp := manifestResponse{Manifests: []string{cmBase}}
					_ = json.NewEncoder(w).Encode(resp)
					return
				}
				_ = json.NewEncoder(w).Encode(baseSpec)
			},
			"POST /api/v1/applications": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{}`))
			},
			"DELETE /api/v1/applications/": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			},
		})
		defer ts.Close()

		client := newTestClient(ts)
		_, err := client.GetApplicationDiff(context.Background(), "my-app", DiffOptions{
			Mode:         DiffModeBranch,
			BaseBranch:   "main",
			TargetBranch: "feature/update",
			PRNumber:     10,
			PRRepo:       "https://github.com/org/repo",
			Timeout:      5 * time.Second,
			BaseAppSpec:  baseSpec,
			HeadAppSpec:  headSpec,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestGetManagedResourcesForDiff(t *testing.T) {
	t.Run("returns managed resources", func(t *testing.T) {
		ts := diffTestServer(t, map[string]http.HandlerFunc{
			"GET /api/v1/applications/": func(w http.ResponseWriter, r *http.Request) {
				resp := struct {
					Items []ManagedResource `json:"items"`
				}{
					Items: []ManagedResource{
						{
							Kind:                "Deployment",
							Group:               "apps",
							Name:                "web",
							Namespace:           "default",
							NormalizedLiveState: `{"kind":"Deployment"}`,
						},
						{
							Kind:      "ConfigMap",
							Name:      "config",
							Namespace: "default",
							LiveState: `{"kind":"ConfigMap"}`,
						},
					},
				}
				_ = json.NewEncoder(w).Encode(resp)
			},
		})
		defer ts.Close()

		client := newTestClient(ts)
		resources, err := client.getManagedResources(context.Background(), "my-app")
		if err != nil {
			t.Fatalf("getManagedResources() error = %v", err)
		}
		if len(resources) != 2 {
			t.Errorf("expected 2 resources, got %d", len(resources))
		}
	})

	t.Run("returns error on API failure", func(t *testing.T) {
		ts := diffTestServer(t, map[string]http.HandlerFunc{
			"GET /api/v1/applications/": func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, `{"message":"not found"}`, http.StatusNotFound)
			},
		})
		defer ts.Close()

		client := newTestClient(ts)
		_, err := client.getManagedResources(context.Background(), "my-app")
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestTempAppManager_CreateTempApp(t *testing.T) {
	t.Run("creates temp app from live ArgoCD spec", func(t *testing.T) {
		app := sampleApp("my-app", "https://github.com/org/repo", "manifests", "main")

		ts := diffTestServer(t, map[string]http.HandlerFunc{
			"GET /api/v1/applications/": func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(app)
			},
			"POST /api/v1/applications": func(w http.ResponseWriter, r *http.Request) {
				var created v1alpha1.Application
				_ = json.NewDecoder(r.Body).Decode(&created)
				// Verify temp app has expected labels
				if created.Labels[labelTempApp] != "true" {
					t.Error("expected temp app label")
				}
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(created)
			},
		})
		defer ts.Close()

		client := newTestClient(ts)
		mgr := NewTempAppManager(client)

		name, err := mgr.CreateTempApp(context.Background(), TempAppConfig{
			OriginalAppName: "my-app",
			TargetBranch:    "feature/xyz",
			PRNumber:        5,
			PRRepo:          "https://github.com/org/repo",
			Suffix:          "head",
		})
		if err != nil {
			t.Fatalf("CreateTempApp() error = %v", err)
		}
		if name == "" {
			t.Error("expected non-empty temp app name")
		}
	})

	t.Run("creates temp app from spec override", func(t *testing.T) {
		appSpec := sampleApp("my-app", "https://github.com/org/repo", "manifests", "main")

		ts := diffTestServer(t, map[string]http.HandlerFunc{
			"POST /api/v1/applications": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{}`))
			},
		})
		defer ts.Close()

		client := newTestClient(ts)
		mgr := NewTempAppManager(client)

		name, err := mgr.CreateTempApp(context.Background(), TempAppConfig{
			OriginalAppName: "my-app",
			TargetBranch:    "feature/xyz",
			PRNumber:        5,
			PRRepo:          "https://github.com/org/repo",
			Suffix:          "base",
			AppSpecOverride: appSpec,
		})
		if err != nil {
			t.Fatalf("CreateTempApp() error = %v", err)
		}
		if name == "" {
			t.Error("expected non-empty temp app name")
		}
	})

	t.Run("error when get app fails", func(t *testing.T) {
		ts := diffTestServer(t, map[string]http.HandlerFunc{
			"GET /api/v1/applications/": func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, `{"message":"not found"}`, http.StatusNotFound)
			},
		})
		defer ts.Close()

		client := newTestClient(ts)
		mgr := NewTempAppManager(client)

		_, err := mgr.CreateTempApp(context.Background(), TempAppConfig{
			OriginalAppName: "nonexistent",
			TargetBranch:    "feature/xyz",
			PRNumber:        5,
			PRRepo:          "https://github.com/org/repo",
			Suffix:          "head",
		})
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestTempAppManager_CreateNewApp(t *testing.T) {
	t.Run("creates new app when it does not exist", func(t *testing.T) {
		appSpec := sampleApp("new-app", "https://github.com/org/repo", "manifests", "main")

		ts := diffTestServer(t, map[string]http.HandlerFunc{
			"GET /api/v1/applications/": func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, `{"message":"not found"}`, http.StatusNotFound)
			},
			"POST /api/v1/applications": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{}`))
			},
		})
		defer ts.Close()

		client := newTestClient(ts)
		mgr := NewTempAppManager(client)

		name, err := mgr.CreateNewApp(context.Background(), TempAppConfig{
			OriginalAppName: "new-app",
			TargetBranch:    "feature/add",
			PRNumber:        1,
			PRRepo:          "https://github.com/org/repo",
			AppSpecOverride: appSpec,
		})
		if err != nil {
			t.Fatalf("CreateNewApp() error = %v", err)
		}
		if name != "new-app" {
			t.Errorf("expected name %q, got %q", "new-app", name)
		}
	})

	t.Run("deletes existing app before creating", func(t *testing.T) {
		appSpec := sampleApp("existing-app", "https://github.com/org/repo", "manifests", "main")
		var deleteCalled bool

		ts := diffTestServer(t, map[string]http.HandlerFunc{
			"GET /api/v1/applications/": func(w http.ResponseWriter, r *http.Request) {
				// App exists
				_ = json.NewEncoder(w).Encode(appSpec)
			},
			"DELETE /api/v1/applications/": func(w http.ResponseWriter, r *http.Request) {
				deleteCalled = true
				w.WriteHeader(http.StatusOK)
			},
			"POST /api/v1/applications": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{}`))
			},
		})
		defer ts.Close()

		client := newTestClient(ts)
		mgr := NewTempAppManager(client)

		_, err := mgr.CreateNewApp(context.Background(), TempAppConfig{
			OriginalAppName: "existing-app",
			TargetBranch:    "feature/add",
			PRNumber:        1,
			PRRepo:          "https://github.com/org/repo",
			AppSpecOverride: appSpec,
		})
		if err != nil {
			t.Fatalf("CreateNewApp() error = %v", err)
		}
		if !deleteCalled {
			t.Error("expected existing app to be deleted before creating")
		}
	})

	t.Run("error when no spec override provided", func(t *testing.T) {
		ts := diffTestServer(t, map[string]http.HandlerFunc{})
		defer ts.Close()

		client := newTestClient(ts)
		mgr := NewTempAppManager(client)

		_, err := mgr.CreateNewApp(context.Background(), TempAppConfig{
			OriginalAppName: "my-app",
			TargetBranch:    "feature/add",
			PRNumber:        1,
			PRRepo:          "https://github.com/org/repo",
		})
		if err == nil {
			t.Fatal("expected error when AppSpecOverride is nil")
		}
	})
}

func TestTempAppManager_WaitForManifests(t *testing.T) {
	cm := manifestJSON("ConfigMap", "test", "default", map[string]any{"data": map[string]any{"key": "val"}})

	t.Run("returns manifests when available immediately", func(t *testing.T) {
		ts := diffTestServer(t, map[string]http.HandlerFunc{
			"GET /api/v1/applications/": func(w http.ResponseWriter, r *http.Request) {
				resp := manifestResponse{Manifests: []string{cm}}
				_ = json.NewEncoder(w).Encode(resp)
			},
		})
		defer ts.Close()

		client := newTestClient(ts)
		mgr := NewTempAppManager(client)

		manifests, err := mgr.WaitForManifests(context.Background(), "test-app", 5*time.Second)
		if err != nil {
			t.Fatalf("WaitForManifests() error = %v", err)
		}
		if len(manifests) != 1 {
			t.Errorf("expected 1 manifest, got %d", len(manifests))
		}
	})

	t.Run("retries when manifests not yet ready", func(t *testing.T) {
		callCount := 0
		ts := diffTestServer(t, map[string]http.HandlerFunc{
			"GET /api/v1/applications/": func(w http.ResponseWriter, r *http.Request) {
				callCount++
				if callCount <= 2 {
					http.Error(w, "rendering in progress", http.StatusInternalServerError)
					return
				}
				resp := manifestResponse{Manifests: []string{cm}}
				_ = json.NewEncoder(w).Encode(resp)
			},
		})
		defer ts.Close()

		client := newTestClient(ts)
		mgr := NewTempAppManager(client)

		manifests, err := mgr.WaitForManifests(context.Background(), "test-app", 30*time.Second)
		if err != nil {
			t.Fatalf("WaitForManifests() error = %v", err)
		}
		if len(manifests) != 1 {
			t.Errorf("expected 1 manifest, got %d", len(manifests))
		}
		if callCount <= 2 {
			t.Error("expected at least 2 retries")
		}
	})

	t.Run("returns error on context cancellation", func(t *testing.T) {
		ts := diffTestServer(t, map[string]http.HandlerFunc{
			"GET /api/v1/applications/": func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "rendering in progress", http.StatusInternalServerError)
			},
		})
		defer ts.Close()

		client := newTestClient(ts)
		mgr := NewTempAppManager(client)

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		_, err := mgr.WaitForManifests(ctx, "test-app", 30*time.Second)
		if err == nil {
			t.Fatal("expected error on cancelled context")
		}
	})
}

func TestTempAppManager_DeleteTempApp(t *testing.T) {
	var deleteCalled bool
	ts := diffTestServer(t, map[string]http.HandlerFunc{
		"DELETE /api/v1/applications/": func(w http.ResponseWriter, r *http.Request) {
			deleteCalled = true
			// Verify cascade=false
			if r.URL.Query().Get("cascade") != "false" {
				t.Error("expected cascade=false for temp app deletion")
			}
			w.WriteHeader(http.StatusOK)
		},
	})
	defer ts.Close()

	client := newTestClient(ts)
	mgr := NewTempAppManager(client)

	err := mgr.DeleteTempApp(context.Background(), "my-app-lemuria-pr1-head")
	if err != nil {
		t.Fatalf("DeleteTempApp() error = %v", err)
	}
	if !deleteCalled {
		t.Error("expected delete to be called")
	}
}

func TestTempAppManager_CleanupStaleApps(t *testing.T) {
	staleTime := time.Now().Add(-8 * 24 * time.Hour).Format(time.RFC3339)
	freshTime := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)

	apps := v1alpha1.ApplicationList{
		Items: []v1alpha1.Application{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "stale-app",
					Labels: map[string]string{
						labelTempApp:   "true",
						labelCreatedAt: staleTime,
					},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "fresh-app",
					Labels: map[string]string{
						labelTempApp:   "true",
						labelCreatedAt: freshTime,
					},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "no-timestamp-app",
					Labels: map[string]string{
						labelTempApp: "true",
					},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "bad-timestamp-app",
					Labels: map[string]string{
						labelTempApp:   "true",
						labelCreatedAt: "not-a-time",
					},
				},
			},
		},
	}

	var deletedApps []string
	ts := diffTestServer(t, map[string]http.HandlerFunc{
		"GET /api/v1/applications": func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(apps)
		},
		"DELETE /api/v1/applications/": func(w http.ResponseWriter, r *http.Request) {
			name := strings.TrimPrefix(r.URL.Path, "/api/v1/applications/")
			deletedApps = append(deletedApps, name)
			w.WriteHeader(http.StatusOK)
		},
	})
	defer ts.Close()

	client := newTestClient(ts)
	mgr := NewTempAppManager(client)

	deleted, err := mgr.CleanupStaleApps(context.Background(), 7*24*time.Hour)
	if err != nil {
		t.Fatalf("CleanupStaleApps() error = %v", err)
	}
	if deleted != 1 {
		t.Errorf("expected 1 deleted, got %d", deleted)
	}
	if len(deletedApps) != 1 || deletedApps[0] != "stale-app" {
		t.Errorf("expected only stale-app to be deleted, got %v", deletedApps)
	}
}

func TestGetMultiSourceManifests(t *testing.T) {
	cm := manifestJSON("ConfigMap", "config", "default", map[string]any{"data": map[string]any{"key": "val"}})
	deploy := manifestJSON("Deployment", "web", "default", map[string]any{"spec": map[string]any{"replicas": 1}})

	t.Run("single source app delegates to GetManifests", func(t *testing.T) {
		ts := diffTestServer(t, map[string]http.HandlerFunc{
			"GET /api/v1/applications/": func(w http.ResponseWriter, r *http.Request) {
				resp := manifestResponse{Manifests: []string{cm}}
				_ = json.NewEncoder(w).Encode(resp)
			},
		})
		defer ts.Close()

		client := newTestClient(ts)
		app := models.Application{
			Name:    "my-app",
			RepoURL: "https://github.com/org/repo",
			Path:    "manifests",
		}

		manifests, err := client.GetMultiSourceManifests(context.Background(), app, "abc123")
		if err != nil {
			t.Fatalf("GetMultiSourceManifests() error = %v", err)
		}
		if len(manifests) != 1 {
			t.Errorf("expected 1 manifest, got %d", len(manifests))
		}
	})

	t.Run("multi source app fetches per-source manifests", func(t *testing.T) {
		callCount := 0
		ts := diffTestServer(t, map[string]http.HandlerFunc{
			"GET /api/v1/applications/": func(w http.ResponseWriter, r *http.Request) {
				callCount++
				manifest := cm
				if callCount > 1 {
					manifest = deploy
				}
				resp := manifestResponse{Manifests: []string{manifest}}
				_ = json.NewEncoder(w).Encode(resp)
			},
		})
		defer ts.Close()

		client := newTestClient(ts)
		app := models.Application{
			Name: "multi-app",
			Sources: []models.ApplicationSource{
				{RepoURL: "https://github.com/org/repo", Path: "base"},
				{RepoURL: "https://charts.example.com", Chart: "nginx"},
			},
		}

		manifests, err := client.GetMultiSourceManifests(context.Background(), app, "abc123")
		if err != nil {
			t.Fatalf("GetMultiSourceManifests() error = %v", err)
		}
		if len(manifests) != 2 {
			t.Errorf("expected 2 manifests, got %d", len(manifests))
		}
	})

	t.Run("multi source app returns error on source failure", func(t *testing.T) {
		ts := diffTestServer(t, map[string]http.HandlerFunc{
			"GET /api/v1/applications/": func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, `{"message":"error"}`, http.StatusInternalServerError)
			},
		})
		defer ts.Close()

		client := newTestClient(ts)
		app := models.Application{
			Name: "multi-app",
			Sources: []models.ApplicationSource{
				{RepoURL: "https://github.com/org/repo", Path: "base"},
			},
		}

		_, err := client.GetMultiSourceManifests(context.Background(), app, "abc123")
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestDiffDeletedApp_BranchMode_TempAppCreateFails(t *testing.T) {
	ts := diffTestServer(t, map[string]http.HandlerFunc{
		"GET /api/v1/applications/": func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, `{"message":"error"}`, http.StatusInternalServerError)
		},
	})
	defer ts.Close()

	client := newTestClient(ts)
	_, err := client.DiffDeletedApp(context.Background(), "delete-me", DiffOptions{
		Mode:         DiffModeBranch,
		BaseBranch:   "main",
		TargetBranch: "feature/remove",
		PRNumber:     1,
		PRRepo:       "https://github.com/org/repo",
		Timeout:      5 * time.Second,
	})
	if err == nil {
		t.Fatal("expected error when temp app creation fails")
	}
	if !strings.Contains(err.Error(), "creating base temp app") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDiffDeletedApp_BranchMode_ManifestWaitFails(t *testing.T) {
	app := sampleApp("delete-me", "https://github.com/org/repo", "manifests", "main")

	ts := diffTestServer(t, map[string]http.HandlerFunc{
		"GET /api/v1/applications/": func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/manifests") {
				http.Error(w, `{"message":"rendering failed"}`, http.StatusInternalServerError)
				return
			}
			_ = json.NewEncoder(w).Encode(app)
		},
		"POST /api/v1/applications": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		},
		"DELETE /api/v1/applications/": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		},
	})
	defer ts.Close()

	client := newTestClient(ts)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err := client.DiffDeletedApp(ctx, "delete-me", DiffOptions{
		Mode:       DiffModeBranch,
		BaseBranch: "main",
		PRNumber:   1,
		PRRepo:     "https://github.com/org/repo",
		Timeout:    1 * time.Second,
	})
	if err == nil {
		t.Fatal("expected error when manifest wait fails")
	}
	if !strings.Contains(err.Error(), "waiting for base manifests") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDiffDeletedBoth_Errors(t *testing.T) {
	t.Run("error when managed resources fail", func(t *testing.T) {
		ts := diffTestServer(t, map[string]http.HandlerFunc{
			"GET /api/v1/applications/": func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/managed-resources") {
					http.Error(w, `{"message":"error"}`, http.StatusInternalServerError)
					return
				}
			},
		})
		defer ts.Close()

		client := newTestClient(ts)
		_, err := client.DiffDeletedApp(context.Background(), "delete-me", DiffOptions{
			Mode:       DiffModeBoth,
			BaseBranch: "main",
			PRNumber:   1,
			PRRepo:     "https://github.com/org/repo",
			Timeout:    5 * time.Second,
		})
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("error when temp app creation fails", func(t *testing.T) {
		ts := diffTestServer(t, map[string]http.HandlerFunc{
			"GET /api/v1/applications/": func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/managed-resources") {
					resp := struct {
						Items []ManagedResource `json:"items"`
					}{Items: []ManagedResource{}}
					_ = json.NewEncoder(w).Encode(resp)
					return
				}
				// GetApplicationRaw fails
				http.Error(w, `{"message":"error"}`, http.StatusInternalServerError)
			},
		})
		defer ts.Close()

		client := newTestClient(ts)
		_, err := client.DiffDeletedApp(context.Background(), "delete-me", DiffOptions{
			Mode:       DiffModeBoth,
			BaseBranch: "main",
			PRNumber:   1,
			PRRepo:     "https://github.com/org/repo",
			Timeout:    5 * time.Second,
		})
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("error when manifest wait fails", func(t *testing.T) {
		app := sampleApp("delete-me", "https://github.com/org/repo", "manifests", "main")

		ts := diffTestServer(t, map[string]http.HandlerFunc{
			"GET /api/v1/applications/": func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/managed-resources") {
					resp := struct {
						Items []ManagedResource `json:"items"`
					}{Items: []ManagedResource{}}
					_ = json.NewEncoder(w).Encode(resp)
					return
				}
				if strings.HasSuffix(r.URL.Path, "/manifests") {
					http.Error(w, `{"message":"rendering failed"}`, http.StatusInternalServerError)
					return
				}
				_ = json.NewEncoder(w).Encode(app)
			},
			"POST /api/v1/applications": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{}`))
			},
			"DELETE /api/v1/applications/": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			},
		})
		defer ts.Close()

		client := newTestClient(ts)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		_, err := client.DiffDeletedApp(ctx, "delete-me", DiffOptions{
			Mode:       DiffModeBoth,
			BaseBranch: "main",
			PRNumber:   1,
			PRRepo:     "https://github.com/org/repo",
			Timeout:    1 * time.Second,
		})
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestDiffBothMode_Errors(t *testing.T) {
	t.Run("error when managed resources fail", func(t *testing.T) {
		ts := diffTestServer(t, map[string]http.HandlerFunc{
			"GET /api/v1/applications/": func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/managed-resources") {
					http.Error(w, `{"message":"error"}`, http.StatusInternalServerError)
					return
				}
			},
		})
		defer ts.Close()

		client := newTestClient(ts)
		_, err := client.GetApplicationDiff(context.Background(), "my-app", DiffOptions{
			Mode:         DiffModeBoth,
			BaseBranch:   "main",
			TargetBranch: "feature/update",
			PRNumber:     10,
			PRRepo:       "https://github.com/org/repo",
		})
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "getting live resources") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("error when base temp app creation fails", func(t *testing.T) {
		ts := diffTestServer(t, map[string]http.HandlerFunc{
			"GET /api/v1/applications/": func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/managed-resources") {
					resp := struct {
						Items []ManagedResource `json:"items"`
					}{Items: []ManagedResource{}}
					_ = json.NewEncoder(w).Encode(resp)
					return
				}
				http.Error(w, `{"message":"error"}`, http.StatusInternalServerError)
			},
		})
		defer ts.Close()

		client := newTestClient(ts)
		_, err := client.GetApplicationDiff(context.Background(), "my-app", DiffOptions{
			Mode:         DiffModeBoth,
			BaseBranch:   "main",
			TargetBranch: "feature/update",
			PRNumber:     10,
			PRRepo:       "https://github.com/org/repo",
		})
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "creating base temp app") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("error when target temp app creation fails", func(t *testing.T) {
		app := sampleApp("my-app", "https://github.com/org/repo", "manifests", "main")
		createCount := 0

		ts := diffTestServer(t, map[string]http.HandlerFunc{
			"GET /api/v1/applications/": func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/managed-resources") {
					resp := struct {
						Items []ManagedResource `json:"items"`
					}{Items: []ManagedResource{}}
					_ = json.NewEncoder(w).Encode(resp)
					return
				}
				_ = json.NewEncoder(w).Encode(app)
			},
			"POST /api/v1/applications": func(w http.ResponseWriter, r *http.Request) {
				createCount++
				if createCount > 1 {
					http.Error(w, `{"message":"error"}`, http.StatusForbidden)
					return
				}
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{}`))
			},
			"DELETE /api/v1/applications/": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			},
		})
		defer ts.Close()

		client := newTestClient(ts)
		_, err := client.GetApplicationDiff(context.Background(), "my-app", DiffOptions{
			Mode:         DiffModeBoth,
			BaseBranch:   "main",
			TargetBranch: "feature/update",
			PRNumber:     10,
			PRRepo:       "https://github.com/org/repo",
		})
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "creating target temp app") {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

func TestDiffNewApp_DefaultTimeout(t *testing.T) {
	cm := manifestJSON("ConfigMap", "config", "default", map[string]any{"data": map[string]any{"key": "val"}})

	ts := diffTestServer(t, map[string]http.HandlerFunc{
		"GET /api/v1/applications/": func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/manifests") {
				resp := manifestResponse{Manifests: []string{cm}}
				_ = json.NewEncoder(w).Encode(resp)
				return
			}
			http.Error(w, `{"message":"not found"}`, http.StatusNotFound)
		},
		"POST /api/v1/applications": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		},
		"DELETE /api/v1/applications/": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		},
	})
	defer ts.Close()

	client := newTestClient(ts)
	appSpec := sampleApp("new-app", "https://github.com/org/repo", "manifests", "main")

	// Timeout=0 should use default (2 minutes)
	_, err := client.DiffNewApp(context.Background(), appSpec, DiffOptions{
		TargetBranch: "feature/add-app",
		PRNumber:     1,
		PRRepo:       "https://github.com/org/repo",
	})
	if err != nil {
		t.Fatalf("DiffNewApp() with default timeout error = %v", err)
	}
}

func TestDiffDeletedApp_DefaultTimeout(t *testing.T) {
	cm := manifestJSON("ConfigMap", "config", "default", map[string]any{"data": map[string]any{"key": "val"}})

	ts := diffTestServer(t, map[string]http.HandlerFunc{
		"GET /api/v1/applications/": func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/managed-resources") {
				resp := struct {
					Items []ManagedResource `json:"items"`
				}{
					Items: []ManagedResource{
						{Kind: "ConfigMap", Name: "config", Namespace: "default", LiveState: cm},
					},
				}
				_ = json.NewEncoder(w).Encode(resp)
				return
			}
		},
	})
	defer ts.Close()

	client := newTestClient(ts)
	// Timeout=0 should use default
	_, err := client.DiffDeletedApp(context.Background(), "delete-me", DiffOptions{
		Mode: DiffModeLive,
	})
	if err != nil {
		t.Fatalf("DiffDeletedApp() with default timeout error = %v", err)
	}
}

func TestCreateNewApp_DeleteExistingFails(t *testing.T) {
	appSpec := sampleApp("existing-app", "https://github.com/org/repo", "manifests", "main")

	ts := diffTestServer(t, map[string]http.HandlerFunc{
		"GET /api/v1/applications/": func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(appSpec)
		},
		"DELETE /api/v1/applications/": func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, `{"message":"cannot delete"}`, http.StatusForbidden)
		},
	})
	defer ts.Close()

	client := newTestClient(ts)
	mgr := NewTempAppManager(client)

	_, err := mgr.CreateNewApp(context.Background(), TempAppConfig{
		OriginalAppName: "existing-app",
		TargetBranch:    "feature/add",
		PRNumber:        1,
		PRRepo:          "https://github.com/org/repo",
		AppSpecOverride: appSpec,
	})
	if err == nil {
		t.Fatal("expected error when delete fails")
	}
	if !strings.Contains(err.Error(), "deleting existing app") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCreateNewApp_CreateFails(t *testing.T) {
	appSpec := sampleApp("new-app", "https://github.com/org/repo", "manifests", "main")

	ts := diffTestServer(t, map[string]http.HandlerFunc{
		"GET /api/v1/applications/": func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, `{"message":"not found"}`, http.StatusNotFound)
		},
		"POST /api/v1/applications": func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, `{"message":"error"}`, http.StatusInternalServerError)
		},
	})
	defer ts.Close()

	client := newTestClient(ts)
	mgr := NewTempAppManager(client)

	_, err := mgr.CreateNewApp(context.Background(), TempAppConfig{
		OriginalAppName: "new-app",
		TargetBranch:    "feature/add",
		PRNumber:        1,
		PRRepo:          "https://github.com/org/repo",
		AppSpecOverride: appSpec,
	})
	if err == nil {
		t.Fatal("expected error when create fails")
	}
	if !strings.Contains(err.Error(), "creating new app") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestBranchMode_TargetManifestWaitFails(t *testing.T) {
	app := sampleApp("my-app", "https://github.com/org/repo", "manifests", "main")
	cmBase := manifestJSON("ConfigMap", "config", "default", map[string]any{"data": map[string]any{"key": "val"}})
	manifestCallCount := 0

	ts := diffTestServer(t, map[string]http.HandlerFunc{
		"GET /api/v1/applications/": func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/manifests") {
				manifestCallCount++
				if manifestCallCount <= 1 {
					// First call (base) succeeds
					resp := manifestResponse{Manifests: []string{cmBase}}
					_ = json.NewEncoder(w).Encode(resp)
					return
				}
				// Second call (target) always fails
				http.Error(w, `{"message":"rendering failed"}`, http.StatusInternalServerError)
				return
			}
			_ = json.NewEncoder(w).Encode(app)
		},
		"POST /api/v1/applications": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		},
		"DELETE /api/v1/applications/": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		},
	})
	defer ts.Close()

	client := newTestClient(ts)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.GetApplicationDiff(ctx, "my-app", DiffOptions{
		Mode:         DiffModeBranch,
		BaseBranch:   "main",
		TargetBranch: "feature/update",
		PRNumber:     10,
		PRRepo:       "https://github.com/org/repo",
		Timeout:      1 * time.Second,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "waiting for target manifests") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestBothMode_BaseManifestWaitFails(t *testing.T) {
	app := sampleApp("my-app", "https://github.com/org/repo", "manifests", "main")

	ts := diffTestServer(t, map[string]http.HandlerFunc{
		"GET /api/v1/applications/": func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/managed-resources") {
				resp := struct {
					Items []ManagedResource `json:"items"`
				}{Items: []ManagedResource{}}
				_ = json.NewEncoder(w).Encode(resp)
				return
			}
			if strings.HasSuffix(r.URL.Path, "/manifests") {
				http.Error(w, `{"message":"rendering failed"}`, http.StatusInternalServerError)
				return
			}
			_ = json.NewEncoder(w).Encode(app)
		},
		"POST /api/v1/applications": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		},
		"DELETE /api/v1/applications/": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		},
	})
	defer ts.Close()

	client := newTestClient(ts)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.GetApplicationDiff(ctx, "my-app", DiffOptions{
		Mode:         DiffModeBoth,
		BaseBranch:   "main",
		TargetBranch: "feature/update",
		PRNumber:     10,
		PRRepo:       "https://github.com/org/repo",
		Timeout:      1 * time.Second,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "waiting for base manifests") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestBothMode_TargetManifestWaitFails(t *testing.T) {
	app := sampleApp("my-app", "https://github.com/org/repo", "manifests", "main")
	cmBase := manifestJSON("ConfigMap", "config", "default", map[string]any{"data": map[string]any{"key": "val"}})
	manifestCallCount := 0

	ts := diffTestServer(t, map[string]http.HandlerFunc{
		"GET /api/v1/applications/": func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/managed-resources") {
				resp := struct {
					Items []ManagedResource `json:"items"`
				}{Items: []ManagedResource{}}
				_ = json.NewEncoder(w).Encode(resp)
				return
			}
			if strings.HasSuffix(r.URL.Path, "/manifests") {
				manifestCallCount++
				if manifestCallCount <= 1 {
					resp := manifestResponse{Manifests: []string{cmBase}}
					_ = json.NewEncoder(w).Encode(resp)
					return
				}
				http.Error(w, `{"message":"rendering failed"}`, http.StatusInternalServerError)
				return
			}
			_ = json.NewEncoder(w).Encode(app)
		},
		"POST /api/v1/applications": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		},
		"DELETE /api/v1/applications/": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		},
	})
	defer ts.Close()

	client := newTestClient(ts)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.GetApplicationDiff(ctx, "my-app", DiffOptions{
		Mode:         DiffModeBoth,
		BaseBranch:   "main",
		TargetBranch: "feature/update",
		PRNumber:     10,
		PRRepo:       "https://github.com/org/repo",
		Timeout:      1 * time.Second,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "waiting for target manifests") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestLiveMode_TargetManifestWaitFails(t *testing.T) {
	app := sampleApp("my-app", "https://github.com/org/repo", "manifests", "main")

	ts := diffTestServer(t, map[string]http.HandlerFunc{
		"GET /api/v1/applications/": func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/managed-resources") {
				resp := struct {
					Items []ManagedResource `json:"items"`
				}{Items: []ManagedResource{}}
				_ = json.NewEncoder(w).Encode(resp)
				return
			}
			if strings.HasSuffix(r.URL.Path, "/manifests") {
				http.Error(w, `{"message":"rendering failed"}`, http.StatusInternalServerError)
				return
			}
			_ = json.NewEncoder(w).Encode(app)
		},
		"POST /api/v1/applications": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		},
		"DELETE /api/v1/applications/": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		},
	})
	defer ts.Close()

	client := newTestClient(ts)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.GetApplicationDiff(ctx, "my-app", DiffOptions{
		Mode:         DiffModeLive,
		TargetBranch: "feature/update",
		PRNumber:     10,
		PRRepo:       "https://github.com/org/repo",
		Timeout:      1 * time.Second,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "waiting for target manifests") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestLiveMode_TempAppCreationFails(t *testing.T) {
	ts := diffTestServer(t, map[string]http.HandlerFunc{
		"GET /api/v1/applications/": func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/managed-resources") {
				resp := struct {
					Items []ManagedResource `json:"items"`
				}{Items: []ManagedResource{}}
				_ = json.NewEncoder(w).Encode(resp)
				return
			}
			// GetApplicationRaw fails
			http.Error(w, `{"message":"error"}`, http.StatusInternalServerError)
		},
	})
	defer ts.Close()

	client := newTestClient(ts)
	_, err := client.GetApplicationDiff(context.Background(), "my-app", DiffOptions{
		Mode:         DiffModeLive,
		TargetBranch: "feature/update",
		PRNumber:     10,
		PRRepo:       "https://github.com/org/repo",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "creating target temp app") {
		t.Errorf("unexpected error: %v", err)
	}
}
