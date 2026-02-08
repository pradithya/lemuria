package argocd

import (
	"testing"
)

func TestNormalizeRepoURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{
			name: "https URL",
			url:  "https://github.com/org/repo",
			want: "github.com/org/repo",
		},
		{
			name: "https URL with .git",
			url:  "https://github.com/org/repo.git",
			want: "github.com/org/repo",
		},
		{
			name: "http URL",
			url:  "http://github.com/org/repo",
			want: "github.com/org/repo",
		},
		{
			name: "ssh URL",
			url:  "git@github.com:org/repo.git",
			want: "github.com/org/repo",
		},
		{
			name: "ssh URL without .git",
			url:  "git@github.com:org/repo",
			want: "github.com/org/repo",
		},
		{
			name: "case insensitive",
			url:  "HTTPS://GitHub.COM/Org/Repo",
			want: "github.com/org/repo",
		},
		{
			name: "already normalized",
			url:  "github.com/org/repo",
			want: "github.com/org/repo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeRepoURL(tt.url); got != tt.want {
				t.Errorf("NormalizeRepoURL(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}
