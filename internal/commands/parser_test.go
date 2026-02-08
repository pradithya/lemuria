package commands

import (
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		want    *Command
		wantErr bool
	}{
		// Basic commands
		{
			name: "simple plan",
			body: "lemuria plan",
			want: &Command{Name: CommandPlan},
		},
		{
			name: "simple sync",
			body: "lemuria sync",
			want: &Command{Name: CommandSync},
		},
		{
			name: "simple unlock",
			body: "lemuria unlock",
			want: &Command{Name: CommandUnlock},
		},
		{
			name: "simple help",
			body: "lemuria help",
			want: &Command{Name: CommandHelp},
		},
		{
			name: "simple rollback",
			body: "lemuria rollback",
			want: &Command{Name: CommandRollback},
		},

		// Case insensitivity
		{
			name: "uppercase LEMURIA",
			body: "LEMURIA plan",
			want: &Command{Name: CommandPlan},
		},
		{
			name: "mixed case Lemuria",
			body: "Lemuria Sync",
			want: &Command{Name: CommandSync},
		},

		// Application flag variants
		{
			name: "plan with -a flag",
			body: "lemuria plan -a my-app",
			want: &Command{Name: CommandPlan, Application: "my-app", RawArgs: "-a my-app"},
		},
		{
			name: "plan with --app flag",
			body: "lemuria plan --app my-app",
			want: &Command{Name: CommandPlan, Application: "my-app", RawArgs: "--app my-app"},
		},
		{
			name: "plan with --application flag",
			body: "lemuria plan --application my-app",
			want: &Command{Name: CommandPlan, Application: "my-app", RawArgs: "--application my-app"},
		},
		{
			name: "plan with bare app name",
			body: "lemuria plan my-app",
			want: &Command{Name: CommandPlan, Application: "my-app", RawArgs: "my-app"},
		},

		// Boolean flags
		{
			name: "sync with --all",
			body: "lemuria sync --all",
			want: &Command{Name: CommandSync, All: true, RawArgs: "--all"},
		},
		{
			name: "sync with --prune",
			body: "lemuria sync --prune",
			want: &Command{Name: CommandSync, Prune: true, RawArgs: "--prune"},
		},
		{
			name: "sync with --dry-run",
			body: "lemuria sync --dry-run",
			want: &Command{Name: CommandSync, DryRun: true, RawArgs: "--dry-run"},
		},

		// Multiple flags
		{
			name: "sync with app and prune",
			body: "lemuria sync -a my-app --prune",
			want: &Command{Name: CommandSync, Application: "my-app", Prune: true, RawArgs: "-a my-app --prune"},
		},

		// Rollback with --id
		{
			name: "rollback with --id",
			body: "lemuria rollback -a my-app --id 5",
			want: &Command{Name: CommandRollback, Application: "my-app", HistoryID: 5, RawArgs: "-a my-app --id 5"},
		},

		// Multi-line comments
		{
			name: "command in multi-line comment",
			body: "Looks good!\nlemuria sync\nThanks!",
			want: &Command{Name: CommandSync},
		},
		{
			name: "command in multi-line with leading whitespace",
			body: "Some text\n  lemuria plan -a my-app\n",
			want: &Command{Name: CommandPlan, Application: "my-app", RawArgs: "-a my-app"},
		},

		// Not a command
		{
			name:    "not a command",
			body:    "this is just a regular comment",
			wantErr: true,
		},
		{
			name:    "empty body",
			body:    "",
			wantErr: true,
		},
		{
			name:    "unknown command",
			body:    "lemuria deploy",
			wantErr: true,
		},
		{
			name:    "partial match",
			body:    "lemuria",
			wantErr: true,
		},

		// Quoted arguments
		{
			name: "app name in quotes",
			body: `lemuria plan -a "my app"`,
			want: &Command{Name: CommandPlan, Application: "my app", RawArgs: `-a "my app"`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.body)
			if (err != nil) != tt.wantErr {
				t.Errorf("Parse() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}
			if got.Name != tt.want.Name {
				t.Errorf("Parse() Name = %v, want %v", got.Name, tt.want.Name)
			}
			if got.Application != tt.want.Application {
				t.Errorf("Parse() Application = %v, want %v", got.Application, tt.want.Application)
			}
			if got.All != tt.want.All {
				t.Errorf("Parse() All = %v, want %v", got.All, tt.want.All)
			}
			if got.Prune != tt.want.Prune {
				t.Errorf("Parse() Prune = %v, want %v", got.Prune, tt.want.Prune)
			}
			if got.DryRun != tt.want.DryRun {
				t.Errorf("Parse() DryRun = %v, want %v", got.DryRun, tt.want.DryRun)
			}
			if got.HistoryID != tt.want.HistoryID {
				t.Errorf("Parse() HistoryID = %v, want %v", got.HistoryID, tt.want.HistoryID)
			}
			if tt.want.RawArgs != "" && got.RawArgs != tt.want.RawArgs {
				t.Errorf("Parse() RawArgs = %v, want %v", got.RawArgs, tt.want.RawArgs)
			}
		})
	}
}

func TestCommandString(t *testing.T) {
	tests := []struct {
		name string
		cmd  Command
		want string
	}{
		{
			name: "simple plan",
			cmd:  Command{Name: CommandPlan},
			want: "lemuria plan",
		},
		{
			name: "sync with app",
			cmd:  Command{Name: CommandSync, Application: "my-app"},
			want: "lemuria sync -a my-app",
		},
		{
			name: "sync with all flags",
			cmd:  Command{Name: CommandSync, Application: "my-app", All: true, Prune: true, DryRun: true},
			want: "lemuria sync -a my-app --all --prune --dry-run",
		},
		{
			name: "rollback with id",
			cmd:  Command{Name: CommandRollback, Application: "my-app", HistoryID: 5},
			want: "lemuria rollback -a my-app --id 5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cmd.String(); got != tt.want {
				t.Errorf("Command.String() = %v, want %v", got, tt.want)
			}
		})
	}
}
