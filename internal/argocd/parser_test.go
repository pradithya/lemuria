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
