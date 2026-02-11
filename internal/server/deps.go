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

package server

import (
	"fmt"
	"log/slog"

	"github.com/org/lemuria/internal/argocd"
	"github.com/org/lemuria/internal/commands"
	"github.com/org/lemuria/internal/config"
	"github.com/org/lemuria/internal/github"
	"github.com/org/lemuria/internal/gitlab"
	"github.com/org/lemuria/internal/lock"
)

// Dependencies holds the core dependencies shared between server and worker modes.
type Dependencies struct {
	ArgoClient     *argocd.Client
	LockManager    lock.Manager
	GithubClient   *github.Client
	GitlabClient   *gitlab.Client
	GithubExecutor *commands.Executor
	GitlabExecutor *commands.Executor
}

// InitDependencies creates all shared dependencies (ArgoCD client, lock manager,
// VCS clients, executors) from the given config.
func InitDependencies(cfg *config.Config) (*Dependencies, error) {
	argoClient, err := argocd.NewClient(cfg.ArgoCD)
	if err != nil {
		return nil, fmt.Errorf("creating Argo CD client: %w", err)
	}

	lockMgr, err := lock.NewRedisManager(cfg.Redis)
	if err != nil {
		return nil, fmt.Errorf("creating lock manager: %w", err)
	}

	deps := &Dependencies{
		ArgoClient:  argoClient,
		LockManager: lockMgr,
	}

	if cfg.HasGitHub() {
		ghClient, err := github.NewClient(cfg.GitHub)
		if err != nil {
			return nil, fmt.Errorf("creating GitHub client: %w", err)
		}
		deps.GithubClient = ghClient
		deps.GithubExecutor = commands.NewExecutor(ghClient, argoClient, lockMgr, cfg)
		slog.Info("GitHub provider initialized")
	}

	if cfg.HasGitLab() {
		glClient, err := gitlab.NewClient(cfg.GitLab)
		if err != nil {
			return nil, fmt.Errorf("creating GitLab client: %w", err)
		}
		deps.GitlabClient = glClient
		deps.GitlabExecutor = commands.NewExecutor(glClient, argoClient, lockMgr, cfg)
		slog.Info("GitLab provider initialized")
	}

	return deps, nil
}

// Close releases all resources held by the dependencies.
func (d *Dependencies) Close() error {
	if d.LockManager != nil {
		if err := d.LockManager.Close(); err != nil {
			slog.Error("error closing lock manager", "error", err)
		}
	}
	return nil
}
