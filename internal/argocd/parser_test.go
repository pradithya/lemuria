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

		raw, err := ParseRawApplicationFromYAML(yaml, "my-app")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if raw == nil {
			t.Fatal("expected non-nil result")
		}

		metadata, ok := raw["metadata"].(map[string]any)
		if !ok {
			t.Fatal("expected metadata to be a map")
		}
		if metadata["name"] != "my-app" {
			t.Errorf("expected name 'my-app', got %v", metadata["name"])
		}

		spec, ok := raw["spec"].(map[string]any)
		if !ok {
			t.Fatal("expected spec to be a map")
		}
		if spec == nil {
			t.Fatal("expected spec to be non-nil")
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

		raw, err := ParseRawApplicationFromYAML(yaml, "app-two")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		metadata := raw["metadata"].(map[string]any)
		if metadata["name"] != "app-two" {
			t.Errorf("expected name 'app-two', got %v", metadata["name"])
		}

		// Verify the full structure is preserved (sources with helm values)
		spec := raw["spec"].(map[string]any)
		sources, ok := spec["sources"].([]interface{})
		if !ok {
			t.Fatal("expected sources to be a slice")
		}
		if len(sources) != 2 {
			t.Fatalf("expected 2 sources, got %d", len(sources))
		}

		helmSource := sources[1].(map[string]any)
		helm, ok := helmSource["helm"].(map[string]any)
		if !ok {
			t.Fatal("expected helm to be a map")
		}
		values, ok := helm["values"].(string)
		if !ok {
			t.Fatal("expected helm values to be a string")
		}
		if values != "cpu: 52m\n" {
			t.Errorf("expected helm values 'cpu: 52m\\n', got %q", values)
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
