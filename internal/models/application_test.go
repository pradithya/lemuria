package models

import (
	"testing"
)

func TestResourceKeyString(t *testing.T) {
	tests := []struct {
		name string
		key  ResourceKey
		want string
	}{
		{
			name: "with namespace",
			key:  ResourceKey{Kind: "Deployment", Name: "my-app", Namespace: "default"},
			want: "default/Deployment/my-app",
		},
		{
			name: "without namespace",
			key:  ResourceKey{Kind: "ClusterRole", Name: "admin"},
			want: "ClusterRole/admin",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.key.String(); got != tt.want {
				t.Errorf("ResourceKey.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestApplicationIsMultiSource(t *testing.T) {
	tests := []struct {
		name string
		app  Application
		want bool
	}{
		{
			name: "single source",
			app:  Application{RepoURL: "https://github.com/org/repo"},
			want: false,
		},
		{
			name: "multi source",
			app: Application{
				Sources: []ApplicationSource{
					{RepoURL: "https://github.com/org/repo"},
					{RepoURL: "https://charts.example.com"},
				},
			},
			want: true,
		},
		{
			name: "empty sources",
			app:  Application{},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.app.IsMultiSource(); got != tt.want {
				t.Errorf("IsMultiSource() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestApplicationGetRepoURLs(t *testing.T) {
	tests := []struct {
		name string
		app  Application
		want []string
	}{
		{
			name: "single source",
			app:  Application{RepoURL: "https://github.com/org/repo"},
			want: []string{"https://github.com/org/repo"},
		},
		{
			name: "multi source",
			app: Application{
				Sources: []ApplicationSource{
					{RepoURL: "https://github.com/org/repo"},
					{RepoURL: "https://charts.example.com"},
				},
			},
			want: []string{"https://github.com/org/repo", "https://charts.example.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.app.GetRepoURLs()
			if len(got) != len(tt.want) {
				t.Errorf("GetRepoURLs() returned %d URLs, want %d", len(got), len(tt.want))
				return
			}
			for i, u := range got {
				if u != tt.want[i] {
					t.Errorf("GetRepoURLs()[%d] = %q, want %q", i, u, tt.want[i])
				}
			}
		})
	}
}

func TestApplicationChangeTypeHelpers(t *testing.T) {
	tests := []struct {
		name       string
		changeType ApplicationChangeType
		isNew      bool
		isDeleted  bool
		isExisting bool
	}{
		{"new", ApplicationNew, true, false, false},
		{"deleted", ApplicationDeleted, false, true, false},
		{"existing", ApplicationExisting, false, false, true},
		{"empty (treated as existing)", "", false, false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := &Application{ChangeType: tt.changeType}
			if got := app.IsNew(); got != tt.isNew {
				t.Errorf("IsNew() = %v, want %v", got, tt.isNew)
			}
			if got := app.IsDeleted(); got != tt.isDeleted {
				t.Errorf("IsDeleted() = %v, want %v", got, tt.isDeleted)
			}
			if got := app.IsExisting(); got != tt.isExisting {
				t.Errorf("IsExisting() = %v, want %v", got, tt.isExisting)
			}
		})
	}
}

func TestApplicationHasAutoSync(t *testing.T) {
	tests := []struct {
		name    string
		enabled bool
		want    bool
	}{
		{"enabled", true, true},
		{"disabled", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := &Application{AutoSyncEnabled: tt.enabled}
			if got := app.HasAutoSync(); got != tt.want {
				t.Errorf("HasAutoSync() = %v, want %v", got, tt.want)
			}
		})
	}
}
