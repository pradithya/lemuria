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

package vcs

import (
	"testing"

	"github.com/org/lemuria/internal/models"
)

func TestGetAllFilePaths(t *testing.T) {
	tests := []struct {
		name  string
		files []models.ChangedFile
		want  []string
	}{
		{
			name: "normal files without renames",
			files: []models.ChangedFile{
				{Filename: "apps/app-a.yaml", Status: models.FileStatusModified},
				{Filename: "apps/app-b.yaml", Status: models.FileStatusAdded},
			},
			want: []string{"apps/app-a.yaml", "apps/app-b.yaml"},
		},
		{
			name: "renamed file includes both Filename and PreviousFilename",
			files: []models.ChangedFile{
				{
					Filename:         "apps/new-name.yaml",
					PreviousFilename: "apps/old-name.yaml",
					Status:           models.FileStatusRenamed,
				},
			},
			want: []string{"apps/new-name.yaml", "apps/old-name.yaml"},
		},
		{
			name: "deduplicates paths",
			files: []models.ChangedFile{
				{Filename: "apps/app.yaml", Status: models.FileStatusModified},
				{
					Filename:         "apps/app.yaml",
					PreviousFilename: "apps/old.yaml",
					Status:           models.FileStatusRenamed,
				},
			},
			want: []string{"apps/app.yaml", "apps/old.yaml"},
		},
		{
			name:  "empty input returns nil",
			files: []models.ChangedFile{},
			want:  nil,
		},
		{
			name: "PreviousFilename ignored for non-renamed status",
			files: []models.ChangedFile{
				{
					Filename:         "apps/app.yaml",
					PreviousFilename: "apps/old.yaml",
					Status:           models.FileStatusModified,
				},
			},
			want: []string{"apps/app.yaml"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetAllFilePaths(tt.files)
			if len(got) != len(tt.want) {
				t.Fatalf("GetAllFilePaths() returned %d paths, want %d\ngot:  %v\nwant: %v", len(got), len(tt.want), got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("GetAllFilePaths()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}
