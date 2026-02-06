package argocd

import (
	"context"
	"fmt"
	"net/url"
	"strings"

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

// SyncApplication triggers a sync for the specified application.
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

	var resp struct {
		Status struct {
			OperationState struct {
				Phase   string `json:"phase"`
				Message string `json:"message"`
			} `json:"operationState"`
		} `json:"status"`
	}

	if err := c.post(ctx, "/api/v1/applications/"+url.PathEscape(name)+"/sync", nil, payload, &resp); err != nil {
		return nil, fmt.Errorf("syncing application %s: %w", name, err)
	}

	return &models.SyncResult{
		Application: name,
		Phase:       models.SyncPhase(resp.Status.OperationState.Phase),
		Message:     resp.Status.OperationState.Message,
	}, nil
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
