package argocd

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/org/lemuria/internal/models"
)

// K8sResource represents a minimal Kubernetes resource for parsing.
type K8sResource struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name      string            `yaml:"name"`
		Namespace string            `yaml:"namespace"`
		Labels    map[string]string `yaml:"labels"`
	} `yaml:"metadata"`
	Spec ApplicationSpec `yaml:"spec"`
}

// ApplicationSpec represents the spec of an ArgoCD Application CR.
type ApplicationSpec struct {
	Project string `yaml:"project"`
	Source  *struct {
		RepoURL        string `yaml:"repoURL"`
		Path           string `yaml:"path"`
		TargetRevision string `yaml:"targetRevision"`
	} `yaml:"source,omitempty"`
	Sources []struct {
		RepoURL        string `yaml:"repoURL"`
		Path           string `yaml:"path"`
		TargetRevision string `yaml:"targetRevision"`
		Chart          string `yaml:"chart"`
		Helm           *struct {
			ValueFiles []string `yaml:"valueFiles"`
			Values     string   `yaml:"values"`
		} `yaml:"helm"`
	} `yaml:"sources,omitempty"`
	Destination struct {
		Server    string `yaml:"server"`
		Namespace string `yaml:"namespace"`
	} `yaml:"destination"`
	SyncPolicy *struct {
		Automated *struct {
			Prune    bool `yaml:"prune"`
			SelfHeal bool `yaml:"selfHeal"`
		} `yaml:"automated,omitempty"`
	} `yaml:"syncPolicy,omitempty"`
}

// ParseApplicationsFromYAML parses ArgoCD Application CRs from YAML content.
// It handles both single-document and multi-document YAML files.
func ParseApplicationsFromYAML(content []byte, sourceFile string) ([]models.Application, error) {
	var apps []models.Application

	decoder := yaml.NewDecoder(bytes.NewReader(content))

	for {
		var resource K8sResource
		err := decoder.Decode(&resource)
		if err == io.EOF {
			break
		}
		if err != nil {
			// Skip invalid YAML documents
			continue
		}

		// Check if this is an ArgoCD Application
		if isArgoCDApplication(resource) {
			app := convertK8sResourceToApplication(resource, sourceFile)
			apps = append(apps, app)
		}
	}

	return apps, nil
}

// isArgoCDApplication checks if a K8s resource is an ArgoCD Application.
func isArgoCDApplication(resource K8sResource) bool {
	// Check API version
	if !strings.HasPrefix(resource.APIVersion, "argoproj.io/") {
		return false
	}

	// Check Kind
	return resource.Kind == "Application"
}

// convertK8sResourceToApplication converts a parsed K8s resource to our Application model.
func convertK8sResourceToApplication(resource K8sResource, sourceFile string) models.Application {
	app := models.Application{
		Name:       resource.Metadata.Name,
		Namespace:  resource.Metadata.Namespace,
		Project:    resource.Spec.Project,
		Labels:     resource.Metadata.Labels,
		SourceFile: sourceFile,
	}

	if app.Namespace == "" {
		app.Namespace = "argocd"
	}

	if app.Project == "" {
		app.Project = "default"
	}

	// Single source
	if resource.Spec.Source != nil {
		app.RepoURL = resource.Spec.Source.RepoURL
		app.Path = resource.Spec.Source.Path
		app.TargetRevision = resource.Spec.Source.TargetRevision
	}

	// Destination
	app.DestinationServer = resource.Spec.Destination.Server
	app.DestinationNamespace = resource.Spec.Destination.Namespace

	// Multi-source
	if len(resource.Spec.Sources) > 0 {
		app.Sources = make([]models.ApplicationSource, len(resource.Spec.Sources))
		for i, src := range resource.Spec.Sources {
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

	// Auto-sync status
	app.AutoSyncEnabled = resource.Spec.SyncPolicy != nil && resource.Spec.SyncPolicy.Automated != nil

	return app
}

// ParseRawApplicationFromYAML parses YAML content and returns the raw spec
// (as map[string]any) for the Application CR matching appName.
// This preserves the full structure needed by buildTempAppSpec, including
// inline Helm values and other fields not captured by the typed structs.
// Handles multi-document YAML files.
func ParseRawApplicationFromYAML(content []byte, appName string) (map[string]any, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(content))

	for {
		var raw map[string]any
		err := decoder.Decode(&raw)
		if err == io.EOF {
			break
		}
		if err != nil {
			continue
		}

		// Check if this is an ArgoCD Application matching appName
		apiVersion, _ := raw["apiVersion"].(string)
		if !strings.HasPrefix(apiVersion, "argoproj.io/") {
			continue
		}
		kind, _ := raw["kind"].(string)
		if kind != "Application" {
			continue
		}
		metadata, _ := raw["metadata"].(map[string]any)
		if metadata == nil {
			continue
		}
		name, _ := metadata["name"].(string)
		if name != appName {
			continue
		}

		return raw, nil
	}

	return nil, fmt.Errorf("application %q not found in YAML", appName)
}

// ParsedApplications holds applications parsed from PR files.
type ParsedApplications struct {
	// New contains applications that are being added in the PR (file status: added)
	New []models.Application
	// Modified contains applications that are being modified (file status: modified)
	Modified []models.Application
	// Deleted contains applications that are being removed (file status: removed)
	Deleted []models.Application
}

// String returns a summary of parsed applications.
func (p *ParsedApplications) String() string {
	return fmt.Sprintf("new: %d, modified: %d, deleted: %d",
		len(p.New), len(p.Modified), len(p.Deleted))
}

// IsEmpty returns true if no applications were parsed.
func (p *ParsedApplications) IsEmpty() bool {
	return len(p.New) == 0 && len(p.Modified) == 0 && len(p.Deleted) == 0
}
