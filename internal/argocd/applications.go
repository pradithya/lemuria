package argocd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	v1alpha1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"

	"github.com/org/lemuria/internal/models"
)

// ListApplications returns all applications from Argo CD.
func (c *Client) ListApplications(ctx context.Context) ([]models.Application, error) {
	return c.ListApplicationsWithSelector(ctx, "")
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
	var resp v1alpha1.Application
	if err := c.get(ctx, "/api/v1/applications/"+url.PathEscape(name), nil, &resp); err != nil {
		return nil, fmt.Errorf("getting application %s: %w", name, err)
	}

	app := convertV1alpha1Application(resp, "")
	return &app, nil
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

// SyncApplication triggers a sync for the specified application and waits for it to complete.
func (c *Client) SyncApplication(ctx context.Context, name string, opts *SyncOptions) (*models.SyncResult, error) {
	payload := map[string]interface{}{
		"name": name,
	}

	if opts != nil {
		if opts.Revision != "" {
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

	// Trigger the sync. The response may not reflect the new operation state yet
	// because ArgoCD processes the operation asynchronously.
	if err := c.post(ctx, "/api/v1/applications/"+url.PathEscape(name)+"/sync", nil, payload, nil); err != nil {
		return nil, fmt.Errorf("syncing application %s: %w", name, err)
	}

	// Poll until the operation reaches a terminal phase.
	return c.waitForSyncComplete(ctx, name, syncWaitTimeout)
}

const syncWaitTimeout = 3 * time.Minute

// waitForSyncComplete watches the application using the streaming Watch API until
// the operation reaches a terminal phase and health stabilizes.
func (c *Client) waitForSyncComplete(ctx context.Context, name string, timeout time.Duration) (*models.SyncResult, error) {
	watchCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	events, err := c.watchApplication(watchCtx, name)
	if err != nil {
		return nil, fmt.Errorf("watching application %s: %w", name, err)
	}

	var syncResult *models.SyncResult

	for event := range events {
		status := extractAppSyncStatus(&event)
		phase := models.SyncPhase(status.OperationPhase)

		// Wait for sync to reach a terminal phase.
		if syncResult == nil {
			switch phase {
			case models.SyncPhaseSucceeded, models.SyncPhaseFailed, models.SyncPhaseError:
				syncResult = buildSyncResult(name, status)
				if phase != models.SyncPhaseSucceeded {
					return syncResult, nil
				}
			default:
				continue
			}
		}

		// Sync succeeded — wait for health to stabilize.
		healthStatus := models.HealthStatus(status.HealthStatus)
		if healthStatus != models.HealthStatusProgressing && healthStatus != "" {
			syncResult.HealthStatus = healthStatus
			return syncResult, nil
		}
	}

	// Stream ended (timeout or connection closed).
	if syncResult == nil {
		return nil, fmt.Errorf("timeout waiting for sync to complete for %s", name)
	}

	// Sync completed but health didn't stabilize before timeout.
	syncResult.HealthStatus = models.HealthStatusProgressing
	return syncResult, nil
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

// SyncOptions configures a sync operation.
type SyncOptions struct {
	Revision  string
	Prune     bool
	DryRun    bool
	Resources []SyncResource
}

// SyncResource identifies a specific resource to sync.
type SyncResource struct {
	Group     string `json:"group"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
}

// FindApplicationsByRepo returns applications that reference the given repository.
func (c *Client) FindApplicationsByRepo(ctx context.Context, repoURL string) ([]models.Application, error) {
	apps, err := c.ListApplications(ctx)
	if err != nil {
		return nil, err
	}

	var matched []models.Application
	for _, app := range apps {
		for _, url := range app.GetRepoURLs() {
			if NormalizeRepoURL(url) == NormalizeRepoURL(repoURL) {
				matched = append(matched, app)
				break
			}
		}
	}

	return matched, nil
}

// NormalizeRepoURL removes protocol and .git suffix for comparison.
func NormalizeRepoURL(u string) string {
	// Convert to lowercase first for case-insensitive prefix matching
	u = strings.ToLower(u)
	u = strings.TrimPrefix(u, "https://")
	u = strings.TrimPrefix(u, "http://")
	u = strings.TrimPrefix(u, "git@")
	u = strings.Replace(u, ":", "/", 1)
	u = strings.TrimSuffix(u, ".git")
	return u
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

// RollbackOptions configures a rollback operation.
type RollbackOptions struct {
	ID     int64
	Prune  bool
	DryRun bool
}

// RollbackApplication rolls back an application to a previous deployment.
func (c *Client) RollbackApplication(ctx context.Context, name string, opts *RollbackOptions) (*models.SyncResult, error) {
	if opts == nil || opts.ID == 0 {
		return nil, fmt.Errorf("rollback ID is required")
	}

	payload := map[string]interface{}{
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
