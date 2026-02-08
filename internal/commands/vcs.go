package commands

import (
	"context"

	"github.com/org/lemuria/internal/models"
)

// VCSClient defines the VCS operations needed by the Executor.
type VCSClient interface {
	GetChangedFiles(ctx context.Context, owner, repo string, number int) ([]models.ChangedFile, error)
	GetRepoConfig(ctx context.Context, owner, repo, ref string) ([]byte, error)
	GetFileContent(ctx context.Context, owner, repo, path, ref string) ([]byte, error)
	GetPR(ctx context.Context, owner, repo string, number int) (*models.PullRequestDetail, error)
	IsPRApproved(ctx context.Context, owner, repo string, number int) (bool, error)
	PostComment(ctx context.Context, owner, repo string, number int, body string, isPlan bool) (*models.CommentResult, error)
	AddReaction(ctx context.Context, owner, repo string, commentID int64, reaction string) error
	InvalidatePlanComments(ctx context.Context, owner, repo string, number int) error
	MergePullRequest(ctx context.Context, owner, repo string, number int, title, message, method string) error
	DeleteBranch(ctx context.Context, owner, repo, branch string) error
}
