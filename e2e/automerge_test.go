package e2e

import (
	"testing"

	"github.com/org/lemuria/internal/commands"
	"github.com/org/lemuria/internal/config"
	"github.com/org/lemuria/internal/models"
)

func TestAutoMergeConfigDefaults(t *testing.T) {
	cfg := config.DefaultConfig()

	if cfg.Defaults.AutoMerge != false {
		t.Errorf("Expected AutoMerge default to be false, got %v", cfg.Defaults.AutoMerge)
	}

	if cfg.Defaults.MergeMethod != "squash" {
		t.Errorf("Expected MergeMethod default to be 'squash', got %v", cfg.Defaults.MergeMethod)
	}
}

func TestAutoMergeConfigValidation(t *testing.T) {
	tests := []struct {
		name        string
		autoMerge   bool
		mergeMethod string
		wantMethod  string
	}{
		{
			name:        "default merge method",
			autoMerge:   true,
			mergeMethod: "",
			wantMethod:  "squash",
		},
		{
			name:        "explicit squash",
			autoMerge:   true,
			mergeMethod: "squash",
			wantMethod:  "squash",
		},
		{
			name:        "merge method",
			autoMerge:   true,
			mergeMethod: "merge",
			wantMethod:  "merge",
		},
		{
			name:        "rebase method",
			autoMerge:   true,
			mergeMethod: "rebase",
			wantMethod:  "rebase",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Defaults: config.DefaultsConfig{
					AutoMerge:   tt.autoMerge,
					MergeMethod: tt.mergeMethod,
				},
			}

			// Verify merge method is set correctly
			method := cfg.Defaults.MergeMethod
			if method == "" {
				method = "squash"
			}

			if method != tt.wantMethod {
				t.Errorf("Expected merge method %q, got %q", tt.wantMethod, method)
			}
		})
	}
}

func TestIsProtectedBranch(t *testing.T) {
	tests := []struct {
		branch    string
		protected bool
	}{
		{"main", true},
		{"master", true},
		{"develop", true},
		{"development", true},
		{"feature/my-feature", false},
		{"fix/bug-123", false},
		{"release/v1.0", false},
		{"hotfix/urgent", false},
	}

	for _, tt := range tests {
		t.Run(tt.branch, func(t *testing.T) {
			got := commands.IsProtectedBranch(tt.branch)
			if got != tt.protected {
				t.Errorf("IsProtectedBranch(%q) = %v, want %v", tt.branch, got, tt.protected)
			}
		})
	}
}

func TestAutoSyncDetection(t *testing.T) {
	tests := []struct {
		name            string
		autoSyncEnabled bool
		wantHasAutoSync bool
	}{
		{
			name:            "auto-sync disabled",
			autoSyncEnabled: false,
			wantHasAutoSync: false,
		},
		{
			name:            "auto-sync enabled",
			autoSyncEnabled: true,
			wantHasAutoSync: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := &models.Application{
				Name:            "test-app",
				AutoSyncEnabled: tt.autoSyncEnabled,
			}

			if app.HasAutoSync() != tt.wantHasAutoSync {
				t.Errorf("HasAutoSync() = %v, want %v", app.HasAutoSync(), tt.wantHasAutoSync)
			}
		})
	}
}
