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

package github

import (
	"testing"

	"github.com/org/lemuria/internal/models"
)

func TestMatchesPath(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		path    string
		want    bool
	}{
		// Standard glob matching
		{
			name:    "exact match",
			pattern: "apps/my-app/deployment.yaml",
			path:    "apps/my-app/deployment.yaml",
			want:    true,
		},
		{
			name:    "single wildcard",
			pattern: "apps/my-app/*.yaml",
			path:    "apps/my-app/deployment.yaml",
			want:    true,
		},
		{
			name:    "single wildcard miss",
			pattern: "apps/my-app/*.yaml",
			path:    "apps/other-app/deployment.yaml",
			want:    false,
		},

		// Double star patterns
		{
			name:    "double star matches nested paths",
			pattern: "apps/**",
			path:    "apps/my-app/deployment.yaml",
			want:    true,
		},
		{
			name:    "double star with suffix",
			pattern: "apps/**/*.yaml",
			path:    "apps/my-app/deployment.yaml",
			want:    true,
		},
		{
			name:    "double star prefix mismatch",
			pattern: "apps/**",
			path:    "other/my-app/deployment.yaml",
			want:    false,
		},
		{
			name:    "double star deeply nested",
			pattern: "apps/**",
			path:    "apps/a/b/c/d.yaml",
			want:    true,
		},

		// Edge cases
		{
			name:    "empty pattern",
			pattern: "",
			path:    "file.yaml",
			want:    false,
		},
		{
			name:    "root wildcard",
			pattern: "*.yaml",
			path:    "deployment.yaml",
			want:    true,
		},
		{
			name:    "root wildcard no match nested",
			pattern: "*.yaml",
			path:    "apps/deployment.yaml",
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MatchesPath(tt.pattern, tt.path); got != tt.want {
				t.Errorf("MatchesPath(%q, %q) = %v, want %v", tt.pattern, tt.path, got, tt.want)
			}
		})
	}
}

func TestFilterFilesByPatterns(t *testing.T) {
	allFiles := []models.ChangedFile{
		{Filename: "apps/frontend/deployment.yaml", Status: models.FileStatusModified},
		{Filename: "apps/backend/deployment.yaml", Status: models.FileStatusModified},
		{Filename: "apps/backend/service.yaml", Status: models.FileStatusAdded},
		{Filename: "README.md", Status: models.FileStatusModified},
		{Filename: "base/kustomization.yaml", Status: models.FileStatusModified},
	}

	tests := []struct {
		name     string
		files    []models.ChangedFile
		patterns []string
		want     int
		wantName string // first match name
	}{
		{
			name:     "match frontend app",
			files:    allFiles,
			patterns: []string{"apps/frontend/**"},
			want:     1,
			wantName: "apps/frontend/deployment.yaml",
		},
		{
			name:     "match all backend files",
			files:    allFiles,
			patterns: []string{"apps/backend/**"},
			want:     2,
		},
		{
			name:     "match all apps",
			files:    allFiles,
			patterns: []string{"apps/**"},
			want:     3,
		},
		{
			name:     "multiple patterns",
			files:    allFiles,
			patterns: []string{"apps/frontend/**", "base/**"},
			want:     2,
		},
		{
			name:     "no matches",
			files:    allFiles,
			patterns: []string{"other/**"},
			want:     0,
		},
		{
			name:     "empty files",
			files:    nil,
			patterns: []string{"**"},
			want:     0,
		},
		{
			name:     "empty patterns",
			files:    allFiles,
			patterns: nil,
			want:     0,
		},
		{
			name:     "deduplication",
			files:    allFiles,
			patterns: []string{"apps/**", "apps/backend/**"},
			want:     3, // no duplicates
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FilterFilesByPatterns(tt.files, tt.patterns)
			if len(got) != tt.want {
				t.Errorf("FilterFilesByPatterns() returned %d files, want %d", len(got), tt.want)
			}
			if tt.wantName != "" && len(got) > 0 && got[0].Filename != tt.wantName {
				t.Errorf("FilterFilesByPatterns() first file = %q, want %q", got[0].Filename, tt.wantName)
			}
		})
	}
}

func TestGetFilePaths(t *testing.T) {
	tests := []struct {
		name  string
		files []models.ChangedFile
		want  []string
	}{
		{
			name:  "empty",
			files: nil,
			want:  []string{},
		},
		{
			name: "multiple files",
			files: []models.ChangedFile{
				{Filename: "a.yaml"},
				{Filename: "b.yaml"},
				{Filename: "c.yaml"},
			},
			want: []string{"a.yaml", "b.yaml", "c.yaml"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetFilePaths(tt.files)
			if len(got) != len(tt.want) {
				t.Errorf("GetFilePaths() returned %d paths, want %d", len(got), len(tt.want))
				return
			}
			for i, p := range got {
				if p != tt.want[i] {
					t.Errorf("GetFilePaths()[%d] = %q, want %q", i, p, tt.want[i])
				}
			}
		})
	}
}

func TestIsYAMLFile(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		want     bool
	}{
		{"yaml extension", "deployment.yaml", true},
		{"yml extension", "service.yml", true},
		{"YAML uppercase", "config.YAML", true},
		{"YML uppercase", "values.YML", true},
		{"json file", "config.json", false},
		{"go file", "main.go", false},
		{"no extension", "Makefile", false},
		{"yaml in path", "apps/my-app/deploy.yaml", true},
		{"markdown", "README.md", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsYAMLFile(tt.filename); got != tt.want {
				t.Errorf("IsYAMLFile(%q) = %v, want %v", tt.filename, got, tt.want)
			}
		})
	}
}
