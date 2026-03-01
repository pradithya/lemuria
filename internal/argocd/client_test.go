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
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	v1alpha1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/org/lemuria/internal/models"
)

// newTestClient creates a Client pointing at the given test server.
func newTestClient(ts *httptest.Server) *Client {
	return &Client{
		baseURL:    ts.URL,
		token:      "test-token",
		httpClient: ts.Client(),
	}
}

func TestClientGet(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		wantErr    bool
		wantErrMsg string
	}{
		{
			name:   "successful GET with JSON response",
			status: http.StatusOK,
			body:   `{"Version":"2.10.0"}`,
		},
		{
			name:       "404 error returns error with body",
			status:     http.StatusNotFound,
			body:       `{"message":"not found"}`,
			wantErr:    true,
			wantErrMsg: "API error (status 404)",
		},
		{
			name:       "500 error returns error with body",
			status:     http.StatusInternalServerError,
			body:       `{"message":"internal error"}`,
			wantErr:    true,
			wantErrMsg: "API error (status 500)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer test-token" {
					t.Error("missing or incorrect Authorization header")
				}
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer ts.Close()

			client := newTestClient(ts)
			var result map[string]string
			err := client.get(context.Background(), "/api/test", nil, &result)

			if (err != nil) != tt.wantErr {
				t.Fatalf("get() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if !strings.Contains(err.Error(), tt.wantErrMsg) {
					t.Errorf("error = %q, want to contain %q", err.Error(), tt.wantErrMsg)
				}
			}
			if !tt.wantErr {
				if result["Version"] != "2.10.0" {
					t.Errorf("result = %v, want Version=2.10.0", result)
				}
			}
		})
	}
}

func TestIsNotFound(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "404 error",
			err:  fmt.Errorf("API error (status 404): not found"),
			want: true,
		},
		{
			name: "500 error",
			err:  fmt.Errorf("API error (status 500): internal error"),
			want: false,
		},
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "wrapped 404",
			err:  fmt.Errorf("getting app: %w", fmt.Errorf("API error (status 404): not found")),
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsNotFound(tt.err)
			if got != tt.want {
				t.Errorf("IsNotFound(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestClientPost(t *testing.T) {
	tests := []struct {
		name       string
		payload    any
		status     int
		respBody   string
		wantErr    bool
		wantErrMsg string
	}{
		{
			name:     "successful POST with payload and result",
			payload:  map[string]string{"name": "my-app"},
			status:   http.StatusOK,
			respBody: `{"status":"ok"}`,
		},
		{
			name:     "POST with nil payload",
			payload:  nil,
			status:   http.StatusOK,
			respBody: `{"status":"ok"}`,
		},
		{
			name:       "error response",
			payload:    map[string]string{"name": "bad"},
			status:     http.StatusBadRequest,
			respBody:   `{"error":"bad request"}`,
			wantErr:    true,
			wantErrMsg: "API error (status 400)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Errorf("method = %q, want POST", r.Method)
				}
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.respBody))
			}))
			defer ts.Close()

			client := newTestClient(ts)
			var result map[string]string
			err := client.post(context.Background(), "/api/test", nil, tt.payload, &result)

			if (err != nil) != tt.wantErr {
				t.Fatalf("post() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && result["status"] != "ok" {
				t.Errorf("result = %v, want status=ok", result)
			}
		})
	}
}

func TestClientDelete(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		wantErr bool
	}{
		{
			name:   "successful DELETE 200",
			status: http.StatusOK,
		},
		{
			name:   "successful DELETE 204",
			status: http.StatusNoContent,
		},
		{
			name:    "error response",
			status:  http.StatusForbidden,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodDelete {
					t.Errorf("method = %q, want DELETE", r.Method)
				}
				w.WriteHeader(tt.status)
			}))
			defer ts.Close()

			client := newTestClient(ts)
			err := client.delete(context.Background(), "/api/test", nil)

			if (err != nil) != tt.wantErr {
				t.Fatalf("delete() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestListApplications(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/applications" {
			t.Errorf("path = %q, want /api/v1/applications", r.URL.Path)
		}

		// Verify that ListApplications passes the temp-app exclusion selector
		selector := r.URL.Query().Get("selector")
		wantSelector := "!" + labelTempApp
		if selector != wantSelector {
			t.Errorf("selector = %q, want %q", selector, wantSelector)
		}

		resp := map[string]any{
			"items": []map[string]any{
				{
					"metadata": map[string]any{
						"name":      "app-one",
						"namespace": "argocd",
					},
					"spec": map[string]any{
						"project": "default",
						"source": map[string]any{
							"repoURL":        "https://github.com/org/repo",
							"path":           "manifests",
							"targetRevision": "main",
						},
						"destination": map[string]any{
							"server":    "https://kubernetes.default.svc",
							"namespace": "production",
						},
					},
					"status": map[string]any{
						"sync":   map[string]any{"status": "Synced"},
						"health": map[string]any{"status": "Healthy"},
					},
				},
				{
					"metadata": map[string]any{
						"name":      "app-two",
						"namespace": "argocd",
					},
					"spec": map[string]any{
						"project": "my-project",
						"source": map[string]any{
							"repoURL":        "https://github.com/org/other",
							"path":           "k8s",
							"targetRevision": "develop",
						},
						"destination": map[string]any{
							"server": "https://kubernetes.default.svc",
						},
					},
					"status": map[string]any{
						"sync":   map[string]any{"status": "OutOfSync"},
						"health": map[string]any{"status": "Degraded"},
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	client := newTestClient(ts)
	apps, err := client.ListApplications(context.Background())
	if err != nil {
		t.Fatalf("ListApplications() error = %v", err)
	}

	if len(apps) != 2 {
		t.Fatalf("got %d apps, want 2", len(apps))
	}

	if apps[0].Name != "app-one" {
		t.Errorf("apps[0].Name = %q, want app-one", apps[0].Name)
	}
	if apps[0].RepoURL != "https://github.com/org/repo" {
		t.Errorf("apps[0].RepoURL = %q", apps[0].RepoURL)
	}
	if apps[1].Name != "app-two" {
		t.Errorf("apps[1].Name = %q, want app-two", apps[1].Name)
	}
	if apps[1].Project != "my-project" {
		t.Errorf("apps[1].Project = %q, want my-project", apps[1].Project)
	}
}

func TestListApplicationsExcludesTempApps(t *testing.T) {
	// Simulate an ArgoCD API that respects the selector and only returns
	// non-temp apps. This test verifies:
	// 1. ListApplications sends the correct selector to exclude temp apps
	// 2. ListApplicationsWithSelector (used by CleanupStaleApps) can still find temp apps
	var listCalls []string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		selector := r.URL.Query().Get("selector")
		listCalls = append(listCalls, selector)

		var items []map[string]any

		// Real app - always present
		realApp := map[string]any{
			"metadata": map[string]any{
				"name":      "my-app",
				"namespace": "argocd",
			},
			"spec": map[string]any{
				"project": "default",
				"source": map[string]any{
					"repoURL":        "https://github.com/org/repo",
					"path":           "manifests",
					"targetRevision": "main",
				},
				"destination": map[string]any{
					"server": "https://kubernetes.default.svc",
				},
			},
			"status": map[string]any{
				"sync":   map[string]any{"status": "Synced"},
				"health": map[string]any{"status": "Healthy"},
			},
		}

		// Temp app - only returned when explicitly selecting temp apps
		tempApp := map[string]any{
			"metadata": map[string]any{
				"name":      "my-app-lemuria-pr1-base",
				"namespace": "argocd",
				"labels": map[string]any{
					labelTempApp: "true",
				},
			},
			"spec": map[string]any{
				"project": "default",
				"source": map[string]any{
					"repoURL":        "https://github.com/org/repo",
					"path":           "manifests",
					"targetRevision": "main",
				},
				"destination": map[string]any{
					"server": "https://kubernetes.default.svc",
				},
			},
			"status": map[string]any{
				"sync":   map[string]any{"status": "OutOfSync"},
				"health": map[string]any{"status": "Progressing"},
			},
		}

		switch selector {
		case labelTempApp + "=true":
			// CleanupStaleApps path: only return temp apps
			items = []map[string]any{tempApp}
		case "!" + labelTempApp:
			// ListApplications path: only return real apps
			items = []map[string]any{realApp}
		default:
			// No selector: return all
			items = []map[string]any{realApp, tempApp}
		}

		resp := map[string]any{"items": items}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	client := newTestClient(ts)

	// ListApplications should exclude temp apps
	apps, err := client.ListApplications(context.Background())
	if err != nil {
		t.Fatalf("ListApplications() error = %v", err)
	}
	if len(apps) != 1 {
		t.Fatalf("ListApplications() got %d apps, want 1 (should exclude temp apps)", len(apps))
	}
	if apps[0].Name != "my-app" {
		t.Errorf("ListApplications() apps[0].Name = %q, want my-app", apps[0].Name)
	}

	// ListApplicationsWithSelector for temp apps should return only temp apps
	tempApps, err := client.ListApplicationsWithSelector(context.Background(), labelTempApp+"=true")
	if err != nil {
		t.Fatalf("ListApplicationsWithSelector() error = %v", err)
	}
	if len(tempApps) != 1 {
		t.Fatalf("ListApplicationsWithSelector(temp) got %d apps, want 1", len(tempApps))
	}
	if tempApps[0].Name != "my-app-lemuria-pr1-base" {
		t.Errorf("temp app name = %q, want my-app-lemuria-pr1-base", tempApps[0].Name)
	}

	// Verify the selectors sent to the API
	if len(listCalls) != 2 {
		t.Fatalf("expected 2 API calls, got %d", len(listCalls))
	}
	if listCalls[0] != "!"+labelTempApp {
		t.Errorf("first call selector = %q, want %q", listCalls[0], "!"+labelTempApp)
	}
	if listCalls[1] != labelTempApp+"=true" {
		t.Errorf("second call selector = %q, want %q", listCalls[1], labelTempApp+"=true")
	}
}

func TestGetApplication(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/applications/my-app" {
			t.Errorf("path = %q, want /api/v1/applications/my-app", r.URL.Path)
		}
		resp := map[string]any{
			"metadata": map[string]any{
				"name":      "my-app",
				"namespace": "argocd",
			},
			"spec": map[string]any{
				"project": "default",
				"source": map[string]any{
					"repoURL":        "https://github.com/org/repo",
					"path":           "manifests",
					"targetRevision": "main",
				},
				"destination": map[string]any{
					"server":    "https://kubernetes.default.svc",
					"namespace": "default",
				},
			},
			"status": map[string]any{
				"sync":   map[string]any{"status": "Synced"},
				"health": map[string]any{"status": "Healthy"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	client := newTestClient(ts)
	app, err := client.GetApplication(context.Background(), "my-app")
	if err != nil {
		t.Fatalf("GetApplication() error = %v", err)
	}
	if app.Name != "my-app" {
		t.Errorf("Name = %q, want my-app", app.Name)
	}
	if app.Project != "default" {
		t.Errorf("Project = %q, want default", app.Project)
	}
}

func TestFindApplicationsByRepo(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"items": []map[string]any{
				{
					"metadata": map[string]any{"name": "matching-app", "namespace": "argocd"},
					"spec": map[string]any{
						"project": "default",
						"source": map[string]any{
							"repoURL":        "https://github.com/org/target-repo",
							"path":           "manifests",
							"targetRevision": "main",
						},
						"destination": map[string]any{"server": "https://kubernetes.default.svc"},
					},
					"status": map[string]any{
						"sync":   map[string]any{"status": "Synced"},
						"health": map[string]any{"status": "Healthy"},
					},
				},
				{
					"metadata": map[string]any{"name": "other-app", "namespace": "argocd"},
					"spec": map[string]any{
						"project": "default",
						"source": map[string]any{
							"repoURL":        "https://github.com/org/other-repo",
							"path":           "k8s",
							"targetRevision": "main",
						},
						"destination": map[string]any{"server": "https://kubernetes.default.svc"},
					},
					"status": map[string]any{
						"sync":   map[string]any{"status": "Synced"},
						"health": map[string]any{"status": "Healthy"},
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	client := newTestClient(ts)
	apps, err := client.FindApplicationsByRepo(context.Background(), "https://github.com/org/target-repo.git")
	if err != nil {
		t.Fatalf("FindApplicationsByRepo() error = %v", err)
	}
	if len(apps) != 1 {
		t.Fatalf("got %d apps, want 1", len(apps))
	}
	if apps[0].Name != "matching-app" {
		t.Errorf("Name = %q, want matching-app", apps[0].Name)
	}
}

func TestVersion(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/version" {
			t.Errorf("path = %q, want /api/version", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Version":"2.10.0"}`))
	}))
	defer ts.Close()

	client := newTestClient(ts)
	version, err := client.Version(context.Background())
	if err != nil {
		t.Fatalf("Version() error = %v", err)
	}
	if version != "2.10.0" {
		t.Errorf("Version = %q, want 2.10.0", version)
	}
}

func TestClientPut(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("method = %q, want PUT", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"updated"}`))
	}))
	defer ts.Close()

	client := newTestClient(ts)
	var result map[string]string
	err := client.put(context.Background(), "/api/test", nil, map[string]string{"key": "val"}, &result)
	if err != nil {
		t.Fatalf("put() error = %v", err)
	}
	if result["status"] != "updated" {
		t.Errorf("result = %v, want status=updated", result)
	}
}

func TestGetResourceHealth(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		body      string
		wantErr   bool
		wantCount int
		checkFunc func(t *testing.T, results []models.ResourceHealthInfo)
	}{
		{
			name:   "resources with health info",
			status: http.StatusOK,
			body: `{
				"metadata": {"name": "my-app", "namespace": "argocd"},
				"spec": {"project": "default"},
				"status": {
					"resources": [
						{
							"group": "apps",
							"version": "v1",
							"kind": "Deployment",
							"name": "web",
							"namespace": "default",
							"health": {"status": "Healthy", "message": "running"}
						},
						{
							"version": "v1",
							"kind": "Service",
							"name": "web-svc",
							"namespace": "default",
							"health": {"status": "Healthy"}
						},
						{
							"version": "v1",
							"kind": "ConfigMap",
							"name": "config",
							"namespace": "default"
						}
					]
				}
			}`,
			wantCount: 2,
			checkFunc: func(t *testing.T, results []models.ResourceHealthInfo) {
				if results[0].Resource.APIVersion != "apps/v1" {
					t.Errorf("results[0].APIVersion = %q, want apps/v1", results[0].Resource.APIVersion)
				}
				if results[0].Resource.Kind != "Deployment" {
					t.Errorf("results[0].Kind = %q, want Deployment", results[0].Resource.Kind)
				}
				if results[0].HealthStatus != models.HealthStatusHealthy {
					t.Errorf("results[0].HealthStatus = %q, want Healthy", results[0].HealthStatus)
				}
				if results[0].HealthMessage != "running" {
					t.Errorf("results[0].HealthMessage = %q, want running", results[0].HealthMessage)
				}
				// Service has no group, so APIVersion should just be "v1"
				if results[1].Resource.APIVersion != "v1" {
					t.Errorf("results[1].APIVersion = %q, want v1", results[1].Resource.APIVersion)
				}
			},
		},
		{
			name:      "no resources with health",
			status:    http.StatusOK,
			body:      `{"metadata": {"name": "empty-app"}, "spec": {}, "status": {"resources": []}}`,
			wantCount: 0,
		},
		{
			name:    "API error",
			status:  http.StatusNotFound,
			body:    `{"message": "app not found"}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/applications/my-app" {
					t.Errorf("path = %q, want /api/v1/applications/my-app", r.URL.Path)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer ts.Close()

			client := newTestClient(ts)
			results, err := client.GetResourceHealth(context.Background(), "my-app")

			if (err != nil) != tt.wantErr {
				t.Fatalf("GetResourceHealth() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if len(results) != tt.wantCount {
				t.Fatalf("got %d results, want %d", len(results), tt.wantCount)
			}
			if tt.checkFunc != nil {
				tt.checkFunc(t, results)
			}
		})
	}
}

func TestUpdateApplicationSpec(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		wantErr bool
	}{
		{
			name:   "successful update",
			status: http.StatusOK,
		},
		{
			name:    "API error",
			status:  http.StatusBadRequest,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPut {
					t.Errorf("method = %q, want PUT", r.Method)
				}
				if r.URL.Path != "/api/v1/applications/my-app/spec" {
					t.Errorf("path = %q, want /api/v1/applications/my-app/spec", r.URL.Path)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(`{}`))
			}))
			defer ts.Close()

			client := newTestClient(ts)
			spec := v1alpha1.ApplicationSpec{
				Project: "default",
				Source: &v1alpha1.ApplicationSource{
					RepoURL:        "https://github.com/org/repo",
					Path:           "manifests",
					TargetRevision: "feature-branch",
				},
			}

			err := client.UpdateApplicationSpec(context.Background(), "my-app", spec)

			if (err != nil) != tt.wantErr {
				t.Fatalf("UpdateApplicationSpec() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestGetApplicationHistory(t *testing.T) {
	tests := []struct {
		name        string
		status      int
		body        string
		wantErr     bool
		wantHistory int
	}{
		{
			name:   "application with history",
			status: http.StatusOK,
			body: `{
				"metadata": {"name": "my-app", "namespace": "argocd"},
				"spec": {"project": "default"},
				"status": {
					"history": [
						{"id": 1, "revision": "abc123"},
						{"id": 2, "revision": "def456"}
					]
				}
			}`,
			wantHistory: 2,
		},
		{
			name:        "application with no history",
			status:      http.StatusOK,
			body:        `{"metadata": {"name": "new-app"}, "spec": {}, "status": {}}`,
			wantHistory: 0,
		},
		{
			name:    "API error",
			status:  http.StatusInternalServerError,
			body:    `{"message": "internal error"}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/applications/my-app" {
					t.Errorf("path = %q, want /api/v1/applications/my-app", r.URL.Path)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer ts.Close()

			client := newTestClient(ts)
			history, err := client.GetApplicationHistory(context.Background(), "my-app")

			if (err != nil) != tt.wantErr {
				t.Fatalf("GetApplicationHistory() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if len(history) != tt.wantHistory {
				t.Errorf("got %d history entries, want %d", len(history), tt.wantHistory)
			}
		})
	}
}

func TestRollbackApplication(t *testing.T) {
	tests := []struct {
		name       string
		opts       *RollbackOptions
		status     int
		body       string
		wantErr    bool
		wantErrMsg string
		wantPhase  models.SyncPhase
	}{
		{
			name:   "successful rollback",
			opts:   &RollbackOptions{ID: 5},
			status: http.StatusOK,
			body: `{
				"status": {
					"operationState": {
						"phase": "Succeeded",
						"message": "rollback completed"
					}
				}
			}`,
			wantPhase: models.SyncPhaseSucceeded,
		},
		{
			name:   "rollback with prune and dry-run",
			opts:   &RollbackOptions{ID: 3, Prune: true, DryRun: true},
			status: http.StatusOK,
			body: `{
				"status": {
					"operationState": {
						"phase": "Succeeded",
						"message": "dry run completed"
					}
				}
			}`,
			wantPhase: models.SyncPhaseSucceeded,
		},
		{
			name:       "nil opts returns error",
			opts:       nil,
			wantErr:    true,
			wantErrMsg: "rollback ID is required",
		},
		{
			name:       "zero ID returns error",
			opts:       &RollbackOptions{ID: 0},
			wantErr:    true,
			wantErrMsg: "rollback ID is required",
		},
		{
			name:    "API error",
			opts:    &RollbackOptions{ID: 1},
			status:  http.StatusBadRequest,
			body:    `{"message": "bad request"}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Errorf("method = %q, want POST", r.Method)
				}
				if r.URL.Path != "/api/v1/applications/my-app/rollback" {
					t.Errorf("path = %q, want /api/v1/applications/my-app/rollback", r.URL.Path)
				}
				// Verify payload
				var payload map[string]any
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Errorf("failed to decode request body: %v", err)
				}
				if id, ok := payload["id"].(float64); ok {
					if int64(id) != tt.opts.ID {
						t.Errorf("payload id = %v, want %v", id, tt.opts.ID)
					}
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer ts.Close()

			client := newTestClient(ts)
			result, err := client.RollbackApplication(context.Background(), "my-app", tt.opts)

			if (err != nil) != tt.wantErr {
				t.Fatalf("RollbackApplication() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				if tt.wantErrMsg != "" && err != nil && !strings.Contains(err.Error(), tt.wantErrMsg) {
					t.Errorf("error = %q, want to contain %q", err.Error(), tt.wantErrMsg)
				}
				return
			}
			if result.Phase != tt.wantPhase {
				t.Errorf("Phase = %q, want %q", result.Phase, tt.wantPhase)
			}
			if result.Application != "my-app" {
				t.Errorf("Application = %q, want my-app", result.Application)
			}
		})
	}
}

func TestCreateApplication(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		wantErr bool
	}{
		{
			name:   "successful create",
			status: http.StatusOK,
		},
		{
			name:    "conflict error",
			status:  http.StatusConflict,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Errorf("method = %q, want POST", r.Method)
				}
				if r.URL.Path != "/api/v1/applications" {
					t.Errorf("path = %q, want /api/v1/applications", r.URL.Path)
				}
				// Verify the payload is a valid Application
				var app v1alpha1.Application
				if err := json.NewDecoder(r.Body).Decode(&app); err != nil {
					t.Errorf("failed to decode request body: %v", err)
				}
				if app.Name != "new-app" {
					t.Errorf("app name = %q, want new-app", app.Name)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(`{}`))
			}))
			defer ts.Close()

			client := newTestClient(ts)
			app := &v1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "new-app",
					Namespace: "argocd",
				},
				Spec: v1alpha1.ApplicationSpec{
					Project: "default",
					Source: &v1alpha1.ApplicationSource{
						RepoURL:        "https://github.com/org/repo",
						Path:           "manifests",
						TargetRevision: "main",
					},
					Destination: v1alpha1.ApplicationDestination{
						Server:    "https://kubernetes.default.svc",
						Namespace: "default",
					},
				},
			}

			err := client.CreateApplication(context.Background(), app)

			if (err != nil) != tt.wantErr {
				t.Fatalf("CreateApplication() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDeleteApplication(t *testing.T) {
	tests := []struct {
		name        string
		cascade     bool
		status      int
		wantErr     bool
		wantCascade string
	}{
		{
			name:        "delete with cascade",
			cascade:     true,
			status:      http.StatusOK,
			wantCascade: "true",
		},
		{
			name:        "delete without cascade",
			cascade:     false,
			status:      http.StatusOK,
			wantCascade: "false",
		},
		{
			name:        "delete returns 204",
			cascade:     true,
			status:      http.StatusNoContent,
			wantCascade: "true",
		},
		{
			name:    "API error",
			cascade: true,
			status:  http.StatusForbidden,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodDelete {
					t.Errorf("method = %q, want DELETE", r.Method)
				}
				if r.URL.Path != "/api/v1/applications/my-app" {
					t.Errorf("path = %q, want /api/v1/applications/my-app", r.URL.Path)
				}
				cascade := r.URL.Query().Get("cascade")
				if !tt.wantErr && cascade != tt.wantCascade {
					t.Errorf("cascade = %q, want %q", cascade, tt.wantCascade)
				}
				w.WriteHeader(tt.status)
			}))
			defer ts.Close()

			client := newTestClient(ts)
			err := client.DeleteApplication(context.Background(), "my-app", tt.cascade)

			if (err != nil) != tt.wantErr {
				t.Fatalf("DeleteApplication() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestGetApplicationRaw(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		body     string
		wantErr  bool
		wantName string
	}{
		{
			name:   "successful get",
			status: http.StatusOK,
			body: `{
				"metadata": {"name": "raw-app", "namespace": "argocd"},
				"spec": {
					"project": "default",
					"source": {
						"repoURL": "https://github.com/org/repo",
						"path": "manifests",
						"targetRevision": "main"
					},
					"destination": {
						"server": "https://kubernetes.default.svc",
						"namespace": "default"
					}
				},
				"status": {
					"sync": {"status": "Synced"},
					"health": {"status": "Healthy"}
				}
			}`,
			wantName: "raw-app",
		},
		{
			name:    "API error",
			status:  http.StatusNotFound,
			body:    `{"message": "not found"}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/applications/raw-app" {
					t.Errorf("path = %q, want /api/v1/applications/raw-app", r.URL.Path)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer ts.Close()

			client := newTestClient(ts)
			app, err := client.GetApplicationRaw(context.Background(), "raw-app")

			if (err != nil) != tt.wantErr {
				t.Fatalf("GetApplicationRaw() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if app.Name != tt.wantName {
				t.Errorf("Name = %q, want %q", app.Name, tt.wantName)
			}
			// Verify it returns the typed v1alpha1.Application (not models.Application)
			if app.Spec.Project != "default" {
				t.Errorf("Spec.Project = %q, want default", app.Spec.Project)
			}
			if app.Spec.Source == nil {
				t.Fatal("Spec.Source should not be nil")
			}
			if app.Spec.Source.RepoURL != "https://github.com/org/repo" {
				t.Errorf("Spec.Source.RepoURL = %q, want https://github.com/org/repo", app.Spec.Source.RepoURL)
			}
		})
	}
}

func TestGetApplicationWithSpecialCharacters(t *testing.T) {
	// Verify URL path escaping for application names with special characters.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The name "my app/test" should be URL-encoded
		expectedPath := "/api/v1/applications/my%20app%2Ftest"
		if r.URL.RawPath != expectedPath {
			t.Errorf("path = %q, want %q", r.URL.RawPath, expectedPath)
		}
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{
			"metadata": map[string]any{"name": "my app/test", "namespace": "argocd"},
			"spec":     map[string]any{"project": "default"},
			"status":   map[string]any{},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	client := newTestClient(ts)
	app, err := client.GetApplication(context.Background(), "my app/test")
	if err != nil {
		t.Fatalf("GetApplication() error = %v", err)
	}
	if app.Name != "my app/test" {
		t.Errorf("Name = %q, want %q", app.Name, "my app/test")
	}
}

func TestGetManagedResources(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		body      string
		wantErr   bool
		wantCount int
	}{
		{
			name:   "returns managed resources",
			status: http.StatusOK,
			body: `{
				"items": [
					{
						"group": "apps",
						"kind": "Deployment",
						"namespace": "default",
						"name": "web",
						"liveState": "{\"kind\":\"Deployment\"}",
						"normalizedLiveState": "{\"kind\":\"Deployment\"}"
					},
					{
						"kind": "ConfigMap",
						"namespace": "default",
						"name": "config",
						"liveState": "{\"kind\":\"ConfigMap\"}"
					}
				]
			}`,
			wantCount: 2,
		},
		{
			name:      "empty items",
			status:    http.StatusOK,
			body:      `{"items": []}`,
			wantCount: 0,
		},
		{
			name:    "API error",
			status:  http.StatusNotFound,
			body:    `{"message": "not found"}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/applications/my-app/managed-resources" {
					t.Errorf("path = %q, want /api/v1/applications/my-app/managed-resources", r.URL.Path)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer ts.Close()

			client := newTestClient(ts)
			resources, err := client.getManagedResources(context.Background(), "my-app")

			if (err != nil) != tt.wantErr {
				t.Fatalf("getManagedResources() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if len(resources) != tt.wantCount {
				t.Errorf("got %d resources, want %d", len(resources), tt.wantCount)
			}
		})
	}
}

func TestGetManifests(t *testing.T) {
	tests := []struct {
		name          string
		params        *GetManifestsParams
		status        int
		body          string
		wantErr       bool
		wantManifests int
		wantRevision  string
		checkQuery    func(t *testing.T, query url.Values)
	}{
		{
			name:   "basic manifests fetch",
			params: nil,
			status: http.StatusOK,
			body: `{
				"manifests": [
					"{\"apiVersion\":\"apps/v1\",\"kind\":\"Deployment\",\"metadata\":{\"name\":\"web\",\"namespace\":\"default\"}}",
					"{\"apiVersion\":\"v1\",\"kind\":\"Service\",\"metadata\":{\"name\":\"web-svc\",\"namespace\":\"default\"}}"
				],
				"revision": "abc123"
			}`,
			wantManifests: 2,
			wantRevision:  "abc123",
		},
		{
			name:   "with revision param",
			params: &GetManifestsParams{Revision: "feature-branch"},
			status: http.StatusOK,
			body:   `{"manifests": [], "revision": "def456"}`,
			checkQuery: func(t *testing.T, query url.Values) {
				if query.Get("revision") != "feature-branch" {
					t.Errorf("revision query = %q, want feature-branch", query.Get("revision"))
				}
			},
			wantManifests: 0,
			wantRevision:  "def456",
		},
		{
			name: "with source positions and revisions (multi-source)",
			params: &GetManifestsParams{
				SourcePositions: []int{1, 2},
				Revisions:       []string{"main", "v1.0.0"},
			},
			status: http.StatusOK,
			body:   `{"manifests": ["{\"apiVersion\":\"v1\",\"kind\":\"ConfigMap\",\"metadata\":{\"name\":\"test\"}}"], "revision": ""}`,
			checkQuery: func(t *testing.T, query url.Values) {
				positions := query["sourcePositions"]
				if len(positions) != 2 || positions[0] != "1" || positions[1] != "2" {
					t.Errorf("sourcePositions = %v, want [1 2]", positions)
				}
				revisions := query["revisions"]
				if len(revisions) != 2 || revisions[0] != "main" || revisions[1] != "v1.0.0" {
					t.Errorf("revisions = %v, want [main v1.0.0]", revisions)
				}
			},
			wantManifests: 1,
		},
		{
			name:   "skips unparseable manifests",
			status: http.StatusOK,
			body: `{
				"manifests": [
					"{\"apiVersion\":\"v1\",\"kind\":\"ConfigMap\",\"metadata\":{\"name\":\"valid\"}}",
					"not valid json"
				]
			}`,
			wantManifests: 1,
		},
		{
			name:    "API error",
			status:  http.StatusInternalServerError,
			body:    `{"message": "internal error"}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !strings.HasPrefix(r.URL.Path, "/api/v1/applications/my-app/manifests") {
					t.Errorf("path = %q, want prefix /api/v1/applications/my-app/manifests", r.URL.Path)
				}
				if tt.checkQuery != nil {
					tt.checkQuery(t, r.URL.Query())
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer ts.Close()

			client := newTestClient(ts)
			manifests, revision, err := client.GetManifests(context.Background(), "my-app", tt.params)

			if (err != nil) != tt.wantErr {
				t.Fatalf("GetManifests() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if len(manifests) != tt.wantManifests {
				t.Errorf("got %d manifests, want %d", len(manifests), tt.wantManifests)
			}
			if tt.wantRevision != "" && revision != tt.wantRevision {
				t.Errorf("revision = %q, want %q", revision, tt.wantRevision)
			}
		})
	}
}

func TestGetManifestsFetchRevision(t *testing.T) {
	// When FetchRevision is true and the manifests API returns no revision,
	// GetManifests should fetch the revision from the application status.
	callCount := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/manifests"):
			// Manifests API returns empty revision (ArgoCD v3+)
			_, _ = w.Write([]byte(`{"manifests": ["{\"apiVersion\":\"v1\",\"kind\":\"ConfigMap\",\"metadata\":{\"name\":\"test\"}}"], "revision": ""}`))
		case r.URL.Path == "/api/v1/applications/my-app":
			// Application API returns revision in status
			_, _ = w.Write([]byte(`{
				"metadata": {"name": "my-app"},
				"spec": {"project": "default"},
				"status": {"sync": {"revision": "fetched-rev-123"}}
			}`))
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer ts.Close()

	client := newTestClient(ts)
	_, revision, err := client.GetManifests(context.Background(), "my-app", &GetManifestsParams{FetchRevision: true})
	if err != nil {
		t.Fatalf("GetManifests() error = %v", err)
	}
	if revision != "fetched-rev-123" {
		t.Errorf("revision = %q, want fetched-rev-123", revision)
	}
	if callCount != 2 {
		t.Errorf("expected 2 API calls (manifests + app status), got %d", callCount)
	}
}

func TestGetLiveManifests(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		body      string
		wantErr   bool
		wantCount int
	}{
		{
			name:   "returns live manifests",
			status: http.StatusOK,
			body: `{
				"items": [
					{"liveState": "{\"apiVersion\":\"v1\",\"kind\":\"ConfigMap\",\"metadata\":{\"name\":\"live-cm\",\"namespace\":\"default\"}}"},
					{"liveState": "{\"apiVersion\":\"apps/v1\",\"kind\":\"Deployment\",\"metadata\":{\"name\":\"web\",\"namespace\":\"default\"}}"}
				]
			}`,
			wantCount: 2,
		},
		{
			name:   "skips empty and null live states",
			status: http.StatusOK,
			body: `{
				"items": [
					{"liveState": "{\"apiVersion\":\"v1\",\"kind\":\"ConfigMap\",\"metadata\":{\"name\":\"valid\"}}"},
					{"liveState": ""},
					{"liveState": "null"}
				]
			}`,
			wantCount: 1,
		},
		{
			name:    "API error",
			status:  http.StatusNotFound,
			body:    `{"message": "not found"}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/applications/my-app/managed-resources" {
					t.Errorf("path = %q, want /api/v1/applications/my-app/managed-resources", r.URL.Path)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer ts.Close()

			client := newTestClient(ts)
			manifests, err := client.GetLiveManifests(context.Background(), "my-app")

			if (err != nil) != tt.wantErr {
				t.Fatalf("GetLiveManifests() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if len(manifests) != tt.wantCount {
				t.Errorf("got %d manifests, want %d", len(manifests), tt.wantCount)
			}
		})
	}
}

func TestClientRequest_ConnectionError(t *testing.T) {
	// Create a client pointing to a closed server to trigger a transport error.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	client := newTestClient(ts)
	ts.Close() // Close immediately to force connection errors

	var result struct{}
	err := client.get(context.Background(), "/api/v1/applications", nil, &result)
	if err == nil {
		t.Fatal("expected error for connection failure")
	}
}
