package argocd

import (
	"context"
	"fmt"
	"net/url"

	"github.com/org/lemuria/internal/models"
)

// applicationSetResponse represents the Argo CD ApplicationSet API response.
type applicationSetResponse struct {
	Metadata struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	} `json:"metadata"`
	Spec struct {
		Template struct {
			Metadata struct {
				Name string `json:"name"`
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
				} `json:"sources,omitempty"`
				Destination struct {
					Server    string `json:"server"`
					Namespace string `json:"namespace"`
				} `json:"destination"`
			} `json:"spec"`
		} `json:"template"`
	} `json:"spec"`
}

// ListApplicationSets returns all ApplicationSets from Argo CD.
func (c *Client) ListApplicationSets(ctx context.Context) ([]models.ApplicationSet, error) {
	var resp struct {
		Items []applicationSetResponse `json:"items"`
	}

	if err := c.get(ctx, "/api/v1/applicationsets", nil, &resp); err != nil {
		return nil, fmt.Errorf("listing applicationsets: %w", err)
	}

	appSets := make([]models.ApplicationSet, 0, len(resp.Items))
	for _, item := range resp.Items {
		appSets = append(appSets, convertApplicationSet(item))
	}

	return appSets, nil
}

// GetApplicationSet returns a specific ApplicationSet by name.
func (c *Client) GetApplicationSet(ctx context.Context, name string) (*models.ApplicationSet, error) {
	var resp applicationSetResponse
	if err := c.get(ctx, "/api/v1/applicationsets/"+url.PathEscape(name), nil, &resp); err != nil {
		return nil, fmt.Errorf("getting applicationset %s: %w", name, err)
	}

	appSet := convertApplicationSet(resp)
	return &appSet, nil
}

// convertApplicationSet converts the API response to our model.
func convertApplicationSet(resp applicationSetResponse) models.ApplicationSet {
	appSet := models.ApplicationSet{
		Name:      resp.Metadata.Name,
		Namespace: resp.Metadata.Namespace,
	}

	// Template application
	template := models.Application{
		Name:                 resp.Spec.Template.Metadata.Name,
		Project:              resp.Spec.Template.Spec.Project,
		DestinationServer:    resp.Spec.Template.Spec.Destination.Server,
		DestinationNamespace: resp.Spec.Template.Spec.Destination.Namespace,
	}

	if resp.Spec.Template.Spec.Source != nil {
		template.RepoURL = resp.Spec.Template.Spec.Source.RepoURL
		template.Path = resp.Spec.Template.Spec.Source.Path
		template.TargetRevision = resp.Spec.Template.Spec.Source.TargetRevision
	}

	if len(resp.Spec.Template.Spec.Sources) > 0 {
		template.Sources = make([]models.ApplicationSource, len(resp.Spec.Template.Spec.Sources))
		for i, src := range resp.Spec.Template.Spec.Sources {
			template.Sources[i] = models.ApplicationSource{
				RepoURL:        src.RepoURL,
				Path:           src.Path,
				TargetRevision: src.TargetRevision,
			}
		}
	}

	appSet.Template = template
	return appSet
}

// GetApplicationsByApplicationSet returns all applications created by an ApplicationSet.
func (c *Client) GetApplicationsByApplicationSet(ctx context.Context, name string) ([]models.Application, error) {
	// First try label selector (some versions of Argo CD add the label)
	selector := fmt.Sprintf("argocd.argoproj.io/application-set-name=%s", name)
	apps, err := c.ListApplicationsWithSelector(ctx, selector)
	if err != nil {
		return nil, err
	}

	if len(apps) > 0 {
		return apps, nil
	}

	// Fallback: get all apps and filter by ownerReference-derived ApplicationSetName
	allApps, err := c.ListApplications(ctx)
	if err != nil {
		return nil, err
	}

	var filtered []models.Application
	for _, app := range allApps {
		if app.ApplicationSetName == name {
			filtered = append(filtered, app)
		}
	}

	return filtered, nil
}
