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
	"context"

	"github.com/org/lemuria/internal/models"
)

// Client defines the VCS operations needed by the Executor.
type Client interface {
	GetChangedFiles(ctx context.Context, owner, repo string, number int) ([]models.ChangedFile, error)
	GetRepoConfig(ctx context.Context, owner, repo, ref string) ([]byte, error)
	GetFileContent(ctx context.Context, owner, repo, path, ref string) ([]byte, error)
	GetPR(ctx context.Context, owner, repo string, number int) (*models.PullRequestDetail, error)
	IsPRApproved(ctx context.Context, owner, repo string, number int) (bool, error)
	PostComment(ctx context.Context, owner, repo string, number int, body string, isPlan bool) (*models.CommentResult, error)
	UpdateComment(ctx context.Context, owner, repo string, number int, commentID int64, body string) error
	AddReaction(ctx context.Context, owner, repo string, commentID int64, reaction string) error
	InvalidatePlanComments(ctx context.Context, owner, repo string, number int) error
	MergePullRequest(ctx context.Context, owner, repo string, number int, title, message, method string) error
	DeleteBranch(ctx context.Context, owner, repo, branch string) error
	// MaxCommentSize returns the maximum comment body size in characters
	// for this VCS provider. Returns 0 if there is no limit.
	MaxCommentSize() int
}
