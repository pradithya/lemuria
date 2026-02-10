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
	"fmt"
	"testing"
	"time"

	v1alpha1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/org/lemuria/internal/argocd"
)

// createTestApplication creates an ArgoCD application for testing.
// It uses the argoproj/argocd-example-apps guestbook app as the source.
func createTestApplication(ctx context.Context, t *testing.T, client *argocd.Client, name, namespace string) {
	t.Helper()

	if namespace == "" {
		namespace = "e2e-test-apps"
	}

	app := &v1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "argocd",
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
				Namespace: namespace,
			},
		},
	}

	if err := client.CreateApplication(ctx, app); err != nil {
		t.Fatalf("Failed to create test application %s: %v", name, err)
	}

	t.Logf("Created test application: %s", name)
}

// createTestHelmChartApplication creates an ArgoCD application that sources from
// an external Helm chart repository (not a git repo). This is used to test detection
// of Application CR changes where the app's source doesn't reference the PR repo.
func createTestHelmChartApplication(ctx context.Context, t *testing.T, client *argocd.Client, name, namespace string) {
	t.Helper()

	if namespace == "" {
		namespace = "e2e-test-apps"
	}

	app := &v1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "argocd",
		},
		Spec: v1alpha1.ApplicationSpec{
			Project: "default",
			Source: &v1alpha1.ApplicationSource{
				RepoURL:        "https://argoproj.github.io/argo-helm",
				Chart:          "argocd-apps",
				TargetRevision: "1.4.1",
			},
			Destination: v1alpha1.ApplicationDestination{
				Server:    "https://kubernetes.default.svc",
				Namespace: namespace,
			},
		},
	}

	if err := client.CreateApplication(ctx, app); err != nil {
		t.Fatalf("Failed to create test Helm chart application %s: %v", name, err)
	}

	t.Logf("Created test Helm chart application: %s", name)
}

// deleteTestApplication deletes an ArgoCD application with cascade.
func deleteTestApplication(ctx context.Context, t *testing.T, client *argocd.Client, name string) {
	t.Helper()
	if err := client.DeleteApplication(ctx, name, true); err != nil {
		t.Logf("Warning: failed to delete test application %s: %v", name, err)
	}
}

// waitForAppReady polls until an application exists and is in a known state.
func waitForAppReady(ctx context.Context, t *testing.T, client *argocd.Client, name string, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		app, err := client.GetApplication(ctx, name)
		if err == nil && app != nil {
			t.Logf("Application %s is ready (sync: %s, health: %s)", name, app.SyncStatus, app.HealthStatus)
			return
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("Timed out waiting for application %s to be ready", name)
}

// uniqueAppName generates a unique application name for a test.
func uniqueAppName(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano()%100000)
}

// cleanupForceUnlock force-unlocks an application regardless of owner.
func cleanupForceUnlock(ctx context.Context, t *testing.T, app string) {
	t.Helper()
	if err := lockManager.ForceUnlock(ctx, app); err != nil {
		t.Logf("Warning: failed to force unlock %s: %v", app, err)
	}
}

// createTestApplicationWithBadImage creates an ArgoCD application that uses a non-existent image.
// This will cause the deployment to enter ImagePullBackOff state, resulting in Degraded health.
func createTestApplicationWithBadImage(ctx context.Context, t *testing.T, client *argocd.Client, name, namespace string) {
	t.Helper()

	if namespace == "" {
		namespace = "e2e-test-apps"
	}

	app := &v1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "argocd",
		},
		Spec: v1alpha1.ApplicationSpec{
			Project: "default",
			Source: &v1alpha1.ApplicationSource{
				RepoURL:        "https://github.com/argoproj/argocd-example-apps.git",
				TargetRevision: "HEAD",
				Path:           "kustomize-guestbook",
				Kustomize: &v1alpha1.ApplicationSourceKustomize{
					Images: v1alpha1.KustomizeImages{
						// Override guestbook image with a non-existent image
						"gcr.io/heptio-images/ks-guestbook-demo=nonexistent.invalid/bad-image:v999",
					},
				},
			},
			Destination: v1alpha1.ApplicationDestination{
				Server:    "https://kubernetes.default.svc",
				Namespace: namespace,
			},
		},
	}

	if err := client.CreateApplication(ctx, app); err != nil {
		t.Fatalf("Failed to create test application with bad image %s: %v", name, err)
	}

	t.Logf("Created test application with bad image: %s", name)
}
