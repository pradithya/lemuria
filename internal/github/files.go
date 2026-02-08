package github

import (
	"context"
	"fmt"

	"github.com/google/go-github/v60/github"

	"github.com/org/lemuria/internal/models"
	"github.com/org/lemuria/internal/vcs"
)

// GetChangedFiles returns the list of files changed in a PR.
func (c *Client) GetChangedFiles(ctx context.Context, owner, repo string, number int) ([]models.ChangedFile, error) {
	client, err := c.GetInstallationClient(ctx, owner)
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
				Filename:  f.GetFilename(),
				Status:    f.GetStatus(),
				Additions: f.GetAdditions(),
				Deletions: f.GetDeletions(),
				Changes:   f.GetChanges(),
				Patch:     f.GetPatch(),
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
	client, err := c.GetInstallationClient(ctx, owner)
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

// IsYAMLFile checks if a filename has a YAML extension.
// Deprecated: Use vcs.IsYAMLFile instead.
func IsYAMLFile(filename string) bool {
	return vcs.IsYAMLFile(filename)
}
