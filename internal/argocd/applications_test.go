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
	"sync/atomic"
	"testing"
	"time"

	v1alpha1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/org/lemuria/internal/models"
)

func TestNormalizeRepoURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{
			name: "https URL",
			url:  "https://github.com/org/repo",
			want: "github.com/org/repo",
		},
		{
			name: "https URL with .git",
			url:  "https://github.com/org/repo.git",
			want: "github.com/org/repo",
		},
		{
			name: "http URL",
			url:  "http://github.com/org/repo",
			want: "github.com/org/repo",
		},
		{
			name: "ssh URL",
			url:  "git@github.com:org/repo.git",
			want: "github.com/org/repo",
		},
		{
			name: "ssh URL without .git",
			url:  "git@github.com:org/repo",
			want: "github.com/org/repo",
		},
		{
			name: "case insensitive",
			url:  "HTTPS://GitHub.COM/Org/Repo",
			want: "github.com/org/repo",
		},
		{
			name: "already normalized",
			url:  "github.com/org/repo",
			want: "github.com/org/repo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeRepoURL(tt.url); got != tt.want {
				t.Errorf("NormalizeRepoURL(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

func TestConvertV1alpha1Application(t *testing.T) {
	tests := []struct {
		name       string
		app        v1alpha1.Application
		sourceFile string
		want       models.Application
	}{
		{
			name: "minimal app defaults namespace and project",
			app: v1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{Name: "my-app"},
			},
			want: models.Application{
				Name:      "my-app",
				Namespace: "argocd",
				Project:   "default",
			},
		},
		{
			name: "single-source app with all fields",
			app: v1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "full-app",
					Namespace: "custom-ns",
					Labels:    map[string]string{"team": "platform"},
				},
				Spec: v1alpha1.ApplicationSpec{
					Project: "my-project",
					Source: &v1alpha1.ApplicationSource{
						RepoURL:        "https://github.com/org/repo",
						Path:           "manifests",
						TargetRevision: "main",
					},
					Destination: v1alpha1.ApplicationDestination{
						Server:    "https://kubernetes.default.svc",
						Namespace: "production",
					},
				},
				Status: v1alpha1.ApplicationStatus{
					Sync: v1alpha1.SyncStatus{
						Status: v1alpha1.SyncStatusCodeSynced,
					},
					Health: v1alpha1.AppHealthStatus{
						Status: "Healthy",
					},
				},
			},
			sourceFile: "apps/full-app.yaml",
			want: models.Application{
				Name:                 "full-app",
				Namespace:            "custom-ns",
				Project:              "my-project",
				RepoURL:              "https://github.com/org/repo",
				Path:                 "manifests",
				TargetRevision:       "main",
				DestinationServer:    "https://kubernetes.default.svc",
				DestinationNamespace: "production",
				SyncStatus:           models.SyncStatusSynced,
				HealthStatus:         "Healthy",
				Labels:               map[string]string{"team": "platform"},
				SourceFile:           "apps/full-app.yaml",
			},
		},
		{
			name: "multi-source app with Helm values",
			app: v1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "multi-src",
					Namespace: "argocd",
				},
				Spec: v1alpha1.ApplicationSpec{
					Project: "default",
					Sources: v1alpha1.ApplicationSources{
						{
							RepoURL:        "https://github.com/org/repo",
							Path:           "k8s",
							TargetRevision: "main",
						},
						{
							RepoURL:        "https://charts.example.com",
							Chart:          "nginx",
							TargetRevision: "1.0.0",
							Helm: &v1alpha1.ApplicationSourceHelm{
								ValueFiles: []string{"values.yaml"},
								Values:     "replicas: 3\n",
							},
						},
					},
					Destination: v1alpha1.ApplicationDestination{
						Server: "https://kubernetes.default.svc",
					},
				},
			},
			want: models.Application{
				Name:              "multi-src",
				Namespace:         "argocd",
				Project:           "default",
				DestinationServer: "https://kubernetes.default.svc",
				Sources: []models.ApplicationSource{
					{RepoURL: "https://github.com/org/repo", Path: "k8s", TargetRevision: "main"},
					{
						RepoURL:        "https://charts.example.com",
						Chart:          "nginx",
						TargetRevision: "1.0.0",
						Helm: &models.HelmSource{
							ValueFiles: []string{"values.yaml"},
							Values:     "replicas: 3\n",
						},
					},
				},
			},
		},
		{
			name: "app with ApplicationSet label",
			app: v1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "generated-app",
					Namespace: "argocd",
					Labels: map[string]string{
						"argocd.argoproj.io/application-set-name": "my-appset",
						"team": "backend",
					},
				},
				Spec: v1alpha1.ApplicationSpec{
					Project: "default",
				},
			},
			want: models.Application{
				Name:               "generated-app",
				Namespace:          "argocd",
				Project:            "default",
				ApplicationSetName: "my-appset",
				Labels: map[string]string{
					"argocd.argoproj.io/application-set-name": "my-appset",
					"team": "backend",
				},
			},
		},
		{
			name: "app with ownerReference to ApplicationSet",
			app: v1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "owned-app",
					Namespace: "argocd",
					OwnerReferences: []metav1.OwnerReference{
						{Kind: "ApplicationSet", Name: "owner-appset"},
					},
				},
				Spec: v1alpha1.ApplicationSpec{Project: "default"},
			},
			want: models.Application{
				Name:               "owned-app",
				Namespace:          "argocd",
				Project:            "default",
				ApplicationSetName: "owner-appset",
			},
		},
		{
			name: "app with auto-sync enabled",
			app: v1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{Name: "auto-sync-app", Namespace: "argocd"},
				Spec: v1alpha1.ApplicationSpec{
					Project: "default",
					SyncPolicy: &v1alpha1.SyncPolicy{
						Automated: &v1alpha1.SyncPolicyAutomated{Prune: true},
					},
				},
			},
			want: models.Application{
				Name:            "auto-sync-app",
				Namespace:       "argocd",
				Project:         "default",
				AutoSyncEnabled: true,
			},
		},
		{
			name: "app with syncPolicy but no automated",
			app: v1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{Name: "manual-app", Namespace: "argocd"},
				Spec: v1alpha1.ApplicationSpec{
					Project:    "default",
					SyncPolicy: &v1alpha1.SyncPolicy{},
				},
			},
			want: models.Application{
				Name:            "manual-app",
				Namespace:       "argocd",
				Project:         "default",
				AutoSyncEnabled: false,
			},
		},
		{
			name:       "app with sourceFile parameter",
			sourceFile: "environments/prod/app.yaml",
			app: v1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{Name: "file-app", Namespace: "argocd"},
				Spec:       v1alpha1.ApplicationSpec{Project: "default"},
			},
			want: models.Application{
				Name:       "file-app",
				Namespace:  "argocd",
				Project:    "default",
				SourceFile: "environments/prod/app.yaml",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := convertV1alpha1Application(tt.app, tt.sourceFile)
			if got.Name != tt.want.Name {
				t.Errorf("Name = %q, want %q", got.Name, tt.want.Name)
			}
			if got.Namespace != tt.want.Namespace {
				t.Errorf("Namespace = %q, want %q", got.Namespace, tt.want.Namespace)
			}
			if got.Project != tt.want.Project {
				t.Errorf("Project = %q, want %q", got.Project, tt.want.Project)
			}
			if got.RepoURL != tt.want.RepoURL {
				t.Errorf("RepoURL = %q, want %q", got.RepoURL, tt.want.RepoURL)
			}
			if got.Path != tt.want.Path {
				t.Errorf("Path = %q, want %q", got.Path, tt.want.Path)
			}
			if got.TargetRevision != tt.want.TargetRevision {
				t.Errorf("TargetRevision = %q, want %q", got.TargetRevision, tt.want.TargetRevision)
			}
			if got.DestinationServer != tt.want.DestinationServer {
				t.Errorf("DestinationServer = %q, want %q", got.DestinationServer, tt.want.DestinationServer)
			}
			if got.DestinationNamespace != tt.want.DestinationNamespace {
				t.Errorf("DestinationNamespace = %q, want %q", got.DestinationNamespace, tt.want.DestinationNamespace)
			}
			if got.SyncStatus != tt.want.SyncStatus {
				t.Errorf("SyncStatus = %q, want %q", got.SyncStatus, tt.want.SyncStatus)
			}
			if got.HealthStatus != tt.want.HealthStatus {
				t.Errorf("HealthStatus = %q, want %q", got.HealthStatus, tt.want.HealthStatus)
			}
			if got.ApplicationSetName != tt.want.ApplicationSetName {
				t.Errorf("ApplicationSetName = %q, want %q", got.ApplicationSetName, tt.want.ApplicationSetName)
			}
			if got.AutoSyncEnabled != tt.want.AutoSyncEnabled {
				t.Errorf("AutoSyncEnabled = %v, want %v", got.AutoSyncEnabled, tt.want.AutoSyncEnabled)
			}
			if got.SourceFile != tt.want.SourceFile {
				t.Errorf("SourceFile = %q, want %q", got.SourceFile, tt.want.SourceFile)
			}
			if len(got.Sources) != len(tt.want.Sources) {
				t.Fatalf("Sources length = %d, want %d", len(got.Sources), len(tt.want.Sources))
			}
			for i, src := range got.Sources {
				wantSrc := tt.want.Sources[i]
				if src.RepoURL != wantSrc.RepoURL {
					t.Errorf("Sources[%d].RepoURL = %q, want %q", i, src.RepoURL, wantSrc.RepoURL)
				}
				if src.Path != wantSrc.Path {
					t.Errorf("Sources[%d].Path = %q, want %q", i, src.Path, wantSrc.Path)
				}
				if src.TargetRevision != wantSrc.TargetRevision {
					t.Errorf("Sources[%d].TargetRevision = %q, want %q", i, src.TargetRevision, wantSrc.TargetRevision)
				}
				if src.Chart != wantSrc.Chart {
					t.Errorf("Sources[%d].Chart = %q, want %q", i, src.Chart, wantSrc.Chart)
				}
				if (src.Helm == nil) != (wantSrc.Helm == nil) {
					t.Errorf("Sources[%d].Helm nil = %v, want %v", i, src.Helm == nil, wantSrc.Helm == nil)
				}
				if src.Helm != nil && wantSrc.Helm != nil {
					if src.Helm.Values != wantSrc.Helm.Values {
						t.Errorf("Sources[%d].Helm.Values = %q, want %q", i, src.Helm.Values, wantSrc.Helm.Values)
					}
				}
			}
		})
	}
}

func TestExtractAppSyncStatus(t *testing.T) {
	tests := []struct {
		name  string
		event watchEvent
		want  appSyncStatus
	}{
		{
			name: "event with operationState",
			event: func() watchEvent {
				var e watchEvent
				e.Result.Application.Status.Health.Status = "Healthy"
				e.Result.Application.Status.OperationState = &struct {
					Phase      string `json:"phase"`
					Message    string `json:"message"`
					SyncResult struct {
						Revision  string               `json:"revision"`
						Resources []syncResourceResult `json:"resources"`
					} `json:"syncResult"`
				}{
					Phase:   "Succeeded",
					Message: "sync completed",
				}
				e.Result.Application.Status.OperationState.SyncResult.Revision = "abc123"
				e.Result.Application.Status.OperationState.SyncResult.Resources = []syncResourceResult{
					{Kind: "Deployment", Name: "app", Status: "Synced"},
				}
				return e
			}(),
			want: func() appSyncStatus {
				s := appSyncStatus{
					HealthStatus:   "Healthy",
					OperationPhase: "Succeeded",
					Message:        "sync completed",
				}
				s.SyncResult.Revision = "abc123"
				s.SyncResult.Resources = []syncResourceResult{
					{Kind: "Deployment", Name: "app", Status: "Synced"},
				}
				return s
			}(),
		},
		{
			name: "event with nil operationState",
			event: func() watchEvent {
				var e watchEvent
				e.Result.Application.Status.Health.Status = "Progressing"
				return e
			}(),
			want: appSyncStatus{
				HealthStatus: "Progressing",
			},
		},
		{
			name: "event with health status only",
			event: func() watchEvent {
				var e watchEvent
				e.Result.Application.Status.Health.Status = "Degraded"
				return e
			}(),
			want: appSyncStatus{
				HealthStatus: "Degraded",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractAppSyncStatus(&tt.event)
			if got.HealthStatus != tt.want.HealthStatus {
				t.Errorf("HealthStatus = %q, want %q", got.HealthStatus, tt.want.HealthStatus)
			}
			if got.OperationPhase != tt.want.OperationPhase {
				t.Errorf("OperationPhase = %q, want %q", got.OperationPhase, tt.want.OperationPhase)
			}
			if got.Message != tt.want.Message {
				t.Errorf("Message = %q, want %q", got.Message, tt.want.Message)
			}
			if got.SyncResult.Revision != tt.want.SyncResult.Revision {
				t.Errorf("SyncResult.Revision = %q, want %q", got.SyncResult.Revision, tt.want.SyncResult.Revision)
			}
			if len(got.SyncResult.Resources) != len(tt.want.SyncResult.Resources) {
				t.Errorf("SyncResult.Resources length = %d, want %d", len(got.SyncResult.Resources), len(tt.want.SyncResult.Resources))
			}
		})
	}
}

func TestBuildSyncResult(t *testing.T) {
	tests := []struct {
		name          string
		appName       string
		status        *appSyncStatus
		wantPhase     models.SyncPhase
		wantResources int
		wantMessage   string
	}{
		{
			name:    "successful sync with resources",
			appName: "my-app",
			status: func() *appSyncStatus {
				s := &appSyncStatus{
					OperationPhase: "Succeeded",
					HealthStatus:   "Healthy",
				}
				s.SyncResult.Revision = "abc123"
				s.SyncResult.Resources = []syncResourceResult{
					{Group: "apps", Version: "v1", Kind: "Deployment", Name: "web", Namespace: "default", Status: "Synced", Message: "deployed"},
					{Version: "v1", Kind: "Service", Name: "web-svc", Namespace: "default", Status: "Synced"},
				}
				return s
			}(),
			wantPhase:     models.SyncPhaseSucceeded,
			wantResources: 2,
		},
		{
			name:    "failed sync with message",
			appName: "failing-app",
			status: func() *appSyncStatus {
				s := &appSyncStatus{
					OperationPhase: "Failed",
					Message:        "sync failed: resource quota exceeded",
				}
				return s
			}(),
			wantPhase:   models.SyncPhaseFailed,
			wantMessage: "sync failed: resource quota exceeded",
		},
		{
			name:    "hook resources are filtered out",
			appName: "hook-app",
			status: func() *appSyncStatus {
				s := &appSyncStatus{OperationPhase: "Succeeded"}
				s.SyncResult.Resources = []syncResourceResult{
					{Version: "v1", Kind: "ConfigMap", Name: "cm", Status: "Synced"},
					{Version: "v1", Kind: "Job", Name: "pre-hook", Status: "Synced", HookType: "PreSync"},
					{Version: "v1", Kind: "Job", Name: "post-hook", Status: "Synced", HookType: "PostSync"},
				}
				return s
			}(),
			wantPhase:     models.SyncPhaseSucceeded,
			wantResources: 1,
		},
		{
			name:    "resource with group builds correct apiVersion",
			appName: "group-app",
			status: func() *appSyncStatus {
				s := &appSyncStatus{OperationPhase: "Succeeded"}
				s.SyncResult.Resources = []syncResourceResult{
					{Group: "apps", Version: "v1", Kind: "Deployment", Name: "web", Status: "Synced"},
				}
				return s
			}(),
			wantPhase:     models.SyncPhaseSucceeded,
			wantResources: 1,
		},
		{
			name:    "resource without group uses just version",
			appName: "core-app",
			status: func() *appSyncStatus {
				s := &appSyncStatus{OperationPhase: "Succeeded"}
				s.SyncResult.Resources = []syncResourceResult{
					{Version: "v1", Kind: "Service", Name: "svc", Status: "Synced"},
				}
				return s
			}(),
			wantPhase:     models.SyncPhaseSucceeded,
			wantResources: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildSyncResult(tt.appName, tt.status)
			if got.Application != tt.appName {
				t.Errorf("Application = %q, want %q", got.Application, tt.appName)
			}
			if got.Phase != tt.wantPhase {
				t.Errorf("Phase = %q, want %q", got.Phase, tt.wantPhase)
			}
			if len(got.Resources) != tt.wantResources {
				t.Errorf("Resources count = %d, want %d", len(got.Resources), tt.wantResources)
			}
			if tt.wantMessage != "" && got.Message != tt.wantMessage {
				t.Errorf("Message = %q, want %q", got.Message, tt.wantMessage)
			}

			// Verify apiVersion formatting
			if tt.name == "resource with group builds correct apiVersion" && len(got.Resources) > 0 {
				if got.Resources[0].Resource.APIVersion != "apps/v1" {
					t.Errorf("APIVersion = %q, want %q", got.Resources[0].Resource.APIVersion, "apps/v1")
				}
			}
			if tt.name == "resource without group uses just version" && len(got.Resources) > 0 {
				if got.Resources[0].Resource.APIVersion != "v1" {
					t.Errorf("APIVersion = %q, want %q", got.Resources[0].Resource.APIVersion, "v1")
				}
			}
		})
	}
}

func TestSyncApplicationWatchBeforeSync(t *testing.T) {
	// Verify that the watch stream is opened BEFORE the sync POST is triggered.
	// This prevents a race condition where the sync completes before the watch
	// starts, causing the watch to miss all events.

	var watchOpened atomic.Int64
	var syncTriggered atomic.Int64
	var counter atomic.Int64

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/stream/applications" && r.Method == http.MethodGet:
			// Record the order of the watch request
			watchOpened.Store(counter.Add(1))

			// Stream a sync-succeeded + healthy event then close
			flusher, ok := w.(http.Flusher)
			if !ok {
				http.Error(w, "streaming not supported", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)

			event := watchEvent{}
			event.Result.Type = "MODIFIED"
			event.Result.Application.Status.Health.Status = "Healthy"
			event.Result.Application.Status.OperationState = &struct {
				Phase      string `json:"phase"`
				Message    string `json:"message"`
				SyncResult struct {
					Revision  string               `json:"revision"`
					Resources []syncResourceResult `json:"resources"`
				} `json:"syncResult"`
			}{
				Phase:   "Succeeded",
				Message: "sync completed",
			}
			event.Result.Application.Status.OperationState.SyncResult.Revision = "abc123"

			data, _ := json.Marshal(event)
			_, _ = w.Write(data)
			_, _ = w.Write([]byte("\n"))
			flusher.Flush()
			// Keep connection open briefly so client can read
			time.Sleep(100 * time.Millisecond)

		case r.URL.Path == "/api/v1/applications/test-app/sync" && r.Method == http.MethodPost:
			// Record the order of the sync request
			syncTriggered.Store(counter.Add(1))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))

		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer ts.Close()

	client := &Client{
		baseURL:    ts.URL,
		token:      "test-token",
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := client.SyncApplication(ctx, "test-app", &SyncOptions{
		Timeout: 15 * time.Second,
	})
	if err != nil {
		t.Fatalf("SyncApplication failed: %v", err)
	}

	if result == nil {
		t.Fatal("Expected non-nil SyncResult")
	}

	// Verify ordering: watch must be opened before sync is triggered
	watchOrder := watchOpened.Load()
	syncOrder := syncTriggered.Load()

	if watchOrder == 0 {
		t.Error("Watch stream was never opened")
	}
	if syncOrder == 0 {
		t.Error("Sync was never triggered")
	}
	if watchOrder >= syncOrder {
		t.Errorf("Watch was opened (order=%d) AFTER sync was triggered (order=%d); expected watch first",
			watchOrder, syncOrder)
	}
}

func TestWaitForSyncCompleteWithPreOpenedEvents(t *testing.T) {
	// Verify that waitForSyncComplete uses a pre-opened events channel
	// instead of opening a new watch stream.

	client := &Client{
		baseURL:    "http://unused",
		token:      "test-token",
		httpClient: &http.Client{},
	}

	// Create a pre-opened events channel and send events on it
	events := make(chan watchEvent, 2)

	// Send a sync-succeeded event
	succeededEvent := watchEvent{}
	succeededEvent.Result.Application.Status.Health.Status = "Healthy"
	succeededEvent.Result.Application.Status.OperationState = &struct {
		Phase      string `json:"phase"`
		Message    string `json:"message"`
		SyncResult struct {
			Revision  string               `json:"revision"`
			Resources []syncResourceResult `json:"resources"`
		} `json:"syncResult"`
	}{
		Phase:   "Succeeded",
		Message: "sync completed",
	}
	succeededEvent.Result.Application.Status.OperationState.SyncResult.Revision = "def456"

	events <- succeededEvent
	close(events)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := client.waitForSyncComplete(ctx, "test-app", 5*time.Second, events)
	if err != nil {
		t.Fatalf("waitForSyncComplete failed: %v", err)
	}

	if result == nil {
		t.Fatal("Expected non-nil SyncResult")
	}
	if result.Phase != models.SyncPhaseSucceeded {
		t.Errorf("Phase = %q, want %q", result.Phase, models.SyncPhaseSucceeded)
	}
	if result.Revision != "def456" {
		t.Errorf("Revision = %q, want %q", result.Revision, "def456")
	}
}

func TestWaitForSyncCompleteNilEventsOpensWatch(t *testing.T) {
	// Verify that waitForSyncComplete opens its own watch stream when
	// preOpenedEvents is nil (used by callers like RollbackApplication
	// that don't pre-open a watch).

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/stream/applications" && r.Method == http.MethodGet {
			flusher, ok := w.(http.Flusher)
			if !ok {
				http.Error(w, "streaming not supported", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)

			event := watchEvent{}
			event.Result.Application.Status.Health.Status = "Healthy"
			event.Result.Application.Status.OperationState = &struct {
				Phase      string `json:"phase"`
				Message    string `json:"message"`
				SyncResult struct {
					Revision  string               `json:"revision"`
					Resources []syncResourceResult `json:"resources"`
				} `json:"syncResult"`
			}{
				Phase: "Succeeded",
			}

			data, _ := json.Marshal(event)
			_, _ = w.Write(data)
			_, _ = w.Write([]byte("\n"))
			flusher.Flush()
			time.Sleep(100 * time.Millisecond)
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer ts.Close()

	client := &Client{
		baseURL:    ts.URL,
		token:      "test-token",
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := client.waitForSyncComplete(ctx, "test-app", 10*time.Second, nil)
	if err != nil {
		t.Fatalf("waitForSyncComplete with nil events failed: %v", err)
	}

	if result == nil {
		t.Fatal("Expected non-nil SyncResult")
	}
	if result.Phase != models.SyncPhaseSucceeded {
		t.Errorf("Phase = %q, want %q", result.Phase, models.SyncPhaseSucceeded)
	}
}
