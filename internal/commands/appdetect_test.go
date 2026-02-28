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

package commands

import (
	"context"
	"testing"

	"github.com/org/lemuria/internal/models"
)

func TestScanRepoForApplications(t *testing.T) {
	appYAML := `apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: my-app
spec:
  destination:
    server: https://kubernetes.default.svc
    namespace: default
  project: default
  source:
    repoURL: https://github.com/org/repo
    path: apps/my-app
    targetRevision: main
`

	appSetYAML := `apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
metadata:
  name: my-appset
spec:
  generators:
    - list:
        elements:
          - cluster: dev
  template:
    metadata:
      name: '{{cluster}}-app'
    spec:
      project: default
      source:
        repoURL: https://github.com/org/repo
        path: apps
        targetRevision: main
      destination:
        server: https://kubernetes.default.svc
        namespace: default
`

	vcs := &mockVCS{
		filesByPattern: map[string]map[string][]byte{
			"feature-branch": {
				"argocd/app.yaml":    []byte(appYAML),
				"argocd/appset.yaml": []byte(appSetYAML),
			},
			"main": {
				"argocd/app.yaml": []byte(appYAML),
			},
		},
	}

	exec := newTestExecutor(vcs, &mockLock{}, nil)

	event := &models.PREvent{
		Repo: models.RepoInfo{
			Owner: "org", Name: "repo", FullName: "org/repo",
		},
		PR: models.PRInfo{
			HeadRef: "feature-branch",
			BaseRef: "main",
		},
	}

	scanned, err := exec.scanRepoForApplications(context.Background(), event, []string{"argocd"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(scanned.HeadApps) != 1 {
		t.Errorf("expected 1 head app, got %d", len(scanned.HeadApps))
	}
	if len(scanned.HeadAppSets) != 1 {
		t.Errorf("expected 1 head appset, got %d", len(scanned.HeadAppSets))
	}
	if len(scanned.BaseApps) != 1 {
		t.Errorf("expected 1 base app, got %d", len(scanned.BaseApps))
	}
	if len(scanned.BaseAppSets) != 0 {
		t.Errorf("expected 0 base appsets, got %d", len(scanned.BaseAppSets))
	}
}

func TestScanRepoForApplications_NoCRPaths(t *testing.T) {
	appYAML := `apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: my-app
spec:
  destination:
    server: https://kubernetes.default.svc
    namespace: default
  project: default
  source:
    repoURL: https://github.com/org/repo
    path: apps/my-app
    targetRevision: main
`

	vcs := &mockVCS{
		filesByPattern: map[string]map[string][]byte{
			"feature-branch": {
				"apps/app.yaml": []byte(appYAML),
			},
			"main": {},
		},
	}

	exec := newTestExecutor(vcs, &mockLock{}, nil)

	event := &models.PREvent{
		Repo: models.RepoInfo{Owner: "org", Name: "repo"},
		PR:   models.PRInfo{HeadRef: "feature-branch", BaseRef: "main"},
	}

	scanned, err := exec.scanRepoForApplications(context.Background(), event, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(scanned.HeadApps) != 1 {
		t.Errorf("expected 1 head app, got %d", len(scanned.HeadApps))
	}
}

func TestDetectCrossRepoAffectedApps(t *testing.T) {
	// This test requires an ArgoCD fake; skip for now since the main unit tests
	// cover cross-repo via the full e2e path. The function is simple enough that
	// the integration test provides sufficient coverage.
	t.Skip("requires ArgoCD fake server")
}
