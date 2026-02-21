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
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/google/go-github/v60/github"

	"github.com/org/lemuria/internal/models"
	"github.com/org/lemuria/internal/vcs"
)

// GetChangedFiles returns the list of files changed in a PR.
func (c *Client) GetChangedFiles(ctx context.Context, owner, repo string, number int) ([]models.ChangedFile, error) {
	client, err := c.getInstallationClient(ctx, owner)
	if err != nil {
		return nil, err
	}

	opts := &github.ListOptions{PerPage: 100}
	var allFiles []models.ChangedFile

	for {
		files, resp, err := client.PullRequests.ListFiles(ctx, owner, repo, number, opts)
		if err != nil {
			return nil, fmt.Errorf("listing PR files: %w", err)
		}

		for _, f := range files {
			allFiles = append(allFiles, models.ChangedFile{
				Filename:         f.GetFilename(),
				PreviousFilename: f.GetPreviousFilename(),
				Status:           models.FileStatus(f.GetStatus()),
				Additions:        f.GetAdditions(),
				Deletions:        f.GetDeletions(),
				Changes:          f.GetChanges(),
				Patch:            f.GetPatch(),
			})
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return allFiles, nil
}

// MatchesPath checks if a file path matches a glob pattern.
// Deprecated: Use vcs.MatchesPath instead.
func MatchesPath(pattern, path string) bool {
	return vcs.MatchesPath(pattern, path)
}

// FilterFilesByPatterns returns files that match any of the given patterns.
// Deprecated: Use vcs.FilterFilesByPatterns instead.
func FilterFilesByPatterns(files []models.ChangedFile, patterns []string) []models.ChangedFile {
	return vcs.FilterFilesByPatterns(files, patterns)
}

// GetFilePaths extracts just the file paths from changed files.
// Deprecated: Use vcs.GetFilePaths instead.
func GetFilePaths(files []models.ChangedFile) []string {
	return vcs.GetFilePaths(files)
}

// GetFileContent retrieves the content of a file at a specific ref (branch, tag, or commit SHA).
func (c *Client) GetFileContent(ctx context.Context, owner, repo, path, ref string) ([]byte, error) {
	client, err := c.getInstallationClient(ctx, owner)
	if err != nil {
		return nil, err
	}

	content, _, _, err := client.Repositories.GetContents(ctx, owner, repo, path, &github.RepositoryContentGetOptions{
		Ref: ref,
	})
	if err != nil {
		return nil, fmt.Errorf("getting file content for %s: %w", path, err)
	}

	decoded, err := content.GetContent()
	if err != nil {
		return nil, fmt.Errorf("decoding file content: %w", err)
	}

	return []byte(decoded), nil
}

// GetFileContents retrieves the contents of multiple files at a specific ref
// by downloading a tarball archive (single HTTP call) and extracting the
// requested paths in-memory.
func (c *Client) GetFileContents(ctx context.Context, owner, repo string, paths []string, ref string) (map[string][]byte, error) {
	if len(paths) == 0 {
		return map[string][]byte{}, nil
	}

	client, err := c.getInstallationClient(ctx, owner)
	if err != nil {
		return nil, err
	}

	archiveURL, _, err := client.Repositories.GetArchiveLink(ctx, owner, repo, github.Tarball, &github.RepositoryContentGetOptions{
		Ref: ref,
	}, 3)
	if err != nil {
		return nil, fmt.Errorf("getting archive link: %w", err)
	}

	resp, err := http.Get(archiveURL.String())
	if err != nil {
		return nil, fmt.Errorf("downloading archive: %w", err)
	}
	defer func() {
		err := resp.Body.Close()
		if err != nil {
			slog.Error("error closing response body", "error", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("downloading archive: unexpected status %d", resp.StatusCode)
	}

	return vcs.ExtractFilesFromTarGz(resp.Body, paths)
}

// IsYAMLFile checks if a filename has a YAML extension.
// Deprecated: Use vcs.IsYAMLFile instead.
func IsYAMLFile(filename string) bool {
	return vcs.IsYAMLFile(filename)
}
