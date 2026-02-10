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
	"fmt"
	"net/url"

	v1alpha1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"

	"github.com/org/lemuria/internal/models"
)

// ListApplicationSets returns all ApplicationSets from Argo CD.
func (c *Client) ListApplicationSets(ctx context.Context) ([]models.ApplicationSet, error) {
	var resp v1alpha1.ApplicationSetList
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
	var resp v1alpha1.ApplicationSet
	if err := c.get(ctx, "/api/v1/applicationsets/"+url.PathEscape(name), nil, &resp); err != nil {
		return nil, fmt.Errorf("getting applicationset %s: %w", name, err)
	}

	appSet := convertApplicationSet(resp)
	return &appSet, nil
}

// CreateApplicationSet creates a new ApplicationSet in ArgoCD.
func (c *Client) CreateApplicationSet(ctx context.Context, appSet *v1alpha1.ApplicationSet) error {
	if err := c.post(ctx, "/api/v1/applicationsets", nil, appSet, nil); err != nil {
		return fmt.Errorf("creating applicationset: %w", err)
	}
	return nil
}

// DeleteApplicationSet deletes an ApplicationSet from ArgoCD.
func (c *Client) DeleteApplicationSet(ctx context.Context, name string) error {
	if err := c.delete(ctx, "/api/v1/applicationsets/"+url.PathEscape(name), nil); err != nil {
		return fmt.Errorf("deleting applicationset %s: %w", name, err)
	}
	return nil
}

// GenerateApplications calls the ArgoCD Generate API to preview what applications
// an ApplicationSet spec would produce without creating anything in the cluster.
func (c *Client) GenerateApplications(ctx context.Context, appSet *v1alpha1.ApplicationSet) ([]models.Application, error) {
	payload := map[string]any{
		"applicationSet": appSet,
	}

	var resp struct {
		Applications []v1alpha1.Application `json:"applications"`
	}
	if err := c.post(ctx, "/api/v1/applicationsets/generate", nil, payload, &resp); err != nil {
		return nil, fmt.Errorf("generating applications for applicationset %s: %w", appSet.Name, err)
	}

	apps := make([]models.Application, 0, len(resp.Applications))
	for _, item := range resp.Applications {
		apps = append(apps, convertV1alpha1Application(item, ""))
	}

	return apps, nil
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

// convertApplicationSet converts a v1alpha1.ApplicationSet to our domain model.
func convertApplicationSet(appSet v1alpha1.ApplicationSet) models.ApplicationSet {
	result := models.ApplicationSet{
		Name:      appSet.Name,
		Namespace: appSet.Namespace,
	}

	tmpl := appSet.Spec.Template
	template := models.Application{
		Name:                 tmpl.Name,
		Project:              tmpl.Spec.Project,
		DestinationServer:    tmpl.Spec.Destination.Server,
		DestinationNamespace: tmpl.Spec.Destination.Namespace,
	}

	if tmpl.Spec.Source != nil {
		template.RepoURL = tmpl.Spec.Source.RepoURL
		template.Path = tmpl.Spec.Source.Path
		template.TargetRevision = tmpl.Spec.Source.TargetRevision
	}

	if len(tmpl.Spec.Sources) > 0 {
		template.Sources = make([]models.ApplicationSource, len(tmpl.Spec.Sources))
		for i, src := range tmpl.Spec.Sources {
			template.Sources[i] = models.ApplicationSource{
				RepoURL:        src.RepoURL,
				Path:           src.Path,
				TargetRevision: src.TargetRevision,
			}
		}
	}

	result.Template = template
	return result
}
