package argocd

import (
	"testing"

	"github.com/org/lemuria/internal/models"
)

func TestResourceKey(t *testing.T) {
	tests := []struct {
		name      string
		group     string
		kind      string
		namespace string
		resName   string
		want      string
	}{
		{
			name:      "with namespace",
			group:     "apps",
			kind:      "Deployment",
			namespace: "default",
			resName:   "my-app",
			want:      "apps/Deployment/default/my-app",
		},
		{
			name:      "without namespace",
			group:     "",
			kind:      "Namespace",
			namespace: "",
			resName:   "my-ns",
			want:      "/Namespace/my-ns",
		},
		{
			name:      "core api group",
			group:     "",
			kind:      "ConfigMap",
			namespace: "kube-system",
			resName:   "my-cm",
			want:      "/ConfigMap/kube-system/my-cm",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resourceKey(tt.group, tt.kind, tt.namespace, tt.resName)
			if got != tt.want {
				t.Errorf("resourceKey() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseResourceKey(t *testing.T) {
	tests := []struct {
		key  string
		want models.ResourceKey
	}{
		{
			key: "apps/Deployment/default/my-app",
			want: models.ResourceKey{
				APIVersion: "apps",
				Kind:       "Deployment",
				Namespace:  "default",
				Name:       "my-app",
			},
		},
		{
			key: "/Namespace/my-ns",
			want: models.ResourceKey{
				APIVersion: "",
				Kind:       "Namespace",
				Namespace:  "",
				Name:       "my-ns",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			got := parseResourceKey(tt.key)
			if got != tt.want {
				t.Errorf("parseResourceKey(%q) = %v, want %v", tt.key, got, tt.want)
			}
		})
	}
}

func TestExtractGroup(t *testing.T) {
	tests := []struct {
		apiVersion string
		want       string
	}{
		{"apps/v1", "apps"},
		{"v1", ""},
		{"networking.k8s.io/v1", "networking.k8s.io"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.apiVersion, func(t *testing.T) {
			got := extractGroup(tt.apiVersion)
			if got != tt.want {
				t.Errorf("extractGroup(%q) = %v, want %v", tt.apiVersion, got, tt.want)
			}
		})
	}
}

func TestComputeDiff(t *testing.T) {
	tests := []struct {
		name        string
		currentJSON string
		targetJSON  string
		wantDiff    bool
	}{
		{
			name:        "different values produces diff",
			currentJSON: `{"spec": {"replicas": 2}}`,
			targetJSON:  `{"spec": {"replicas": 3}}`,
			wantDiff:    true,
		},
		{
			name:        "same values no diff",
			currentJSON: `{"spec": {"replicas": 2}}`,
			targetJSON:  `{"spec": {"replicas": 2}}`,
			wantDiff:    false,
		},
		{
			name:        "empty current produces diff",
			currentJSON: "",
			targetJSON:  `{"spec": {"replicas": 3}}`,
			wantDiff:    true,
		},
		{
			name:        "empty target produces diff",
			currentJSON: `{"spec": {"replicas": 2}}`,
			targetJSON:  "",
			wantDiff:    true,
		},
		{
			name:        "both empty no diff",
			currentJSON: "",
			targetJSON:  "",
			wantDiff:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diff := computeDiff(tt.currentJSON, tt.targetJSON)
			hasDiff := diff != ""
			if hasDiff != tt.wantDiff {
				t.Errorf("computeDiff() hasDiff = %v, want %v; diff = %q", hasDiff, tt.wantDiff, diff)
			}
		})
	}
}

func TestComputeManifestsDiff(t *testing.T) {
	current := []models.Manifest{
		{
			APIVersion: "v1",
			Kind:       "ConfigMap",
			Name:       "existing",
			Namespace:  "default",
			Raw:        `{"data": {"key": "old"}}`,
		},
		{
			APIVersion: "v1",
			Kind:       "ConfigMap",
			Name:       "to-delete",
			Namespace:  "default",
			Raw:        `{"data": {"key": "value"}}`,
		},
	}

	target := []models.Manifest{
		{
			APIVersion: "v1",
			Kind:       "ConfigMap",
			Name:       "existing",
			Namespace:  "default",
			Raw:        `{"data": {"key": "new"}}`,
		},
		{
			APIVersion: "v1",
			Kind:       "ConfigMap",
			Name:       "new-resource",
			Namespace:  "default",
			Raw:        `{"data": {"key": "value"}}`,
		},
	}

	diffs := computeManifestsDiff(current, target)

	// Should have 3 diffs: 1 update, 1 create, 1 delete
	if len(diffs) != 3 {
		t.Errorf("Expected 3 diffs, got %d", len(diffs))
	}

	actions := make(map[models.DiffAction]int)
	for _, d := range diffs {
		actions[d.Action]++
	}

	if actions[models.DiffActionUpdate] != 1 {
		t.Errorf("Expected 1 update, got %d", actions[models.DiffActionUpdate])
	}
	if actions[models.DiffActionCreate] != 1 {
		t.Errorf("Expected 1 create, got %d", actions[models.DiffActionCreate])
	}
	if actions[models.DiffActionDelete] != 1 {
		t.Errorf("Expected 1 delete, got %d", actions[models.DiffActionDelete])
	}
}

func TestBuildManifestMap(t *testing.T) {
	manifests := []models.Manifest{
		{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
			Name:       "my-app",
			Namespace:  "default",
			Raw:        `{"kind": "Deployment"}`,
		},
		{
			APIVersion: "v1",
			Kind:       "ConfigMap",
			Name:       "my-config",
			Namespace:  "default",
			Raw:        `{"kind": "ConfigMap"}`,
		},
	}

	m := buildManifestMap(manifests)

	if len(m) != 2 {
		t.Errorf("Expected 2 entries, got %d", len(m))
	}

	if _, ok := m["apps/Deployment/default/my-app"]; !ok {
		t.Error("Expected to find Deployment in map")
	}

	if _, ok := m["/ConfigMap/default/my-config"]; !ok {
		t.Error("Expected to find ConfigMap in map")
	}
}

func TestFormatCreateDiff(t *testing.T) {
	targetJSON := `{"apiVersion": "v1", "kind": "ConfigMap", "metadata": {"name": "test"}}`
	diff := formatCreateDiff(targetJSON)

	if diff == "" {
		t.Error("Expected non-empty diff for create")
	}

	if !contains(diff, "+") {
		t.Error("Expected diff to contain additions")
	}
}

func TestFormatDeleteDiff(t *testing.T) {
	currentJSON := `{"apiVersion": "v1", "kind": "ConfigMap", "metadata": {"name": "test"}}`
	diff := formatDeleteDiff(currentJSON)

	if diff == "" {
		t.Error("Expected non-empty diff for delete")
	}

	if !contains(diff, "-") {
		t.Error("Expected diff to contain deletions")
	}
}

func TestJSONToYAML(t *testing.T) {
	tests := []struct {
		name string
		json string
		want string
	}{
		{
			name: "empty string",
			json: "",
			want: "",
		},
		{
			name: "null",
			json: "null",
			want: "",
		},
		{
			name: "invalid JSON returns as-is",
			json: "not valid json",
			want: "not valid json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := jsonToYAML(tt.json)
			if got != tt.want {
				t.Errorf("jsonToYAML(%q) = %q, want %q", tt.json, got, tt.want)
			}
		})
	}

	t.Run("valid JSON converts to YAML", func(t *testing.T) {
		json := `{"apiVersion": "v1", "kind": "ConfigMap"}`
		result := jsonToYAML(json)
		if result == "" {
			t.Error("Expected non-empty result for valid JSON")
		}
		if contains(result, "{") || contains(result, "}") {
			t.Error("Expected YAML format, not JSON")
		}
	})
}

func TestCleanForDiff(t *testing.T) {
	data := map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]any{
			"name":              "test",
			"managedFields":     []any{},
			"resourceVersion":   "12345",
			"uid":               "abc-123",
			"creationTimestamp": "2024-01-01T00:00:00Z",
			"generation":        1,
			"annotations": map[string]any{
				"kubectl.kubernetes.io/last-applied-configuration": "{}",
			},
		},
		"status": map[string]any{
			"ready": true,
		},
		"data": map[string]any{
			"key": "value",
		},
	}

	cleanForDiff(data)

	metadata := data["metadata"].(map[string]any)

	if _, ok := metadata["managedFields"]; ok {
		t.Error("managedFields should be removed")
	}
	if _, ok := metadata["resourceVersion"]; ok {
		t.Error("resourceVersion should be removed")
	}
	if _, ok := metadata["uid"]; ok {
		t.Error("uid should be removed")
	}
	if _, ok := metadata["creationTimestamp"]; ok {
		t.Error("creationTimestamp should be removed")
	}
	if _, ok := metadata["generation"]; ok {
		t.Error("generation should be removed")
	}
	if _, ok := metadata["annotations"]; ok {
		t.Error("empty annotations should be removed")
	}
	if _, ok := data["status"]; ok {
		t.Error("status should be removed")
	}
	if metadata["name"] != "test" {
		t.Error("name should be preserved")
	}
	if data["data"] == nil {
		t.Error("data should be preserved")
	}
}

func TestCleanForDiff_LabelsInSelectors(t *testing.T) {
	t.Run("removes known labels from metadata, selector, and pod template", func(t *testing.T) {
		data := map[string]any{
			"apiVersion": "apps/v1",
			"kind":       "Deployment",
			"metadata": map[string]any{
				"name": "my-app",
				"labels": map[string]any{
					"app":                         "my-app",
					"app.kubernetes.io/instance":  "argocd-app-name",
					"argocd.argoproj.io/instance": "argocd-app-name",
				},
			},
			"spec": map[string]any{
				"selector": map[string]any{
					"matchLabels": map[string]any{
						"app":                         "my-app",
						"app.kubernetes.io/instance":  "argocd-app-name",
						"argocd.argoproj.io/instance": "argocd-app-name",
					},
				},
				"template": map[string]any{
					"metadata": map[string]any{
						"labels": map[string]any{
							"app":                         "my-app",
							"app.kubernetes.io/instance":  "argocd-app-name",
							"argocd.argoproj.io/instance": "argocd-app-name",
						},
					},
				},
			},
		}

		cleanForDiff(data)

		// Check metadata.labels
		metaLabels := data["metadata"].(map[string]any)["labels"].(map[string]any)
		if _, ok := metaLabels["app.kubernetes.io/instance"]; ok {
			t.Error("app.kubernetes.io/instance should be removed from metadata.labels")
		}
		if _, ok := metaLabels["argocd.argoproj.io/instance"]; ok {
			t.Error("argocd.argoproj.io/instance should be removed from metadata.labels")
		}
		if metaLabels["app"] != "my-app" {
			t.Error("non-argocd labels should be preserved in metadata.labels")
		}

		// Check spec.selector.matchLabels
		spec := data["spec"].(map[string]any)
		selector := spec["selector"].(map[string]any)
		matchLabels := selector["matchLabels"].(map[string]any)
		if _, ok := matchLabels["app.kubernetes.io/instance"]; ok {
			t.Error("app.kubernetes.io/instance should be removed from spec.selector.matchLabels")
		}
		if _, ok := matchLabels["argocd.argoproj.io/instance"]; ok {
			t.Error("argocd.argoproj.io/instance should be removed from spec.selector.matchLabels")
		}
		if matchLabels["app"] != "my-app" {
			t.Error("non-argocd labels should be preserved in spec.selector.matchLabels")
		}

		// Check spec.template.metadata.labels
		tmpl := spec["template"].(map[string]any)
		tmplLabels := tmpl["metadata"].(map[string]any)["labels"].(map[string]any)
		if _, ok := tmplLabels["app.kubernetes.io/instance"]; ok {
			t.Error("app.kubernetes.io/instance should be removed from spec.template.metadata.labels")
		}
		if _, ok := tmplLabels["argocd.argoproj.io/instance"]; ok {
			t.Error("argocd.argoproj.io/instance should be removed from spec.template.metadata.labels")
		}
		if tmplLabels["app"] != "my-app" {
			t.Error("non-argocd labels should be preserved in spec.template.metadata.labels")
		}
	})

	t.Run("removes empty matchLabels and labels after cleaning", func(t *testing.T) {
		data := map[string]any{
			"apiVersion": "apps/v1",
			"kind":       "Deployment",
			"metadata": map[string]any{
				"name": "my-app",
				"labels": map[string]any{
					"app.kubernetes.io/instance": "argocd-app-name",
				},
			},
			"spec": map[string]any{
				"selector": map[string]any{
					"matchLabels": map[string]any{
						"app.kubernetes.io/instance": "argocd-app-name",
					},
				},
				"template": map[string]any{
					"metadata": map[string]any{
						"labels": map[string]any{
							"app.kubernetes.io/instance": "argocd-app-name",
						},
					},
				},
			},
		}

		cleanForDiff(data)

		metadata := data["metadata"].(map[string]any)
		if _, ok := metadata["labels"]; ok {
			t.Error("empty metadata.labels should be removed")
		}

		spec := data["spec"].(map[string]any)
		selector := spec["selector"].(map[string]any)
		if _, ok := selector["matchLabels"]; ok {
			t.Error("empty spec.selector.matchLabels should be removed")
		}

		tmplMeta := spec["template"].(map[string]any)["metadata"].(map[string]any)
		if _, ok := tmplMeta["labels"]; ok {
			t.Error("empty spec.template.metadata.labels should be removed")
		}
	})

	t.Run("handles resources without spec selectors", func(t *testing.T) {
		data := map[string]any{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]any{
				"name": "test",
				"labels": map[string]any{
					"app.kubernetes.io/instance": "argocd-app-name",
				},
			},
			"data": map[string]any{
				"key": "value",
			},
		}

		cleanForDiff(data)

		metadata := data["metadata"].(map[string]any)
		if _, ok := metadata["labels"]; ok {
			t.Error("empty metadata.labels should be removed")
		}
		if data["data"] == nil {
			t.Error("data should be preserved")
		}
	})
}

func TestSummarizeDiffs(t *testing.T) {
	diffs := []models.ManifestDiff{
		{Action: models.DiffActionCreate},
		{Action: models.DiffActionCreate},
		{Action: models.DiffActionUpdate},
		{Action: models.DiffActionDelete},
		{Action: models.DiffActionNone},
	}

	summary := SummarizeDiffs(diffs)

	if summary.TotalResources != 5 {
		t.Errorf("TotalResources = %d, want 5", summary.TotalResources)
	}
	if summary.Created != 2 {
		t.Errorf("Created = %d, want 2", summary.Created)
	}
	if summary.Updated != 1 {
		t.Errorf("Updated = %d, want 1", summary.Updated)
	}
	if summary.Deleted != 1 {
		t.Errorf("Deleted = %d, want 1", summary.Deleted)
	}
	if summary.Unchanged != 1 {
		t.Errorf("Unchanged = %d, want 1", summary.Unchanged)
	}
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
