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

package argocd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	v1alpha1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"

	"github.com/org/lemuria/internal/models"
)

// SyncOptions configures a sync operation.
type SyncOptions struct {
	Revision        string
	Revisions       []string // per-source revisions (multi-source apps)
	SourcePositions []int64  // 1-based source positions (multi-source apps)
	Prune           bool
	DryRun          bool
	Resources       []SyncResource
	Timeout         time.Duration // Maximum time to wait for sync and healthy state. 0 means use default (10m).
}

// SyncResource identifies a specific resource to sync.
type SyncResource struct {
	Group     string `json:"group"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
}

// RollbackOptions configures a rollback operation.
type RollbackOptions struct {
	ID     int64
	Prune  bool
	DryRun bool
}

// appSyncStatus holds the operation state and health from the ArgoCD API.
type appSyncStatus struct {
	OperationPhase string
	HealthStatus   string
	Message        string
	SyncResult     struct {
		Revision  string
		Resources []syncResourceResult
	}
}

type syncResourceResult struct {
	Group     string `json:"group"`
	Version   string `json:"version"`
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	Message   string `json:"message"`
	HookType  string `json:"hookType,omitempty"`
}

// watchEvent represents a single event from the ArgoCD Watch API stream.
type watchEvent struct {
	Result struct {
		Type        string `json:"type"`
		Application struct {
			Status struct {
				Health struct {
					Status string `json:"status"`
				} `json:"health"`
				OperationState *struct {
					Phase      string `json:"phase"`
					Message    string `json:"message"`
					SyncResult struct {
						Revision  string               `json:"revision"`
						Resources []syncResourceResult `json:"resources"`
					} `json:"syncResult"`
				} `json:"operationState"`
			} `json:"status"`
		} `json:"application"`
	} `json:"result"`
}

// ListApplications returns all non-temporary applications from Argo CD.
// Temporary applications (created for diff generation) are excluded via label selector.
func (c *Client) ListApplications(ctx context.Context) ([]models.Application, error) {
	return c.ListApplicationsWithSelector(ctx, "!"+labelTempApp)
}

// ListApplicationsWithSelector returns applications matching a label selector.
func (c *Client) ListApplicationsWithSelector(ctx context.Context, selector string) ([]models.Application, error) {
	query := url.Values{}
	if selector != "" {
		query.Set("selector", selector)
	}

	var resp v1alpha1.ApplicationList
	if err := c.get(ctx, "/api/v1/applications", query, &resp); err != nil {
		return nil, fmt.Errorf("listing applications: %w", err)
	}

	apps := make([]models.Application, 0, len(resp.Items))
	for _, item := range resp.Items {
		apps = append(apps, convertV1alpha1Application(item, ""))
	}

	return apps, nil
}

// GetApplication returns a specific application by name.
func (c *Client) GetApplication(ctx context.Context, name string) (*models.Application, error) {
	slog.Debug("fetching application", "application", name)
	var resp v1alpha1.Application
	if err := c.get(ctx, "/api/v1/applications/"+url.PathEscape(name), nil, &resp); err != nil {
		slog.Debug("failed to fetch application", "application", name, "error", err)
		return nil, fmt.Errorf("getting application %s: %w", name, err)
	}

	app := convertV1alpha1Application(resp, "")
	slog.Debug("application fetched", "application", name, "healthStatus", app.HealthStatus, "syncStatus", app.SyncStatus)
	return &app, nil
}

// GetResourceHealth fetches per-resource health information for an application.
func (c *Client) GetResourceHealth(ctx context.Context, name string) ([]models.ResourceHealthInfo, error) {
	var resp v1alpha1.Application
	if err := c.get(ctx, "/api/v1/applications/"+url.PathEscape(name), nil, &resp); err != nil {
		return nil, fmt.Errorf("getting resource health for %s: %w", name, err)
	}

	var results []models.ResourceHealthInfo
	for _, r := range resp.Status.Resources {
		if r.Health == nil {
			continue
		}
		apiVersion := r.Version
		if r.Group != "" {
			apiVersion = r.Group + "/" + r.Version
		}
		results = append(results, models.ResourceHealthInfo{
			Resource: models.ResourceKey{
				APIVersion: apiVersion,
				Kind:       r.Kind,
				Name:       r.Name,
				Namespace:  r.Namespace,
			},
			HealthStatus:  models.HealthStatus(r.Health.Status),
			HealthMessage: r.Health.Message,
		})
	}

	return results, nil
}

// SyncApplication triggers a sync for the specified application and waits for it to complete.
func (c *Client) SyncApplication(ctx context.Context, name string, opts *SyncOptions) (*models.SyncResult, error) {
	payload := map[string]any{
		"name": name,
	}

	if opts != nil {
		if len(opts.Revisions) > 0 {
			payload["revisions"] = opts.Revisions
			payload["sourcePositions"] = opts.SourcePositions
		} else if opts.Revision != "" {
			payload["revision"] = opts.Revision
		}
		if opts.Prune {
			payload["prune"] = true
		}
		if opts.DryRun {
			payload["dryRun"] = true
		}
		if len(opts.Resources) > 0 {
			payload["resources"] = opts.Resources
		}
	}

	// Determine timeout: use opts.Timeout if set, otherwise default.
	timeout := defaultSyncWaitTimeout
	if opts != nil && opts.Timeout > 0 {
		timeout = opts.Timeout
	}

	// Open the watch stream BEFORE triggering sync to avoid a race condition
	// where the sync completes before the watch stream is established, causing
	// the watch to miss all events and eventually time out.
	watchCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	events, err := c.watchApplication(watchCtx, name)
	if err != nil {
		return nil, fmt.Errorf("watching application %s: %w", name, err)
	}

	// Trigger the sync. The response may not reflect the new operation state yet
	// because ArgoCD processes the operation asynchronously.
	if err := c.post(ctx, "/api/v1/applications/"+url.PathEscape(name)+"/sync", nil, payload, nil); err != nil {
		return nil, fmt.Errorf("syncing application %s: %w", name, err)
	}

	// Wait until the operation reaches a terminal phase using the pre-opened watch stream.
	return c.waitForSyncComplete(ctx, name, timeout, events)
}

// FindApplicationsByRepo returns applications that reference the given repository.
func (c *Client) FindApplicationsByRepo(ctx context.Context, repoURL string) ([]models.Application, error) {
	apps, err := c.ListApplications(ctx)
	if err != nil {
		return nil, err
	}

	normalizedRepo := NormalizeRepoURL(repoURL)
	var matched []models.Application
	for _, app := range apps {
		for _, appURL := range app.GetRepoURLs() {
			if NormalizeRepoURL(appURL) == normalizedRepo {
				matched = append(matched, app)
				break
			}
		}
	}

	return matched, nil
}

// UpdateApplicationSpec updates only the spec of an existing application.
func (c *Client) UpdateApplicationSpec(ctx context.Context, name string, spec v1alpha1.ApplicationSpec) error {
	if err := c.put(ctx, "/api/v1/applications/"+url.PathEscape(name)+"/spec", nil, &spec, nil); err != nil {
		return fmt.Errorf("updating application spec %s: %w", name, err)
	}
	return nil
}

// GetApplicationHistory returns the deployment history for an application.
func (c *Client) GetApplicationHistory(ctx context.Context, name string) ([]v1alpha1.RevisionHistory, error) {
	var resp v1alpha1.Application
	if err := c.get(ctx, "/api/v1/applications/"+url.PathEscape(name), nil, &resp); err != nil {
		return nil, fmt.Errorf("getting application history for %s: %w", name, err)
	}

	return resp.Status.History, nil
}

// RollbackApplication rolls back an application to a previous deployment.
func (c *Client) RollbackApplication(ctx context.Context, name string, opts *RollbackOptions) (*models.SyncResult, error) {
	if opts == nil || opts.ID == 0 {
		return nil, fmt.Errorf("rollback ID is required")
	}

	payload := map[string]any{
		"id": opts.ID,
	}

	if opts.Prune {
		payload["prune"] = true
	}
	if opts.DryRun {
		payload["dryRun"] = true
	}

	var resp struct {
		Status struct {
			OperationState struct {
				Phase   string `json:"phase"`
				Message string `json:"message"`
			} `json:"operationState"`
		} `json:"status"`
	}

	if err := c.post(ctx, "/api/v1/applications/"+url.PathEscape(name)+"/rollback", nil, payload, &resp); err != nil {
		return nil, fmt.Errorf("rolling back application %s: %w", name, err)
	}

	return &models.SyncResult{
		Application: name,
		Phase:       models.SyncPhase(resp.Status.OperationState.Phase),
		Message:     resp.Status.OperationState.Message,
	}, nil
}

// CreateApplication creates a new application in ArgoCD.
func (c *Client) CreateApplication(ctx context.Context, app *v1alpha1.Application) error {
	if err := c.post(ctx, "/api/v1/applications", nil, app, nil); err != nil {
		return fmt.Errorf("creating application: %w", err)
	}
	return nil
}

// DeleteApplication deletes an application from ArgoCD.
func (c *Client) DeleteApplication(ctx context.Context, name string, cascade bool) error {
	query := url.Values{}
	if cascade {
		query.Set("cascade", "true")
	} else {
		query.Set("cascade", "false")
	}

	if err := c.delete(ctx, "/api/v1/applications/"+url.PathEscape(name), query); err != nil {
		return fmt.Errorf("deleting application %s: %w", name, err)
	}
	return nil
}

// GetApplicationRaw returns the application as a typed v1alpha1.Application.
func (c *Client) GetApplicationRaw(ctx context.Context, name string) (*v1alpha1.Application, error) {
	var resp v1alpha1.Application
	if err := c.get(ctx, "/api/v1/applications/"+url.PathEscape(name), nil, &resp); err != nil {
		return nil, fmt.Errorf("getting application %s: %w", name, err)
	}
	return &resp, nil
}

// defaultSyncWaitTimeout is the default maximum time to wait for a sync operation
// to complete and the application to become healthy, used when no timeout is
// specified in SyncOptions. Aligned with ArgoCD's default operation timeout.
const defaultSyncWaitTimeout = 10 * time.Minute

// healthStabilizationPeriod is the duration to wait after seeing Healthy status
// to confirm it's stable. This catches cases where health briefly shows Healthy
// before transitioning to Degraded (e.g., when a pod fails to pull an image).
const healthStabilizationPeriod = 10 * time.Second

// missingHealthGracePeriod is the duration to wait for health to transition
// from Missing after sync succeeds. If health stays Missing for this period,
// we treat it as success (the app has no health-assessable resources or
// ArgoCD cannot evaluate health).
const missingHealthGracePeriod = 30 * time.Second

// waitForSyncComplete watches the application using the streaming Watch API until
// the operation reaches a terminal phase and the application becomes healthy.
// Returns an error if the application enters a degraded state or fails to become
// healthy within the timeout period.
//
// When preOpenedEvents is non-nil, it is used instead of opening a new watch stream.
// This allows callers (like SyncApplication) to open the watch before triggering the
// operation, avoiding a race where the operation completes before the watch starts.
func (c *Client) waitForSyncComplete(ctx context.Context, name string, timeout time.Duration, preOpenedEvents <-chan watchEvent) (*models.SyncResult, error) {
	slog.Debug("waiting for sync to complete", "application", name, "timeout", timeout)

	// Always enforce the timeout regardless of whether the events channel
	// was pre-opened by the caller. This ensures consistent timeout semantics
	// and prevents indefinite waits if the caller's channel context is longer.
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	events := preOpenedEvents
	if events == nil {
		var err error
		events, err = c.watchApplication(ctx, name)
		if err != nil {
			return nil, fmt.Errorf("watching application %s: %w", name, err)
		}
	}

	// When using a pre-opened watch stream (opened before sync trigger), the
	// first events may reflect the PREVIOUS operation's state. We must skip
	// these stale events to avoid prematurely treating the old operation's
	// terminal phase as the new sync's result. We skip until we see a
	// non-terminal phase (e.g., Running), which indicates the new sync has started.
	skipStaleEvents := preOpenedEvents != nil

	var syncResult *models.SyncResult
	var healthyAt time.Time // When we first saw Healthy status
	var missingAt time.Time // When health first stayed Missing after sync succeeded

	// stabilizationTimer fires after the health stabilization period.
	// It's nil until the app first becomes healthy and is stopped if health
	// regresses (e.g., back to Progressing). This ensures we don't block
	// indefinitely on the watch channel waiting for events that may never come.
	var stabilizationTimer *time.Timer
	// missingGraceTimer fires after the missing health grace period.
	// It's nil until health stays Missing after sync succeeds, and is stopped
	// if health transitions to a non-Missing state. This prevents waiting
	// forever for apps that have no health-assessable resources.
	var missingGraceTimer *time.Timer
	defer func() {
		if stabilizationTimer != nil {
			stabilizationTimer.Stop()
		}
		if missingGraceTimer != nil {
			missingGraceTimer.Stop()
		}
	}()

	for {
		var event watchEvent
		var ok bool

		// When a timer is running, use select so we can return as soon
		// as the timer fires, even without a new event.
		if stabilizationTimer != nil || missingGraceTimer != nil {
			stabilizationCh := nilTimerChan(stabilizationTimer)
			missingGraceCh := nilTimerChan(missingGraceTimer)
			select {
			case event, ok = <-events:
				if !ok {
					// Channel closed — handle below the loop.
					goto streamEnded
				}
			case <-stabilizationCh:
				slog.Debug("health stabilized, sync complete",
					"application", name,
					"stableDuration", time.Since(healthyAt),
				)
				syncResult.HealthStatus = models.HealthStatusHealthy
				return syncResult, nil
			case <-missingGraceCh:
				slog.Debug("health stayed Missing after grace period, treating as success",
					"application", name,
					"missingDuration", time.Since(missingAt),
				)
				syncResult.HealthStatus = models.HealthStatusMissing
				return syncResult, nil
			}
		} else {
			event, ok = <-events
			if !ok {
				goto streamEnded
			}
		}

		status := extractAppSyncStatus(&event)
		phase := models.SyncPhase(status.OperationPhase)
		healthStatus := models.HealthStatus(status.HealthStatus)

		slog.Debug("received watch event",
			"application", name,
			"phase", phase,
			"health", healthStatus,
			"syncCompleted", syncResult != nil,
			"healthyAt", healthyAt,
			"skipStale", skipStaleEvents,
		)

		// When using a pre-opened watch, skip events from the previous operation.
		// Terminal phases (Succeeded/Failed/Error) or empty phases (no operation)
		// before we see a Running phase are stale — they predate the sync POST.
		if skipStaleEvents {
			isTerminal := phase == models.SyncPhaseSucceeded || phase == models.SyncPhaseFailed || phase == models.SyncPhaseError
			noOperation := status.OperationPhase == ""
			if isTerminal || noOperation {
				slog.Debug("skipping stale event from previous operation",
					"application", name,
					"phase", phase,
					"health", healthStatus,
				)
				continue
			}
			// Non-terminal, non-empty phase (e.g., Running) means the new sync has started.
			skipStaleEvents = false
			slog.Debug("new sync operation detected, processing events normally",
				"application", name,
				"phase", phase,
			)
		}

		// Wait for sync to reach a terminal phase.
		if syncResult == nil {
			switch phase {
			case models.SyncPhaseSucceeded, models.SyncPhaseFailed, models.SyncPhaseError:
				slog.Debug("sync reached terminal phase",
					"application", name,
					"phase", phase,
					"health", healthStatus,
				)
				syncResult = buildSyncResult(name, status)
				if phase != models.SyncPhaseSucceeded {
					slog.Debug("sync failed, returning early",
						"application", name,
						"phase", phase,
					)
					return syncResult, nil
				}
				slog.Debug("sync succeeded, now waiting for health",
					"application", name,
					"currentHealth", healthStatus,
				)
			default:
				continue
			}
		}

		// Sync succeeded — wait for health to become Healthy or reach a terminal state.
		switch healthStatus {
		case models.HealthStatusHealthy:
			// Reset missing grace timer since health transitioned
			if !missingAt.IsZero() {
				missingAt = time.Time{}
				if missingGraceTimer != nil {
					missingGraceTimer.Stop()
					missingGraceTimer = nil
				}
			}
			// Track when we first saw Healthy
			if healthyAt.IsZero() {
				healthyAt = time.Now()
				stabilizationTimer = time.NewTimer(healthStabilizationPeriod)
				slog.Debug("application became healthy, starting stabilization period",
					"application", name,
					"stabilizationPeriod", healthStabilizationPeriod,
				)
			}

			// An event arrived while the timer is still running — check if
			// the stabilization period has already elapsed.
			if time.Since(healthyAt) >= healthStabilizationPeriod {
				slog.Debug("health stabilized, sync complete",
					"application", name,
					"stableDuration", time.Since(healthyAt),
				)
				syncResult.HealthStatus = healthStatus
				return syncResult, nil
			}

			// Still in stabilization period, continue watching
			slog.Debug("health stabilization in progress",
				"application", name,
				"elapsed", time.Since(healthyAt),
				"remaining", healthStabilizationPeriod-time.Since(healthyAt),
			)
			continue

		case models.HealthStatusDegraded:
			// Failure: application entered degraded state
			slog.Debug("application health degraded",
				"application", name,
				"wasHealthy", !healthyAt.IsZero(),
			)
			syncResult.Phase = models.SyncPhaseFailed
			syncResult.HealthStatus = healthStatus
			syncResult.Message = "application health is Degraded"
			return syncResult, nil

		case models.HealthStatusSuspended:
			// Suspended is a valid terminal state (user-intended)
			slog.Debug("application is suspended",
				"application", name,
			)
			syncResult.HealthStatus = healthStatus
			return syncResult, nil

		case models.HealthStatusMissing:
			// Missing can be transient for new apps (health not yet evaluated)
			// or permanent for apps with no health-assessable resources.
			// Reset healthy timer and start missing grace timer.
			if !healthyAt.IsZero() {
				slog.Debug("health changed from Healthy to Missing, resetting stabilization",
					"application", name,
				)
				healthyAt = time.Time{}
				if stabilizationTimer != nil {
					stabilizationTimer.Stop()
					stabilizationTimer = nil
				}
			}
			// Start missing grace timer if not already running
			if missingAt.IsZero() {
				missingAt = time.Now()
				missingGraceTimer = time.NewTimer(missingHealthGracePeriod)
				slog.Debug("health is missing, starting grace period",
					"application", name,
					"gracePeriod", missingHealthGracePeriod,
				)
			} else if time.Since(missingAt) >= missingHealthGracePeriod {
				// Grace period elapsed during event processing
				slog.Debug("health stayed Missing after grace period, treating as success",
					"application", name,
					"missingDuration", time.Since(missingAt),
				)
				syncResult.HealthStatus = models.HealthStatusMissing
				return syncResult, nil
			}
			slog.Debug("application health is missing, waiting for evaluation",
				"application", name,
			)
			continue

		case models.HealthStatusUnknown:
			// Failure: unexpected terminal health state
			slog.Debug("application health is unknown",
				"application", name,
				"health", healthStatus,
			)
			syncResult.Phase = models.SyncPhaseFailed
			syncResult.HealthStatus = healthStatus
			syncResult.Message = fmt.Sprintf("application health is %s", healthStatus)
			return syncResult, nil

		case models.HealthStatusProgressing, "":
			// Reset healthy timer if we go back to progressing
			if !healthyAt.IsZero() {
				slog.Debug("health changed from Healthy to Progressing, resetting stabilization",
					"application", name,
				)
				healthyAt = time.Time{}
				if stabilizationTimer != nil {
					stabilizationTimer.Stop()
					stabilizationTimer = nil
				}
			}
			// Reset missing grace timer since health transitioned
			if !missingAt.IsZero() {
				missingAt = time.Time{}
				if missingGraceTimer != nil {
					missingGraceTimer.Stop()
					missingGraceTimer = nil
				}
			}
			slog.Debug("application still progressing, waiting for next event",
				"application", name,
			)
			continue

		default:
			// Unknown health status, treat as failure
			slog.Debug("unexpected health status",
				"application", name,
				"health", healthStatus,
			)
			syncResult.Phase = models.SyncPhaseFailed
			syncResult.HealthStatus = healthStatus
			syncResult.Message = fmt.Sprintf("unexpected health status: %s", healthStatus)
			return syncResult, nil
		}
	}

streamEnded:

	// Stream ended (timeout or connection closed) before health stabilized.
	if syncResult == nil {
		slog.Debug("timeout waiting for sync operation",
			"application", name,
		)
		return nil, fmt.Errorf("timeout waiting for sync to complete for %s", name)
	}

	// If we saw healthy but didn't stabilize, still consider it a success
	// (the stream ended, likely due to timeout, but last known state was healthy)
	if !healthyAt.IsZero() {
		slog.Debug("stream ended during stabilization, accepting healthy state",
			"application", name,
			"healthyDuration", time.Since(healthyAt),
		)
		syncResult.HealthStatus = models.HealthStatusHealthy
		return syncResult, nil
	}

	// Sync completed but health didn't become healthy before timeout.
	slog.Debug("timeout waiting for healthy state after sync",
		"application", name,
		"lastHealth", syncResult.HealthStatus,
	)
	return &models.SyncResult{
		Application:  syncResult.Application,
		Revision:     syncResult.Revision,
		Phase:        models.SyncPhaseFailed,
		Message:      "timeout waiting for application to become healthy",
		Resources:    syncResult.Resources,
		HealthStatus: models.HealthStatusProgressing,
	}, nil
}

// watchApplication opens a streaming connection to the ArgoCD Watch API
// and returns a channel of watch events. The channel is closed when the
// context is cancelled or the connection is closed.
func (c *Client) watchApplication(ctx context.Context, name string) (<-chan watchEvent, error) {
	query := url.Values{}
	query.Set("name", name)

	u, err := url.Parse(c.baseURL + "/api/v1/stream/applications")
	if err != nil {
		return nil, fmt.Errorf("parsing watch URL: %w", err)
	}
	u.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("creating watch request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)

	// Use a separate client without timeout — the watch stream is long-lived.
	watchClient := *c.httpClient
	watchClient.Timeout = 0

	resp, err := watchClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("starting watch stream: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return nil, fmt.Errorf("watch API error (status %d): %s", resp.StatusCode, string(body))
	}

	ch := make(chan watchEvent)
	go func() {
		defer close(ch)
		defer func() { _ = resp.Body.Close() }()

		decoder := json.NewDecoder(resp.Body)
		for {
			var event watchEvent
			if err := decoder.Decode(&event); err != nil {
				return // stream ended, connection closed, or context cancelled
			}
			select {
			case ch <- event:
			case <-ctx.Done():
				return
			}
		}
	}()

	return ch, nil
}

// extractAppSyncStatus extracts the sync status from a watch event.
func extractAppSyncStatus(event *watchEvent) *appSyncStatus {
	result := &appSyncStatus{
		HealthStatus: event.Result.Application.Status.Health.Status,
	}

	if event.Result.Application.Status.OperationState != nil {
		op := event.Result.Application.Status.OperationState
		result.OperationPhase = op.Phase
		result.Message = op.Message
		result.SyncResult.Revision = op.SyncResult.Revision
		result.SyncResult.Resources = op.SyncResult.Resources
	}

	return result
}

// convertV1alpha1Application converts a v1alpha1.Application to our domain model.
// sourceFile is the git file path where this app CR is defined (empty for API-sourced apps).
func convertV1alpha1Application(app v1alpha1.Application, sourceFile string) models.Application {
	result := models.Application{
		Name:                 app.Name,
		Namespace:            app.Namespace,
		Project:              app.Spec.Project,
		DestinationServer:    app.Spec.Destination.Server,
		DestinationNamespace: app.Spec.Destination.Namespace,
		SyncStatus:           models.SyncStatus(app.Status.Sync.Status),
		HealthStatus:         models.HealthStatus(app.Status.Health.Status),
		Labels:               app.Labels,
		AutoSyncEnabled:      app.Spec.SyncPolicy != nil && app.Spec.SyncPolicy.Automated != nil,
		SourceFile:           sourceFile,
	}

	if result.Namespace == "" {
		result.Namespace = "argocd"
	}

	if result.Project == "" {
		result.Project = "default"
	}

	// Single source
	if app.Spec.Source != nil {
		result.RepoURL = app.Spec.Source.RepoURL
		result.Path = app.Spec.Source.Path
		result.TargetRevision = app.Spec.Source.TargetRevision
	}

	// Multi-source
	if len(app.Spec.Sources) > 0 {
		result.Sources = make([]models.ApplicationSource, len(app.Spec.Sources))
		for i, src := range app.Spec.Sources {
			result.Sources[i] = models.ApplicationSource{
				RepoURL:        src.RepoURL,
				Path:           src.Path,
				TargetRevision: src.TargetRevision,
				Chart:          src.Chart,
				Ref:            src.Ref,
			}
			if src.Helm != nil {
				result.Sources[i].Helm = &models.HelmSource{
					ValueFiles: src.Helm.ValueFiles,
					Values:     src.Helm.Values,
				}
			}
		}
	}

	// ApplicationSet name from label or ownerReferences
	if appSetName, ok := app.Labels["argocd.argoproj.io/application-set-name"]; ok {
		result.ApplicationSetName = appSetName
	} else {
		for _, owner := range app.OwnerReferences {
			if owner.Kind == "ApplicationSet" {
				result.ApplicationSetName = owner.Name
				break
			}
		}
	}

	return result
}

// nilTimerChan returns the timer's channel, or a nil channel if the timer is nil.
// A nil channel blocks forever in select, effectively disabling that case.
func nilTimerChan(t *time.Timer) <-chan time.Time {
	if t == nil {
		return nil
	}
	return t.C
}

// buildSyncResult converts an appSyncStatus into a SyncResult.
func buildSyncResult(name string, status *appSyncStatus) *models.SyncResult {
	result := &models.SyncResult{
		Application:  name,
		Phase:        models.SyncPhase(status.OperationPhase),
		Message:      status.Message,
		Revision:     status.SyncResult.Revision,
		HealthStatus: models.HealthStatus(status.HealthStatus),
	}

	for _, r := range status.SyncResult.Resources {
		// Skip hook resources (PreSync, PostSync, SyncFail)
		if r.HookType != "" {
			continue
		}
		apiVersion := r.Version
		if r.Group != "" {
			apiVersion = r.Group + "/" + r.Version
		}
		result.Resources = append(result.Resources, models.ResourceResult{
			Resource: models.ResourceKey{
				APIVersion: apiVersion,
				Kind:       r.Kind,
				Name:       r.Name,
				Namespace:  r.Namespace,
			},
			Status:  r.Status,
			Message: r.Message,
		})
	}

	return result
}
