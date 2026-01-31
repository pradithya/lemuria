package argocd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"

	"github.com/org/lemuria/internal/models"
)

// manifestResponse represents the Argo CD manifests API response.
type manifestResponse struct {
	Manifests []string `json:"manifests"`
	Revision  string   `json:"revision"`
}

// GetManifestsParams configures manifest fetching.
type GetManifestsParams struct {
	Revision        string
	SourcePositions []int
	Revisions       []string
}

// GetManifests fetches the target manifests for an application.
func (c *Client) GetManifests(ctx context.Context, name string, params *GetManifestsParams) ([]models.Manifest, string, error) {
	query := url.Values{}

	if params != nil {
		if params.Revision != "" {
			query.Set("revision", params.Revision)
		}
		if len(params.SourcePositions) > 0 {
			for _, pos := range params.SourcePositions {
				query.Add("sourcePositions", strconv.Itoa(pos))
			}
		}
		if len(params.Revisions) > 0 {
			for _, rev := range params.Revisions {
				query.Add("revisions", rev)
			}
		}
	}

	var resp manifestResponse
	if err := c.get(ctx, "/api/v1/applications/"+url.PathEscape(name)+"/manifests", query, &resp); err != nil {
		return nil, "", fmt.Errorf("getting manifests for %s: %w", name, err)
	}

	manifests := make([]models.Manifest, 0, len(resp.Manifests))
	for _, raw := range resp.Manifests {
		manifest, err := parseManifest(raw)
		if err != nil {
			continue // Skip unparseable manifests
		}
		manifests = append(manifests, manifest)
	}

	return manifests, resp.Revision, nil
}

// parseManifest extracts metadata from a raw JSON manifest.
func parseManifest(raw string) (models.Manifest, error) {
	m := models.Manifest{Raw: raw}

	// Parse as JSON (Argo CD returns manifests as JSON strings)
	var obj struct {
		APIVersion string `json:"apiVersion"`
		Kind       string `json:"kind"`
		Metadata   struct {
			Name      string `json:"name"`
			Namespace string `json:"namespace"`
		} `json:"metadata"`
	}

	if err := json.Unmarshal([]byte(raw), &obj); err != nil {
		return m, fmt.Errorf("parsing manifest JSON: %w", err)
	}

	m.APIVersion = obj.APIVersion
	m.Kind = obj.Kind
	m.Name = obj.Metadata.Name
	m.Namespace = obj.Metadata.Namespace

	return m, nil
}

// GetLiveManifests fetches the currently live manifests for an application.
func (c *Client) GetLiveManifests(ctx context.Context, name string) ([]models.Manifest, error) {
	var resp struct {
		Items []struct {
			TargetState string `json:"targetState"`
			LiveState   string `json:"liveState"`
		} `json:"items"`
	}

	if err := c.get(ctx, "/api/v1/applications/"+url.PathEscape(name)+"/managed-resources", nil, &resp); err != nil {
		return nil, fmt.Errorf("getting live manifests for %s: %w", name, err)
	}

	manifests := make([]models.Manifest, 0, len(resp.Items))
	for _, item := range resp.Items {
		if item.LiveState == "" || item.LiveState == "null" {
			continue
		}
		manifest, err := parseManifest(item.LiveState)
		if err != nil {
			continue
		}
		manifests = append(manifests, manifest)
	}

	return manifests, nil
}

// GetMultiSourceManifests fetches manifests for a multi-source application.
func (c *Client) GetMultiSourceManifests(ctx context.Context, app models.Application, revision string) ([]models.Manifest, error) {
	if !app.IsMultiSource() {
		manifests, _, err := c.GetManifests(ctx, app.Name, &GetManifestsParams{Revision: revision})
		return manifests, err
	}

	var allManifests []models.Manifest
	for i := range app.Sources {
		manifests, _, err := c.GetManifests(ctx, app.Name, &GetManifestsParams{
			SourcePositions: []int{i},
			Revisions:       []string{revision},
		})
		if err != nil {
			return nil, fmt.Errorf("getting manifests for source %d: %w", i, err)
		}
		allManifests = append(allManifests, manifests...)
	}

	return allManifests, nil
}
