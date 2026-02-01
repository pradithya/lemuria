package github

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/google/go-github/v60/github"

	"github.com/org/lemuria/internal/models"
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
func IsYAMLFile(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	return ext == ".yaml" || ext == ".yml"
}
