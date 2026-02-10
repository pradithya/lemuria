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
		"### Application: <code>my-app</code>",
		"1 to create",
		"1 to update",
		"Deployment/my-app",
		"ConfigMap/my-config",
		"Locked by this PR",
		"### Application: <code>locked-app</code>",
		"Locked by PR #99",
		"### Application: <code>error-app</code>",
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
			"### Application: <code>new-app</code>",
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
			"### Application: <code>deleted-app</code>",
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

func TestEscapeMarkdown(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "plain text unchanged",
			input: "hello world",
			want:  "hello world",
		},
		{
			name:  "pipe characters escaped",
			input: "key|value",
			want:  `key\|value`,
		},
		{
			name:  "brackets escaped",
			input: "[link](url)",
			want:  `\[link\]\(url\)`,
		},
		{
			name:  "hash escaped",
			input: "# heading",
			want:  `\# heading`,
		},
		{
			name:  "bold/italic characters escaped",
			input: "**bold** and _italic_",
			want:  `\*\*bold\*\* and \_italic\_`,
		},
		{
			name:  "single backtick escaped",
			input: "use `code` here",
			want:  "use \\`code\\` here",
		},
		{
			name:  "triple backticks escaped",
			input: "```code block```",
			want:  "\\`\\`\\`code block\\`\\`\\`",
		},
		{
			name:  "angle brackets HTML-escaped",
			input: "<script>alert('xss')</script>",
			want:  "&lt;script&gt;alert\\('xss'\\)&lt;/script&gt;",
		},
		{
			name:  "run of backticks in the middle",
			input: "before````after",
			want:  "before\\`\\`\\`\\`after",
		},
		{
			name:  "mixed special characters",
			input: "app|name with [brackets] and `backticks`",
			want:  "app\\|name with \\[brackets\\] and \\`backticks\\`",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := escapeMarkdown(tt.input)
			if got != tt.want {
				t.Errorf("escapeMarkdown(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSanitizeDiffForMarkdown(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "no backticks unchanged",
			input: "plain text\nwith newlines\n",
			want:  "plain text\nwith newlines\n",
		},
		{
			name:  "single backtick unchanged",
			input: "one ` backtick",
			want:  "one ` backtick",
		},
		{
			name:  "two backticks get zero-width space",
			input: "two ``backticks",
			want:  "two ``\u200Bbackticks",
		},
		{
			name:  "triple backticks broken by zero-width space",
			input: "```fence```",
			want:  "``\u200B`fence``\u200B`",
		},
		{
			name:  "four backticks broken after second",
			input: "````",
			want:  "``\u200B``",
		},
		{
			name:  "backticks separated by other chars",
			input: "` ` `",
			want:  "` ` `",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeDiffForMarkdown(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeDiffForMarkdown(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestRenderError(t *testing.T) {
	renderer := NewRenderer()

	t.Run("simple error", func(t *testing.T) {
		output := renderer.RenderError(testError("connection refused"))
		if !strings.Contains(output, "## Lemuria Error") {
			t.Error("expected error heading")
		}
		if !strings.Contains(output, "connection refused") {
			t.Error("expected error message")
		}
	})

	t.Run("error with backticks does not break fence", func(t *testing.T) {
		output := renderer.RenderError(testError("bad input ``` causes issues"))
		// The triple backtick in the error message should be sanitized
		// so that it does not appear as a raw ``` that would close the fence
		if strings.Contains(output, "bad input ``` causes") {
			t.Error("expected triple backticks in error message to be sanitized")
		}
		// The output should still contain the error text (with sanitization)
		if !strings.Contains(output, "bad input") {
			t.Error("expected error message content to be present")
		}
	})
}

func TestRenderAppPlanEscapesError(t *testing.T) {
	renderer := NewRenderer()

	results := []PlanResult{
		{
			Application: "my-app",
			Error:       testError("failed: [click here](http://evil.com)"),
		},
	}

	output := renderer.RenderPlan(results, 1)

	// The markdown link should be escaped
	if strings.Contains(output, "[click here](http://evil.com)") {
		t.Error("expected markdown link in error message to be escaped")
	}
	if !strings.Contains(output, "failed:") {
		t.Error("expected error message content to be present")
	}
}

func TestRenderAppPlanUsesHTMLCodeForAppName(t *testing.T) {
	renderer := NewRenderer()

	results := []PlanResult{
		{
			Application: "app`with`backticks",
			Created:     1,
			LockStatus:  "Locked by this PR",
		},
	}

	output := renderer.RenderPlan(results, 1)

	// App name should be wrapped in HTML <code> tags, not backtick fences
	if !strings.Contains(output, "<code>app`with`backticks</code>") {
		t.Errorf("expected HTML code tag for app name with backticks, got:\n%s", output)
	}
	// Should NOT use markdown backtick code spans
	if strings.Contains(output, "`app`with`backticks`") {
		t.Error("should not use markdown backticks for app name containing backticks")
	}
}

func TestRenderSyncUsesHTMLCodeForAppName(t *testing.T) {
	renderer := NewRenderer()

	results := []SyncResultEntry{
		{
			Application: "app<br>injection",
			Phase:       "Succeeded",
		},
	}

	output := renderer.RenderSync(results)

	// HTML special chars in app name should be escaped
	if strings.Contains(output, "app<br>injection") {
		t.Error("expected HTML entities in app name to be escaped")
	}
	if !strings.Contains(output, "<code>app&lt;br&gt;injection</code>") {
		t.Errorf("expected HTML-escaped app name in code tags, got:\n%s", output)
	}
}

// errTest is a test error.
var errTest = testError("test error")

type testError string

func (e testError) Error() string { return string(e) }
