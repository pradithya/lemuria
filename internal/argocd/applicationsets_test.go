package argocd

import (
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
