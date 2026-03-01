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
	"encoding/json"
	"fmt"
	"log/slog"

	v1alpha1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"

	"github.com/org/lemuria/internal/argocd"
	"github.com/org/lemuria/internal/lock"
	"github.com/org/lemuria/internal/models"
)

// maxParentDepth is the maximum recursion depth for parent app detection
// to prevent runaway traversal in pathological configurations.
const maxParentDepth = 10

// autoSyncState holds info needed to restore auto-sync for a single app.
type autoSyncState struct {
	Application        string
	OriginalPolicy     *v1alpha1.SyncPolicyAutomated
	ApplicationSetName string
}

// disableAutoSync disables auto-sync on a single application if it is enabled.
// Returns nil state if auto-sync was not enabled.
func disableAutoSync(ctx context.Context, argoClient *argocd.Client, appName string) (*autoSyncState, error) {
	raw, err := argoClient.GetApplicationRaw(ctx, appName)
	if err != nil {
		return nil, fmt.Errorf("getting application %s: %w", appName, err)
	}

	if raw.Spec.SyncPolicy == nil || raw.Spec.SyncPolicy.Automated == nil {
		return nil, nil
	}

	state := &autoSyncState{
		Application:    appName,
		OriginalPolicy: raw.Spec.SyncPolicy.Automated.DeepCopy(),
	}

	// Detect ApplicationSet ownership
	if appSetName, ok := raw.Labels["argocd.argoproj.io/application-set-name"]; ok {
		state.ApplicationSetName = appSetName
	} else {
		for _, owner := range raw.OwnerReferences {
			if owner.Kind == "ApplicationSet" {
				state.ApplicationSetName = owner.Name
				break
			}
		}
	}

	// Disable auto-sync
	raw.Spec.SyncPolicy.Automated = nil
	if err := argoClient.UpdateApplicationSpec(ctx, appName, raw.Spec); err != nil {
		return nil, fmt.Errorf("disabling auto-sync for %s: %w", appName, err)
	}

	slog.Info("disabled auto-sync", "app", appName, "applicationset", state.ApplicationSetName)
	return state, nil
}

// disableAppSetAutoSync disables auto-sync on an ApplicationSet template.
// Returns the serialized original SyncPolicyAutomated (nil if not enabled).
func disableAppSetAutoSync(ctx context.Context, argoClient *argocd.Client, appSetName string) ([]byte, error) {
	raw, err := argoClient.GetApplicationSetRaw(ctx, appSetName)
	if err != nil {
		return nil, fmt.Errorf("getting applicationset %s: %w", appSetName, err)
	}

	tmplPolicy := raw.Spec.Template.Spec.SyncPolicy
	if tmplPolicy == nil || tmplPolicy.Automated == nil {
		return nil, nil
	}

	originalBytes, err := json.Marshal(tmplPolicy.Automated)
	if err != nil {
		return nil, fmt.Errorf("marshaling applicationset sync policy: %w", err)
	}

	raw.Spec.Template.Spec.SyncPolicy.Automated = nil
	if err := argoClient.UpdateApplicationSet(ctx, appSetName, raw); err != nil {
		return nil, fmt.Errorf("disabling auto-sync on applicationset %s: %w", appSetName, err)
	}

	slog.Info("disabled auto-sync on applicationset template", "applicationset", appSetName)
	return originalBytes, nil
}

// disableParentAutoSync recursively finds and disables auto-sync on parent apps
// in the apps-of-apps pattern. It acquires locks on parent apps to prevent
// concurrent PRs from interfering.
func disableParentAutoSync(
	ctx context.Context,
	argoClient *argocd.Client,
	lockMgr lock.Manager,
	appName string,
	repo, repoURL, provider string,
	prNumber int,
	user string,
	visited map[string]bool,
	depth int,
) error {
	if depth >= maxParentDepth {
		slog.Warn("max parent depth reached, stopping recursion",
			"app", appName, "depth", depth)
		return nil
	}

	// Mark this app as visited to prevent cycles in recursive calls.
	// Note: callers may pre-set appName in visited to prevent disabling
	// auto-sync on it directly, but we still need to find its parents.
	visited[appName] = true

	parents, err := argoClient.FindParentApps(ctx, appName)
	if err != nil {
		slog.Warn("failed to find parent apps", "app", appName, "error", err)
		return nil
	}

	for _, parent := range parents {
		if visited[parent.Name] {
			continue
		}

		// Acquire lock on parent app
		lockResult, err := lockMgr.Lock(ctx, models.LockRequest{
			Application: parent.Name,
			PRNumber:    prNumber,
			Repo:        repo,
			RepoURL:     repoURL,
			Provider:    provider,
			User:        user,
			ChangeType:  models.ApplicationExisting,
		})
		if err != nil {
			slog.Warn("failed to lock parent app",
				"parent", parent.Name, "child", appName, "error", err)
			continue
		}
		if !lockResult.Acquired {
			slog.Warn("cannot lock parent app, held by another PR",
				"parent", parent.Name, "child", appName,
				"held_by_pr", lockResult.HeldBy.PRNumber)
			continue
		}

		// Disable auto-sync on parent
		state, err := disableAutoSync(ctx, argoClient, parent.Name)
		if err != nil {
			slog.Warn("failed to disable auto-sync on parent app",
				"parent", parent.Name, "error", err)
			continue
		}

		if state != nil {
			policyJSON, err := json.Marshal(state.OriginalPolicy)
			if err != nil {
				slog.Warn("failed to marshal parent sync policy",
					"parent", parent.Name, "error", err)
				continue
			}

			// Update the parent's lock with auto-sync state
			parentLock, err := lockMgr.Get(ctx, parent.Name)
			if err == nil && parentLock != nil {
				parentLock.AutoSyncDisabled = true
				parentLock.OriginalSyncPolicy = policyJSON
				parentLock.IsParentApp = true
				if err := lockMgr.UpdateLock(ctx, parentLock); err != nil {
					slog.Warn("failed to store auto-sync state in parent lock",
						"parent", parent.Name, "error", err)
				}
			}

			// Also store in parent-autosync index for PR-level lookup
			if err := lockMgr.StoreParentAutoSync(ctx, parent.Name, repo, prNumber, policyJSON); err != nil {
				slog.Warn("failed to store parent auto-sync state",
					"parent", parent.Name, "error", err)
			}
		}

		// Recurse to find grandparents
		if err := disableParentAutoSync(ctx, argoClient, lockMgr, parent.Name, repo, repoURL, provider, prNumber, user, visited, depth+1); err != nil {
			slog.Warn("failed to disable grandparent auto-sync",
				"parent", parent.Name, "error", err)
		}
	}

	return nil
}

// restoreAutoSync restores auto-sync on a single application from stored lock data.
func restoreAutoSync(ctx context.Context, argoClient *argocd.Client, l models.Lock) error {
	if !l.AutoSyncDisabled || len(l.OriginalSyncPolicy) == 0 {
		return nil
	}

	var policy v1alpha1.SyncPolicyAutomated
	if err := json.Unmarshal(l.OriginalSyncPolicy, &policy); err != nil {
		return fmt.Errorf("unmarshaling sync policy for %s: %w", l.Application, err)
	}

	raw, err := argoClient.GetApplicationRaw(ctx, l.Application)
	if err != nil {
		if argocd.IsNotFound(err) {
			slog.Warn("application not found for auto-sync restore, app may be deleted",
				"app", l.Application)
			return nil
		}
		return fmt.Errorf("getting application %s for auto-sync restore: %w", l.Application, err)
	}

	if raw.Spec.SyncPolicy == nil {
		raw.Spec.SyncPolicy = &v1alpha1.SyncPolicy{}
	}
	raw.Spec.SyncPolicy.Automated = &policy

	if err := argoClient.UpdateApplicationSpec(ctx, l.Application, raw.Spec); err != nil {
		return fmt.Errorf("restoring auto-sync for %s: %w", l.Application, err)
	}

	slog.Info("restored auto-sync", "app", l.Application)
	return nil
}

// restoreAppSetAutoSync restores auto-sync on an ApplicationSet template.
func restoreAppSetAutoSync(ctx context.Context, argoClient *argocd.Client, appSetName string, policyJSON []byte) error {
	if len(policyJSON) == 0 {
		return nil
	}

	var policy v1alpha1.SyncPolicyAutomated
	if err := json.Unmarshal(policyJSON, &policy); err != nil {
		return fmt.Errorf("unmarshaling applicationset sync policy for %s: %w", appSetName, err)
	}

	raw, err := argoClient.GetApplicationSetRaw(ctx, appSetName)
	if err != nil {
		if argocd.IsNotFound(err) {
			slog.Warn("applicationset not found for auto-sync restore, may be deleted",
				"applicationset", appSetName)
			return nil
		}
		return fmt.Errorf("getting applicationset %s for auto-sync restore: %w", appSetName, err)
	}

	if raw.Spec.Template.Spec.SyncPolicy == nil {
		raw.Spec.Template.Spec.SyncPolicy = &v1alpha1.SyncPolicy{}
	}
	raw.Spec.Template.Spec.SyncPolicy.Automated = &policy

	if err := argoClient.UpdateApplicationSet(ctx, appSetName, raw); err != nil {
		return fmt.Errorf("restoring auto-sync on applicationset %s: %w", appSetName, err)
	}

	slog.Info("restored auto-sync on applicationset template", "applicationset", appSetName)
	return nil
}

// restoreAllAutoSync orchestrates auto-sync restoration for all apps, parents,
// and ApplicationSet templates. Order: children first, then parents, then
// ApplicationSet templates so that children are restored before parents start
// auto-syncing.
func restoreAllAutoSync(ctx context.Context, argoClient *argocd.Client, lockMgr lock.Manager, locks []models.Lock, repo string, prNumber int) {
	// 1. Restore child apps first
	for _, l := range locks {
		if l.AutoSyncDisabled && !l.IsParentApp {
			if err := restoreAutoSync(ctx, argoClient, l); err != nil {
				slog.Warn("failed to restore auto-sync",
					"app", l.Application, "error", err)
			}
		}
	}

	// 2. Restore parent apps
	for _, l := range locks {
		if l.AutoSyncDisabled && l.IsParentApp {
			if err := restoreAutoSync(ctx, argoClient, l); err != nil {
				slog.Warn("failed to restore parent auto-sync",
					"app", l.Application, "error", err)
			}
		}
	}

	// 3. Restore ApplicationSet templates
	restoredAppSets := make(map[string]bool)
	for _, l := range locks {
		if l.ApplicationSetName == "" || restoredAppSets[l.ApplicationSetName] {
			continue
		}
		restoredAppSets[l.ApplicationSetName] = true

		policyJSON, err := lockMgr.GetAppSetAutoSync(ctx, l.ApplicationSetName, repo, prNumber)
		if err != nil {
			slog.Warn("failed to get appset auto-sync state",
				"applicationset", l.ApplicationSetName, "error", err)
			continue
		}
		if policyJSON == nil {
			continue
		}

		if err := restoreAppSetAutoSync(ctx, argoClient, l.ApplicationSetName, policyJSON); err != nil {
			slog.Warn("failed to restore appset auto-sync",
				"applicationset", l.ApplicationSetName, "error", err)
		}
		if err := lockMgr.DeleteAppSetAutoSync(ctx, l.ApplicationSetName, repo, prNumber); err != nil {
			slog.Warn("failed to clean up appset auto-sync state",
				"applicationset", l.ApplicationSetName, "error", err)
		}
	}

	// 4. Clean up parent-autosync index keys
	for _, l := range locks {
		if l.IsParentApp {
			if err := lockMgr.DeleteParentAutoSync(ctx, l.Application, repo, prNumber); err != nil {
				slog.Warn("failed to clean up parent auto-sync state",
					"app", l.Application, "error", err)
			}
		}
	}
}
