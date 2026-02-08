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
				Application: "my-app",
				Phase:       "Succeeded",
				PlanOutput:  "2 to create, 1 to update",
				Resources: []models.ResourceResult{
					{
						Resource: models.ResourceKey{Kind: "Deployment", Name: "web", Namespace: "default"},
						Status:   "Synced",
						Message:  "deployment.apps/web configured",
					},
					{
						Resource: models.ResourceKey{Kind: "Service", Name: "web-svc", Namespace: "default"},
						Status:   "Synced",
						Message:  "service/web-svc created",
					},
				},
			}},
			contains: []string{
				"Sync successful",
				"2 to create, 1 to update",
				"Resource Results (2 resources)",
				"Deployment/web",
				"Service/web-svc",
				"Synced",
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

// errTest is a test error.
var errTest = testError("test error")

type testError string

func (e testError) Error() string { return string(e) }
