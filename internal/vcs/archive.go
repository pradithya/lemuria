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
	"compress/gzip"
	"io"
	"log/slog"
	"path"
	"strings"
)

// ExtractFilesFromTarGz reads a gzipped tar stream and extracts the contents
// of the requested files. Both GitHub and GitLab prefix archive entries with a
// top-level directory (e.g. "owner-repo-sha/"), so the first path component is
// stripped before matching against paths.
//
// Files not found in the archive are simply absent from the returned map.
// The function returns early once all requested files have been found.
func ExtractFilesFromTarGz(r io.Reader, paths []string) (map[string][]byte, error) {
	if len(paths) == 0 {
		return map[string][]byte{}, nil
	}

	wanted := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		wanted[p] = struct{}{}
	}

	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, err
	}
	defer func() {
		err := gz.Close()
		if err != nil {
			slog.Error("error closing tar file")
		}
	}()

	tr := tar.NewReader(gz)
	result := make(map[string][]byte, len(paths))

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return result, err
		}

		if hdr.Typeflag != tar.TypeReg {
			continue
		}

		// Strip the first path component (archive root directory).
		stripped := stripFirstComponent(hdr.Name)
		if stripped == "" {
			continue
		}

		if _, ok := wanted[stripped]; !ok {
			continue
		}

		data, err := io.ReadAll(tr)
		if err != nil {
			return result, err
		}
		result[stripped] = data

		if len(result) == len(wanted) {
			break
		}
	}

	return result, nil
}

// ExtractFilesByPattern reads a gzipped tar stream and extracts files matching
// the given filename patterns (e.g. "*.yaml", "*.yml") and optionally limited
// to specific path prefixes (e.g. "argocd/", "apps/"). Both GitHub and GitLab
// prefix archive entries with a top-level directory, so the first path component
// is stripped before matching.
//
// If patterns is empty, all files are extracted. If pathPrefixes is empty, no
// prefix filtering is applied. Returns a map of stripped path → file content.
func ExtractFilesByPattern(r io.Reader, patterns []string, pathPrefixes []string) (map[string][]byte, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, err
	}
	defer func() {
		if cerr := gz.Close(); cerr != nil {
			slog.Error("error closing gzip reader", "error", cerr)
		}
	}()

	tr := tar.NewReader(gz)
	result := make(map[string][]byte)

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return result, err
		}

		if hdr.Typeflag != tar.TypeReg {
			continue
		}

		stripped := stripFirstComponent(hdr.Name)
		if stripped == "" {
			continue
		}

		// Check path prefix filter
		if len(pathPrefixes) > 0 && !matchesAnyPrefix(stripped, pathPrefixes) {
			continue
		}

		// Check filename pattern filter
		if len(patterns) > 0 && !matchesAnyPattern(path.Base(stripped), patterns) {
			continue
		}

		data, err := io.ReadAll(tr)
		if err != nil {
			return result, err
		}
		result[stripped] = data
	}

	return result, nil
}

// matchesAnyPrefix checks if the path starts with any of the given prefixes.
func matchesAnyPrefix(filePath string, prefixes []string) bool {
	for _, prefix := range prefixes {
		p := prefix
		if !strings.HasSuffix(p, "/") {
			p += "/"
		}
		if strings.HasPrefix(filePath, p) || filePath == strings.TrimSuffix(p, "/") {
			return true
		}
	}
	return false
}

// matchesAnyPattern checks if the filename matches any of the given glob patterns.
// Uses path.Match (not filepath.Match) since tar archives always use forward slashes.
func matchesAnyPattern(filename string, patterns []string) bool {
	for _, pattern := range patterns {
		matched, err := path.Match(pattern, filename)
		if err != nil {
			slog.Warn("invalid glob pattern", "pattern", pattern, "error", err)
			continue
		}
		if matched {
			return true
		}
	}
	return false
}

// stripFirstComponent removes the first path component from name.
// For example "owner-repo-abc123/dir/file.yaml" becomes "dir/file.yaml".
func stripFirstComponent(name string) string {
	clean := path.Clean(name)
	idx := strings.IndexByte(clean, '/')
	if idx < 0 || idx == len(clean)-1 {
		return ""
	}
	return clean[idx+1:]
}
