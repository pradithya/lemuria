package gitlab

import (
	"context"
	"fmt"

	gogitlab "github.com/xanzy/go-gitlab"

	"github.com/org/lemuria/internal/models"
)

// GetChangedFiles returns the list of files changed in a merge request.
func (c *Client) GetChangedFiles(ctx context.Context, owner, repo string, number int) ([]models.ChangedFile, error) {
	project := projectPath(owner, repo)

	opts := &gogitlab.ListMergeRequestDiffsOptions{
		ListOptions: gogitlab.ListOptions{PerPage: 100},
	}

	var allFiles []models.ChangedFile

	for {
		diffs, resp, err := c.client.MergeRequests.ListMergeRequestDiffs(project, number, opts, gogitlab.WithContext(ctx))
		if err != nil {
			return nil, fmt.Errorf("listing MR diffs: %w", err)
		}

		for _, d := range diffs {
			status := diffStatus(d)
			allFiles = append(allFiles, models.ChangedFile{
				Filename:  filePath(d),
				Status:    status,
				Additions: 0, // GitLab diff API does not provide line counts per file
				Deletions: 0,
				Changes:   0,
				Patch:     d.Diff,
			})
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return allFiles, nil
}

// diffStatus determines the change status from a GitLab merge request diff.
func diffStatus(d *gogitlab.MergeRequestDiff) string {
	if d.NewFile {
		return "added"
	}
	if d.DeletedFile {
		return "removed"
	}
	if d.RenamedFile {
		return "renamed"
	}
	return "modified"
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
