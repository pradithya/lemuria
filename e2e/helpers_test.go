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
