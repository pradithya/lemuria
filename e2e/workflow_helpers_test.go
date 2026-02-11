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

package e2e

import (
	"time"

	"github.com/org/lemuria/internal/commands"
	"github.com/org/lemuria/internal/config"
	"github.com/org/lemuria/internal/models"
)

// newTestExecutor creates a commands.Executor with real ArgoCD + Redis and a mock VCS client.
func newTestExecutor(gh *MockVCSClient, cfg *config.Config) *commands.Executor {
	if cfg == nil {
		cfg = &config.Config{
			ArgoCD: config.ArgoCDConfig{
				DiffMode:       "live",
				TempAppTimeout: 2 * time.Minute,
			},
			Defaults: config.DefaultsConfig{
				RequireApproval: false,
				AutoMerge:       false,
			},
		}
	}

	return commands.NewExecutor(gh, argoClient, lockManager, cfg)
}

// newPREvent creates a PREvent for testing (defaults to GitHub provider).
func newPREvent(repo, owner, repoName string, prNumber int, headSHA, headRef, baseRef string, commentBody string) *models.PREvent {
	event := &models.PREvent{
		Provider: models.VCSProviderGitHub,
		Type:     models.EventTypeIssueComment,
		Action:   models.PRActionCreated,
		Repo: models.RepoInfo{
			Owner:    owner,
			Name:     repoName,
			FullName: repo,
			HTMLURL:  "https://github.com/" + repo,
		},
		PR: models.PRInfo{
			Number:  prNumber,
			State:   models.PRStateOpen,
			HeadSHA: headSHA,
			HeadRef: headRef,
			BaseRef: baseRef,
		},
		Sender: models.UserInfo{
			Login: "test-user",
		},
		ReceivedAt: time.Now(),
	}

	if commentBody != "" {
		event.Comment = &models.Comment{
			ID:        12345,
			Body:      commentBody,
			Author:    models.UserInfo{Login: "test-user"},
			CreatedAt: time.Now(),
		}
	}

	return event
}

// newGitLabPREvent creates a PREvent for testing with GitLab provider.
func newGitLabPREvent(repo, owner, repoName string, prNumber int, headSHA, headRef, baseRef string, commentBody string) *models.PREvent {
	event := newPREvent(repo, owner, repoName, prNumber, headSHA, headRef, baseRef, commentBody)
	event.Provider = models.VCSProviderGitLab
	event.Repo.HTMLURL = "https://gitlab.com/" + repo
	return event
}
