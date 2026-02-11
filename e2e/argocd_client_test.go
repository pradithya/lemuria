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
	"context"
	"testing"
	"time"

	v1alpha1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/org/lemuria/internal/argocd"
)

func TestArgoCDVersion(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping ArgoCD test in short mode")
	}
	version, err := argoClient.Version(testCtx)
	if err != nil {
		t.Fatalf("Failed to get Argo CD version: %v", err)
	}

	if version == "" {
		t.Error("Expected non-empty version")
	}

	t.Logf("Argo CD version: %s", version)
}

func TestListApplications(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping ArgoCD test in short mode")
	}
	apps, err := argoClient.ListApplications(testCtx)
	if err != nil {
		t.Fatalf("Failed to list applications: %v", err)
	}

	t.Logf("Found %d applications", len(apps))

	// We should have at least the test apps we created
	if len(apps) == 0 {
		t.Log("Warning: No applications found - test apps may not be created yet")
	}

	for _, app := range apps {
		t.Logf("  - %s (sync: %s, health: %s)", app.Name, app.SyncStatus, app.HealthStatus)
	}
}

func TestGetApplication(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping ArgoCD test in short mode")
	}
	// First, list to see what's available
	apps, err := argoClient.ListApplications(testCtx)
	if err != nil {
		t.Fatalf("Failed to list applications: %v", err)
	}

	if len(apps) == 0 {
		t.Skip("No applications available to test")
	}

	// Get the first app
	appName := apps[0].Name
	app, err := argoClient.GetApplication(testCtx, appName)
	if err != nil {
		t.Fatalf("Failed to get application %s: %v", appName, err)
	}

	if app.Name != appName {
		t.Errorf("Expected app name %s, got %s", appName, app.Name)
	}

	t.Logf("Application %s: project=%s, repoURL=%s, path=%s",
		app.Name, app.Project, app.RepoURL, app.Path)
}

func TestListApplicationSets(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping ArgoCD test in short mode")
	}
	appSets, err := argoClient.ListApplicationSets(testCtx)
	if err != nil {
		t.Fatalf("Failed to list ApplicationSets: %v", err)
	}

	t.Logf("Found %d ApplicationSets", len(appSets))

	for _, appSet := range appSets {
		t.Logf("  - %s", appSet.Name)
	}
}

func TestGetApplicationsByApplicationSet(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping ArgoCD test in short mode")
	}
	// First check if we have any ApplicationSets
	appSets, err := argoClient.ListApplicationSets(testCtx)
	if err != nil {
		t.Fatalf("Failed to list ApplicationSets: %v", err)
	}

	if len(appSets) == 0 {
		t.Skip("No ApplicationSets available to test")
	}

	appSetName := appSets[0].Name
	apps, err := argoClient.GetApplicationsByApplicationSet(testCtx, appSetName)
	if err != nil {
		t.Fatalf("Failed to get applications by ApplicationSet %s: %v", appSetName, err)
	}

	t.Logf("ApplicationSet %s has %d applications", appSetName, len(apps))
	for _, app := range apps {
		t.Logf("  - %s", app.Name)
	}
}

func TestGetManifests(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping ArgoCD test in short mode")
	}
	apps, err := argoClient.ListApplications(testCtx)
	if err != nil {
		t.Fatalf("Failed to list applications: %v", err)
	}

	if len(apps) == 0 {
		t.Skip("No applications available to test")
	}

	appName := apps[0].Name
	manifests, revision, err := argoClient.GetManifests(testCtx, appName, &argocd.GetManifestsParams{FetchRevision: true})
	if err != nil {
		t.Fatalf("Failed to get manifests for %s: %v", appName, err)
	}

	t.Logf("Got %d manifests for %s at revision %s", len(manifests), appName, revision)

	for _, m := range manifests {
		t.Logf("  - %s/%s: %s", m.Kind, m.Name, m.Namespace)
	}
}

func TestListApplicationsExcludesTempApps(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping ArgoCD test in short mode")
	}

	// Create a temp app with the lemuria temp-app label (mimicking what TempAppManager does)
	tempAppName := uniqueAppName("e2e-temp-app")
	tempApp := &v1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      tempAppName,
			Namespace: "argocd",
			Labels: map[string]string{
				"lemuria.io/temp-app":      "true",
				"lemuria.io/original-app":  "some-real-app",
				"lemuria.io/pr-number":     "999",
			},
		},
		Spec: v1alpha1.ApplicationSpec{
			Project: "default",
			Source: &v1alpha1.ApplicationSource{
				RepoURL:        "https://github.com/argoproj/argocd-example-apps.git",
				TargetRevision: "HEAD",
				Path:           "guestbook",
			},
			Destination: v1alpha1.ApplicationDestination{
				Server:    "https://kubernetes.default.svc",
				Namespace: "e2e-test-apps",
			},
		},
	}

	if err := argoClient.CreateApplication(testCtx, tempApp); err != nil {
		t.Fatalf("Failed to create temp application: %v", err)
	}
	defer func() {
		_ = argoClient.DeleteApplication(context.Background(), tempAppName, false)
		t.Logf("Cleaned up temp app: %s", tempAppName)
	}()

	t.Logf("Created temp app: %s", tempAppName)

	// Also create a real app to ensure it IS included
	realAppName := uniqueAppName("e2e-real-app")
	createTestApplication(testCtx, t, argoClient, realAppName, "e2e-test-apps")
	defer deleteTestApplication(context.Background(), t, argoClient, realAppName)

	// ListApplications should NOT include the temp app
	apps, err := argoClient.ListApplications(testCtx)
	if err != nil {
		t.Fatalf("ListApplications() error = %v", err)
	}

	for _, app := range apps {
		if app.Name == tempAppName {
			t.Errorf("ListApplications() returned temp app %q — it should be excluded", tempAppName)
		}
	}

	foundReal := false
	for _, app := range apps {
		if app.Name == realAppName {
			foundReal = true
			break
		}
	}
	if !foundReal {
		t.Errorf("ListApplications() did not return real app %q", realAppName)
	}

	t.Logf("ListApplications() returned %d apps (correctly excluded temp app)", len(apps))

	// ListApplicationsWithSelector for temp apps should find it
	tempApps, err := argoClient.ListApplicationsWithSelector(testCtx, "lemuria.io/temp-app=true")
	if err != nil {
		t.Fatalf("ListApplicationsWithSelector(temp) error = %v", err)
	}

	foundTemp := false
	for _, app := range tempApps {
		if app.Name == tempAppName {
			foundTemp = true
			break
		}
	}
	if !foundTemp {
		t.Errorf("ListApplicationsWithSelector(temp) did not return temp app %q", tempAppName)
	}

	t.Logf("ListApplicationsWithSelector(temp) returned %d temp apps (correctly found temp app)", len(tempApps))
}

func TestGetApplicationDiffV2(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping ArgoCD test in short mode")
	}
	apps, err := argoClient.ListApplications(testCtx)
	if err != nil {
		t.Fatalf("Failed to list applications: %v", err)
	}

	if len(apps) == 0 {
		t.Skip("No applications available to test")
	}

	appName := apps[0].Name
	// Test live mode: compare live state vs target branch
	diffs, err := argoClient.GetApplicationDiff(testCtx, appName, argocd.DiffOptions{
		Mode:         argocd.DiffModeLive,
		TargetBranch: "HEAD",
		PRNumber:     0,
		PRRepo:       "https://github.com/test/repo",
		Timeout:      2 * time.Minute,
	})
	if err != nil {
		t.Fatalf("Failed to get diff for %s: %v", appName, err)
	}

	t.Logf("Got %d diffs for %s", len(diffs), appName)

	for _, d := range diffs {
		t.Logf("  - %s: action=%s", d.Resource.String(), d.Action)
	}
}
