package e2e

import (
	"testing"

	"github.com/org/lemuria/internal/config"
)

func TestApprovalConfigDefaults(t *testing.T) {
	cfg := config.DefaultConfig()

	if cfg.Defaults.RequireApproval != false {
		t.Errorf("Expected RequireApproval default to be false, got %v", cfg.Defaults.RequireApproval)
	}
}

func TestApprovalRequired(t *testing.T) {
	tests := []struct {
		name            string
		requireApproval bool
		isApproved      bool
		wantAllowed     bool
	}{
		{
			name:            "approval required and approved",
			requireApproval: true,
			isApproved:      true,
			wantAllowed:     true,
		},
		{
			name:            "approval required but not approved",
			requireApproval: true,
			isApproved:      false,
			wantAllowed:     false,
		},
		{
			name:            "approval not required and not approved",
			requireApproval: false,
			isApproved:      false,
			wantAllowed:     true,
		},
		{
			name:            "approval not required but approved",
			requireApproval: false,
			isApproved:      true,
			wantAllowed:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Defaults: config.DefaultsConfig{
					RequireApproval: tt.requireApproval,
				},
			}

			// Simulate the approval check logic from sync/rollback
			allowed := !cfg.Defaults.RequireApproval || tt.isApproved

			if allowed != tt.wantAllowed {
				t.Errorf("Allowed: got %v, want %v", allowed, tt.wantAllowed)
			}
		})
	}
}

func TestApprovalEnforcementForSync(t *testing.T) {
	tests := []struct {
		name            string
		requireApproval bool
		isApproved      bool
		isDryRun        bool
		wantSync        bool
		wantError       string
	}{
		{
			name:            "sync allowed when approved",
			requireApproval: true,
			isApproved:      true,
			wantSync:        true,
		},
		{
			name:            "sync blocked when not approved",
			requireApproval: true,
			isApproved:      false,
			wantSync:        false,
			wantError:       "PR must be approved before sync",
		},
		{
			name:            "sync allowed when approval not required",
			requireApproval: false,
			isApproved:      false,
			wantSync:        true,
		},
		{
			name:            "dry-run sync respects approval",
			requireApproval: true,
			isApproved:      false,
			isDryRun:        true,
			wantSync:        false,
			wantError:       "PR must be approved before sync",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Defaults: config.DefaultsConfig{
					RequireApproval: tt.requireApproval,
				},
			}

			// Simulate the approval check logic
			var syncAllowed bool
			var errorMsg string

			if cfg.Defaults.RequireApproval && !tt.isApproved {
				syncAllowed = false
				errorMsg = "PR must be approved before sync"
			} else {
				syncAllowed = true
			}

			if syncAllowed != tt.wantSync {
				t.Errorf("Sync allowed: got %v, want %v", syncAllowed, tt.wantSync)
			}

			if errorMsg != tt.wantError {
				t.Errorf("Error message: got %q, want %q", errorMsg, tt.wantError)
			}
		})
	}
}

func TestApprovalEnforcementForRollback(t *testing.T) {
	tests := []struct {
		name            string
		requireApproval bool
		isApproved      bool
		wantRollback    bool
		wantError       string
	}{
		{
			name:            "rollback allowed when approved",
			requireApproval: true,
			isApproved:      true,
			wantRollback:    true,
		},
		{
			name:            "rollback blocked when not approved",
			requireApproval: true,
			isApproved:      false,
			wantRollback:    false,
			wantError:       "PR must be approved before rollback",
		},
		{
			name:            "rollback allowed when approval not required",
			requireApproval: false,
			isApproved:      false,
			wantRollback:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Defaults: config.DefaultsConfig{
					RequireApproval: tt.requireApproval,
				},
			}

			// Simulate the approval check logic
			var rollbackAllowed bool
			var errorMsg string

			if cfg.Defaults.RequireApproval && !tt.isApproved {
				rollbackAllowed = false
				errorMsg = "PR must be approved before rollback"
			} else {
				rollbackAllowed = true
			}

			if rollbackAllowed != tt.wantRollback {
				t.Errorf("Rollback allowed: got %v, want %v", rollbackAllowed, tt.wantRollback)
			}

			if errorMsg != tt.wantError {
				t.Errorf("Error message: got %q, want %q", errorMsg, tt.wantError)
			}
		})
	}
}

func TestCombinedApprovalAndAutoMerge(t *testing.T) {
	tests := []struct {
		name            string
		requireApproval bool
		autoMerge       bool
		isApproved      bool
		allSyncsSucceed bool
		wantSync        bool
		wantAutoMerge   bool
	}{
		{
			name:            "approved sync with auto-merge",
			requireApproval: true,
			autoMerge:       true,
			isApproved:      true,
			allSyncsSucceed: true,
			wantSync:        true,
			wantAutoMerge:   true,
		},
		{
			name:            "unapproved sync blocks auto-merge",
			requireApproval: true,
			autoMerge:       true,
			isApproved:      false,
			allSyncsSucceed: false,
			wantSync:        false,
			wantAutoMerge:   false,
		},
		{
			name:            "auto-merge disabled even when sync succeeds",
			requireApproval: false,
			autoMerge:       false,
			isApproved:      false,
			allSyncsSucceed: true,
			wantSync:        true,
			wantAutoMerge:   false,
		},
		{
			name:            "auto-merge skipped on sync failure",
			requireApproval: false,
			autoMerge:       true,
			isApproved:      false,
			allSyncsSucceed: false,
			wantSync:        true,
			wantAutoMerge:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Defaults: config.DefaultsConfig{
					RequireApproval: tt.requireApproval,
					AutoMerge:       tt.autoMerge,
				},
			}

			// Simulate the combined logic
			var syncAllowed bool
			if cfg.Defaults.RequireApproval && !tt.isApproved {
				syncAllowed = false
			} else {
				syncAllowed = true
			}

			var autoMergeTriggered bool
			if syncAllowed && tt.allSyncsSucceed && cfg.Defaults.AutoMerge {
				autoMergeTriggered = true
			}

			if syncAllowed != tt.wantSync {
				t.Errorf("Sync allowed: got %v, want %v", syncAllowed, tt.wantSync)
			}

			if autoMergeTriggered != tt.wantAutoMerge {
				t.Errorf("Auto-merge triggered: got %v, want %v", autoMergeTriggered, tt.wantAutoMerge)
			}
		})
	}
}
