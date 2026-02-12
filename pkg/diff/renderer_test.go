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

package diff

import (
	"strings"
	"testing"

	"github.com/org/lemuria/internal/models"
)

func TestRenderSync(t *testing.T) {
	renderer := NewRenderer()

	tests := []struct {
		name     string
		results  []SyncResultEntry
		contains []string
		excludes []string
	}{
		{
			name: "successful sync with plan output and resources",
			results: []SyncResultEntry{{
				Application:  "my-app",
				Phase:        "Succeeded",
				HealthStatus: "Healthy",
				PlanOutput:   "2 to create, 1 to update",
				Resources: []models.ResourceResult{
					{
						Resource:     models.ResourceKey{Kind: "Deployment", Name: "web", Namespace: "default"},
						Status:       "Synced",
						Message:      "deployment.apps/web configured",
						HealthStatus: "Healthy",
					},
					{
						Resource:     models.ResourceKey{Kind: "Service", Name: "web-svc", Namespace: "default"},
						Status:       "Synced",
						Message:      "service/web-svc created",
						HealthStatus: "Healthy",
					},
				},
			}},
			contains: []string{
				"Sync successful",
				"💚 Healthy",
				"2 to create, 1 to update",
				"Resource Results (2 resources)",
				"Deployment/web",
				"Service/web-svc",
				"Synced",
				"| Health |",
				"All applications synced successfully",
			},
		},
		{
			name: "successful sync without plan output",
			results: []SyncResultEntry{{
				Application: "my-app",
				Phase:       "Succeeded",
			}},
			contains: []string{
				"Sync successful",
				"All applications synced successfully",
			},
			excludes: []string{
				"Planned changes",
				"Resource Results",
			},
		},
		{
			name: "running phase with plan output but no resources",
			results: []SyncResultEntry{{
				Application: "my-app",
				Phase:       "Running",
				PlanOutput:  "1 to update",
			}},
			contains: []string{
				"Sync in progress",
				"1 to update",
			},
			excludes: []string{
				"Resource Results",
				"All applications synced successfully",
			},
		},
		{
			name: "failed sync with message",
			results: []SyncResultEntry{{
				Application: "my-app",
				Phase:       "Failed",
				Message:     "ComparisonError: failed to sync",
			}},
			contains: []string{
				"Sync failed",
				"ComparisonError: failed to sync",
			},
			excludes: []string{
				"All applications synced successfully",
			},
		},
		{
			name: "sync error",
			results: []SyncResultEntry{{
				Application: "my-app",
				Error:       errTest,
			}},
			contains: []string{
				"Error",
				"test error",
			},
			excludes: []string{
				"Planned changes",
				"Resource Results",
				"All applications synced successfully",
			},
		},
		{
			name: "multiple apps mixed results",
			results: []SyncResultEntry{
				{
					Application: "app-1",
					Phase:       "Succeeded",
					PlanOutput:  "1 to update",
					Resources: []models.ResourceResult{
						{Resource: models.ResourceKey{Kind: "Deployment", Name: "web"}, Status: "Synced", Message: "configured"},
					},
				},
				{
					Application: "app-2",
					Phase:       "Failed",
					Message:     "sync failed",
				},
			},
			contains: []string{
				"app-1",
				"app-2",
				"Sync successful",
				"Sync failed",
				"1 to update",
			},
			excludes: []string{
				"All applications synced successfully",
			},
		},
		{
			name: "resource with SyncFailed status",
			results: []SyncResultEntry{{
				Application: "my-app",
				Phase:       "Failed",
				Message:     "one or more resources failed",
				PlanOutput:  "1 to create",
				Resources: []models.ResourceResult{
					{Resource: models.ResourceKey{Kind: "Deployment", Name: "bad"}, Status: "SyncFailed", Message: "error applying"},
				},
			}},
			contains: []string{
				"❌ SyncFailed",
				"Resource Results (1 resources)",
			},
		},
		{
			name: "resource with Pruned status",
			results: []SyncResultEntry{{
				Application: "my-app",
				Phase:       "Succeeded",
				Resources: []models.ResourceResult{
					{Resource: models.ResourceKey{Kind: "ConfigMap", Name: "old"}, Status: "Pruned", Message: "pruned"},
				},
			}},
			contains: []string{
				"🗑️ Pruned",
			},
		},
		{
			name: "successful sync with degraded health",
			results: []SyncResultEntry{{
				Application:  "my-app",
				Phase:        "Succeeded",
				HealthStatus: "Degraded",
			}},
			contains: []string{
				"Sync successful",
				"❤️ Degraded",
			},
		},
		{
			name: "sync without health status",
			results: []SyncResultEntry{{
				Application: "my-app",
				Phase:       "Succeeded",
			}},
			contains: []string{
				"Sync successful",
			},
			excludes: []string{
				"Health:",
			},
		},
		{
			name: "sync with progressing health",
			results: []SyncResultEntry{{
				Application:  "my-app",
				Phase:        "Succeeded",
				HealthStatus: "Progressing",
			}},
			contains: []string{
				"Sync successful",
				"⏳ Progressing",
			},
		},
		{
			name: "per-resource health rendering with healthy and degraded",
			results: []SyncResultEntry{{
				Application:  "my-app",
				Phase:        "Failed",
				Message:      "application health is Degraded",
				HealthStatus: "Degraded",
				Resources: []models.ResourceResult{
					{
						Resource:     models.ResourceKey{Kind: "Deployment", Name: "web", Namespace: "default"},
						Status:       "Synced",
						HealthStatus: "Healthy",
					},
					{
						Resource:      models.ResourceKey{Kind: "Deployment", Name: "worker", Namespace: "default"},
						Status:        "Synced",
						HealthStatus:  "Degraded",
						HealthMessage: "container backoff: ImagePullBackOff",
					},
				},
			}},
			contains: []string{
				"Sync failed",
				"💚 Healthy",
				"❤️ Degraded",
				"ImagePullBackOff",
			},
		},
		{
			name: "always-shown message on success",
			results: []SyncResultEntry{{
				Application:  "my-app",
				Phase:        "Succeeded",
				Message:      "successfully synced (all tasks run)",
				HealthStatus: "Healthy",
			}},
			contains: []string{
				"Sync successful",
				"**Message:** successfully synced (all tasks run)",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := renderer.RenderSync(tt.results)

			for _, want := range tt.contains {
				if !strings.Contains(output, want) {
					t.Errorf("expected output to contain %q, got:\n%s", want, output)
				}
			}

			for _, notWant := range tt.excludes {
				if strings.Contains(output, notWant) {
					t.Errorf("expected output NOT to contain %q, got:\n%s", notWant, output)
				}
			}
		})
	}
}

func TestRenderResourceTableMessageTruncation(t *testing.T) {
	renderer := NewRenderer()

	longMsg := strings.Repeat("a", 100)
	results := []SyncResultEntry{{
		Application: "my-app",
		Phase:       "Succeeded",
		Resources: []models.ResourceResult{
			{Resource: models.ResourceKey{Kind: "Deployment", Name: "web"}, Status: "Synced", Message: longMsg},
		},
	}}

	output := renderer.RenderSync(results)

	// The message should be truncated to 77 chars + "..."
	if strings.Contains(output, longMsg) {
		t.Error("expected long message to be truncated")
	}
	if !strings.Contains(output, "...") {
		t.Error("expected truncated message to end with ...")
	}
}

func TestRenderResourceTablePipeEscaping(t *testing.T) {
	renderer := NewRenderer()

	results := []SyncResultEntry{{
		Application: "my-app",
		Phase:       "Succeeded",
		Resources: []models.ResourceResult{
			{Resource: models.ResourceKey{Kind: "Deployment", Name: "web"}, Status: "Synced", Message: "key|value"},
		},
	}}

	output := renderer.RenderSync(results)

	// Pipe characters should be escaped to avoid breaking the markdown table
	if strings.Contains(output, "key|value") {
		t.Error("expected pipe characters to be escaped in table")
	}
	if !strings.Contains(output, `key\|value`) {
		t.Error("expected escaped pipe in output")
	}
}

func TestRenderPlan(t *testing.T) {
	renderer := NewRenderer()

	results := []PlanResult{
		{
			Application: "my-app",
			Diffs: []models.ManifestDiff{
				{
					Resource: models.ResourceKey{
						Kind:      "Deployment",
						Name:      "my-app",
						Namespace: "default",
					},
					Action: models.DiffActionUpdate,
					Diff:   "--- live\n+++ target\n@@ -1,2 +1,2 @@\n-replicas: 2\n+replicas: 3\n",
				},
				{
					Resource: models.ResourceKey{
						Kind: "ConfigMap",
						Name: "my-config",
					},
					Action: models.DiffActionCreate,
				},
			},
			Created:    1,
			Updated:    1,
			Deleted:    0,
			LockStatus: "Locked by this PR",
		},
		{
			Application: "locked-app",
			LockStatus:  "Locked by PR #99 (otheruser)",
		},
		{
			Application: "error-app",
			Error:       testError("connection refused"),
		},
	}

	output := renderer.RenderPlan(results, 42)

	expectations := []string{
		"## Lemuria Plan",
		"### Application: `my-app`",
		"1 to create",
		"1 to update",
		"Deployment/my-app",
		"ConfigMap/my-config",
		"Locked by this PR",
		"### Application: `locked-app`",
		"Locked by PR #99",
		"### Application: `error-app`",
		"Error",
		"connection refused",
		"lemuria sync",
		"lemuria unlock",
	}

	for _, exp := range expectations {
		if !strings.Contains(output, exp) {
			t.Errorf("Expected output to contain %q", exp)
		}
	}
}

func TestRenderNewAndDeletedApps(t *testing.T) {
	renderer := NewRenderer()

	t.Run("renders new application", func(t *testing.T) {
		results := []PlanResult{
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
		results := []PlanResult{
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
			"will be deleted",
			"apps/deleted-app.yaml",
		}

		for _, exp := range expectations {
			if !strings.Contains(output, exp) {
				t.Errorf("expected output to contain %q\nOutput: %s", exp, output)
			}
		}
	})

	t.Run("renders new application with diffs", func(t *testing.T) {
		results := []PlanResult{
			{
				Application: "new-app",
				ChangeType:  models.ApplicationNew,
				SourceFile:  "apps/new-app.yaml",
				Created:     2,
				Diffs: []models.ManifestDiff{
					{
						Resource: models.ResourceKey{Kind: "Deployment", Name: "web", Namespace: "default"},
						Action:   models.DiffActionCreate,
						Diff:     "--- live\n+++ target\n@@ -0,0 +1,3 @@\n+apiVersion: apps/v1\n+kind: Deployment\n+replicas: 3\n",
					},
					{
						Resource: models.ResourceKey{Kind: "Service", Name: "web-svc", Namespace: "default"},
						Action:   models.DiffActionCreate,
						Diff:     "--- live\n+++ target\n@@ -0,0 +1,2 @@\n+apiVersion: v1\n+kind: Service\n",
					},
				},
			},
		}

		output := renderer.RenderPlan(results, 42)

		expectations := []string{
			"### Application: `new-app`",
			"New application",
			"apps/new-app.yaml",
			"2 to create",
			"Deployment/web",
			"Service/web-svc",
			"```diff",
		}

		for _, exp := range expectations {
			if !strings.Contains(output, exp) {
				t.Errorf("expected output to contain %q\nOutput: %s", exp, output)
			}
		}

		// Should NOT contain the "cannot generate a diff" message
		if strings.Contains(output, "cannot generate a diff") {
			t.Errorf("should not contain 'cannot generate a diff' when diffs are available\nOutput: %s", output)
		}
	})

	t.Run("renders deleted application with diffs", func(t *testing.T) {
		results := []PlanResult{
			{
				Application: "deleted-app",
				ChangeType:  models.ApplicationDeleted,
				SourceFile:  "apps/deleted-app.yaml",
				Warning:     "This application will be removed after the PR is merged.",
				Deleted:     2,
				Diffs: []models.ManifestDiff{
					{
						Resource: models.ResourceKey{Kind: "Deployment", Name: "web", Namespace: "default"},
						Action:   models.DiffActionDelete,
						Diff:     "--- live\n+++ target\n@@ -1,3 +0,0 @@\n-apiVersion: apps/v1\n-kind: Deployment\n-replicas: 2\n",
					},
					{
						Resource: models.ResourceKey{Kind: "ConfigMap", Name: "config", Namespace: "default"},
						Action:   models.DiffActionDelete,
						Diff:     "--- live\n+++ target\n@@ -1,2 +0,0 @@\n-apiVersion: v1\n-kind: ConfigMap\n",
					},
				},
			},
		}

		output := renderer.RenderPlan(results, 42)

		expectations := []string{
			"### Application: `deleted-app`",
			"will be deleted",
			"apps/deleted-app.yaml",
			"orphaned or pruned",
			"2 to delete",
			"Deployment/web",
			"ConfigMap/config",
			"```diff",
		}

		for _, exp := range expectations {
			if !strings.Contains(output, exp) {
				t.Errorf("expected output to contain %q\nOutput: %s", exp, output)
			}
		}
	})

	t.Run("renders mixed new, existing, and deleted", func(t *testing.T) {
		results := []PlanResult{
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

		if !strings.Contains(output, "1 to create") {
			t.Error("expected existing app changes to be rendered")
		}
		if !strings.Contains(output, "Locked by this PR") {
			t.Error("expected lock status for existing app")
		}
		if !strings.Contains(output, "new-app") {
			t.Error("expected new app to be mentioned")
		}
		if !strings.Contains(output, "deleted-app") {
			t.Error("expected deleted app to be mentioned")
		}
	})
}

func TestRenderPlanGrouping(t *testing.T) {
	renderer := NewRenderer()

	tests := []struct {
		name     string
		results  []PlanResult
		contains []string
		excludes []string
	}{
		{
			name: "standalone apps only - no grouping header",
			results: []PlanResult{
				{Application: "app-a", LockStatus: "Locked by this PR"},
				{Application: "app-b", LockStatus: "Locked by this PR"},
			},
			contains: []string{
				"### Application: `app-a`",
				"### Application: `app-b`",
			},
			excludes: []string{
				"### ApplicationSet:",
			},
		},
		{
			name: "applicationset group with header",
			results: []PlanResult{
				{Application: "appset-dev", ApplicationSetName: "my-appset", LockStatus: "Locked by this PR"},
				{Application: "appset-staging", ApplicationSetName: "my-appset", LockStatus: "Locked by this PR"},
			},
			contains: []string{
				"### ApplicationSet: `my-appset` (2 applications)",
				"### Application: `appset-dev`",
				"### Application: `appset-staging`",
			},
		},
		{
			name: "mixed standalone and applicationset apps",
			results: []PlanResult{
				{Application: "standalone", LockStatus: "Locked by this PR"},
				{Application: "appset-dev", ApplicationSetName: "my-appset", LockStatus: "Locked by this PR"},
				{Application: "appset-staging", ApplicationSetName: "my-appset", LockStatus: "Locked by this PR"},
			},
			contains: []string{
				"### Application: `standalone`",
				"### ApplicationSet: `my-appset` (2 applications)",
				"### Application: `appset-dev`",
				"### Application: `appset-staging`",
			},
		},
		{
			name: "multiple applicationset groups",
			results: []PlanResult{
				{Application: "set-a-1", ApplicationSetName: "set-a", LockStatus: "Locked by this PR"},
				{Application: "set-b-1", ApplicationSetName: "set-b", LockStatus: "Locked by this PR"},
				{Application: "set-a-2", ApplicationSetName: "set-a", LockStatus: "Locked by this PR"},
			},
			contains: []string{
				"### ApplicationSet: `set-a` (2 applications)",
				"### ApplicationSet: `set-b` (1 application)",
				"### Application: `set-a-1`",
				"### Application: `set-a-2`",
				"### Application: `set-b-1`",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := renderer.RenderPlan(tt.results, 1)

			for _, want := range tt.contains {
				if !strings.Contains(output, want) {
					t.Errorf("expected output to contain %q, got:\n%s", want, output)
				}
			}

			for _, notWant := range tt.excludes {
				if strings.Contains(output, notWant) {
					t.Errorf("expected output NOT to contain %q, got:\n%s", notWant, output)
				}
			}
		})
	}
}

func TestRenderPlanStandaloneBeforeGroups(t *testing.T) {
	renderer := NewRenderer()

	results := []PlanResult{
		{Application: "appset-dev", ApplicationSetName: "my-appset", LockStatus: "Locked by this PR"},
		{Application: "standalone", LockStatus: "Locked by this PR"},
	}

	output := renderer.RenderPlan(results, 1)

	// Standalone should appear before the ApplicationSet group
	standaloneIdx := strings.Index(output, "### Application: `standalone`")
	appSetIdx := strings.Index(output, "### ApplicationSet: `my-appset`")

	if standaloneIdx == -1 {
		t.Fatal("expected standalone app in output")
	}
	if appSetIdx == -1 {
		t.Fatal("expected applicationset group in output")
	}
	if standaloneIdx > appSetIdx {
		t.Error("expected standalone apps to appear before applicationset groups")
	}
}

func TestRenderPlanGeneratedAppNew(t *testing.T) {
	renderer := NewRenderer()

	results := []PlanResult{
		{
			Application:        "my-appset-prod",
			ApplicationSetName: "my-appset",
			ChangeType:         models.ApplicationNew,
			IsGeneratedApp:     true,
			SourceFile:         "bootstrap/appsets.yaml",
		},
	}

	output := renderer.RenderPlan(results, 1)

	// Should mention "will be generated by ApplicationSet"
	if !strings.Contains(output, "will be generated by ApplicationSet `my-appset` after merge") {
		t.Errorf("expected generated app message, got:\n%s", output)
	}
	// Should show source file
	if !strings.Contains(output, "bootstrap/appsets.yaml") {
		t.Errorf("expected source file in output, got:\n%s", output)
	}
	// Should NOT show the regular "Application CR is applied" message
	if strings.Contains(output, "Application CR is applied") {
		t.Errorf("should not show regular new app message for generated apps, got:\n%s", output)
	}
}

func TestRenderPlanGeneratedAppDeleted(t *testing.T) {
	renderer := NewRenderer()

	results := []PlanResult{
		{
			Application:        "my-appset-staging",
			ApplicationSetName: "my-appset",
			ChangeType:         models.ApplicationDeleted,
			IsGeneratedApp:     true,
			SourceFile:         "bootstrap/appsets.yaml",
		},
	}

	output := renderer.RenderPlan(results, 1)

	// Should mention "generator no longer produces this app"
	if !strings.Contains(output, "ApplicationSet `my-appset` generator no longer produces this app") {
		t.Errorf("expected generated app deleted message, got:\n%s", output)
	}
	// Should show source file
	if !strings.Contains(output, "bootstrap/appsets.yaml") {
		t.Errorf("expected source file in output, got:\n%s", output)
	}
	// Should NOT show the regular "Application CR is removed" message
	if strings.Contains(output, "Application CR is removed") {
		t.Errorf("should not show regular deleted app message for generated apps, got:\n%s", output)
	}
}

func TestRenderPlanNonGeneratedNewApp(t *testing.T) {
	renderer := NewRenderer()

	results := []PlanResult{
		{
			Application: "direct-app",
			ChangeType:  models.ApplicationNew,
			SourceFile:  "apps/direct.yaml",
		},
	}

	output := renderer.RenderPlan(results, 1)

	// Should show regular new app message
	if !strings.Contains(output, "will be created when the Application CR is applied") {
		t.Errorf("expected regular new app message, got:\n%s", output)
	}
}

func TestRenderPlanMixedGeneratedAndRegular(t *testing.T) {
	renderer := NewRenderer()

	results := []PlanResult{
		{
			Application: "direct-app",
			ChangeType:  models.ApplicationNew,
			SourceFile:  "apps/direct.yaml",
		},
		{
			Application:        "my-appset-prod",
			ApplicationSetName: "my-appset",
			ChangeType:         models.ApplicationNew,
			IsGeneratedApp:     true,
			SourceFile:         "bootstrap/appsets.yaml",
		},
		{
			Application:        "my-appset-staging",
			ApplicationSetName: "my-appset",
			ChangeType:         models.ApplicationDeleted,
			IsGeneratedApp:     true,
			SourceFile:         "bootstrap/appsets.yaml",
		},
	}

	output := renderer.RenderPlan(results, 1)

	// Regular new app
	if !strings.Contains(output, "will be created when the Application CR is applied") {
		t.Errorf("expected regular new app message, got:\n%s", output)
	}
	// Generated new app
	if !strings.Contains(output, "will be generated by ApplicationSet `my-appset` after merge") {
		t.Errorf("expected generated new app message, got:\n%s", output)
	}
	// Generated deleted app
	if !strings.Contains(output, "generator no longer produces this app") {
		t.Errorf("expected generated deleted app message, got:\n%s", output)
	}
	// ApplicationSet grouping
	if !strings.Contains(output, "ApplicationSet: `my-appset`") {
		t.Errorf("expected ApplicationSet group header, got:\n%s", output)
	}
}

// errTest is a test error.
var errTest = testError("test error")

type testError string

func (e testError) Error() string { return string(e) }
