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

	v1alpha1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestBuildTempAppSpec_WithGitSourcedOverride(t *testing.T) {
	// Simulate a raw Application spec as read from a git YAML file
	original := &v1alpha1.Application{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "argoproj.io/v1alpha1",
			Kind:       "Application",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-app",
			Namespace: "argocd",
		},
		Spec: v1alpha1.ApplicationSpec{
			Sources: v1alpha1.ApplicationSources{
				{
					RepoURL:        "https://github.com/org/repo",
					Path:           "manifests",
					TargetRevision: "main",
				},
				{
					RepoURL:        "https://charts.example.com",
					Chart:          "nginx",
					TargetRevision: "1.0.0",
					Helm: &v1alpha1.ApplicationSourceHelm{
						Values: "cpu: 76m\nmemory: 128Mi\n",
					},
				},
			},
			Destination: v1alpha1.ApplicationDestination{
				Server:    "https://kubernetes.default.svc",
				Namespace: "default",
			},
			SyncPolicy: &v1alpha1.SyncPolicy{
				Automated: &v1alpha1.SyncPolicyAutomated{
					Prune:    true,
					SelfHeal: true,
				},
			},
		},
	}

	cfg := TempAppConfig{
		OriginalAppName: "my-app",
		TargetBranch:    "feature/update-values",
		PRNumber:        42,
		PRRepo:          "https://github.com/org/repo",
		Suffix:          "head",
	}

	tempApp := buildTempAppSpec(original, "my-app-lemuria-pr42-head", cfg)

	// Verify name is changed
	if tempApp.Name != "my-app-lemuria-pr42-head" {
		t.Errorf("expected temp app name, got %v", tempApp.Name)
	}

	// Verify labels are set
	if tempApp.Labels[labelTempApp] != "true" {
		t.Error("expected temp app label")
	}
	if tempApp.Labels[labelOriginalApp] != "my-app" {
		t.Errorf("expected original app label, got %v", tempApp.Labels[labelOriginalApp])
	}

	// Verify automated sync policy is removed
	if tempApp.Spec.SyncPolicy == nil {
		t.Fatal("expected syncPolicy to be non-nil")
	}
	if tempApp.Spec.SyncPolicy.Automated != nil {
		t.Error("expected automated sync policy to be removed")
	}

	// Verify status is cleared
	if len(tempApp.Status.Resources) > 0 || tempApp.Status.Sync.Status != "" {
		t.Error("expected status to be cleared")
	}

	// Verify helm values are preserved (the key use case for this feature)
	if len(tempApp.Spec.Sources) != 2 {
		t.Fatalf("expected 2 sources, got %d", len(tempApp.Spec.Sources))
	}

	helmSource := tempApp.Spec.Sources[1]
	if helmSource.Helm == nil {
		t.Fatal("expected helm to be non-nil")
	}
	if helmSource.Helm.Values != "cpu: 76m\nmemory: 128Mi\n" {
		t.Errorf("expected helm values preserved, got %q", helmSource.Helm.Values)
	}

	// Verify the matching source's targetRevision is updated
	if tempApp.Spec.Sources[0].TargetRevision != "feature/update-values" {
		t.Errorf("expected git source targetRevision to be updated, got %v", tempApp.Spec.Sources[0].TargetRevision)
	}

	// Verify the external chart source's targetRevision is NOT updated
	if helmSource.TargetRevision != "1.0.0" {
		t.Errorf("expected chart source targetRevision to be unchanged, got %v", helmSource.TargetRevision)
	}
}

func TestBuildTempAppSpec_WithLiveSpec(t *testing.T) {
	// Simulate a spec from live ArgoCD (has status, managedFields, etc.)
	original := &v1alpha1.Application{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "argoproj.io/v1alpha1",
			Kind:       "Application",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:            "my-app",
			Namespace:       "argocd",
			ResourceVersion: "12345",
			UID:             "abc-123",
			Generation:      3,
			ManagedFields:   []metav1.ManagedFieldsEntry{},
		},
		Spec: v1alpha1.ApplicationSpec{
			Source: &v1alpha1.ApplicationSource{
				RepoURL:        "https://github.com/org/repo",
				Path:           "manifests",
				TargetRevision: "main",
			},
			Destination: v1alpha1.ApplicationDestination{
				Server:    "https://kubernetes.default.svc",
				Namespace: "default",
			},
		},
		Status: v1alpha1.ApplicationStatus{
			Health: v1alpha1.AppHealthStatus{
				Status: "Healthy",
			},
		},
	}

	cfg := TempAppConfig{
		OriginalAppName: "my-app",
		TargetBranch:    "feature/xyz",
		PRNumber:        10,
		PRRepo:          "https://github.com/org/repo",
		Suffix:          "base",
	}

	tempApp := buildTempAppSpec(original, "my-app-lemuria-pr10-base", cfg)

	// Verify live metadata fields are stripped
	if tempApp.ResourceVersion != "" {
		t.Error("expected resourceVersion to be removed")
	}
	if tempApp.UID != "" {
		t.Error("expected uid to be removed")
	}
	if !tempApp.CreationTimestamp.IsZero() {
		t.Error("expected creationTimestamp to be removed")
	}
	if tempApp.Generation != 0 {
		t.Error("expected generation to be removed")
	}
	if tempApp.ManagedFields != nil {
		t.Error("expected managedFields to be removed")
	}

	// Verify status is cleared
	if tempApp.Status.Health.Status != "" {
		t.Error("expected status to be cleared")
	}
}

func TestBuildTempAppSpec_HelmReleaseName(t *testing.T) {
	tests := []struct {
		name            string
		original        *v1alpha1.Application
		cfg             TempAppConfig
		wantReleaseName string // expected ReleaseName on the chart source
		sourceIndex     int    // which source to check (-1 for singular Source)
	}{
		{
			name: "helm chart source gets ReleaseName set to original app name",
			original: &v1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{Name: "cert-manager"},
				Spec: v1alpha1.ApplicationSpec{
					Source: &v1alpha1.ApplicationSource{
						RepoURL:        "https://charts.jetstack.io",
						Chart:          "cert-manager",
						TargetRevision: "v1.14.0",
					},
				},
			},
			cfg: TempAppConfig{
				OriginalAppName: "cert-manager",
				TargetBranch:    "feature/update",
				PRNumber:        14,
				PRRepo:          "https://github.com/org/repo",
				Suffix:          "base",
			},
			wantReleaseName: "cert-manager",
			sourceIndex:     -1,
		},
		{
			name: "existing ReleaseName is preserved",
			original: &v1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{Name: "my-app"},
				Spec: v1alpha1.ApplicationSpec{
					Source: &v1alpha1.ApplicationSource{
						RepoURL:        "https://charts.example.com",
						Chart:          "nginx",
						TargetRevision: "1.0.0",
						Helm: &v1alpha1.ApplicationSourceHelm{
							ReleaseName: "custom-release",
						},
					},
				},
			},
			cfg: TempAppConfig{
				OriginalAppName: "my-app",
				TargetBranch:    "feature/update",
				PRNumber:        5,
				PRRepo:          "https://github.com/org/repo",
				Suffix:          "head",
			},
			wantReleaseName: "custom-release",
			sourceIndex:     -1,
		},
		{
			name: "multi-source: helm chart source gets ReleaseName, git source unaffected",
			original: &v1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{Name: "grafana"},
				Spec: v1alpha1.ApplicationSpec{
					Sources: v1alpha1.ApplicationSources{
						{
							RepoURL:        "https://github.com/org/repo",
							Path:           "apps/grafana",
							TargetRevision: "main",
						},
						{
							RepoURL:        "https://grafana.github.io/helm-charts",
							Chart:          "grafana",
							TargetRevision: "7.0.0",
						},
					},
				},
			},
			cfg: TempAppConfig{
				OriginalAppName: "grafana",
				TargetBranch:    "feature/update-values",
				PRNumber:        14,
				PRRepo:          "https://github.com/org/repo",
				Suffix:          "head",
			},
			wantReleaseName: "grafana",
			sourceIndex:     1,
		},
		{
			name: "git-only source does not get ReleaseName",
			original: &v1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{Name: "my-app"},
				Spec: v1alpha1.ApplicationSpec{
					Source: &v1alpha1.ApplicationSource{
						RepoURL:        "https://github.com/org/repo",
						Path:           "manifests",
						TargetRevision: "main",
					},
				},
			},
			cfg: TempAppConfig{
				OriginalAppName: "my-app",
				TargetBranch:    "feature/update",
				PRNumber:        7,
				PRRepo:          "https://github.com/other-org/other-repo",
				Suffix:          "base",
			},
			wantReleaseName: "",
			sourceIndex:     -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempName := generateTempAppName(tt.cfg.OriginalAppName, tt.cfg.PRNumber, tt.cfg.Suffix)
			result := buildTempAppSpec(tt.original, tempName, tt.cfg)

			var gotReleaseName string
			if tt.sourceIndex == -1 {
				// Check singular Source
				if result.Spec.Source != nil && result.Spec.Source.Helm != nil {
					gotReleaseName = result.Spec.Source.Helm.ReleaseName
				}
			} else {
				src := result.Spec.Sources[tt.sourceIndex]
				if src.Helm != nil {
					gotReleaseName = src.Helm.ReleaseName
				}
			}

			if gotReleaseName != tt.wantReleaseName {
				t.Errorf("ReleaseName = %q, want %q", gotReleaseName, tt.wantReleaseName)
			}

			// For multi-source, verify git sources don't get Helm set
			if tt.sourceIndex >= 0 {
				for i, src := range result.Spec.Sources {
					if i == tt.sourceIndex {
						continue
					}
					if src.Chart == "" && src.Helm != nil && src.Helm.ReleaseName != "" {
						t.Errorf("git source[%d] should not have ReleaseName set, got %q", i, src.Helm.ReleaseName)
					}
				}
			}
		})
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
		name  string
		input string
		want  string
	}{
		{"slash replaced", "org/repo", "org-repo"},
		{"simple", "simple", "simple"},
		{"leading slash trimmed", "/leading-slash", "leading-slash"},
		{"trailing slash trimmed", "trailing-slash/", "trailing-slash"},
		{
			"long value truncated to 63 chars",
			"https://github.com/very-long-organization-name/very-long-repository-name-exceeding-limit",
			"https---github.com-very-long-organization-name-very-long-reposi",
		},
		{"colons replaced", "host:8080/path", "host-8080-path"},
		{
			"trailing special chars after truncation trimmed",
			// 66 chars; positions 0-59 are alphanum, 60-62 are "---", 63-65 are "xyz"
			// s[:63] = 60 alphanum chars + "---" → TrimRight removes "---"
			"abcdefghijklmnopqrstuvwxyz1234abcdefghijklmnopqrstuvwxyz1234---xyz",
			"abcdefghijklmnopqrstuvwxyz1234abcdefghijklmnopqrstuvwxyz1234",
		},
		{"https URL sanitized", "https://github.com/org/repo", "https---github.com-org-repo"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeLabelValue(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeLabelValue(%q) = %q, want %q", tt.input, got, tt.want)
			}
			if len(got) > 63 {
				t.Errorf("result length %d > 63", len(got))
			}
		})
	}
}
