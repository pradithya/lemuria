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
	"bytes"
	"context"
	"fmt"

	gogitlab "gitlab.com/gitlab-org/api/client-go"

	"github.com/org/lemuria/internal/models"
	"github.com/org/lemuria/internal/vcs"
)

// GetChangedFiles returns the list of files changed in a merge request.
func (c *Client) GetChangedFiles(ctx context.Context, owner, repo string, number int) ([]models.ChangedFile, error) {
	project := projectPath(owner, repo)

	opts := &gogitlab.ListMergeRequestDiffsOptions{
		ListOptions: gogitlab.ListOptions{PerPage: 100},
	}

	var allFiles []models.ChangedFile

	for {
		diffs, resp, err := c.client.MergeRequests.ListMergeRequestDiffs(project, int64(number), opts, gogitlab.WithContext(ctx))
		if err != nil {
			return nil, fmt.Errorf("listing MR diffs: %w", err)
		}

		for _, d := range diffs {
			status := diffStatus(d)
			cf := models.ChangedFile{
				Filename:  filePath(d),
				Status:    status,
				Additions: 0, // GitLab diff API does not provide line counts per file
				Deletions: 0,
				Changes:   0,
				Patch:     d.Diff,
			}
			if d.RenamedFile && d.OldPath != d.NewPath {
				cf.PreviousFilename = d.OldPath
			}
			allFiles = append(allFiles, cf)
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return allFiles, nil
}

// diffStatus determines the change status from a GitLab merge request diff.
func diffStatus(d *gogitlab.MergeRequestDiff) models.FileStatus {
	if d.NewFile {
		return models.FileStatusAdded
	}
	if d.DeletedFile {
		return models.FileStatusRemoved
	}
	if d.RenamedFile {
		return models.FileStatusRenamed
	}
	return models.FileStatusModified
}

// filePath returns the most relevant file path from a diff entry.
// For renamed files it returns the new path; otherwise the old path (which
// equals the new path for non-renames).
func filePath(d *gogitlab.MergeRequestDiff) string {
	if d.NewPath != "" {
		return d.NewPath
	}
	return d.OldPath
}

// GetRepoConfig fetches the .lemuria.yaml configuration from a repository at a
// specific ref (branch, tag, or commit SHA).
func (c *Client) GetRepoConfig(ctx context.Context, owner, repo, ref string) ([]byte, error) {
	return c.GetFileContent(ctx, owner, repo, ".lemuria.yaml", ref)
}

// GetFileContents retrieves the contents of multiple files at a specific ref
// by downloading a tar.gz archive (single HTTP call) and extracting the
// requested paths in-memory.
func (c *Client) GetFileContents(ctx context.Context, owner, repo string, paths []string, ref string) (map[string][]byte, error) {
	if len(paths) == 0 {
		return map[string][]byte{}, nil
	}

	project := projectPath(owner, repo)
	format := "tar.gz"

	data, _, err := c.client.Repositories.Archive(project, &gogitlab.ArchiveOptions{
		Format: gogitlab.Ptr(format),
		SHA:    gogitlab.Ptr(ref),
	}, gogitlab.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("downloading archive at ref %s: %w", ref, err)
	}

	return vcs.ExtractFilesFromTarGz(bytes.NewReader(data), paths)
}

// GetFilesByPattern retrieves all files matching the given filename patterns
// (e.g. "*.yaml") and optionally limited to specific path prefixes, by
// downloading a tar.gz archive and extracting matching entries in-memory.
func (c *Client) GetFilesByPattern(ctx context.Context, owner, repo, ref string, patterns []string, pathPrefixes []string) (map[string][]byte, error) {
	project := projectPath(owner, repo)
	format := "tar.gz"

	data, _, err := c.client.Repositories.Archive(project, &gogitlab.ArchiveOptions{
		Format: gogitlab.Ptr(format),
		SHA:    gogitlab.Ptr(ref),
	}, gogitlab.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("downloading archive at ref %s: %w", ref, err)
	}

	return vcs.ExtractFilesByPattern(bytes.NewReader(data), patterns, pathPrefixes)
}

// GetFileContent retrieves the raw content of a file at a specific ref.
func (c *Client) GetFileContent(ctx context.Context, owner, repo, path, ref string) ([]byte, error) {
	project := projectPath(owner, repo)

	content, _, err := c.client.RepositoryFiles.GetRawFile(project, path, &gogitlab.GetRawFileOptions{
		Ref: gogitlab.Ptr(ref),
	}, gogitlab.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("getting file %s at ref %s: %w", path, ref, err)
	}

	return content, nil
}
