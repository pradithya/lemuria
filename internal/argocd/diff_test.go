package argocd

import (
	"testing"

	"github.com/org/lemuria/internal/models"
)

func TestComputeDiffFromArgoCD(t *testing.T) {
	tests := []struct {
		name     string
		item     ResourceDiff
		wantDiff bool // true if we expect a non-empty diff
	}{
		{
			name: "modified resource with normalized states",
			item: ResourceDiff{
				Kind:                "Deployment",
				Name:                "my-app",
				Modified:            true,
				NormalizedLiveState: `{"replicas": 2}`,
				PredictedLiveState:  `{"replicas": 3}`,
			},
			wantDiff: true,
		},
		{
			name: "unmodified resource returns empty",
			item: ResourceDiff{
				Kind:                "Deployment",
				Name:                "my-app",
				Modified:            false,
				NormalizedLiveState: `{"replicas": 2}`,
				PredictedLiveState:  `{"replicas": 2}`,
			},
			wantDiff: false,
		},
		{
			name: "new resource with only target state",
			item: ResourceDiff{
				Kind:        "ConfigMap",
				Name:        "my-config",
				Modified:    true,
				LiveState:   "null",
				TargetState: `{"data": {"key": "value"}}`,
			},
			wantDiff: true,
		},
		{
			name: "resource being deleted",
			item: ResourceDiff{
				Kind:        "ConfigMap",
				Name:        "old-config",
				Modified:    true,
				LiveState:   `{"data": {"key": "value"}}`,
				TargetState: "null",
			},
			wantDiff: true,
		},
		{
			name: "empty states returns empty diff",
			item: ResourceDiff{
				Kind:        "ConfigMap",
				Name:        "empty",
				LiveState:   "",
				TargetState: "",
			},
			wantDiff: false,
		},
		{
			name: "null states returns empty diff",
			item: ResourceDiff{
				Kind:        "ConfigMap",
				Name:        "null",
				LiveState:   "null",
				TargetState: "null",
			},
			wantDiff: false,
		},
		{
			name: "fallback to raw states when normalized not available",
			item: ResourceDiff{
				Kind:        "Service",
				Name:        "my-service",
				Modified:    true,
				LiveState:   `{"spec": {"type": "ClusterIP"}}`,
				TargetState: `{"spec": {"type": "LoadBalancer"}}`,
			},
			wantDiff: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diff := computeDiffFromArgoCD(tt.item)
			hasDiff := diff != ""
			if hasDiff != tt.wantDiff {
				t.Errorf("computeDiffFromArgoCD() hasDiff = %v, want %v; diff = %q", hasDiff, tt.wantDiff, diff)
			}
		})
	}
}

func TestDetermineDiffAction(t *testing.T) {
	tests := []struct {
		name string
		item ResourceDiff
		want models.DiffAction
	}{
		{
			name: "new resource (live empty)",
			item: ResourceDiff{
				Kind:        "Deployment",
				Name:        "new-app",
				LiveState:   "",
				TargetState: `{"kind": "Deployment"}`,
			},
			want: models.DiffActionCreate,
		},
		{
			name: "new resource (live null)",
			item: ResourceDiff{
				Kind:        "Deployment",
				Name:        "new-app",
				LiveState:   "null",
				TargetState: `{"kind": "Deployment"}`,
			},
			want: models.DiffActionCreate,
		},
		{
			name: "deleted resource (target empty)",
			item: ResourceDiff{
				Kind:        "Deployment",
				Name:        "deleted-app",
				LiveState:   `{"kind": "Deployment"}`,
				TargetState: "",
			},
			want: models.DiffActionDelete,
		},
		{
			name: "deleted resource (target null)",
			item: ResourceDiff{
				Kind:        "Deployment",
				Name:        "deleted-app",
				LiveState:   `{"kind": "Deployment"}`,
				TargetState: "null",
			},
			want: models.DiffActionDelete,
		},
		{
			name: "modified resource",
			item: ResourceDiff{
				Kind:        "Deployment",
				Name:        "my-app",
				Modified:    true,
				LiveState:   `{"replicas": 2}`,
				TargetState: `{"replicas": 3}`,
			},
			want: models.DiffActionUpdate,
		},
		{
			name: "unchanged resource (Modified=false)",
			item: ResourceDiff{
				Kind:        "Deployment",
				Name:        "my-app",
				Modified:    false,
				LiveState:   `{"replicas": 2}`,
				TargetState: `{"replicas": 2}`,
			},
			want: models.DiffActionNone,
		},
		{
			name: "fallback to normalized states diff",
			item: ResourceDiff{
				Kind:                "Deployment",
				Name:                "my-app",
				Modified:            false,
				LiveState:           `{"replicas": 2}`,
				TargetState:         `{"replicas": 2}`,
				NormalizedLiveState: `{"replicas": 2}`,
				PredictedLiveState:  `{"replicas": 3}`, // different
			},
			want: models.DiffActionUpdate,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := determineDiffAction(tt.item)
			if got != tt.want {
				t.Errorf("determineDiffAction() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestJSONToYAML(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		wantErr bool
	}{
		{
			name: "valid JSON object",
			json: `{"apiVersion": "v1", "kind": "ConfigMap", "metadata": {"name": "test"}}`,
		},
		{
			name: "empty string",
			json: "",
		},
		{
			name: "null",
			json: "null",
		},
		{
			name:    "invalid JSON returns as-is",
			json:    "not valid json",
			wantErr: false, // returns as-is
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := jsonToYAML(tt.json)
			// Just verify it doesn't panic
			if tt.json == "" || tt.json == "null" {
				if result != "" {
					t.Errorf("Expected empty result for empty/null input, got %q", result)
				}
			}
		})
	}
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
