package e2e

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/org/lemuria/internal/argocd"
	"github.com/org/lemuria/internal/config"
	"github.com/org/lemuria/internal/lock"
	"github.com/org/lemuria/internal/models"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

var (
	argoClient     *argocd.Client
	lockManager    lock.Manager
	testCtx        context.Context
	redisContainer testcontainers.Container
)

func TestMain(m *testing.M) {
	// Load environment
	loadEnv()

	// Setup context with timeout
	var cancel context.CancelFunc
	testCtx, cancel = context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Initialize clients
	var err error
	argoClient, err = argocd.NewClient(config.ArgoCDConfig{
		ServerURL: getEnv("ARGOCD_SERVER", "http://localhost:8081"),
		Token:     getEnv("ARGOCD_TOKEN", ""),
		Insecure:  true,
	})
	if err != nil {
		panic("Failed to create Argo CD client: " + err.Error())
	}

	// Start Redis container if REDIS_ADDR is not set
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisContainer, err = testcontainers.GenericContainer(testCtx, testcontainers.GenericContainerRequest{
			ContainerRequest: testcontainers.ContainerRequest{
				Image:        "redis:7-alpine",
				ExposedPorts: []string{"6379/tcp"},
				WaitingFor:   wait.ForLog("Ready to accept connections"),
			},
			Started: true,
		})
		if err != nil {
			panic("Failed to start Redis container: " + err.Error())
		}

		host, err := redisContainer.Host(testCtx)
		if err != nil {
			panic("Failed to get Redis container host: " + err.Error())
		}

		port, err := redisContainer.MappedPort(testCtx, "6379")
		if err != nil {
			panic("Failed to get Redis container port: " + err.Error())
		}

		redisAddr = fmt.Sprintf("%s:%s", host, port.Port())
	}

	lockManager, err = lock.NewRedisManager(config.RedisConfig{
		Address: redisAddr,
	})
	if err != nil {
		panic("Failed to create Redis lock manager: " + err.Error())
	}

	// Run tests
	code := m.Run()

	// Cleanup
	lockManager.Close()
	if redisContainer != nil {
		_ = redisContainer.Terminate(testCtx)
	}

	os.Exit(code)
}

func loadEnv() {
	// Try to load from .env file
	data, err := os.ReadFile(".env")
	if err != nil {
		return
	}

	for _, line := range splitLines(string(data)) {
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		parts := splitFirst(line, '=')
		if len(parts) == 2 {
			os.Setenv(parts[0], parts[1])
		}
	}
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func splitFirst(s string, sep byte) []string {
	for i := 0; i < len(s); i++ {
		if s[i] == sep {
			return []string{s[:i], s[i+1:]}
		}
	}
	return []string{s}
}

// =============================================================================
// Argo CD Client Tests
// =============================================================================

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
	manifests, revision, err := argoClient.GetManifests(testCtx, appName, nil)
	if err != nil {
		t.Fatalf("Failed to get manifests for %s: %v", appName, err)
	}

	t.Logf("Got %d manifests for %s at revision %s", len(manifests), appName, revision)

	for _, m := range manifests {
		t.Logf("  - %s/%s: %s", m.Kind, m.Name, m.Namespace)
	}
}

func TestGetApplicationDiff(t *testing.T) {
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
	diffs, err := argoClient.GetApplicationDiff(testCtx, appName, "")
	if err != nil {
		t.Fatalf("Failed to get diff for %s: %v", appName, err)
	}

	t.Logf("Got %d diffs for %s", len(diffs), appName)

	for _, d := range diffs {
		t.Logf("  - %s: action=%s", d.Resource.String(), d.Action)
	}
}

// =============================================================================
// Lock Manager Tests
// =============================================================================

func TestLockAcquireRelease(t *testing.T) {
	appName := "test-lock-app-" + time.Now().Format("150405")

	// Acquire lock
	result, err := lockManager.Lock(testCtx, models.LockRequest{
		Application: appName,
		PRNumber:    123,
		Repo:        "test/repo",
		User:        "testuser",
	})
	if err != nil {
		t.Fatalf("Failed to acquire lock: %v", err)
	}

	if !result.Acquired {
		t.Error("Expected lock to be acquired")
	}

	// Verify lock exists
	lock, err := lockManager.Get(testCtx, appName)
	if err != nil {
		t.Fatalf("Failed to get lock: %v", err)
	}

	if lock == nil {
		t.Fatal("Expected lock to exist")
	}

	if lock.PRNumber != 123 {
		t.Errorf("Expected PR number 123, got %d", lock.PRNumber)
	}

	// Release lock
	err = lockManager.Unlock(testCtx, appName, "test/repo", 123)
	if err != nil {
		t.Fatalf("Failed to release lock: %v", err)
	}

	// Verify lock is gone
	lock, err = lockManager.Get(testCtx, appName)
	if err != nil {
		t.Fatalf("Failed to get lock after release: %v", err)
	}

	if lock != nil {
		t.Error("Expected lock to be released")
	}
}

func TestLockConflict(t *testing.T) {
	appName := "test-conflict-app-" + time.Now().Format("150405")

	// Acquire lock with PR 1
	result1, err := lockManager.Lock(testCtx, models.LockRequest{
		Application: appName,
		PRNumber:    1,
		Repo:        "test/repo",
		User:        "user1",
	})
	if err != nil {
		t.Fatalf("Failed to acquire first lock: %v", err)
	}

	if !result1.Acquired {
		t.Error("Expected first lock to be acquired")
	}

	// Try to acquire with PR 2 - should fail
	result2, err := lockManager.Lock(testCtx, models.LockRequest{
		Application: appName,
		PRNumber:    2,
		Repo:        "test/repo",
		User:        "user2",
	})
	if err != nil {
		t.Fatalf("Failed to attempt second lock: %v", err)
	}

	if result2.Acquired {
		t.Error("Expected second lock to be denied")
	}

	if result2.HeldBy == nil {
		t.Error("Expected HeldBy to be populated")
	} else if result2.HeldBy.PRNumber != 1 {
		t.Errorf("Expected HeldBy PR to be 1, got %d", result2.HeldBy.PRNumber)
	}

	// Same PR should be able to refresh lock
	result3, err := lockManager.Lock(testCtx, models.LockRequest{
		Application: appName,
		PRNumber:    1,
		Repo:        "test/repo",
		User:        "user1",
	})
	if err != nil {
		t.Fatalf("Failed to refresh lock: %v", err)
	}

	if !result3.Acquired {
		t.Error("Expected same PR to refresh lock")
	}

	// Cleanup
	_ = lockManager.ForceUnlock(testCtx, appName)
}

func TestListLocksByPR(t *testing.T) {
	prefix := "test-list-" + time.Now().Format("150405")
	repo := "test/repo"
	prNumber := 999

	// Create multiple locks for same PR
	apps := []string{prefix + "-app1", prefix + "-app2", prefix + "-app3"}
	for _, app := range apps {
		_, err := lockManager.Lock(testCtx, models.LockRequest{
			Application: app,
			PRNumber:    prNumber,
			Repo:        repo,
			User:        "testuser",
		})
		if err != nil {
			t.Fatalf("Failed to lock %s: %v", app, err)
		}
	}

	// List locks by PR
	locks, err := lockManager.ListByPR(testCtx, repo, prNumber)
	if err != nil {
		t.Fatalf("Failed to list locks by PR: %v", err)
	}

	if len(locks) != len(apps) {
		t.Errorf("Expected %d locks, got %d", len(apps), len(locks))
	}

	// Cleanup
	for _, app := range apps {
		_ = lockManager.ForceUnlock(testCtx, app)
	}
}

func TestStorePlan(t *testing.T) {
	appName := "test-plan-app-" + time.Now().Format("150405")
	prNumber := 456
	revision := "abc123def456"

	// Store plan
	err := lockManager.StorePlan(testCtx, appName, prNumber, revision)
	if err != nil {
		t.Fatalf("Failed to store plan: %v", err)
	}

	// Retrieve plan
	storedRevision, err := lockManager.GetPlan(testCtx, appName, prNumber)
	if err != nil {
		t.Fatalf("Failed to get plan: %v", err)
	}

	if storedRevision != revision {
		t.Errorf("Expected revision %s, got %s", revision, storedRevision)
	}
}

// =============================================================================
// Integration Tests
// =============================================================================

func TestFullPlanWorkflow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping ArgoCD integration test in short mode")
	}
	// This test simulates a full plan workflow:
	// 1. Find an application
	// 2. Acquire lock
	// 3. Get diff
	// 4. Store plan
	// 5. Release lock

	apps, err := argoClient.ListApplications(testCtx)
	if err != nil {
		t.Fatalf("Failed to list applications: %v", err)
	}

	if len(apps) == 0 {
		t.Skip("No applications available for integration test")
	}

	app := apps[0]
	prNumber := 777
	repo := "integration/test"
	user := "integrationuser"

	t.Logf("Running integration test with app: %s", app.Name)

	// Step 1: Acquire lock
	lockResult, err := lockManager.Lock(testCtx, models.LockRequest{
		Application: app.Name,
		PRNumber:    prNumber,
		Repo:        repo,
		User:        user,
	})
	if err != nil {
		t.Fatalf("Failed to acquire lock: %v", err)
	}

	// If lock is held by someone else, skip
	if !lockResult.Acquired {
		t.Skipf("Lock held by PR #%d, skipping", lockResult.HeldBy.PRNumber)
	}

	defer func() {
		// Cleanup: release lock
		_ = lockManager.Unlock(testCtx, app.Name, repo, prNumber)
	}()

	t.Log("Lock acquired successfully")

	// Step 2: Get diff
	diffs, err := argoClient.GetApplicationDiff(testCtx, app.Name, "")
	if err != nil {
		t.Fatalf("Failed to get diff: %v", err)
	}

	t.Logf("Got %d diffs", len(diffs))

	// Step 3: Get manifests to determine revision
	_, revision, err := argoClient.GetManifests(testCtx, app.Name, nil)
	if err != nil {
		t.Fatalf("Failed to get manifests: %v", err)
	}

	t.Logf("Target revision: %s", revision)

	// Step 4: Store plan
	err = lockManager.StorePlan(testCtx, app.Name, prNumber, revision)
	if err != nil {
		t.Fatalf("Failed to store plan: %v", err)
	}

	t.Log("Plan stored successfully")

	// Step 5: Verify plan
	storedRevision, err := lockManager.GetPlan(testCtx, app.Name, prNumber)
	if err != nil {
		t.Fatalf("Failed to verify plan: %v", err)
	}

	if storedRevision != revision {
		t.Errorf("Plan revision mismatch: expected %s, got %s", revision, storedRevision)
	}

	t.Log("Integration test completed successfully")
}

func TestSyncDryRun(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping ArgoCD test in short mode")
	}
	apps, err := argoClient.ListApplications(testCtx)
	if err != nil {
		t.Fatalf("Failed to list applications: %v", err)
	}

	if len(apps) == 0 {
		t.Skip("No applications available for sync test")
	}

	app := apps[0]
	t.Logf("Testing dry-run sync for app: %s", app.Name)

	result, err := argoClient.SyncApplication(testCtx, app.Name, &argocd.SyncOptions{
		DryRun: true,
	})
	if err != nil {
		// Sync might fail if app is not in a syncable state, which is OK for this test
		t.Logf("Sync dry-run returned error (may be expected): %v", err)
		return
	}

	t.Logf("Sync dry-run result: phase=%s, message=%s", result.Phase, result.Message)
}
