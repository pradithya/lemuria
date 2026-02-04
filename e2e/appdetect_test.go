package e2e

import (
	"strings"
	"testing"

	"github.com/org/lemuria/internal/argocd"
	"github.com/org/lemuria/internal/models"
	"github.com/org/lemuria/pkg/diff"
)

func TestParseApplicationsFromYAML(t *testing.T) {
	tests := []struct {
		name      string
		yaml      string
		wantCount int
		wantNames []string
		wantRepo  string
	}{
		{
			name: "single application",
			yaml: `
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: my-app
  namespace: argocd
spec:
  project: default
  source:
    repoURL: https://github.com/org/repo
    path: apps/my-app
    targetRevision: HEAD
  destination:
    server: https://kubernetes.default.svc
    namespace: default
`,
			wantCount: 1,
			wantNames: []string{"my-app"},
			wantRepo:  "https://github.com/org/repo",
		},
		{
			name: "multi-document yaml with multiple apps",
			yaml: `
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: app-one
spec:
  project: default
  source:
    repoURL: https://github.com/org/repo
    path: apps/one
    targetRevision: main
  destination:
    server: https://kubernetes.default.svc
    namespace: app-one
---
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: app-two
spec:
  project: production
  source:
    repoURL: https://github.com/org/repo
    path: apps/two
    targetRevision: main
  destination:
    server: https://kubernetes.default.svc
    namespace: app-two
`,
			wantCount: 2,
			wantNames: []string{"app-one", "app-two"},
			wantRepo:  "https://github.com/org/repo",
		},
		{
			name: "non-application resources are skipped",
			yaml: `
apiVersion: v1
kind: ConfigMap
metadata:
  name: my-config
data:
  key: value
---
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: my-app
spec:
  project: default
  source:
    repoURL: https://github.com/org/repo
    path: apps/my-app
    targetRevision: HEAD
  destination:
    server: https://kubernetes.default.svc
    namespace: default
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-deploy
spec:
  replicas: 1
`,
			wantCount: 1,
			wantNames: []string{"my-app"},
			wantRepo:  "https://github.com/org/repo",
		},
		{
			name: "application with auto-sync",
			yaml: `
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: auto-sync-app
spec:
  project: default
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
			name:      "empty yaml",
			yaml:      "",
			wantCount: 0,
			wantNames: nil,
		},
		{
			name: "multi-source application",
			yaml: `
apiVersion: argoproj.io/v1alpha1
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
			apps, err := argocd.ParseApplicationsFromYAML([]byte(tt.yaml), "test.yaml")
			if err != nil {
				t.Fatalf("ParseApplicationsFromYAML() error = %v", err)
			}

			if len(apps) != tt.wantCount {
				t.Errorf("got %d apps, want %d", len(apps), tt.wantCount)
			}

			for i, wantName := range tt.wantNames {
				if i >= len(apps) {
					t.Errorf("missing app at index %d", i)
					continue
				}
				if apps[i].Name != wantName {
					t.Errorf("app[%d].Name = %q, want %q", i, apps[i].Name, wantName)
				}
			}

			if tt.wantRepo != "" && len(apps) > 0 {
				if apps[0].RepoURL != tt.wantRepo {
					t.Errorf("app[0].RepoURL = %q, want %q", apps[0].RepoURL, tt.wantRepo)
				}
			}

			// Verify source file is set
			for i, app := range apps {
				if app.SourceFile != "test.yaml" {
					t.Errorf("app[%d].SourceFile = %q, want 'test.yaml'", i, app.SourceFile)
				}
			}
		})
	}
}

func TestParseApplicationAutoSyncDetection(t *testing.T) {
	yamlWithAutoSync := `
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: auto-sync-app
spec:
  project: default
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

	yamlWithoutAutoSync := `
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: manual-app
spec:
  project: default
  source:
    repoURL: https://github.com/org/repo
    path: apps/manual
    targetRevision: HEAD
  destination:
    server: https://kubernetes.default.svc
    namespace: default
`

	t.Run("detects auto-sync enabled", func(t *testing.T) {
		apps, err := argocd.ParseApplicationsFromYAML([]byte(yamlWithAutoSync), "test.yaml")
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
		apps, err := argocd.ParseApplicationsFromYAML([]byte(yamlWithoutAutoSync), "test.yaml")
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

func TestParsedApplications(t *testing.T) {
	t.Run("IsEmpty", func(t *testing.T) {
		empty := &argocd.ParsedApplications{}
		if !empty.IsEmpty() {
			t.Error("expected empty ParsedApplications to be empty")
		}

		withNew := &argocd.ParsedApplications{
			New: []models.Application{{Name: "test"}},
		}
		if withNew.IsEmpty() {
			t.Error("expected non-empty ParsedApplications to not be empty")
		}
	})

	t.Run("String", func(t *testing.T) {
		parsed := &argocd.ParsedApplications{
			New:      []models.Application{{Name: "new-app"}},
			Modified: []models.Application{{Name: "mod-app"}, {Name: "mod-app-2"}},
			Deleted:  []models.Application{{Name: "del-app"}},
		}
		str := parsed.String()
		if !strings.Contains(str, "new: 1") {
			t.Errorf("expected string to contain 'new: 1', got: %s", str)
		}
		if !strings.Contains(str, "modified: 2") {
			t.Errorf("expected string to contain 'modified: 2', got: %s", str)
		}
		if !strings.Contains(str, "deleted: 1") {
			t.Errorf("expected string to contain 'deleted: 1', got: %s", str)
		}
	})
}

func TestApplicationChangeType(t *testing.T) {
	t.Run("IsNew", func(t *testing.T) {
		app := models.Application{ChangeType: models.ApplicationNew}
		if !app.IsNew() {
			t.Error("expected IsNew() to return true")
		}
		if app.IsDeleted() {
			t.Error("expected IsDeleted() to return false")
		}
		if app.IsExisting() {
			t.Error("expected IsExisting() to return false")
		}
	})

	t.Run("IsDeleted", func(t *testing.T) {
		app := models.Application{ChangeType: models.ApplicationDeleted}
		if !app.IsDeleted() {
			t.Error("expected IsDeleted() to return true")
		}
		if app.IsNew() {
			t.Error("expected IsNew() to return false")
		}
		if app.IsExisting() {
			t.Error("expected IsExisting() to return false")
		}
	})

	t.Run("IsExisting", func(t *testing.T) {
		app := models.Application{ChangeType: models.ApplicationExisting}
		if !app.IsExisting() {
			t.Error("expected IsExisting() to return true")
		}
		if app.IsNew() {
			t.Error("expected IsNew() to return false")
		}
		if app.IsDeleted() {
			t.Error("expected IsDeleted() to return false")
		}
	})

	t.Run("default is existing", func(t *testing.T) {
		app := models.Application{}
		if !app.IsExisting() {
			t.Error("expected default to be IsExisting()")
		}
	})
}

func TestRenderNewAndDeletedApps(t *testing.T) {
	renderer := diff.NewRenderer()

	t.Run("renders new application", func(t *testing.T) {
		results := []diff.PlanResult{
			{
				Application: "new-app",
				ChangeType:  models.ApplicationNew,
				SourceFile:  "apps/new-app.yaml",
			},
		}

		output := renderer.RenderPlan(results, 42)

		expectations := []string{
			"## Lemuria Plan",
			"### Application: `new-app`",
			"🆕",
			"New application",
			"will be created",
			"apps/new-app.yaml",
		}

		for _, exp := range expectations {
			if !strings.Contains(output, exp) {
				t.Errorf("expected output to contain %q\nOutput: %s", exp, output)
			}
		}
	})

	t.Run("renders deleted application", func(t *testing.T) {
		results := []diff.PlanResult{
			{
				Application: "deleted-app",
				ChangeType:  models.ApplicationDeleted,
				SourceFile:  "apps/deleted-app.yaml",
			},
		}

		output := renderer.RenderPlan(results, 42)

		expectations := []string{
			"## Lemuria Plan",
			"### Application: `deleted-app`",
			"🗑️",
			"will be deleted",
			"apps/deleted-app.yaml",
		}

		for _, exp := range expectations {
			if !strings.Contains(output, exp) {
				t.Errorf("expected output to contain %q\nOutput: %s", exp, output)
			}
		}
	})

	t.Run("renders mixed new, existing, and deleted", func(t *testing.T) {
		results := []diff.PlanResult{
			{
				Application: "existing-app",
				ChangeType:  models.ApplicationExisting,
				LockStatus:  "Locked by this PR",
				Created:     1,
				Updated:     2,
			},
			{
				Application: "new-app",
				ChangeType:  models.ApplicationNew,
				SourceFile:  "apps/new.yaml",
			},
			{
				Application: "deleted-app",
				ChangeType:  models.ApplicationDeleted,
				SourceFile:  "apps/deleted.yaml",
			},
		}

		output := renderer.RenderPlan(results, 42)

		// Check existing app is rendered normally
		if !strings.Contains(output, "1 to create") {
			t.Error("expected existing app changes to be rendered")
		}
		if !strings.Contains(output, "Locked by this PR") {
			t.Error("expected lock status for existing app")
		}

		// Check new app
		if !strings.Contains(output, "new-app") || !strings.Contains(output, "🆕") {
			t.Error("expected new app to be marked with 🆕")
		}

		// Check deleted app
		if !strings.Contains(output, "deleted-app") || !strings.Contains(output, "🗑️") {
			t.Error("expected deleted app to be marked with 🗑️")
		}

		t.Logf("Output:\n%s", output)
	})
}

func TestNormalizeRepoURL(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"https://github.com/org/repo", "github.com/org/repo"},
		{"https://github.com/org/repo.git", "github.com/org/repo"},
		{"git@github.com:org/repo.git", "github.com/org/repo"},
		{"http://github.com/org/repo", "github.com/org/repo"},
		{"HTTPS://GITHUB.COM/ORG/REPO", "github.com/org/repo"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := argocd.NormalizeRepoURL(tt.input)
			if got != tt.want {
				t.Errorf("NormalizeRepoURL(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
