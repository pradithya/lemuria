package commands

import (
	"context"

	"github.com/google/go-github/v60/github"

	"github.com/org/lemuria/internal/models"
)

// GitHubClient defines the GitHub operations needed by the Executor.
type GitHubClient interface {
	GetChangedFiles(ctx context.Context, owner, repo string, number int) ([]models.ChangedFile, error)
	GetRepoConfig(ctx context.Context, owner, repo, ref string) ([]byte, error)
	GetFileContent(ctx context.Context, owner, repo, path, ref string) ([]byte, error)
	GetPR(ctx context.Context, owner, repo string, number int) (*github.PullRequest, error)
	IsPRApproved(ctx context.Context, owner, repo string, number int) (bool, error)
	PostComment(ctx context.Context, owner, repo string, number int, body string, isPlan bool) (*github.IssueComment, error)
	AddReaction(ctx context.Context, owner, repo string, commentID int64, reaction string) error
	InvalidatePlanComments(ctx context.Context, owner, repo string, number int) error
	MergePullRequest(ctx context.Context, owner, repo string, number int, title, message, method string) error
	DeleteBranch(ctx context.Context, owner, repo, branch string) error
}
