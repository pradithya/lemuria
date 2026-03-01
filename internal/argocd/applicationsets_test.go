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

	v1alpha1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestConvertApplicationSet(t *testing.T) {
	tests := []struct {
		name          string
		appSet        v1alpha1.ApplicationSet
		wantName      string
		wantNamespace string
		wantTmplName  string
		wantProject   string
		wantRepoURL   string
		wantPath      string
		wantSources   int
	}{
		{
			name: "single-source template",
			appSet: v1alpha1.ApplicationSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-appset",
					Namespace: "argocd",
				},
				Spec: v1alpha1.ApplicationSetSpec{
					Template: v1alpha1.ApplicationSetTemplate{
						ApplicationSetTemplateMeta: v1alpha1.ApplicationSetTemplateMeta{
							Name: "{{name}}",
						},
						Spec: v1alpha1.ApplicationSpec{
							Project: "default",
							Source: &v1alpha1.ApplicationSource{
								RepoURL:        "https://github.com/org/repo",
								Path:           "{{path}}",
								TargetRevision: "main",
							},
							Destination: v1alpha1.ApplicationDestination{
								Server:    "https://kubernetes.default.svc",
								Namespace: "{{namespace}}",
							},
						},
					},
				},
			},
			wantName:      "my-appset",
			wantNamespace: "argocd",
			wantTmplName:  "{{name}}",
			wantProject:   "default",
			wantRepoURL:   "https://github.com/org/repo",
			wantPath:      "{{path}}",
			wantSources:   0,
		},
		{
			name: "multi-source template",
			appSet: v1alpha1.ApplicationSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "multi-appset",
					Namespace: "argocd",
				},
				Spec: v1alpha1.ApplicationSetSpec{
					Template: v1alpha1.ApplicationSetTemplate{
						ApplicationSetTemplateMeta: v1alpha1.ApplicationSetTemplateMeta{
							Name: "{{name}}",
						},
						Spec: v1alpha1.ApplicationSpec{
							Project: "ops",
							Sources: v1alpha1.ApplicationSources{
								{
									RepoURL:        "https://github.com/org/config",
									Path:           "envs/{{env}}",
									TargetRevision: "main",
								},
								{
									RepoURL:        "https://charts.example.com",
									Chart:          "app",
									TargetRevision: "2.0.0",
								},
							},
							Destination: v1alpha1.ApplicationDestination{
								Server: "https://kubernetes.default.svc",
							},
						},
					},
				},
			},
			wantName:      "multi-appset",
			wantNamespace: "argocd",
			wantTmplName:  "{{name}}",
			wantProject:   "ops",
			wantSources:   2,
		},
		{
			name: "minimal appset",
			appSet: v1alpha1.ApplicationSet{
				ObjectMeta: metav1.ObjectMeta{Name: "minimal"},
				Spec: v1alpha1.ApplicationSetSpec{
					Template: v1alpha1.ApplicationSetTemplate{
						Spec: v1alpha1.ApplicationSpec{},
					},
				},
			},
			wantName: "minimal",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := convertApplicationSet(tt.appSet)
			if got.Name != tt.wantName {
				t.Errorf("Name = %q, want %q", got.Name, tt.wantName)
			}
			if got.Namespace != tt.wantNamespace {
				t.Errorf("Namespace = %q, want %q", got.Namespace, tt.wantNamespace)
			}
			if got.Template.Name != tt.wantTmplName {
				t.Errorf("Template.Name = %q, want %q", got.Template.Name, tt.wantTmplName)
			}
			if got.Template.Project != tt.wantProject {
				t.Errorf("Template.Project = %q, want %q", got.Template.Project, tt.wantProject)
			}
			if got.Template.RepoURL != tt.wantRepoURL {
				t.Errorf("Template.RepoURL = %q, want %q", got.Template.RepoURL, tt.wantRepoURL)
			}
			if got.Template.Path != tt.wantPath {
				t.Errorf("Template.Path = %q, want %q", got.Template.Path, tt.wantPath)
			}
			if len(got.Template.Sources) != tt.wantSources {
				t.Errorf("Template.Sources length = %d, want %d", len(got.Template.Sources), tt.wantSources)
			}
		})
	}
}

func TestListApplicationSets(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		wantErr bool
		wantLen int
	}{
		{
			name:   "returns multiple applicationsets",
			status: http.StatusOK,
			body: `{
				"items": [
					{
						"metadata": {"name": "appset-1", "namespace": "argocd"},
						"spec": {
							"template": {
								"metadata": {"name": "{{name}}"},
								"spec": {
									"project": "default",
									"source": {
										"repoURL": "https://github.com/org/repo",
										"path": "{{path}}",
										"targetRevision": "main"
									},
									"destination": {
										"server": "https://kubernetes.default.svc",
										"namespace": "{{namespace}}"
									}
								}
							}
						}
					},
					{
						"metadata": {"name": "appset-2", "namespace": "argocd"},
						"spec": {
							"template": {
								"metadata": {"name": "{{name}}-2"},
								"spec": {
									"project": "ops"
								}
							}
						}
					}
				]
			}`,
			wantLen: 2,
		},
		{
			name:    "empty list",
			status:  http.StatusOK,
			body:    `{"items": []}`,
			wantLen: 0,
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
				if r.URL.Path != "/api/v1/applicationsets" {
					t.Errorf("path = %q, want /api/v1/applicationsets", r.URL.Path)
				}
				if r.Method != http.MethodGet {
					t.Errorf("method = %q, want GET", r.Method)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer ts.Close()

			client := newTestClient(ts)
			appSets, err := client.ListApplicationSets(context.Background())

			if (err != nil) != tt.wantErr {
				t.Fatalf("ListApplicationSets() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if len(appSets) != tt.wantLen {
				t.Fatalf("got %d appsets, want %d", len(appSets), tt.wantLen)
			}
			if tt.wantLen > 0 {
				if appSets[0].Name != "appset-1" {
					t.Errorf("appSets[0].Name = %q, want appset-1", appSets[0].Name)
				}
				if appSets[0].Template.RepoURL != "https://github.com/org/repo" {
					t.Errorf("appSets[0].Template.RepoURL = %q", appSets[0].Template.RepoURL)
				}
				if appSets[1].Name != "appset-2" {
					t.Errorf("appSets[1].Name = %q, want appset-2", appSets[1].Name)
				}
			}
		})
	}
}

func TestGetApplicationSet(t *testing.T) {
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
				"metadata": {"name": "my-appset", "namespace": "argocd"},
				"spec": {
					"template": {
						"metadata": {"name": "{{name}}"},
						"spec": {
							"project": "default",
							"source": {
								"repoURL": "https://github.com/org/repo",
								"path": "k8s/{{env}}",
								"targetRevision": "main"
							},
							"destination": {
								"server": "https://kubernetes.default.svc"
							}
						}
					}
				}
			}`,
			wantName: "my-appset",
		},
		{
			name:    "not found",
			status:  http.StatusNotFound,
			body:    `{"message": "applicationset not found"}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/applicationsets/my-appset" {
					t.Errorf("path = %q, want /api/v1/applicationsets/my-appset", r.URL.Path)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer ts.Close()

			client := newTestClient(ts)
			appSet, err := client.GetApplicationSet(context.Background(), "my-appset")

			if (err != nil) != tt.wantErr {
				t.Fatalf("GetApplicationSet() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if appSet.Name != tt.wantName {
				t.Errorf("Name = %q, want %q", appSet.Name, tt.wantName)
			}
			if appSet.Template.RepoURL != "https://github.com/org/repo" {
				t.Errorf("Template.RepoURL = %q", appSet.Template.RepoURL)
			}
		})
	}
}

func TestCreateApplicationSet(t *testing.T) {
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
				if r.URL.Path != "/api/v1/applicationsets" {
					t.Errorf("path = %q, want /api/v1/applicationsets", r.URL.Path)
				}
				// Verify payload
				var appSet v1alpha1.ApplicationSet
				if err := json.NewDecoder(r.Body).Decode(&appSet); err != nil {
					t.Errorf("failed to decode body: %v", err)
				}
				if appSet.Name != "new-appset" {
					t.Errorf("appSet.Name = %q, want new-appset", appSet.Name)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(`{}`))
			}))
			defer ts.Close()

			client := newTestClient(ts)
			appSet := &v1alpha1.ApplicationSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "new-appset",
					Namespace: "argocd",
				},
				Spec: v1alpha1.ApplicationSetSpec{
					Template: v1alpha1.ApplicationSetTemplate{
						Spec: v1alpha1.ApplicationSpec{
							Project: "default",
						},
					},
				},
			}

			err := client.CreateApplicationSet(context.Background(), appSet)

			if (err != nil) != tt.wantErr {
				t.Fatalf("CreateApplicationSet() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDeleteApplicationSet(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		wantErr bool
	}{
		{
			name:   "successful delete",
			status: http.StatusOK,
		},
		{
			name:   "delete returns 204",
			status: http.StatusNoContent,
		},
		{
			name:    "not found",
			status:  http.StatusNotFound,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodDelete {
					t.Errorf("method = %q, want DELETE", r.Method)
				}
				if r.URL.Path != "/api/v1/applicationsets/my-appset" {
					t.Errorf("path = %q, want /api/v1/applicationsets/my-appset", r.URL.Path)
				}
				w.WriteHeader(tt.status)
			}))
			defer ts.Close()

			client := newTestClient(ts)
			err := client.DeleteApplicationSet(context.Background(), "my-appset")

			if (err != nil) != tt.wantErr {
				t.Fatalf("DeleteApplicationSet() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestGenerateApplications(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		body      string
		wantErr   bool
		wantCount int
	}{
		{
			name:   "generates applications",
			status: http.StatusOK,
			body: `{
				"applications": [
					{
						"metadata": {"name": "generated-app-1", "namespace": "argocd"},
						"spec": {
							"project": "default",
							"source": {
								"repoURL": "https://github.com/org/repo",
								"path": "envs/dev",
								"targetRevision": "main"
							},
							"destination": {
								"server": "https://kubernetes.default.svc",
								"namespace": "dev"
							}
						}
					},
					{
						"metadata": {"name": "generated-app-2", "namespace": "argocd"},
						"spec": {
							"project": "default",
							"source": {
								"repoURL": "https://github.com/org/repo",
								"path": "envs/staging",
								"targetRevision": "main"
							},
							"destination": {
								"server": "https://kubernetes.default.svc",
								"namespace": "staging"
							}
						}
					}
				]
			}`,
			wantCount: 2,
		},
		{
			name:      "empty result",
			status:    http.StatusOK,
			body:      `{"applications": []}`,
			wantCount: 0,
		},
		{
			name:    "API error",
			status:  http.StatusBadRequest,
			body:    `{"message": "invalid appset"}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Errorf("method = %q, want POST", r.Method)
				}
				if r.URL.Path != "/api/v1/applicationsets/generate" {
					t.Errorf("path = %q, want /api/v1/applicationsets/generate", r.URL.Path)
				}
				// Verify payload contains applicationSet field
				var payload map[string]any
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Errorf("failed to decode body: %v", err)
				}
				if _, ok := payload["applicationSet"]; !ok {
					t.Error("payload missing 'applicationSet' field")
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer ts.Close()

			client := newTestClient(ts)
			appSet := &v1alpha1.ApplicationSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-appset",
					Namespace: "argocd",
				},
			}
			apps, err := client.GenerateApplications(context.Background(), appSet)

			if (err != nil) != tt.wantErr {
				t.Fatalf("GenerateApplications() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if len(apps) != tt.wantCount {
				t.Fatalf("got %d apps, want %d", len(apps), tt.wantCount)
			}
			if tt.wantCount > 0 {
				if apps[0].Name != "generated-app-1" {
					t.Errorf("apps[0].Name = %q, want generated-app-1", apps[0].Name)
				}
				if apps[0].Path != "envs/dev" {
					t.Errorf("apps[0].Path = %q, want envs/dev", apps[0].Path)
				}
				if apps[1].Name != "generated-app-2" {
					t.Errorf("apps[1].Name = %q, want generated-app-2", apps[1].Name)
				}
			}
		})
	}
}

func TestGetApplicationsByApplicationSet(t *testing.T) {
	tests := []struct {
		name      string
		handler   http.HandlerFunc
		wantErr   bool
		wantCount int
		wantNames []string
	}{
		{
			name: "found via label selector",
			handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				selector := r.URL.Query().Get("selector")
				w.Header().Set("Content-Type", "application/json")

				if strings.Contains(selector, "argocd.argoproj.io/application-set-name=my-appset") {
					resp := map[string]any{
						"items": []map[string]any{
							{
								"metadata": map[string]any{
									"name":      "app-from-appset-1",
									"namespace": "argocd",
									"labels": map[string]any{
										"argocd.argoproj.io/application-set-name": "my-appset",
									},
								},
								"spec": map[string]any{"project": "default"},
								"status": map[string]any{
									"sync":   map[string]any{"status": "Synced"},
									"health": map[string]any{"status": "Healthy"},
								},
							},
						},
					}
					_ = json.NewEncoder(w).Encode(resp)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{}})
			}),
			wantCount: 1,
			wantNames: []string{"app-from-appset-1"},
		},
		{
			name: "fallback to ownerReference when label selector returns empty",
			handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				selector := r.URL.Query().Get("selector")
				w.Header().Set("Content-Type", "application/json")

				if strings.Contains(selector, "argocd.argoproj.io/application-set-name=my-appset") {
					// Label selector returns empty
					_ = json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{}})
					return
				}

				// ListApplications returns all apps, some with ownerReference to this appset
				resp := map[string]any{
					"items": []map[string]any{
						{
							"metadata": map[string]any{
								"name":      "owned-app",
								"namespace": "argocd",
								"ownerReferences": []map[string]any{
									{"kind": "ApplicationSet", "name": "my-appset"},
								},
							},
							"spec": map[string]any{"project": "default"},
							"status": map[string]any{
								"sync":   map[string]any{"status": "Synced"},
								"health": map[string]any{"status": "Healthy"},
							},
						},
						{
							"metadata": map[string]any{
								"name":      "unrelated-app",
								"namespace": "argocd",
							},
							"spec": map[string]any{"project": "default"},
							"status": map[string]any{
								"sync":   map[string]any{"status": "Synced"},
								"health": map[string]any{"status": "Healthy"},
							},
						},
					},
				}
				_ = json.NewEncoder(w).Encode(resp)
			}),
			wantCount: 1,
			wantNames: []string{"owned-app"},
		},
		{
			name: "no matching applications",
			handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{}})
			}),
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := httptest.NewServer(tt.handler)
			defer ts.Close()

			client := newTestClient(ts)
			apps, err := client.GetApplicationsByApplicationSet(context.Background(), "my-appset")

			if (err != nil) != tt.wantErr {
				t.Fatalf("GetApplicationsByApplicationSet() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if len(apps) != tt.wantCount {
				t.Fatalf("got %d apps, want %d", len(apps), tt.wantCount)
			}
			for i, wantName := range tt.wantNames {
				if apps[i].Name != wantName {
					t.Errorf("apps[%d].Name = %q, want %q", i, apps[i].Name, wantName)
				}
			}
		})
	}
}

func TestGetApplicationSetRaw(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		appSet   *v1alpha1.ApplicationSet
		wantErr  bool
	}{
		{
			name:   "successful get",
			status: http.StatusOK,
			appSet: &v1alpha1.ApplicationSet{
				ObjectMeta: metav1.ObjectMeta{Name: "test-appset"},
				Spec: v1alpha1.ApplicationSetSpec{
					Template: v1alpha1.ApplicationSetTemplate{
						Spec: v1alpha1.ApplicationSpec{
							Project: "default",
							SyncPolicy: &v1alpha1.SyncPolicy{
								Automated: &v1alpha1.SyncPolicyAutomated{Prune: true},
							},
						},
					},
				},
			},
		},
		{
			name:    "not found",
			status:  http.StatusNotFound,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("method = %q, want GET", r.Method)
				}
				if r.URL.Path != "/api/v1/applicationsets/test-appset" {
					t.Errorf("path = %q, want /api/v1/applicationsets/test-appset", r.URL.Path)
				}
				if tt.wantErr {
					http.Error(w, `{"message":"not found"}`, tt.status)
					return
				}
				_ = json.NewEncoder(w).Encode(tt.appSet)
			}))
			defer ts.Close()

			client := newTestClient(ts)
			result, err := client.GetApplicationSetRaw(context.Background(), "test-appset")
			if (err != nil) != tt.wantErr {
				t.Fatalf("GetApplicationSetRaw() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if result.Name != "test-appset" {
				t.Errorf("Name = %q, want %q", result.Name, "test-appset")
			}
			if result.Spec.Template.Spec.SyncPolicy == nil || result.Spec.Template.Spec.SyncPolicy.Automated == nil {
				t.Fatal("expected SyncPolicy.Automated to be set")
			}
			if !result.Spec.Template.Spec.SyncPolicy.Automated.Prune {
				t.Error("expected Prune to be true")
			}
		})
	}
}

func TestUpdateApplicationSet(t *testing.T) {
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
				if r.URL.Path != "/api/v1/applicationsets/test-appset" {
					t.Errorf("path = %q, want /api/v1/applicationsets/test-appset", r.URL.Path)
				}
				var appSet v1alpha1.ApplicationSet
				if err := json.NewDecoder(r.Body).Decode(&appSet); err != nil {
					t.Errorf("failed to decode body: %v", err)
				}
				if appSet.Name != "test-appset" {
					t.Errorf("appSet.Name = %q, want test-appset", appSet.Name)
				}
				if tt.wantErr {
					http.Error(w, `{"message":"error"}`, tt.status)
					return
				}
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(map[string]any{})
			}))
			defer ts.Close()

			client := newTestClient(ts)
			appSet := &v1alpha1.ApplicationSet{
				ObjectMeta: metav1.ObjectMeta{Name: "test-appset"},
				Spec: v1alpha1.ApplicationSetSpec{
					Template: v1alpha1.ApplicationSetTemplate{
						Spec: v1alpha1.ApplicationSpec{Project: "default"},
					},
				},
			}
			err := client.UpdateApplicationSet(context.Background(), "test-appset", appSet)
			if (err != nil) != tt.wantErr {
				t.Fatalf("UpdateApplicationSet() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
