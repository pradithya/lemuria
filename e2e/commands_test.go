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

package e2e

import (
	"testing"

	"github.com/org/lemuria/internal/commands"
)

func TestCommandParsing(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		wantCmd       commands.CommandType
		wantApp       string
		wantAll       bool
		wantPrune     bool
		wantDryRun    bool
		wantHistoryID int64
		wantErr       bool
	}{
		{
			name:    "simple plan",
			input:   "lemuria plan",
			wantCmd: commands.CommandPlan,
		},
		{
			name:    "plan with app flag",
			input:   "lemuria plan -a my-app",
			wantCmd: commands.CommandPlan,
			wantApp: "my-app",
		},
		{
			name:    "plan with long app flag",
			input:   "lemuria plan --app my-app",
			wantCmd: commands.CommandPlan,
			wantApp: "my-app",
		},
		{
			name:    "plan all",
			input:   "lemuria plan --all",
			wantCmd: commands.CommandPlan,
			wantAll: true,
		},
		{
			name:    "simple sync",
			input:   "lemuria sync",
			wantCmd: commands.CommandSync,
		},
		{
			name:      "sync with prune",
			input:     "lemuria sync --prune",
			wantCmd:   commands.CommandSync,
			wantPrune: true,
		},
		{
			name:       "sync dry-run",
			input:      "lemuria sync --dry-run",
			wantCmd:    commands.CommandSync,
			wantDryRun: true,
		},
		{
			name:       "sync all options",
			input:      "lemuria sync -a my-app --prune --dry-run",
			wantCmd:    commands.CommandSync,
			wantApp:    "my-app",
			wantPrune:  true,
			wantDryRun: true,
		},
		{
			name:    "unlock",
			input:   "lemuria unlock",
			wantCmd: commands.CommandUnlock,
		},
		{
			name:    "unlock with app",
			input:   "lemuria unlock -a my-app",
			wantCmd: commands.CommandUnlock,
			wantApp: "my-app",
		},
		{
			name:    "help",
			input:   "lemuria help",
			wantCmd: commands.CommandHelp,
		},
		{
			name:    "case insensitive",
			input:   "Lemuria Plan",
			wantCmd: commands.CommandPlan,
		},
		{
			name:    "with leading whitespace",
			input:   "  lemuria plan",
			wantCmd: commands.CommandPlan,
		},
		{
			name:    "multi-line with command",
			input:   "This is a comment\nlemuria plan\nMore text",
			wantCmd: commands.CommandPlan,
		},
		{
			name:    "not a command",
			input:   "just some text",
			wantErr: true,
		},
		{
			name:    "partial command",
			input:   "lemuria",
			wantErr: true,
		},
		{
			name:    "unknown command",
			input:   "lemuria deploy",
			wantErr: true,
		},
		{
			name:    "rollback command",
			input:   "lemuria rollback -a my-app",
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
			name:       "rollback with all options",
			input:      "lemuria rollback -a my-app --dry-run --prune",
			wantCmd:    commands.CommandRollback,
			wantApp:    "my-app",
			wantDryRun: true,
			wantPrune:  true,
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

			if cmd.All != tt.wantAll {
				t.Errorf("All: got %v, want %v", cmd.All, tt.wantAll)
			}

			if cmd.Prune != tt.wantPrune {
				t.Errorf("Prune: got %v, want %v", cmd.Prune, tt.wantPrune)
			}

			if cmd.DryRun != tt.wantDryRun {
				t.Errorf("DryRun: got %v, want %v", cmd.DryRun, tt.wantDryRun)
			}

			if cmd.HistoryID != tt.wantHistoryID {
				t.Errorf("HistoryID: got %d, want %d", cmd.HistoryID, tt.wantHistoryID)
			}
		})
	}
}

func TestCommandString(t *testing.T) {
	tests := []struct {
		cmd  commands.Command
		want string
	}{
		{
			cmd:  commands.Command{Name: commands.CommandPlan},
			want: "lemuria plan",
		},
		{
			cmd:  commands.Command{Name: commands.CommandPlan, Application: "my-app"},
			want: "lemuria plan -a my-app",
		},
		{
			cmd:  commands.Command{Name: commands.CommandSync, Prune: true, DryRun: true},
			want: "lemuria sync --prune --dry-run",
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
