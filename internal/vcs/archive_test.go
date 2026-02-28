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
	"archive/tar"
	"bytes"
	"compress/gzip"
	"testing"
)

// buildTarGz creates a gzipped tar archive with the given files.
func buildTarGz(t *testing.T, files map[string]string) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	for name, content := range files {
		hdr := &tar.Header{
			Name:     name,
			Mode:     0o644,
			Size:     int64(len(content)),
			Typeflag: tar.TypeReg,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("writing header for %s: %v", name, err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatalf("writing content for %s: %v", name, err)
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatalf("closing tar writer: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("closing gzip writer: %v", err)
	}
	return &buf
}

func TestExtractFilesFromTarGz(t *testing.T) {
	archive := buildTarGz(t, map[string]string{
		"repo-abc123/apps/app.yaml":    "app-content",
		"repo-abc123/apps/values.yaml": "values-content",
		"repo-abc123/README.md":        "readme",
	})

	result, err := ExtractFilesFromTarGz(archive, []string{"apps/app.yaml", "README.md"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if string(result["apps/app.yaml"]) != "app-content" {
		t.Errorf("app.yaml content = %q, want 'app-content'", result["apps/app.yaml"])
	}
	if string(result["README.md"]) != "readme" {
		t.Errorf("README.md content = %q, want 'readme'", result["README.md"])
	}
}

func TestExtractFilesFromTarGz_EmptyPaths(t *testing.T) {
	result, err := ExtractFilesFromTarGz(bytes.NewReader(nil), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty result, got %d files", len(result))
	}
}

func TestExtractFilesByPattern(t *testing.T) {
	tests := []struct {
		name         string
		files        map[string]string
		patterns     []string
		prefixes     []string
		wantPaths    []string
		wantNotPaths []string
	}{
		{
			name: "yaml pattern matches yaml files",
			files: map[string]string{
				"repo-abc/apps/app.yaml":   "yaml1",
				"repo-abc/apps/values.yml": "yaml2",
				"repo-abc/apps/readme.md":  "md",
				"repo-abc/config/app.json": "json",
			},
			patterns:     []string{"*.yaml", "*.yml"},
			prefixes:     nil,
			wantPaths:    []string{"apps/app.yaml", "apps/values.yml"},
			wantNotPaths: []string{"apps/readme.md", "config/app.json"},
		},
		{
			name: "prefix filter limits scan",
			files: map[string]string{
				"repo-abc/argocd/app.yaml": "app",
				"repo-abc/apps/svc.yaml":   "svc",
				"repo-abc/other/cfg.yaml":  "cfg",
			},
			patterns:     []string{"*.yaml"},
			prefixes:     []string{"argocd"},
			wantPaths:    []string{"argocd/app.yaml"},
			wantNotPaths: []string{"apps/svc.yaml", "other/cfg.yaml"},
		},
		{
			name: "multiple prefixes",
			files: map[string]string{
				"repo-abc/argocd/app.yaml": "a",
				"repo-abc/apps/svc.yaml":   "b",
				"repo-abc/other/cfg.yaml":  "c",
			},
			patterns:     []string{"*.yaml"},
			prefixes:     []string{"argocd", "apps"},
			wantPaths:    []string{"argocd/app.yaml", "apps/svc.yaml"},
			wantNotPaths: []string{"other/cfg.yaml"},
		},
		{
			name: "no patterns extracts all files",
			files: map[string]string{
				"repo-abc/a.yaml": "a",
				"repo-abc/b.txt":  "b",
			},
			patterns:  nil,
			prefixes:  nil,
			wantPaths: []string{"a.yaml", "b.txt"},
		},
		{
			name: "no matching files",
			files: map[string]string{
				"repo-abc/readme.md": "md",
			},
			patterns:     []string{"*.yaml"},
			prefixes:     nil,
			wantPaths:    nil,
			wantNotPaths: []string{"readme.md"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			archive := buildTarGz(t, tt.files)
			result, err := ExtractFilesByPattern(archive, tt.patterns, tt.prefixes)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			for _, p := range tt.wantPaths {
				if _, ok := result[p]; !ok {
					t.Errorf("expected path %q in result, not found", p)
				}
			}
			for _, p := range tt.wantNotPaths {
				if _, ok := result[p]; ok {
					t.Errorf("path %q should not be in result", p)
				}
			}
		})
	}
}

func TestMatchesAnyPrefix(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		prefixes []string
		want     bool
	}{
		{"matches prefix with slash", "argocd/app.yaml", []string{"argocd"}, true},
		{"matches prefix already with slash", "argocd/app.yaml", []string{"argocd/"}, true},
		{"no match", "apps/app.yaml", []string{"argocd"}, false},
		{"nested match", "argocd/nested/app.yaml", []string{"argocd"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchesAnyPrefix(tt.path, tt.prefixes); got != tt.want {
				t.Errorf("matchesAnyPrefix(%q, %v) = %v, want %v", tt.path, tt.prefixes, got, tt.want)
			}
		})
	}
}

func TestMatchesAnyPattern(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		patterns []string
		want     bool
	}{
		{"yaml match", "app.yaml", []string{"*.yaml"}, true},
		{"yml match", "app.yml", []string{"*.yml"}, true},
		{"multi-pattern", "app.yaml", []string{"*.yml", "*.yaml"}, true},
		{"no match", "app.json", []string{"*.yaml", "*.yml"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchesAnyPattern(tt.filename, tt.patterns); got != tt.want {
				t.Errorf("matchesAnyPattern(%q, %v) = %v, want %v", tt.filename, tt.patterns, got, tt.want)
			}
		})
	}
}
