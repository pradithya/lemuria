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
	"testing"

	"github.com/org/lemuria/internal/models"
)

func TestParseRawApplicationFromYAML(t *testing.T) {
	t.Run("single document matching app", func(t *testing.T) {
		yaml := []byte(`apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: my-app
  namespace: argocd
spec:
  source:
    repoURL: https://github.com/org/repo
    path: manifests
    targetRevision: main
  destination:
    server: https://kubernetes.default.svc
    namespace: default
`)

		app, err := ParseRawApplicationFromYAML(yaml, "my-app")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if app == nil {
			t.Fatal("expected non-nil result")
		}

		if app.Name != "my-app" {
			t.Errorf("expected name 'my-app', got %v", app.Name)
		}
		if app.Namespace != "argocd" {
			t.Errorf("expected namespace 'argocd', got %v", app.Namespace)
		}
		if app.Spec.Source == nil {
			t.Fatal("expected source to be non-nil")
		}
		if app.Spec.Source.RepoURL != "https://github.com/org/repo" {
			t.Errorf("expected repoURL, got %v", app.Spec.Source.RepoURL)
		}
	})

	t.Run("multi-document YAML finds correct app", func(t *testing.T) {
		yaml := []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: some-config
data:
  key: value
---
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: app-one
spec:
  source:
    repoURL: https://github.com/org/repo
    path: app-one
    targetRevision: main
  destination:
    server: https://kubernetes.default.svc
---
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: app-two
spec:
  sources:
    - repoURL: https://github.com/org/repo
      path: app-two
      targetRevision: main
    - repoURL: https://charts.example.com
      chart: nginx
      targetRevision: 1.0.0
      helm:
        values: |
          cpu: 52m
  destination:
    server: https://kubernetes.default.svc
`)

		app, err := ParseRawApplicationFromYAML(yaml, "app-two")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if app.Name != "app-two" {
			t.Errorf("expected name 'app-two', got %v", app.Name)
		}

		// Verify the full structure is preserved (sources with helm values)
		if len(app.Spec.Sources) != 2 {
			t.Fatalf("expected 2 sources, got %d", len(app.Spec.Sources))
		}

		helmSource := app.Spec.Sources[1]
		if helmSource.Helm == nil {
			t.Fatal("expected helm to be non-nil")
		}
		if helmSource.Helm.Values != "cpu: 52m\n" {
			t.Errorf("expected helm values 'cpu: 52m\\n', got %q", helmSource.Helm.Values)
		}
	})

	t.Run("app not found returns error", func(t *testing.T) {
		yaml := []byte(`apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: other-app
spec:
  source:
    repoURL: https://github.com/org/repo
    path: manifests
    targetRevision: main
  destination:
    server: https://kubernetes.default.svc
`)

		_, err := ParseRawApplicationFromYAML(yaml, "my-app")
		if err == nil {
			t.Fatal("expected error for app not found")
		}
	})

	t.Run("non-Application resources are skipped", func(t *testing.T) {
		yaml := []byte(`apiVersion: argoproj.io/v1alpha1
kind: AppProject
metadata:
  name: my-app
spec:
  description: test project
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: my-app
data:
  key: value
`)

		_, err := ParseRawApplicationFromYAML(yaml, "my-app")
		if err == nil {
			t.Fatal("expected error since no Application CR matches")
		}
	})

	t.Run("empty YAML returns error", func(t *testing.T) {
		_, err := ParseRawApplicationFromYAML([]byte(""), "my-app")
		if err == nil {
			t.Fatal("expected error for empty YAML")
		}
	})
}

func TestParseApplicationsFromYAML(t *testing.T) {
	tests := []struct {
		name       string
		content    string
		sourceFile string
		wantCount  int
		wantNames  []string
		wantErr    bool
	}{
		{
			name: "single Application document",
			content: `apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: single-app
  namespace: argocd
spec:
  source:
    repoURL: https://github.com/org/repo
    path: manifests
    targetRevision: main
  destination:
    server: https://kubernetes.default.svc
    namespace: default
`,
			sourceFile: "apps.yaml",
			wantCount:  1,
			wantNames:  []string{"single-app"},
		},
		{
			name: "multi-document YAML with mixed resource types",
			content: `apiVersion: v1
kind: ConfigMap
metadata:
  name: my-config
data:
  key: value
---
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: app-one
spec:
  source:
    repoURL: https://github.com/org/repo
    path: app-one
    targetRevision: main
  destination:
    server: https://kubernetes.default.svc
---
apiVersion: v1
kind: Namespace
metadata:
  name: my-ns
`,
			wantCount: 1,
			wantNames: []string{"app-one"},
		},
		{
			name: "multi-document with multiple Applications",
			content: `apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: app-a
spec:
  source:
    repoURL: https://github.com/org/repo
    path: a
    targetRevision: main
  destination:
    server: https://kubernetes.default.svc
---
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: app-b
spec:
  source:
    repoURL: https://github.com/org/repo
    path: b
    targetRevision: main
  destination:
    server: https://kubernetes.default.svc
`,
			wantCount: 2,
			wantNames: []string{"app-a", "app-b"},
		},
		{
			name:      "empty YAML returns empty slice",
			content:   "",
			wantCount: 0,
		},
		{
			name: "non-Application CRs are skipped",
			content: `apiVersion: argoproj.io/v1alpha1
kind: AppProject
metadata:
  name: my-project
spec:
  description: test
---
apiVersion: v1
kind: Service
metadata:
  name: my-svc
spec:
  ports:
    - port: 80
`,
			wantCount: 0,
		},
		{
			name: "non-matching documents are skipped",
			content: `just a plain string
---
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: valid-app
spec:
  source:
    repoURL: https://github.com/org/repo
    path: manifests
    targetRevision: main
  destination:
    server: https://kubernetes.default.svc
`,
			wantCount: 1,
			wantNames: []string{"valid-app"},
		},
		{
			name: "application with auto-sync",
			content: `apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: auto-sync-app
spec:
  source:
    repoURL: https://github.com/org/repo
    path: apps/auto
    targetRevision: HEAD
  destination:
    server: https://kubernetes.default.svc
    namespace: default
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
`,
			wantCount: 1,
			wantNames: []string{"auto-sync-app"},
		},
		{
			name: "multi-source application",
			content: `apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: multi-source-app
spec:
  project: default
  sources:
    - repoURL: https://github.com/org/repo
      path: base
      targetRevision: main
    - repoURL: https://charts.example.com
      chart: my-chart
      targetRevision: 1.0.0
  destination:
    server: https://kubernetes.default.svc
    namespace: default
`,
			wantCount: 1,
			wantNames: []string{"multi-source-app"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			apps, err := ParseApplicationsFromYAML([]byte(tt.content), tt.sourceFile)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseApplicationsFromYAML() error = %v, wantErr %v", err, tt.wantErr)
			}
			if len(apps) != tt.wantCount {
				t.Fatalf("got %d apps, want %d", len(apps), tt.wantCount)
			}
			for i, name := range tt.wantNames {
				if i >= len(apps) {
					break
				}
				if apps[i].Name != name {
					t.Errorf("apps[%d].Name = %q, want %q", i, apps[i].Name, name)
				}
			}
			if tt.sourceFile != "" && len(apps) > 0 {
				if apps[0].SourceFile != tt.sourceFile {
					t.Errorf("SourceFile = %q, want %q", apps[0].SourceFile, tt.sourceFile)
				}
			}
		})
	}
}

func TestParseApplicationAutoSyncDetection(t *testing.T) {
	yamlWithAutoSync := `apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: auto-sync-app
spec:
  source:
    repoURL: https://github.com/org/repo
    path: apps/auto
    targetRevision: HEAD
  destination:
    server: https://kubernetes.default.svc
    namespace: default
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
`

	yamlWithoutAutoSync := `apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: manual-app
spec:
  source:
    repoURL: https://github.com/org/repo
    path: apps/manual
    targetRevision: HEAD
  destination:
    server: https://kubernetes.default.svc
    namespace: default
`

	t.Run("detects auto-sync enabled", func(t *testing.T) {
		apps, err := ParseApplicationsFromYAML([]byte(yamlWithAutoSync), "test.yaml")
		if err != nil {
			t.Fatalf("ParseApplicationsFromYAML() error = %v", err)
		}
		if len(apps) != 1 {
			t.Fatalf("expected 1 app, got %d", len(apps))
		}
		if !apps[0].AutoSyncEnabled {
			t.Error("expected AutoSyncEnabled to be true")
		}
	})

	t.Run("detects auto-sync disabled", func(t *testing.T) {
		apps, err := ParseApplicationsFromYAML([]byte(yamlWithoutAutoSync), "test.yaml")
		if err != nil {
			t.Fatalf("ParseApplicationsFromYAML() error = %v", err)
		}
		if len(apps) != 1 {
			t.Fatalf("expected 1 app, got %d", len(apps))
		}
		if apps[0].AutoSyncEnabled {
			t.Error("expected AutoSyncEnabled to be false")
		}
	})
}

func TestParseApplicationSetsFromYAML(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		wantCount int
		wantNames []string
	}{
		{
			name: "single ApplicationSet document",
			content: `apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
metadata:
  name: my-appset
  namespace: argocd
spec:
  generators:
    - list:
        elements:
          - env: dev
          - env: staging
  template:
    metadata:
      name: my-appset-{{env}}
    spec:
      project: default
      source:
        repoURL: https://github.com/org/repo
        path: apps/{{env}}
        targetRevision: main
      destination:
        server: https://kubernetes.default.svc
        namespace: '{{env}}'
`,
			wantCount: 1,
			wantNames: []string{"my-appset"},
		},
		{
			name: "multi-document with mixed types",
			content: `apiVersion: v1
kind: ConfigMap
metadata:
  name: some-config
data:
  key: value
---
apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
metadata:
  name: appset-one
spec:
  generators:
    - list:
        elements:
          - env: dev
  template:
    metadata:
      name: appset-one-{{env}}
    spec:
      project: default
      source:
        repoURL: https://github.com/org/repo
        path: apps
        targetRevision: main
      destination:
        server: https://kubernetes.default.svc
---
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: standalone-app
spec:
  source:
    repoURL: https://github.com/org/repo
    path: standalone
    targetRevision: main
  destination:
    server: https://kubernetes.default.svc
`,
			wantCount: 1,
			wantNames: []string{"appset-one"},
		},
		{
			name: "multiple ApplicationSets",
			content: `apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
metadata:
  name: appset-a
spec:
  generators:
    - list:
        elements:
          - env: dev
  template:
    metadata:
      name: appset-a-{{env}}
    spec:
      project: default
      source:
        repoURL: https://github.com/org/repo
        path: a
        targetRevision: main
      destination:
        server: https://kubernetes.default.svc
---
apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
metadata:
  name: appset-b
spec:
  generators:
    - list:
        elements:
          - env: prod
  template:
    metadata:
      name: appset-b-{{env}}
    spec:
      project: default
      source:
        repoURL: https://github.com/org/repo
        path: b
        targetRevision: main
      destination:
        server: https://kubernetes.default.svc
`,
			wantCount: 2,
			wantNames: []string{"appset-a", "appset-b"},
		},
		{
			name:      "empty YAML returns empty slice",
			content:   "",
			wantCount: 0,
		},
		{
			name: "non-ApplicationSet CRs are skipped",
			content: `apiVersion: argoproj.io/v1alpha1
kind: AppProject
metadata:
  name: my-project
spec:
  description: test
---
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: my-app
spec:
  source:
    repoURL: https://github.com/org/repo
    path: manifests
    targetRevision: main
  destination:
    server: https://kubernetes.default.svc
`,
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			appSets, err := ParseApplicationSetsFromYAML([]byte(tt.content), "test.yaml")
			if err != nil {
				t.Fatalf("ParseApplicationSetsFromYAML() error = %v", err)
			}
			if len(appSets) != tt.wantCount {
				t.Fatalf("got %d appsets, want %d", len(appSets), tt.wantCount)
			}
			for i, name := range tt.wantNames {
				if i >= len(appSets) {
					break
				}
				if appSets[i].Name != name {
					t.Errorf("appSets[%d].Name = %q, want %q", i, appSets[i].Name, name)
				}
			}
		})
	}
}

func TestParseRawApplicationSetFromYAML(t *testing.T) {
	t.Run("find by name", func(t *testing.T) {
		content := []byte(`apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
metadata:
  name: target-appset
  namespace: argocd
spec:
  generators:
    - list:
        elements:
          - env: dev
  template:
    metadata:
      name: target-appset-{{env}}
    spec:
      project: default
      source:
        repoURL: https://github.com/org/repo
        path: apps
        targetRevision: main
      destination:
        server: https://kubernetes.default.svc
`)
		appSet, err := ParseRawApplicationSetFromYAML(content, "target-appset")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if appSet.Name != "target-appset" {
			t.Errorf("expected name 'target-appset', got %q", appSet.Name)
		}
	})

	t.Run("not found returns error", func(t *testing.T) {
		content := []byte(`apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
metadata:
  name: other-appset
spec:
  generators:
    - list:
        elements:
          - env: dev
  template:
    metadata:
      name: other-appset-{{env}}
    spec:
      project: default
      source:
        repoURL: https://github.com/org/repo
        path: apps
        targetRevision: main
      destination:
        server: https://kubernetes.default.svc
`)
		_, err := ParseRawApplicationSetFromYAML(content, "nonexistent")
		if err == nil {
			t.Fatal("expected error for appset not found")
		}
	})

	t.Run("skips Application CRs", func(t *testing.T) {
		content := []byte(`apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: target-appset
spec:
  source:
    repoURL: https://github.com/org/repo
    path: manifests
    targetRevision: main
  destination:
    server: https://kubernetes.default.svc
`)
		_, err := ParseRawApplicationSetFromYAML(content, "target-appset")
		if err == nil {
			t.Fatal("expected error when only Application CRs exist (not ApplicationSet)")
		}
	})

	t.Run("empty YAML returns error", func(t *testing.T) {
		_, err := ParseRawApplicationSetFromYAML([]byte(""), "my-appset")
		if err == nil {
			t.Fatal("expected error for empty YAML")
		}
	})
}

func TestParsedApplications_String(t *testing.T) {
	tests := []struct {
		name string
		p    ParsedApplications
		want string
	}{
		{
			name: "all zeros",
			p:    ParsedApplications{},
			want: "new: 0, modified: 0, deleted: 0",
		},
		{
			name: "mixed counts",
			p: ParsedApplications{
				New:      make([]models.Application, 2),
				Modified: make([]models.Application, 3),
				Deleted:  make([]models.Application, 1),
			},
			want: "new: 2, modified: 3, deleted: 1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.p.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParsedApplications_IsEmpty(t *testing.T) {
	tests := []struct {
		name string
		p    ParsedApplications
		want bool
	}{
		{
			name: "empty",
			p:    ParsedApplications{},
			want: true,
		},
		{
			name: "has new",
			p:    ParsedApplications{New: make([]models.Application, 1)},
			want: false,
		},
		{
			name: "has modified",
			p:    ParsedApplications{Modified: make([]models.Application, 1)},
			want: false,
		},
		{
			name: "has deleted",
			p:    ParsedApplications{Deleted: make([]models.Application, 1)},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.p.IsEmpty(); got != tt.want {
				t.Errorf("IsEmpty() = %v, want %v", got, tt.want)
			}
		})
	}
}
