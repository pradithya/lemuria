package argocd

import (
	"testing"
)

func TestBuildTempAppSpec_WithGitSourcedOverride(t *testing.T) {
	// Simulate a raw Application spec as read from a git YAML file
	// (no status, no managedFields - just what's in the YAML file)
	original := map[string]any{
		"apiVersion": "argoproj.io/v1alpha1",
		"kind":       "Application",
		"metadata": map[string]any{
			"name":      "my-app",
			"namespace": "argocd",
		},
		"spec": map[string]any{
			"sources": []any{
				map[string]any{
					"repoURL":        "https://github.com/org/repo",
					"path":           "manifests",
					"targetRevision": "main",
				},
				map[string]any{
					"repoURL":        "https://charts.example.com",
					"chart":          "nginx",
					"targetRevision": "1.0.0",
					"helm": map[string]any{
						"values": "cpu: 76m\nmemory: 128Mi\n",
					},
				},
			},
			"destination": map[string]any{
				"server":    "https://kubernetes.default.svc",
				"namespace": "default",
			},
			"syncPolicy": map[string]any{
				"automated": map[string]any{
					"prune":    true,
					"selfHeal": true,
				},
			},
		},
	}

	cfg := TempAppConfig{
		OriginalAppName: "my-app",
		TargetBranch:    "feature/update-values",
		PRNumber:        42,
		PRRepo:          "org/repo",
		Suffix:          "head",
	}

	tempApp := buildTempAppSpec(original, "my-app-lemuria-pr42-head", cfg)

	// Verify name is changed
	metadata := tempApp["metadata"].(map[string]any)
	if metadata["name"] != "my-app-lemuria-pr42-head" {
		t.Errorf("expected temp app name, got %v", metadata["name"])
	}

	// Verify labels are set
	labels := metadata["labels"].(map[string]any)
	if labels[labelTempApp] != "true" {
		t.Error("expected temp app label")
	}
	if labels[labelOriginalApp] != "my-app" {
		t.Errorf("expected original app label, got %v", labels[labelOriginalApp])
	}

	// Verify automated sync policy is removed
	spec := tempApp["spec"].(map[string]any)
	syncPolicy := spec["syncPolicy"].(map[string]any)
	if _, ok := syncPolicy["automated"]; ok {
		t.Error("expected automated sync policy to be removed")
	}

	// Verify status is removed
	if _, ok := tempApp["status"]; ok {
		t.Error("expected status to be removed")
	}

	// Verify helm values are preserved (the key use case for this feature)
	sources := spec["sources"].([]any)
	if len(sources) != 2 {
		t.Fatalf("expected 2 sources, got %d", len(sources))
	}

	helmSource := sources[1].(map[string]any)
	helm := helmSource["helm"].(map[string]any)
	values := helm["values"].(string)
	if values != "cpu: 76m\nmemory: 128Mi\n" {
		t.Errorf("expected helm values preserved, got %q", values)
	}

	// Verify the matching source's targetRevision is updated
	gitSource := sources[0].(map[string]any)
	if gitSource["targetRevision"] != "feature/update-values" {
		t.Errorf("expected git source targetRevision to be updated, got %v", gitSource["targetRevision"])
	}

	// Verify the external chart source's targetRevision is NOT updated
	if helmSource["targetRevision"] != "1.0.0" {
		t.Errorf("expected chart source targetRevision to be unchanged, got %v", helmSource["targetRevision"])
	}
}

func TestBuildTempAppSpec_WithLiveSpec(t *testing.T) {
	// Simulate a spec from live ArgoCD (has status, managedFields, etc.)
	original := map[string]any{
		"apiVersion": "argoproj.io/v1alpha1",
		"kind":       "Application",
		"metadata": map[string]any{
			"name":              "my-app",
			"namespace":         "argocd",
			"resourceVersion":   "12345",
			"uid":               "abc-123",
			"creationTimestamp":  "2024-01-01T00:00:00Z",
			"generation":        3,
			"managedFields":     []any{},
		},
		"spec": map[string]any{
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
			"health": map[string]any{
				"status": "Healthy",
			},
		},
	}

	cfg := TempAppConfig{
		OriginalAppName: "my-app",
		TargetBranch:    "feature/xyz",
		PRNumber:        10,
		PRRepo:          "org/repo",
		Suffix:          "base",
	}

	tempApp := buildTempAppSpec(original, "my-app-lemuria-pr10-base", cfg)

	metadata := tempApp["metadata"].(map[string]any)

	// Verify live metadata fields are stripped
	if _, ok := metadata["resourceVersion"]; ok {
		t.Error("expected resourceVersion to be removed")
	}
	if _, ok := metadata["uid"]; ok {
		t.Error("expected uid to be removed")
	}
	if _, ok := metadata["creationTimestamp"]; ok {
		t.Error("expected creationTimestamp to be removed")
	}
	if _, ok := metadata["generation"]; ok {
		t.Error("expected generation to be removed")
	}
	if _, ok := metadata["managedFields"]; ok {
		t.Error("expected managedFields to be removed")
	}

	// Verify status is removed
	if _, ok := tempApp["status"]; ok {
		t.Error("expected status to be removed")
	}
}

func TestGenerateTempAppName(t *testing.T) {
	tests := []struct {
		name     string
		baseName string
		prNum    int
		suffix   string
		want     string
	}{
		{
			name:     "normal name",
			baseName: "my-app",
			prNum:    42,
			suffix:   "base",
			want:     "my-app-lemuria-pr42-base",
		},
		{
			name:     "long name gets truncated",
			baseName: "very-long-application-name-that-exceeds-kubernetes-limits-really",
			prNum:    999,
			suffix:   "head",
			want:     "very-long-application-name-that-exceeds-kube-lemuria-pr999-head",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := generateTempAppName(tt.baseName, tt.prNum, tt.suffix)
			if got != tt.want {
				t.Errorf("generateTempAppName() = %q, want %q", got, tt.want)
			}
			if len(got) > 63 {
				t.Errorf("name too long: %d > 63", len(got))
			}
		})
	}
}

func TestSanitizeLabelValue(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"org/repo", "org-repo"},
		{"simple", "simple"},
		{"/leading-slash", "leading-slash"},
		{"trailing-slash/", "trailing-slash"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := sanitizeLabelValue(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeLabelValue(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
