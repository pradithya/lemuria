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
	"testing"

	v1alpha1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/org/lemuria/internal/config"
)

func TestFindParentApps(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.Path == "/api/v1/applications":
			resp := v1alpha1.ApplicationList{
				Items: []v1alpha1.Application{
					{
						ObjectMeta: metav1.ObjectMeta{Name: "root-app"},
						Spec: v1alpha1.ApplicationSpec{
							SyncPolicy: &v1alpha1.SyncPolicy{
								Automated: &v1alpha1.SyncPolicyAutomated{Prune: true},
							},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{Name: "child-app"},
						Spec:       v1alpha1.ApplicationSpec{},
					},
					{
						ObjectMeta: metav1.ObjectMeta{Name: "unrelated-app"},
						Spec: v1alpha1.ApplicationSpec{
							SyncPolicy: &v1alpha1.SyncPolicy{
								Automated: &v1alpha1.SyncPolicyAutomated{},
							},
						},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)

		case r.URL.Path == "/api/v1/applications/root-app/managed-resources":
			resp := map[string]any{
				"items": []map[string]any{
					{"kind": "Application", "name": "child-app"},
					{"kind": "Deployment", "name": "some-deploy"},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)

		case r.URL.Path == "/api/v1/applications/unrelated-app/managed-resources":
			resp := map[string]any{
				"items": []map[string]any{
					{"kind": "Deployment", "name": "other-deploy"},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)

		default:
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{})
		}
	}))
	defer ts.Close()

	client, err := NewClient(config.ArgoCDConfig{
		ServerURL: ts.URL,
		Token:     "test",
		Insecure:  true,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	parents, err := client.FindParentApps(context.Background(), "child-app", nil)
	if err != nil {
		t.Fatalf("FindParentApps: %v", err)
	}

	if len(parents) != 1 {
		t.Fatalf("expected 1 parent, got %d", len(parents))
	}
	if parents[0].Name != "root-app" {
		t.Errorf("expected parent 'root-app', got %q", parents[0].Name)
	}
}

func TestBuildParentMap(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.Path == "/api/v1/applications":
			resp := v1alpha1.ApplicationList{
				Items: []v1alpha1.Application{
					{
						ObjectMeta: metav1.ObjectMeta{Name: "root-app"},
						Spec: v1alpha1.ApplicationSpec{
							SyncPolicy: &v1alpha1.SyncPolicy{
								Automated: &v1alpha1.SyncPolicyAutomated{Prune: true},
							},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{Name: "child-app"},
						Spec:       v1alpha1.ApplicationSpec{},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
		case r.URL.Path == "/api/v1/applications/root-app/managed-resources":
			resp := map[string]any{
				"items": []map[string]any{
					{"kind": "Application", "name": "child-app"},
					{"kind": "Application", "name": "child-app-2"},
					{"kind": "Deployment", "name": "deploy"},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
		default:
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{})
		}
	}))
	defer ts.Close()

	client, _ := NewClient(config.ArgoCDConfig{
		ServerURL: ts.URL,
		Token:     "test",
		Insecure:  true,
	})

	pm, err := client.BuildParentMap(context.Background())
	if err != nil {
		t.Fatalf("BuildParentMap: %v", err)
	}

	// child-app should have root-app as parent
	if len(pm["child-app"]) != 1 || pm["child-app"][0].Name != "root-app" {
		t.Errorf("expected child-app → [root-app], got %v", pm["child-app"])
	}
	// child-app-2 should also have root-app as parent
	if len(pm["child-app-2"]) != 1 || pm["child-app-2"][0].Name != "root-app" {
		t.Errorf("expected child-app-2 → [root-app], got %v", pm["child-app-2"])
	}
	// Deployment should not appear in map
	if len(pm["deploy"]) != 0 {
		t.Errorf("expected no parents for deploy, got %v", pm["deploy"])
	}
}

func TestFindParentApps_WithPrecomputedMap(t *testing.T) {
	// No server needed — uses precomputed map
	pm := ParentMap{
		"child-app": {
			{Name: "parent-1"},
			{Name: "parent-2"},
		},
	}

	client, _ := NewClient(config.ArgoCDConfig{
		ServerURL: "http://unused",
		Token:     "test",
		Insecure:  true,
	})

	parents, err := client.FindParentApps(context.Background(), "child-app", pm)
	if err != nil {
		t.Fatalf("FindParentApps: %v", err)
	}
	if len(parents) != 2 {
		t.Fatalf("expected 2 parents, got %d", len(parents))
	}

	// App not in map should return nil
	parents, err = client.FindParentApps(context.Background(), "unknown-app", pm)
	if err != nil {
		t.Fatalf("FindParentApps: %v", err)
	}
	if len(parents) != 0 {
		t.Errorf("expected 0 parents for unknown app, got %d", len(parents))
	}
}

func TestFindParentApps_NoParents(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.Path == "/api/v1/applications":
			resp := v1alpha1.ApplicationList{
				Items: []v1alpha1.Application{
					{
						ObjectMeta: metav1.ObjectMeta{Name: "standalone-app"},
						Spec: v1alpha1.ApplicationSpec{
							SyncPolicy: &v1alpha1.SyncPolicy{
								Automated: &v1alpha1.SyncPolicyAutomated{},
							},
						},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)

		case r.URL.Path == "/api/v1/applications/standalone-app/managed-resources":
			resp := map[string]any{
				"items": []map[string]any{
					{"kind": "Deployment", "name": "my-deploy"},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)

		default:
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{})
		}
	}))
	defer ts.Close()

	client, _ := NewClient(config.ArgoCDConfig{
		ServerURL: ts.URL,
		Token:     "test",
		Insecure:  true,
	})

	parents, err := client.FindParentApps(context.Background(), "some-child", nil)
	if err != nil {
		t.Fatalf("FindParentApps: %v", err)
	}
	if len(parents) != 0 {
		t.Errorf("expected 0 parents, got %d", len(parents))
	}
}
