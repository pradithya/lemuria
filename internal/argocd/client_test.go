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
