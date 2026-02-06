package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/org/lemuria/internal/argocd"
)

// createTestApplication creates an ArgoCD application for testing.
// It uses the argoproj/argocd-example-apps guestbook app as the source.
func createTestApplication(ctx context.Context, t *testing.T, client *argocd.Client, name, namespace string) {
	t.Helper()

	if namespace == "" {
		namespace = "e2e-test-apps"
	}

	app := map[string]interface{}{
		"metadata": map[string]interface{}{
			"name":      name,
			"namespace": "argocd",
		},
		"spec": map[string]interface{}{
			"project": "default",
			"source": map[string]interface{}{
				"repoURL":        "https://github.com/argoproj/argocd-example-apps.git",
				"targetRevision": "HEAD",
				"path":           "guestbook",
			},
			"destination": map[string]interface{}{
				"server":    "https://kubernetes.default.svc",
				"namespace": namespace,
			},
		},
	}

	if err := client.CreateApplication(ctx, app); err != nil {
		t.Fatalf("Failed to create test application %s: %v", name, err)
	}

	t.Logf("Created test application: %s", name)
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

// cleanupLocks removes all locks for a given app and PR from the lock manager.
func cleanupLocks(ctx context.Context, t *testing.T, app string, repo string, prNumber int) {
	t.Helper()
	if err := lockManager.Unlock(ctx, app, repo, prNumber); err != nil {
		t.Logf("Warning: failed to cleanup lock for %s: %v", app, err)
	}
}

// cleanupForceUnlock force-unlocks an application regardless of owner.
func cleanupForceUnlock(ctx context.Context, t *testing.T, app string) {
	t.Helper()
	if err := lockManager.ForceUnlock(ctx, app); err != nil {
		t.Logf("Warning: failed to force unlock %s: %v", app, err)
	}
}
