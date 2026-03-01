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
	"strings"
	"testing"

	v1alpha1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/org/lemuria/internal/models"
)

func TestDisableAutoSync(t *testing.T) {
	tests := []struct {
		name           string
		app            v1alpha1.Application
		expectDisabled bool
		expectAppSet   string
	}{
		{
			name: "auto-sync enabled - disables it",
			app: v1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{Name: "my-app"},
				Spec: v1alpha1.ApplicationSpec{
					SyncPolicy: &v1alpha1.SyncPolicy{
						Automated: &v1alpha1.SyncPolicyAutomated{
							Prune:    true,
							SelfHeal: true,
						},
					},
				},
			},
			expectDisabled: true,
		},
		{
			name: "auto-sync not enabled - returns nil",
			app: v1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{Name: "my-app"},
				Spec:       v1alpha1.ApplicationSpec{},
			},
			expectDisabled: false,
		},
		{
			name: "sync policy exists but no automated - returns nil",
			app: v1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{Name: "my-app"},
				Spec: v1alpha1.ApplicationSpec{
					SyncPolicy: &v1alpha1.SyncPolicy{},
				},
			},
			expectDisabled: false,
		},
		{
			name: "auto-sync with applicationset label",
			app: v1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "my-app",
					Labels: map[string]string{"argocd.argoproj.io/application-set-name": "my-appset"},
				},
				Spec: v1alpha1.ApplicationSpec{
					SyncPolicy: &v1alpha1.SyncPolicy{
						Automated: &v1alpha1.SyncPolicyAutomated{Prune: true},
					},
				},
			},
			expectDisabled: true,
			expectAppSet:   "my-appset",
		},
		{
			name: "auto-sync with applicationset ownerReference",
			app: v1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-app",
					OwnerReferences: []metav1.OwnerReference{
						{Kind: "ApplicationSet", Name: "owner-appset"},
					},
				},
				Spec: v1alpha1.ApplicationSpec{
					SyncPolicy: &v1alpha1.SyncPolicy{
						Automated: &v1alpha1.SyncPolicyAutomated{SelfHeal: true},
					},
				},
			},
			expectDisabled: true,
			expectAppSet:   "owner-appset",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var specUpdated bool
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == "GET" && r.URL.Path == "/api/v1/applications/my-app":
					_ = json.NewEncoder(w).Encode(tt.app)
				case r.Method == "PUT" && r.URL.Path == "/api/v1/applications/my-app/spec":
					specUpdated = true
					// Verify that Automated is nil in the updated spec
					var spec v1alpha1.ApplicationSpec
					_ = json.NewDecoder(r.Body).Decode(&spec)
					if spec.SyncPolicy != nil && spec.SyncPolicy.Automated != nil {
						t.Error("expected Automated to be nil in updated spec")
					}
					w.WriteHeader(http.StatusOK)
				default:
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer ts.Close()

			client := newTestArgoClient(ts)
			state, err := disableAutoSync(context.Background(), client, "my-app")

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.expectDisabled {
				if state == nil {
					t.Fatal("expected non-nil state")
				}
				if !specUpdated {
					t.Error("expected spec to be updated")
				}
				if state.ApplicationSetName != tt.expectAppSet {
					t.Errorf("ApplicationSetName = %q, want %q", state.ApplicationSetName, tt.expectAppSet)
				}
				if state.OriginalPolicy == nil {
					t.Error("expected non-nil OriginalPolicy")
				}
			} else {
				if state != nil {
					t.Error("expected nil state")
				}
				if specUpdated {
					t.Error("expected spec NOT to be updated")
				}
			}
		})
	}
}

func TestDisableAppSetAutoSync(t *testing.T) {
	tests := []struct {
		name           string
		appSet         v1alpha1.ApplicationSet
		expectDisabled bool
	}{
		{
			name: "template has auto-sync - disables it",
			appSet: v1alpha1.ApplicationSet{
				ObjectMeta: metav1.ObjectMeta{Name: "my-appset"},
				Spec: v1alpha1.ApplicationSetSpec{
					Template: v1alpha1.ApplicationSetTemplate{
						Spec: v1alpha1.ApplicationSpec{
							SyncPolicy: &v1alpha1.SyncPolicy{
								Automated: &v1alpha1.SyncPolicyAutomated{
									Prune:    true,
									SelfHeal: true,
								},
							},
						},
					},
				},
			},
			expectDisabled: true,
		},
		{
			name: "template has no auto-sync - returns nil",
			appSet: v1alpha1.ApplicationSet{
				ObjectMeta: metav1.ObjectMeta{Name: "my-appset"},
				Spec: v1alpha1.ApplicationSetSpec{
					Template: v1alpha1.ApplicationSetTemplate{
						Spec: v1alpha1.ApplicationSpec{},
					},
				},
			},
			expectDisabled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var appSetUpdated bool
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == "GET" && r.URL.Path == "/api/v1/applicationsets/my-appset":
					_ = json.NewEncoder(w).Encode(tt.appSet)
				case r.Method == "PUT" && r.URL.Path == "/api/v1/applicationsets/my-appset":
					appSetUpdated = true
					w.WriteHeader(http.StatusOK)
				default:
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer ts.Close()

			client := newTestArgoClient(ts)
			policyBytes, err := disableAppSetAutoSync(context.Background(), client, "my-appset")

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.expectDisabled {
				if policyBytes == nil {
					t.Fatal("expected non-nil policy bytes")
				}
				if !appSetUpdated {
					t.Error("expected applicationset to be updated")
				}
				// Verify we can unmarshal the stored policy
				var policy v1alpha1.SyncPolicyAutomated
				if err := json.Unmarshal(policyBytes, &policy); err != nil {
					t.Fatalf("failed to unmarshal stored policy: %v", err)
				}
			} else {
				if policyBytes != nil {
					t.Error("expected nil policy bytes")
				}
				if appSetUpdated {
					t.Error("expected applicationset NOT to be updated")
				}
			}
		})
	}
}

func TestRestoreAutoSync(t *testing.T) {
	tests := []struct {
		name         string
		lock         models.Lock
		expectUpdate bool
	}{
		{
			name: "auto-sync was disabled - restores it",
			lock: models.Lock{
				Application:      "my-app",
				AutoSyncDisabled: true,
				OriginalSyncPolicy: mustMarshal(t, &v1alpha1.SyncPolicyAutomated{
					Prune:    true,
					SelfHeal: true,
				}),
			},
			expectUpdate: true,
		},
		{
			name: "auto-sync was not disabled - no-op",
			lock: models.Lock{
				Application:      "my-app",
				AutoSyncDisabled: false,
			},
			expectUpdate: false,
		},
		{
			name: "auto-sync disabled but empty policy - no-op",
			lock: models.Lock{
				Application:      "my-app",
				AutoSyncDisabled: true,
			},
			expectUpdate: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var specUpdated bool
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == "GET" && r.URL.Path == "/api/v1/applications/my-app":
					app := v1alpha1.Application{
						ObjectMeta: metav1.ObjectMeta{Name: "my-app"},
						Spec: v1alpha1.ApplicationSpec{
							SyncPolicy: &v1alpha1.SyncPolicy{},
						},
					}
					_ = json.NewEncoder(w).Encode(app)
				case r.Method == "PUT" && r.URL.Path == "/api/v1/applications/my-app/spec":
					specUpdated = true
					// Verify the policy is restored
					var spec v1alpha1.ApplicationSpec
					_ = json.NewDecoder(r.Body).Decode(&spec)
					if spec.SyncPolicy == nil || spec.SyncPolicy.Automated == nil {
						t.Error("expected Automated to be restored in spec")
					}
					w.WriteHeader(http.StatusOK)
				default:
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer ts.Close()

			client := newTestArgoClient(ts)
			err := restoreAutoSync(context.Background(), client, tt.lock)

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if specUpdated != tt.expectUpdate {
				t.Errorf("spec updated = %v, want %v", specUpdated, tt.expectUpdate)
			}
		})
	}
}

func TestRestoreAutoSync_AppDeleted(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"not found"}`, http.StatusNotFound)
	}))
	defer ts.Close()

	client := newTestArgoClient(ts)
	lock := models.Lock{
		Application:      "deleted-app",
		AutoSyncDisabled: true,
		OriginalSyncPolicy: mustMarshal(t, &v1alpha1.SyncPolicyAutomated{
			Prune: true,
		}),
	}

	// Should not return error when app is deleted (404)
	err := restoreAutoSync(context.Background(), client, lock)
	if err != nil {
		t.Fatalf("expected no error for deleted app, got: %v", err)
	}
}

func TestRestoreAutoSync_TransientError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"internal error"}`, http.StatusInternalServerError)
	}))
	defer ts.Close()

	client := newTestArgoClient(ts)
	lock := models.Lock{
		Application:      "my-app",
		AutoSyncDisabled: true,
		OriginalSyncPolicy: mustMarshal(t, &v1alpha1.SyncPolicyAutomated{
			Prune: true,
		}),
	}

	// Should return error on transient (non-404) failures
	err := restoreAutoSync(context.Background(), client, lock)
	if err == nil {
		t.Fatal("expected error for transient failure, got nil")
	}
	if !strings.Contains(err.Error(), "getting application") {
		t.Errorf("expected error about getting application, got: %v", err)
	}
}

func TestRestoreAllAutoSync(t *testing.T) {
	var childRestored, parentRestored bool

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.Path == "/api/v1/applications/child-app":
			app := v1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{Name: "child-app"},
				Spec:       v1alpha1.ApplicationSpec{SyncPolicy: &v1alpha1.SyncPolicy{}},
			}
			_ = json.NewEncoder(w).Encode(app)
		case r.Method == "PUT" && r.URL.Path == "/api/v1/applications/child-app/spec":
			childRestored = true
			w.WriteHeader(http.StatusOK)
		case r.Method == "GET" && r.URL.Path == "/api/v1/applications/parent-app":
			app := v1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{Name: "parent-app"},
				Spec:       v1alpha1.ApplicationSpec{SyncPolicy: &v1alpha1.SyncPolicy{}},
			}
			_ = json.NewEncoder(w).Encode(app)
		case r.Method == "PUT" && r.URL.Path == "/api/v1/applications/parent-app/spec":
			parentRestored = true
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{})
		}
	}))
	defer ts.Close()

	client := newTestArgoClient(ts)
	mockLock := &mockLockManagerForSync{}

	policy := mustMarshal(t, &v1alpha1.SyncPolicyAutomated{Prune: true})
	locks := []models.Lock{
		{
			Application:        "child-app",
			AutoSyncDisabled:   true,
			OriginalSyncPolicy: policy,
			IsParentApp:        false,
		},
		{
			Application:        "parent-app",
			AutoSyncDisabled:   true,
			OriginalSyncPolicy: policy,
			IsParentApp:        true,
		},
	}

	restoreAllAutoSync(context.Background(), client, mockLock, locks, "org/repo", 1)

	if !childRestored {
		t.Error("expected child app auto-sync to be restored")
	}
	if !parentRestored {
		t.Error("expected parent app auto-sync to be restored")
	}
}

func TestDisableParentAutoSync(t *testing.T) {
	// Create a mock ArgoCD server that:
	// - Lists apps with one parent "root-app" that has auto-sync
	// - root-app manages an Application CR named "child-app"
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
							Destination: v1alpha1.ApplicationDestination{
								Namespace: "argocd",
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
		case r.Method == "GET" && r.URL.Path == "/api/v1/applications/root-app/managed-resources":
			resp := map[string]any{
				"items": []map[string]any{
					{"kind": "Application", "name": "child-app"},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
		case r.Method == "GET" && r.URL.Path == "/api/v1/applications/child-app/managed-resources":
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}})
		case r.Method == "GET" && r.URL.Path == "/api/v1/applications/root-app":
			app := v1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{Name: "root-app"},
				Spec: v1alpha1.ApplicationSpec{
					SyncPolicy: &v1alpha1.SyncPolicy{
						Automated: &v1alpha1.SyncPolicyAutomated{Prune: true},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(app)
		case r.Method == "PUT" && r.URL.Path == "/api/v1/applications/root-app/spec":
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{})
		}
	}))
	defer ts.Close()

	client := newTestArgoClient(ts)

	// Mock lock manager that records lock acquisitions
	lockCalls := make(map[string]bool)
	mockLock := &mockAutoSyncLockManager{
		lockCalls: lockCalls,
	}

	visited := make(map[string]bool)
	err := disableParentAutoSync(context.Background(), client, mockLock,
		"child-app", "org/repo", "https://github.com/org/repo", "github",
		1, "user", visited, 0, nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !lockCalls["root-app"] {
		t.Error("expected lock to be acquired on parent app 'root-app'")
	}
}

// mockAutoSyncLockManager tracks lock calls for testing.
type mockAutoSyncLockManager struct {
	mockLockManagerForSync
	lockCalls map[string]bool
	locks     map[string]*models.Lock
}

func (m *mockAutoSyncLockManager) Lock(_ context.Context, req models.LockRequest) (*models.LockResult, error) {
	m.lockCalls[req.Application] = true
	lock := &models.Lock{
		Application: req.Application,
		PRNumber:    req.PRNumber,
		Repo:        req.Repo,
	}
	if m.locks == nil {
		m.locks = make(map[string]*models.Lock)
	}
	m.locks[req.Application] = lock
	return &models.LockResult{Acquired: true, Lock: lock}, nil
}

func (m *mockAutoSyncLockManager) Get(_ context.Context, application string) (*models.Lock, error) {
	if m.locks != nil {
		if lock, ok := m.locks[application]; ok {
			return lock, nil
		}
	}
	return nil, nil
}

func (m *mockAutoSyncLockManager) UpdateLock(_ context.Context, lock *models.Lock) error {
	if m.locks == nil {
		m.locks = make(map[string]*models.Lock)
	}
	m.locks[lock.Application] = lock
	return nil
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}
	return data
}

func TestRestoreAppSetAutoSync_TransientError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"internal error"}`, http.StatusInternalServerError)
	}))
	defer ts.Close()

	client := newTestArgoClient(ts)
	policy := mustMarshal(t, &v1alpha1.SyncPolicyAutomated{Prune: true})

	// Should return error on transient (non-404) failures
	err := restoreAppSetAutoSync(context.Background(), client, "my-appset", policy)
	if err == nil {
		t.Fatal("expected error for transient failure, got nil")
	}
	if !strings.Contains(err.Error(), "getting applicationset") {
		t.Errorf("expected error about getting applicationset, got: %v", err)
	}
}

func TestRestoreAppSetAutoSync_NotFound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"not found"}`, http.StatusNotFound)
	}))
	defer ts.Close()

	client := newTestArgoClient(ts)
	policy := mustMarshal(t, &v1alpha1.SyncPolicyAutomated{Prune: true})

	// Should not return error when appset is deleted (404)
	err := restoreAppSetAutoSync(context.Background(), client, "my-appset", policy)
	if err != nil {
		t.Fatalf("expected no error for deleted appset, got: %v", err)
	}
}

func TestDisableParentAutoSync_ChildWithoutAutoSync(t *testing.T) {
	// Even when the child has no auto-sync, parent detection should still run
	// and find+disable parent apps with auto-sync.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.Path == "/api/v1/applications":
			resp := v1alpha1.ApplicationList{
				Items: []v1alpha1.Application{
					{
						ObjectMeta: metav1.ObjectMeta{Name: "parent-app"},
						Spec: v1alpha1.ApplicationSpec{
							SyncPolicy: &v1alpha1.SyncPolicy{
								Automated: &v1alpha1.SyncPolicyAutomated{Prune: true},
							},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{Name: "child-no-autosync"},
						Spec:       v1alpha1.ApplicationSpec{},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
		case r.Method == "GET" && r.URL.Path == "/api/v1/applications/parent-app/managed-resources":
			resp := map[string]any{
				"items": []map[string]any{
					{"kind": "Application", "name": "child-no-autosync"},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
		case r.Method == "GET" && r.URL.Path == "/api/v1/applications/parent-app":
			app := v1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{Name: "parent-app"},
				Spec: v1alpha1.ApplicationSpec{
					SyncPolicy: &v1alpha1.SyncPolicy{
						Automated: &v1alpha1.SyncPolicyAutomated{Prune: true},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(app)
		case r.Method == "PUT" && r.URL.Path == "/api/v1/applications/parent-app/spec":
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{})
		}
	}))
	defer ts.Close()

	client := newTestArgoClient(ts)
	lockCalls := make(map[string]bool)
	mockLock := &mockAutoSyncLockManager{lockCalls: lockCalls}

	// Child has no auto-sync, but parent detection should still run
	visited := map[string]bool{"child-no-autosync": true}
	err := disableParentAutoSync(context.Background(), client, mockLock,
		"child-no-autosync", "org/repo", "https://github.com/org/repo", "github",
		1, "user", visited, 0, nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !lockCalls["parent-app"] {
		t.Error("expected lock to be acquired on parent-app even though child has no auto-sync")
	}
}

// --- computeSyncTargets tests ---

func TestComputeSyncTargets_NoParentChild(t *testing.T) {
	// All apps are independent — all should be synced, none skipped
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/managed-resources") {
			resp := map[string]any{
				"items": []map[string]any{
					{"kind": "Deployment", "name": "deploy"},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{})
	}))
	defer ts.Close()

	client := newTestArgoClient(ts)
	e := &Executor{argocd: client}

	locks := []models.Lock{
		{Application: "app-a", PlanRevision: "abc"},
		{Application: "app-b", PlanRevision: "abc"},
		{Application: "app-c", PlanRevision: "abc"},
	}

	syncIndices, skippedParents := e.computeSyncTargets(context.Background(), locks)

	if len(syncIndices) != 3 {
		t.Errorf("expected 3 sync targets, got %d", len(syncIndices))
	}
	if len(skippedParents) != 0 {
		t.Errorf("expected 0 skipped parents, got %d", len(skippedParents))
	}
}

func TestComputeSyncTargets_SimpleParentChild(t *testing.T) {
	// parent-app manages child-app → only child synced, parent skipped
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/applications/parent-app/managed-resources":
			resp := map[string]any{
				"items": []map[string]any{
					{"kind": "Application", "name": "child-app"},
					{"kind": "Deployment", "name": "deploy"},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
		case "/api/v1/applications/child-app/managed-resources":
			resp := map[string]any{
				"items": []map[string]any{
					{"kind": "Deployment", "name": "child-deploy"},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
		default:
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{})
		}
	}))
	defer ts.Close()

	client := newTestArgoClient(ts)
	e := &Executor{argocd: client}

	locks := []models.Lock{
		{Application: "parent-app", PlanRevision: "abc"},
		{Application: "child-app", PlanRevision: "abc"},
	}

	syncIndices, skippedParents := e.computeSyncTargets(context.Background(), locks)

	if len(syncIndices) != 1 {
		t.Fatalf("expected 1 sync target, got %d", len(syncIndices))
	}
	if locks[syncIndices[0]].Application != "child-app" {
		t.Errorf("expected child-app to be synced, got %s", locks[syncIndices[0]].Application)
	}

	if len(skippedParents) != 1 {
		t.Fatalf("expected 1 skipped parent, got %d", len(skippedParents))
	}
	if locks[skippedParents[0]].Application != "parent-app" {
		t.Errorf("expected parent-app to be skipped, got %s", locks[skippedParents[0]].Application)
	}
}

func TestComputeSyncTargets_ThreeLevels(t *testing.T) {
	// grandparent → parent → child → only child synced, parent and grandparent skipped
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/applications/grandparent/managed-resources":
			resp := map[string]any{
				"items": []map[string]any{
					{"kind": "Application", "name": "parent"},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
		case "/api/v1/applications/parent/managed-resources":
			resp := map[string]any{
				"items": []map[string]any{
					{"kind": "Application", "name": "child"},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
		case "/api/v1/applications/child/managed-resources":
			resp := map[string]any{
				"items": []map[string]any{
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

	client := newTestArgoClient(ts)
	e := &Executor{argocd: client}

	locks := []models.Lock{
		{Application: "grandparent", PlanRevision: "abc"},
		{Application: "parent", PlanRevision: "abc"},
		{Application: "child", PlanRevision: "abc"},
	}

	syncIndices, skippedParents := e.computeSyncTargets(context.Background(), locks)

	if len(syncIndices) != 1 {
		t.Fatalf("expected 1 sync target, got %d", len(syncIndices))
	}
	if locks[syncIndices[0]].Application != "child" {
		t.Errorf("expected child to be synced, got %s", locks[syncIndices[0]].Application)
	}

	if len(skippedParents) != 2 {
		t.Fatalf("expected 2 skipped parents, got %d", len(skippedParents))
	}
	skippedNames := make(map[string]bool)
	for _, i := range skippedParents {
		skippedNames[locks[i].Application] = true
	}
	if !skippedNames["parent"] || !skippedNames["grandparent"] {
		t.Errorf("expected parent and grandparent to be skipped, got %v", skippedNames)
	}
}

func TestComputeSyncTargets_DiamondPattern(t *testing.T) {
	// parent manages both child1 and child2 → children synced, parent skipped
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/applications/parent/managed-resources":
			resp := map[string]any{
				"items": []map[string]any{
					{"kind": "Application", "name": "child1"},
					{"kind": "Application", "name": "child2"},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
		case "/api/v1/applications/child1/managed-resources",
			"/api/v1/applications/child2/managed-resources":
			resp := map[string]any{
				"items": []map[string]any{
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

	client := newTestArgoClient(ts)
	e := &Executor{argocd: client}

	locks := []models.Lock{
		{Application: "parent", PlanRevision: "abc"},
		{Application: "child1", PlanRevision: "abc"},
		{Application: "child2", PlanRevision: "abc"},
	}

	syncIndices, skippedParents := e.computeSyncTargets(context.Background(), locks)

	if len(syncIndices) != 2 {
		t.Errorf("expected 2 sync targets, got %d", len(syncIndices))
	}
	if len(skippedParents) != 1 {
		t.Fatalf("expected 1 skipped parent, got %d", len(skippedParents))
	}
	if locks[skippedParents[0]].Application != "parent" {
		t.Errorf("expected parent to be skipped, got %s", locks[skippedParents[0]].Application)
	}
}

func TestComputeSyncTargets_ManagedResourceError(t *testing.T) {
	// API error → falls back to syncing all apps (no parents detected)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	client := newTestArgoClient(ts)
	e := &Executor{argocd: client}

	locks := []models.Lock{
		{Application: "app-a", PlanRevision: "abc"},
		{Application: "app-b", PlanRevision: "abc"},
	}

	syncIndices, skippedParents := e.computeSyncTargets(context.Background(), locks)

	if len(syncIndices) != 2 {
		t.Errorf("expected 2 sync targets (fallback), got %d", len(syncIndices))
	}
	if len(skippedParents) != 0 {
		t.Errorf("expected 0 skipped parents, got %d", len(skippedParents))
	}
}

func TestComputeSyncTargets_ParentAppLocksExcluded(t *testing.T) {
	// IsParentApp locks should be excluded entirely (not in sync or skipped)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/applications/child-app/managed-resources":
			resp := map[string]any{
				"items": []map[string]any{
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

	client := newTestArgoClient(ts)
	e := &Executor{argocd: client}

	locks := []models.Lock{
		{Application: "child-app", PlanRevision: "abc"},
		{Application: "parent-app", IsParentApp: true},
	}

	syncIndices, skippedParents := e.computeSyncTargets(context.Background(), locks)

	if len(syncIndices) != 1 {
		t.Fatalf("expected 1 sync target, got %d", len(syncIndices))
	}
	if locks[syncIndices[0]].Application != "child-app" {
		t.Errorf("expected child-app, got %s", locks[syncIndices[0]].Application)
	}
	if len(skippedParents) != 0 {
		t.Errorf("expected 0 skipped parents (IsParentApp excluded entirely), got %d", len(skippedParents))
	}
}

func TestDisableAutoSyncForLocks(t *testing.T) {
	// Tests the full disableAutoSyncForLocks flow: disables auto-sync on apps,
	// stores state in locks, and disables parent auto-sync.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		// ListApplications for BuildParentMap
		case r.Method == "GET" && r.URL.Path == "/api/v1/applications" && r.URL.RawQuery != "":
			resp := v1alpha1.ApplicationList{Items: []v1alpha1.Application{}}
			_ = json.NewEncoder(w).Encode(resp)

		// GetApplicationRaw for disableAutoSync
		case r.Method == "GET" && r.URL.Path == "/api/v1/applications/app-with-autosync":
			app := v1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{Name: "app-with-autosync"},
				Spec: v1alpha1.ApplicationSpec{
					SyncPolicy: &v1alpha1.SyncPolicy{
						Automated: &v1alpha1.SyncPolicyAutomated{Prune: true, SelfHeal: true},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(app)

		case r.Method == "GET" && r.URL.Path == "/api/v1/applications/app-no-autosync":
			app := v1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{Name: "app-no-autosync"},
				Spec:       v1alpha1.ApplicationSpec{},
			}
			_ = json.NewEncoder(w).Encode(app)

		// UpdateApplicationSpec
		case r.Method == "PUT" && strings.HasSuffix(r.URL.Path, "/spec"):
			w.WriteHeader(http.StatusOK)

		// GetManagedResources for parent detection
		case strings.HasSuffix(r.URL.Path, "/managed-resources"):
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}})

		default:
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{})
		}
	}))
	defer ts.Close()

	client := newTestArgoClient(ts)
	lockStore := make(map[string]*models.Lock)
	mockLock := &mockAutoSyncLockManagerFull{
		lockStore: lockStore,
	}

	// Pre-populate locks in the store
	lockStore["app-with-autosync"] = &models.Lock{
		Application: "app-with-autosync",
		PRNumber:    1,
		Repo:        "org/repo",
		ChangeType:  models.ApplicationExisting,
	}
	lockStore["app-no-autosync"] = &models.Lock{
		Application: "app-no-autosync",
		PRNumber:    1,
		Repo:        "org/repo",
		ChangeType:  models.ApplicationExisting,
	}

	e := &Executor{argocd: client, lock: mockLock}

	locks := []models.Lock{
		{Application: "app-with-autosync", ChangeType: models.ApplicationExisting},
		{Application: "app-no-autosync", ChangeType: models.ApplicationExisting},
		{Application: "new-app", ChangeType: models.ApplicationNew}, // should be skipped
	}

	event := &models.PREvent{
		Repo: models.RepoInfo{
			FullName: "org/repo",
			HTMLURL:  "https://github.com/org/repo",
		},
		PR:       models.PRInfo{Number: 1},
		Provider: "github",
		Sender:   models.UserInfo{Login: "user"},
	}

	err := e.disableAutoSyncForLocks(context.Background(), locks, event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify auto-sync state was stored for app-with-autosync
	storedLock := lockStore["app-with-autosync"]
	if !storedLock.AutoSyncDisabled {
		t.Error("expected AutoSyncDisabled=true for app-with-autosync")
	}
	if len(storedLock.OriginalSyncPolicy) == 0 {
		t.Error("expected OriginalSyncPolicy to be set")
	}

	// Verify app-no-autosync was not marked as disabled
	noAutoSyncLock := lockStore["app-no-autosync"]
	if noAutoSyncLock.AutoSyncDisabled {
		t.Error("expected AutoSyncDisabled=false for app-no-autosync")
	}
}

func TestDisableAutoSyncForLocks_AllNewApps(t *testing.T) {
	// When all locks are new apps, no ArgoCD calls should be made.
	e := &Executor{argocd: nil} // nil client — should not be called

	locks := []models.Lock{
		{Application: "new-app-1", ChangeType: models.ApplicationNew},
		{Application: "new-app-2", ChangeType: models.ApplicationDeleted},
	}

	event := &models.PREvent{
		Repo: models.RepoInfo{FullName: "org/repo"},
		PR:   models.PRInfo{Number: 1},
	}

	err := e.disableAutoSyncForLocks(context.Background(), locks, event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDisableAutoSyncForLocks_WithAppSet(t *testing.T) {
	// Tests that ApplicationSet template auto-sync is also disabled.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.Path == "/api/v1/applications" && r.URL.RawQuery != "":
			resp := v1alpha1.ApplicationList{Items: []v1alpha1.Application{}}
			_ = json.NewEncoder(w).Encode(resp)

		case r.Method == "GET" && r.URL.Path == "/api/v1/applications/appset-app":
			app := v1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "appset-app",
					Labels: map[string]string{"argocd.argoproj.io/application-set-name": "my-appset"},
				},
				Spec: v1alpha1.ApplicationSpec{
					SyncPolicy: &v1alpha1.SyncPolicy{
						Automated: &v1alpha1.SyncPolicyAutomated{Prune: true},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(app)

		case r.Method == "GET" && r.URL.Path == "/api/v1/applicationsets/my-appset":
			appSet := v1alpha1.ApplicationSet{
				ObjectMeta: metav1.ObjectMeta{Name: "my-appset"},
				Spec: v1alpha1.ApplicationSetSpec{
					Template: v1alpha1.ApplicationSetTemplate{
						Spec: v1alpha1.ApplicationSpec{
							SyncPolicy: &v1alpha1.SyncPolicy{
								Automated: &v1alpha1.SyncPolicyAutomated{Prune: true, SelfHeal: true},
							},
						},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(appSet)

		case r.Method == "PUT" && r.URL.Path == "/api/v1/applicationsets/my-appset":
			w.WriteHeader(http.StatusOK)

		case r.Method == "PUT" && strings.HasSuffix(r.URL.Path, "/spec"):
			w.WriteHeader(http.StatusOK)

		case strings.HasSuffix(r.URL.Path, "/managed-resources"):
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}})

		default:
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{})
		}
	}))
	defer ts.Close()

	client := newTestArgoClient(ts)
	lockStore := make(map[string]*models.Lock)
	mockLock := &mockAutoSyncLockManagerFull{
		lockStore:     lockStore,
		appSetStored:  make(map[string][]byte),
	}
	lockStore["appset-app"] = &models.Lock{
		Application: "appset-app",
		PRNumber:    1,
		Repo:        "org/repo",
		ChangeType:  models.ApplicationExisting,
	}

	e := &Executor{argocd: client, lock: mockLock}

	locks := []models.Lock{
		{Application: "appset-app", ChangeType: models.ApplicationExisting},
	}
	event := &models.PREvent{
		Repo:     models.RepoInfo{FullName: "org/repo", HTMLURL: "https://github.com/org/repo"},
		PR:       models.PRInfo{Number: 1},
		Provider: "github",
		Sender:   models.UserInfo{Login: "user"},
	}

	err := e.disableAutoSyncForLocks(context.Background(), locks, event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify ApplicationSet auto-sync state was stored
	if len(mockLock.appSetStored) == 0 {
		t.Error("expected ApplicationSet auto-sync state to be stored")
	}
	if _, ok := mockLock.appSetStored["my-appset"]; !ok {
		t.Error("expected 'my-appset' in stored AppSet policies")
	}

	// Verify lock has ApplicationSetName set
	if lockStore["appset-app"].ApplicationSetName != "my-appset" {
		t.Errorf("expected ApplicationSetName='my-appset', got %q", lockStore["appset-app"].ApplicationSetName)
	}
}

func TestDisableAutoSyncForRollback(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.Path == "/api/v1/applications" && r.URL.RawQuery != "":
			// ListApplications for FindParentApps
			resp := v1alpha1.ApplicationList{Items: []v1alpha1.Application{}}
			_ = json.NewEncoder(w).Encode(resp)

		case r.Method == "GET" && r.URL.Path == "/api/v1/applications/my-app":
			app := v1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{Name: "my-app"},
				Spec: v1alpha1.ApplicationSpec{
					SyncPolicy: &v1alpha1.SyncPolicy{
						Automated: &v1alpha1.SyncPolicyAutomated{Prune: true},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(app)

		case r.Method == "PUT" && strings.HasSuffix(r.URL.Path, "/spec"):
			w.WriteHeader(http.StatusOK)

		case strings.HasSuffix(r.URL.Path, "/managed-resources"):
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}})

		default:
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{})
		}
	}))
	defer ts.Close()

	client := newTestArgoClient(ts)
	lockStore := make(map[string]*models.Lock)
	mockLock := &mockAutoSyncLockManagerFull{lockStore: lockStore}
	lockStore["my-app"] = &models.Lock{
		Application: "my-app",
		PRNumber:    1,
		Repo:        "org/repo",
	}

	e := &Executor{argocd: client, lock: mockLock}

	event := &models.PREvent{
		Repo:     models.RepoInfo{FullName: "org/repo", HTMLURL: "https://github.com/org/repo"},
		PR:       models.PRInfo{Number: 1},
		Provider: "github",
		Sender:   models.UserInfo{Login: "user"},
	}

	err := e.disableAutoSyncForRollback(context.Background(), "my-app", event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify state stored in lock
	storedLock := lockStore["my-app"]
	if !storedLock.AutoSyncDisabled {
		t.Error("expected AutoSyncDisabled=true")
	}
	if len(storedLock.OriginalSyncPolicy) == 0 {
		t.Error("expected OriginalSyncPolicy to be set")
	}
}

func TestDisableAutoSyncForRollback_NoAutoSync(t *testing.T) {
	// App without auto-sync — state not modified but parent detection still runs
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.Path == "/api/v1/applications" && r.URL.RawQuery != "":
			resp := v1alpha1.ApplicationList{Items: []v1alpha1.Application{}}
			_ = json.NewEncoder(w).Encode(resp)

		case r.Method == "GET" && r.URL.Path == "/api/v1/applications/manual-app":
			app := v1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{Name: "manual-app"},
				Spec:       v1alpha1.ApplicationSpec{},
			}
			_ = json.NewEncoder(w).Encode(app)

		case strings.HasSuffix(r.URL.Path, "/managed-resources"):
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}})

		default:
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{})
		}
	}))
	defer ts.Close()

	client := newTestArgoClient(ts)
	lockStore := make(map[string]*models.Lock)
	mockLock := &mockAutoSyncLockManagerFull{lockStore: lockStore}
	lockStore["manual-app"] = &models.Lock{Application: "manual-app", PRNumber: 1}

	e := &Executor{argocd: client, lock: mockLock}
	event := &models.PREvent{
		Repo:     models.RepoInfo{FullName: "org/repo", HTMLURL: "https://github.com/org/repo"},
		PR:       models.PRInfo{Number: 1},
		Provider: "github",
		Sender:   models.UserInfo{Login: "user"},
	}

	err := e.disableAutoSyncForRollback(context.Background(), "manual-app", event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Lock should not be marked as auto-sync disabled
	if lockStore["manual-app"].AutoSyncDisabled {
		t.Error("expected AutoSyncDisabled=false for manual-sync app")
	}
}

func TestRestoreAllAutoSync_WithAppSet(t *testing.T) {
	// Tests the full restore flow including ApplicationSet templates and parent cleanup.
	var childRestored, parentRestored, appSetRestored bool

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.Path == "/api/v1/applications/child-app":
			app := v1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{Name: "child-app"},
				Spec:       v1alpha1.ApplicationSpec{SyncPolicy: &v1alpha1.SyncPolicy{}},
			}
			_ = json.NewEncoder(w).Encode(app)
		case r.Method == "PUT" && r.URL.Path == "/api/v1/applications/child-app/spec":
			childRestored = true
			w.WriteHeader(http.StatusOK)
		case r.Method == "GET" && r.URL.Path == "/api/v1/applications/parent-app":
			app := v1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{Name: "parent-app"},
				Spec:       v1alpha1.ApplicationSpec{SyncPolicy: &v1alpha1.SyncPolicy{}},
			}
			_ = json.NewEncoder(w).Encode(app)
		case r.Method == "PUT" && r.URL.Path == "/api/v1/applications/parent-app/spec":
			parentRestored = true
			w.WriteHeader(http.StatusOK)
		case r.Method == "GET" && r.URL.Path == "/api/v1/applicationsets/my-appset":
			appSet := v1alpha1.ApplicationSet{
				ObjectMeta: metav1.ObjectMeta{Name: "my-appset"},
				Spec: v1alpha1.ApplicationSetSpec{
					Template: v1alpha1.ApplicationSetTemplate{
						Spec: v1alpha1.ApplicationSpec{
							SyncPolicy: &v1alpha1.SyncPolicy{},
						},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(appSet)
		case r.Method == "PUT" && r.URL.Path == "/api/v1/applicationsets/my-appset":
			appSetRestored = true
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{})
		}
	}))
	defer ts.Close()

	client := newTestArgoClient(ts)

	policy := mustMarshal(t, &v1alpha1.SyncPolicyAutomated{Prune: true})
	appSetPolicy := mustMarshal(t, &v1alpha1.SyncPolicyAutomated{Prune: true, SelfHeal: true})

	mockLock := &mockAutoSyncLockManagerFull{
		lockStore: make(map[string]*models.Lock),
		appSetStored: map[string][]byte{
			"my-appset": appSetPolicy,
		},
		parentAutoSync: make(map[string][]byte),
	}

	locks := []models.Lock{
		{
			Application:        "child-app",
			AutoSyncDisabled:   true,
			OriginalSyncPolicy: policy,
			IsParentApp:        false,
			ApplicationSetName: "my-appset",
		},
		{
			Application:        "parent-app",
			AutoSyncDisabled:   true,
			OriginalSyncPolicy: policy,
			IsParentApp:        true,
		},
	}

	restoreAllAutoSync(context.Background(), client, mockLock, locks, "org/repo", 1)

	if !childRestored {
		t.Error("expected child app auto-sync to be restored")
	}
	if !parentRestored {
		t.Error("expected parent app auto-sync to be restored")
	}
	if !appSetRestored {
		t.Error("expected ApplicationSet auto-sync to be restored")
	}
	if !mockLock.appSetDeleted["my-appset"] {
		t.Error("expected AppSet auto-sync state to be cleaned up")
	}
}

func TestRestoreAppSetAutoSync_Success(t *testing.T) {
	var updated bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.Path == "/api/v1/applicationsets/my-appset":
			appSet := v1alpha1.ApplicationSet{
				ObjectMeta: metav1.ObjectMeta{Name: "my-appset"},
				Spec: v1alpha1.ApplicationSetSpec{
					Template: v1alpha1.ApplicationSetTemplate{
						Spec: v1alpha1.ApplicationSpec{
							SyncPolicy: &v1alpha1.SyncPolicy{},
						},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(appSet)
		case r.Method == "PUT" && r.URL.Path == "/api/v1/applicationsets/my-appset":
			updated = true
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer ts.Close()

	client := newTestArgoClient(ts)
	policy := mustMarshal(t, &v1alpha1.SyncPolicyAutomated{Prune: true, SelfHeal: true})

	err := restoreAppSetAutoSync(context.Background(), client, "my-appset", policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !updated {
		t.Error("expected ApplicationSet to be updated")
	}
}

func TestRestoreAppSetAutoSync_EmptyPolicy(t *testing.T) {
	// Should be a no-op when policy is empty
	err := restoreAppSetAutoSync(context.Background(), nil, "my-appset", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// mockAutoSyncLockManagerFull provides a more complete mock for testing
// disableAutoSyncForLocks and disableAutoSyncForRollback flows.
type mockAutoSyncLockManagerFull struct {
	mockLockManagerForSync
	lockStore      map[string]*models.Lock
	appSetStored   map[string][]byte
	appSetDeleted  map[string]bool
	parentAutoSync map[string][]byte
}

func (m *mockAutoSyncLockManagerFull) Lock(_ context.Context, req models.LockRequest) (*models.LockResult, error) {
	lock := &models.Lock{
		Application: req.Application,
		PRNumber:    req.PRNumber,
		Repo:        req.Repo,
	}
	if m.lockStore != nil {
		m.lockStore[req.Application] = lock
	}
	return &models.LockResult{Acquired: true, Lock: lock}, nil
}

func (m *mockAutoSyncLockManagerFull) Get(_ context.Context, application string) (*models.Lock, error) {
	if m.lockStore != nil {
		if lock, ok := m.lockStore[application]; ok {
			return lock, nil
		}
	}
	return nil, nil
}

func (m *mockAutoSyncLockManagerFull) UpdateLock(_ context.Context, lock *models.Lock) error {
	if m.lockStore != nil {
		m.lockStore[lock.Application] = lock
	}
	return nil
}

func (m *mockAutoSyncLockManagerFull) StoreAppSetAutoSync(_ context.Context, appSetName, _ string, _ int, data []byte) error {
	if m.appSetStored == nil {
		m.appSetStored = make(map[string][]byte)
	}
	m.appSetStored[appSetName] = data
	return nil
}

func (m *mockAutoSyncLockManagerFull) GetAppSetAutoSync(_ context.Context, appSetName, _ string, _ int) ([]byte, error) {
	if m.appSetStored != nil {
		return m.appSetStored[appSetName], nil
	}
	return nil, nil
}

func (m *mockAutoSyncLockManagerFull) DeleteAppSetAutoSync(_ context.Context, appSetName, _ string, _ int) error {
	if m.appSetDeleted == nil {
		m.appSetDeleted = make(map[string]bool)
	}
	m.appSetDeleted[appSetName] = true
	return nil
}

func (m *mockAutoSyncLockManagerFull) StoreParentAutoSync(_ context.Context, parentApp, _ string, _ int, data []byte) error {
	if m.parentAutoSync == nil {
		m.parentAutoSync = make(map[string][]byte)
	}
	m.parentAutoSync[parentApp] = data
	return nil
}

func (m *mockAutoSyncLockManagerFull) DeleteParentAutoSync(_ context.Context, parentApp, _ string, _ int) error {
	if m.parentAutoSync != nil {
		delete(m.parentAutoSync, parentApp)
	}
	return nil
}

func TestComputeSyncTargets_ParentManagesUnlockedChild(t *testing.T) {
	// parent manages "other-app" which is NOT in the locks — parent should be synced
	// because its child relationship is with an app outside the locked set
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/applications/parent/managed-resources":
			resp := map[string]any{
				"items": []map[string]any{
					{"kind": "Application", "name": "other-app"},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
		case "/api/v1/applications/leaf/managed-resources":
			resp := map[string]any{
				"items": []map[string]any{
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

	client := newTestArgoClient(ts)
	e := &Executor{argocd: client}

	locks := []models.Lock{
		{Application: "parent", PlanRevision: "abc"},
		{Application: "leaf", PlanRevision: "abc"},
	}

	syncIndices, skippedParents := e.computeSyncTargets(context.Background(), locks)

	// Both should be synced — parent's child "other-app" is not in the lock set
	if len(syncIndices) != 2 {
		t.Errorf("expected 2 sync targets, got %d", len(syncIndices))
	}
	if len(skippedParents) != 0 {
		t.Errorf("expected 0 skipped parents, got %d", len(skippedParents))
	}
}
