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
	"path/filepath"
	"strings"

	"github.com/org/lemuria/internal/models"
)

// MatchesPath checks if a file path matches a glob pattern.
func MatchesPath(pattern, path string) bool {
	// Handle ** patterns
	if strings.Contains(pattern, "**") {
		// Convert ** to regex-like matching
		parts := strings.Split(pattern, "**")
		if len(parts) == 2 {
			prefix := parts[0]
			suffix := strings.TrimPrefix(parts[1], "/")

			if !strings.HasPrefix(path, prefix) {
				return false
			}

			if suffix == "" {
				return true
			}

			remaining := strings.TrimPrefix(path, prefix)
			// Check if any path segment matches the suffix pattern
			segments := strings.Split(remaining, "/")
			for i := range segments {
				subPath := strings.Join(segments[i:], "/")
				if matched, _ := filepath.Match(suffix, subPath); matched {
					return true
				}
				// Also try matching just the filename
				if matched, _ := filepath.Match(suffix, segments[len(segments)-1]); matched {
					return true
				}
			}
			return false
		}
	}

	// Standard glob matching
	matched, _ := filepath.Match(pattern, path)
	return matched
}

// FilterFilesByPatterns returns files that match any of the given patterns.
func FilterFilesByPatterns(files []models.ChangedFile, patterns []string) []models.ChangedFile {
	var matched []models.ChangedFile
	seen := make(map[string]bool)

	for _, f := range files {
		for _, pattern := range patterns {
			if MatchesPath(pattern, f.Filename) && !seen[f.Filename] {
				matched = append(matched, f)
				seen[f.Filename] = true
				break
			}
		}
	}

	return matched
}

// GetFilePaths extracts just the file paths from changed files.
func GetFilePaths(files []models.ChangedFile) []string {
	paths := make([]string, len(files))
	for i, f := range files {
		paths[i] = f.Filename
	}
	return paths
}

// IsYAMLFile checks if a filename has a YAML extension.
func IsYAMLFile(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	return ext == ".yaml" || ext == ".yml"
}
