package argocd

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/org/lemuria/internal/models"
)

// applicationResponse represents the Argo CD application API response.
type applicationResponse struct {
	Metadata struct {
		Name            string            `json:"name"`
		Namespace       string            `json:"namespace"`
		Labels          map[string]string `json:"labels"`
		OwnerReferences []struct {
			Kind string `json:"kind"`
			Name string `json:"name"`
		} `json:"ownerReferences"`
	} `json:"metadata"`
	Spec struct {
		Project string `json:"project"`
		Source  *struct {
			RepoURL        string `json:"repoURL"`
			Path           string `json:"path"`
			TargetRevision string `json:"targetRevision"`
		} `json:"source,omitempty"`
		Sources []struct {
			RepoURL        string `json:"repoURL"`
			Path           string `json:"path"`
			TargetRevision string `json:"targetRevision"`
			Chart          string `json:"chart"`
			Helm           *struct {
				ValueFiles []string `json:"valueFiles"`
				Values     string   `json:"values"`
			} `json:"helm"`
		} `json:"sources,omitempty"`
		Destination struct {
			Server    string `json:"server"`
			Namespace string `json:"namespace"`
		} `json:"destination"`
		SyncPolicy *struct {
			Automated *struct {
				Prune    bool `json:"prune"`
				SelfHeal bool `json:"selfHeal"`
			} `json:"automated,omitempty"`
		} `json:"syncPolicy,omitempty"`
	} `json:"spec"`
	Status struct {
		Sync struct {
			Status string `json:"status"`
		} `json:"sync"`
		Health struct {
			Status string `json:"status"`
		} `json:"health"`
	} `json:"status"`
}

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

	var resp struct {
		Items []applicationResponse `json:"items"`
	}

	if err := c.get(ctx, "/api/v1/applications", query, &resp); err != nil {
		return nil, fmt.Errorf("listing applications: %w", err)
	}

	apps := make([]models.Application, 0, len(resp.Items))
	for _, item := range resp.Items {
		apps = append(apps, convertApplication(item))
	}

	return apps, nil
}

// GetApplication returns a specific application by name.
func (c *Client) GetApplication(ctx context.Context, name string) (*models.Application, error) {
	var resp applicationResponse
	if err := c.get(ctx, "/api/v1/applications/"+url.PathEscape(name), nil, &resp); err != nil {
		return nil, fmt.Errorf("getting application %s: %w", name, err)
	}

	app := convertApplication(resp)
	return &app, nil
}

// convertApplication converts the API response to our model.
func convertApplication(resp applicationResponse) models.Application {
	app := models.Application{
		Name:                 resp.Metadata.Name,
		Namespace:            resp.Metadata.Namespace,
		Project:              resp.Spec.Project,
		DestinationServer:    resp.Spec.Destination.Server,
		DestinationNamespace: resp.Spec.Destination.Namespace,
		SyncStatus:           models.SyncStatus(resp.Status.Sync.Status),
		HealthStatus:         models.HealthStatus(resp.Status.Health.Status),
		Labels:               resp.Metadata.Labels,
		AutoSyncEnabled:      resp.Spec.SyncPolicy != nil && resp.Spec.SyncPolicy.Automated != nil,
	}

	// Single source
	if resp.Spec.Source != nil {
		app.RepoURL = resp.Spec.Source.RepoURL
		app.Path = resp.Spec.Source.Path
		app.TargetRevision = resp.Spec.Source.TargetRevision
	}

	// Multi-source
	if len(resp.Spec.Sources) > 0 {
		app.Sources = make([]models.ApplicationSource, len(resp.Spec.Sources))
		for i, src := range resp.Spec.Sources {
			app.Sources[i] = models.ApplicationSource{
				RepoURL:        src.RepoURL,
				Path:           src.Path,
				TargetRevision: src.TargetRevision,
				Chart:          src.Chart,
			}
			if src.Helm != nil {
				app.Sources[i].Helm = &models.HelmSource{
					ValueFiles: src.Helm.ValueFiles,
					Values:     src.Helm.Values,
				}
			}
		}
	}

	// ApplicationSet name from label or ownerReferences
	if appSetName, ok := resp.Metadata.Labels["argocd.argoproj.io/application-set-name"]; ok {
		app.ApplicationSetName = appSetName
	} else {
		// Check ownerReferences for ApplicationSet
		for _, owner := range resp.Metadata.OwnerReferences {
			if owner.Kind == "ApplicationSet" {
				app.ApplicationSetName = owner.Name
				break
			}
		}
	}

	return app
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

// ApplicationHistoryEntry represents a deployment history entry.
type ApplicationHistoryEntry struct {
	ID         int64  `json:"id"`
	Revision   string `json:"revision"`
	DeployedAt string `json:"deployedAt"`
	Source     struct {
		RepoURL        string `json:"repoURL"`
		Path           string `json:"path"`
		TargetRevision string `json:"targetRevision"`
	} `json:"source"`
}

// GetApplicationHistory returns the deployment history for an application.
func (c *Client) GetApplicationHistory(ctx context.Context, name string) ([]ApplicationHistoryEntry, error) {
	var resp struct {
		Status struct {
			History []ApplicationHistoryEntry `json:"history"`
		} `json:"status"`
	}

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
