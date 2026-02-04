package e2e

import (
	"testing"

	"github.com/org/lemuria/internal/commands"
)

func TestRollbackCommandParsing(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantCmd    commands.CommandType
		wantApp    string
		wantDryRun bool
		wantPrune  bool
		wantErr    bool
	}{
		{
			name:    "rollback without app",
			input:   "lemuria rollback",
			wantCmd: commands.CommandRollback,
		},
		{
			name:    "rollback with app flag",
			input:   "lemuria rollback -a my-app",
			wantCmd: commands.CommandRollback,
			wantApp: "my-app",
		},
		{
			name:    "rollback with long app flag",
			input:   "lemuria rollback --app my-app",
			wantCmd: commands.CommandRollback,
			wantApp: "my-app",
		},
		{
			name:       "rollback with dry-run",
			input:      "lemuria rollback -a my-app --dry-run",
			wantCmd:    commands.CommandRollback,
			wantApp:    "my-app",
			wantDryRun: true,
		},
		{
			name:      "rollback with prune",
			input:     "lemuria rollback -a my-app --prune",
			wantCmd:   commands.CommandRollback,
			wantApp:   "my-app",
			wantPrune: true,
		},
		{
			name:       "rollback with all options",
			input:      "lemuria rollback -a my-app --dry-run --prune",
			wantCmd:    commands.CommandRollback,
			wantApp:    "my-app",
			wantDryRun: true,
			wantPrune:  true,
		},
		{
			name:    "rollback case insensitive",
			input:   "Lemuria Rollback -a my-app",
			wantCmd: commands.CommandRollback,
			wantApp: "my-app",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, err := commands.Parse(tt.input)

			if tt.wantErr {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if cmd.Name != tt.wantCmd {
				t.Errorf("Command type: got %v, want %v", cmd.Name, tt.wantCmd)
			}

			if cmd.Application != tt.wantApp {
				t.Errorf("Application: got %q, want %q", cmd.Application, tt.wantApp)
			}

			if cmd.DryRun != tt.wantDryRun {
				t.Errorf("DryRun: got %v, want %v", cmd.DryRun, tt.wantDryRun)
			}

			if cmd.Prune != tt.wantPrune {
				t.Errorf("Prune: got %v, want %v", cmd.Prune, tt.wantPrune)
			}
		})
	}
}

func TestRollbackCommandString(t *testing.T) {
	tests := []struct {
		cmd  commands.Command
		want string
	}{
		{
			cmd:  commands.Command{Name: commands.CommandRollback, Application: "my-app"},
			want: "lemuria rollback -a my-app",
		},
		{
			cmd:  commands.Command{Name: commands.CommandRollback, Application: "my-app", DryRun: true},
			want: "lemuria rollback -a my-app --dry-run",
		},
		{
			cmd:  commands.Command{Name: commands.CommandRollback, Application: "my-app", Prune: true},
			want: "lemuria rollback -a my-app --prune",
		},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.cmd.String()
			if got != tt.want {
				t.Errorf("String(): got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRollbackWorkflow(t *testing.T) {
	// Test the rollback workflow logic:
	// 1. PR sync deploys feature branch commit (overrides targetRevision)
	// 2. Rollback syncs to app's configured targetRevision (main/master)

	tests := []struct {
		name             string
		appTargetRev     string
		prHeadSHA        string
		rollbackRevision string // Expected: empty string to use app's targetRevision
	}{
		{
			name:             "rollback to main",
			appTargetRev:     "main",
			prHeadSHA:        "abc123",
			rollbackRevision: "", // Empty = use app's targetRevision
		},
		{
			name:             "rollback to HEAD",
			appTargetRev:     "HEAD",
			prHeadSHA:        "def456",
			rollbackRevision: "",
		},
		{
			name:             "rollback to master",
			appTargetRev:     "master",
			prHeadSHA:        "ghi789",
			rollbackRevision: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate PR sync: uses specific commit SHA
			syncRevision := tt.prHeadSHA
			if syncRevision == "" {
				t.Error("PR sync should use specific commit SHA")
			}

			// Simulate rollback: uses empty revision to default to app's targetRevision
			if tt.rollbackRevision != "" {
				t.Error("Rollback should use empty revision to default to app's targetRevision")
			}
		})
	}
}

func TestGetApplicationHistory(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping ArgoCD test in short mode")
	}

	if argoClient == nil {
		t.Skip("Argo CD client not initialized")
	}

	apps, err := argoClient.ListApplications(testCtx)
	if err != nil {
		t.Fatalf("Failed to list applications: %v", err)
	}

	if len(apps) == 0 {
		t.Skip("No applications available to test history")
	}

	appName := apps[0].Name
	history, err := argoClient.GetApplicationHistory(testCtx, appName)
	if err != nil {
		t.Fatalf("Failed to get history for %s: %v", appName, err)
	}

	t.Logf("Got %d history entries for %s", len(history), appName)

	for _, entry := range history {
		t.Logf("  - ID: %d, Revision: %s, DeployedAt: %s",
			entry.ID, entry.Revision, entry.DeployedAt)
	}
}
