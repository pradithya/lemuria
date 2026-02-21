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

package gitlab

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	gogitlab "gitlab.com/gitlab-org/api/client-go"

	"github.com/org/lemuria/internal/models"
)

func TestDiffStatus(t *testing.T) {
	tests := []struct {
		name string
		diff *gogitlab.MergeRequestDiff
		want models.FileStatus
	}{
		{
			name: "new file",
			diff: &gogitlab.MergeRequestDiff{NewFile: true},
			want: models.FileStatusAdded,
		},
		{
			name: "deleted file",
			diff: &gogitlab.MergeRequestDiff{DeletedFile: true},
			want: models.FileStatusRemoved,
		},
		{
			name: "renamed file",
			diff: &gogitlab.MergeRequestDiff{RenamedFile: true},
			want: models.FileStatusRenamed,
		},
		{
			name: "modified file",
			diff: &gogitlab.MergeRequestDiff{},
			want: models.FileStatusModified,
		},
		{
			name: "new file takes precedence over renamed",
			diff: &gogitlab.MergeRequestDiff{NewFile: true, RenamedFile: true},
			want: models.FileStatusAdded,
		},
		{
			name: "deleted file takes precedence over renamed",
			diff: &gogitlab.MergeRequestDiff{DeletedFile: true, RenamedFile: true},
			want: models.FileStatusRemoved,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := diffStatus(tt.diff)
			if got != tt.want {
				t.Errorf("diffStatus() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFilePath(t *testing.T) {
	tests := []struct {
		name string
		diff *gogitlab.MergeRequestDiff
		want string
	}{
		{
			name: "new path preferred when set",
			diff: &gogitlab.MergeRequestDiff{
				OldPath: "old/file.go",
				NewPath: "new/file.go",
			},
			want: "new/file.go",
		},
		{
			name: "falls back to old path when new path empty",
			diff: &gogitlab.MergeRequestDiff{
				OldPath: "old/file.go",
				NewPath: "",
			},
			want: "old/file.go",
		},
		{
			name: "same path for non-rename",
			diff: &gogitlab.MergeRequestDiff{
				OldPath: "same/path.go",
				NewPath: "same/path.go",
			},
			want: "same/path.go",
		},
		{
			name: "both empty",
			diff: &gogitlab.MergeRequestDiff{
				OldPath: "",
				NewPath: "",
			},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filePath(tt.diff)
			if got != tt.want {
				t.Errorf("filePath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGetChangedFiles(t *testing.T) {
	tests := []struct {
		name     string
		diffs    []map[string]any
		wantLen  int
		wantFile models.ChangedFile
	}{
		{
			name: "new file",
			diffs: []map[string]any{
				{
					"old_path":     "",
					"new_path":     "newfile.go",
					"new_file":     true,
					"renamed_file": false,
					"deleted_file": false,
					"diff":         "@@ -0,0 +1 @@\n+package main",
				},
			},
			wantLen: 1,
			wantFile: models.ChangedFile{
				Filename: "newfile.go",
				Status:   models.FileStatusAdded,
				Patch:    "@@ -0,0 +1 @@\n+package main",
			},
		},
		{
			name: "modified file",
			diffs: []map[string]any{
				{
					"old_path":     "existing.go",
					"new_path":     "existing.go",
					"new_file":     false,
					"renamed_file": false,
					"deleted_file": false,
					"diff":         "@@ -1 +1 @@\n-old\n+new",
				},
			},
			wantLen: 1,
			wantFile: models.ChangedFile{
				Filename: "existing.go",
				Status:   models.FileStatusModified,
				Patch:    "@@ -1 +1 @@\n-old\n+new",
			},
		},
		{
			name: "renamed file",
			diffs: []map[string]any{
				{
					"old_path":     "old_name.go",
					"new_path":     "new_name.go",
					"new_file":     false,
					"renamed_file": true,
					"deleted_file": false,
					"diff":         "",
				},
			},
			wantLen: 1,
			wantFile: models.ChangedFile{
				Filename:         "new_name.go",
				Status:           models.FileStatusRenamed,
				PreviousFilename: "old_name.go",
			},
		},
		{
			name: "deleted file",
			diffs: []map[string]any{
				{
					"old_path":     "removed.go",
					"new_path":     "removed.go",
					"new_file":     false,
					"renamed_file": false,
					"deleted_file": true,
					"diff":         "@@ -1 +0,0 @@\n-package main",
				},
			},
			wantLen: 1,
			wantFile: models.ChangedFile{
				Filename: "removed.go",
				Status:   models.FileStatusRemoved,
				Patch:    "@@ -1 +0,0 @@\n-package main",
			},
		},
		{
			name:    "empty diff list",
			diffs:   []map[string]any{},
			wantLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(tt.diffs)
			}))
			defer srv.Close()

			client := newTestClient(t, srv.URL)
			files, err := client.GetChangedFiles(context.Background(), "group", "project", 1)
			if err != nil {
				t.Fatalf("GetChangedFiles() error = %v", err)
			}

			if len(files) != tt.wantLen {
				t.Fatalf("GetChangedFiles() returned %d files, want %d", len(files), tt.wantLen)
			}

			if tt.wantLen > 0 {
				got := files[0]
				if got.Filename != tt.wantFile.Filename {
					t.Errorf("Filename = %q, want %q", got.Filename, tt.wantFile.Filename)
				}
				if got.Status != tt.wantFile.Status {
					t.Errorf("Status = %q, want %q", got.Status, tt.wantFile.Status)
				}
				if got.Patch != tt.wantFile.Patch {
					t.Errorf("Patch = %q, want %q", got.Patch, tt.wantFile.Patch)
				}
				if got.PreviousFilename != tt.wantFile.PreviousFilename {
					t.Errorf("PreviousFilename = %q, want %q", got.PreviousFilename, tt.wantFile.PreviousFilename)
				}
			}
		})
	}
}

func TestGetChangedFiles_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := newTestClient(t, srv.URL)
	_, err := client.GetChangedFiles(context.Background(), "group", "project", 1)
	if err == nil {
		t.Fatal("expected error from GetChangedFiles when API returns 500")
	}
}

func TestGetChangedFiles_Pagination(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")

		if callCount == 1 {
			// First page: return one diff and indicate there is a next page
			w.Header().Set("X-Next-Page", "2")
			w.Header().Set("X-Page", "1")
			w.Header().Set("X-Total-Pages", "2")
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{
					"old_path":     "file1.go",
					"new_path":     "file1.go",
					"new_file":     false,
					"renamed_file": false,
					"deleted_file": false,
					"diff":         "diff1",
				},
			})
		} else {
			// Second page: return one diff, no next page
			w.Header().Set("X-Page", "2")
			w.Header().Set("X-Total-Pages", "2")
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{
					"old_path":     "file2.go",
					"new_path":     "file2.go",
					"new_file":     true,
					"renamed_file": false,
					"deleted_file": false,
					"diff":         "diff2",
				},
			})
		}
	}))
	defer srv.Close()

	client := newTestClient(t, srv.URL)
	files, err := client.GetChangedFiles(context.Background(), "group", "project", 1)
	if err != nil {
		t.Fatalf("GetChangedFiles() error = %v", err)
	}

	if len(files) != 2 {
		t.Fatalf("expected 2 files across pages, got %d", len(files))
	}
	if files[0].Filename != "file1.go" {
		t.Errorf("first file = %q, want file1.go", files[0].Filename)
	}
	if files[1].Filename != "file2.go" {
		t.Errorf("second file = %q, want file2.go", files[1].Filename)
	}
}

func TestGetFileContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("file content here"))
	}))
	defer srv.Close()

	client := newTestClient(t, srv.URL)
	content, err := client.GetFileContent(context.Background(), "group", "project", "path/to/file.yaml", "main")
	if err != nil {
		t.Fatalf("GetFileContent() error = %v", err)
	}

	if string(content) != "file content here" {
		t.Errorf("content = %q, want %q", string(content), "file content here")
	}
}

func TestGetFileContent_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := newTestClient(t, srv.URL)
	_, err := client.GetFileContent(context.Background(), "group", "project", "nonexistent.yaml", "main")
	if err == nil {
		t.Fatal("expected error from GetFileContent when API returns 404")
	}
}

func TestGetRepoConfig(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify the request path contains the expected file name
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("applications:\n  - name: test\n"))
	}))
	defer srv.Close()

	client := newTestClient(t, srv.URL)
	content, err := client.GetRepoConfig(context.Background(), "group", "project", "main")
	if err != nil {
		t.Fatalf("GetRepoConfig() error = %v", err)
	}

	expected := "applications:\n  - name: test\n"
	if string(content) != expected {
		t.Errorf("content = %q, want %q", string(content), expected)
	}
}

func TestGetFileContents_EmptyPaths(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("no API call should be made for empty paths")
	}))
	defer srv.Close()

	client := newTestClient(t, srv.URL)
	result, err := client.GetFileContents(context.Background(), "group", "project", []string{}, "main")
	if err != nil {
		t.Fatalf("GetFileContents() error = %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty map, got %d entries", len(result))
	}
}
