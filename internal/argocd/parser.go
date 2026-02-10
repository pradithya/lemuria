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
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	v1alpha1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
	"gopkg.in/yaml.v3"
	sigsyaml "sigs.k8s.io/yaml"

	"github.com/org/lemuria/internal/models"
)

// ParseApplicationsFromYAML parses ArgoCD Application CRs from YAML content.
// It handles both single-document and multi-document YAML files.
func ParseApplicationsFromYAML(content []byte, sourceFile string) ([]models.Application, error) {
	var apps []models.Application

	decoder := yaml.NewDecoder(bytes.NewReader(content))

	for {
		var rawDoc any
		err := decoder.Decode(&rawDoc)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			// Skip invalid YAML documents
			continue
		}

		yamlBytes, err := yaml.Marshal(rawDoc)
		if err != nil {
			continue
		}

		var app v1alpha1.Application
		if err := sigsyaml.Unmarshal(yamlBytes, &app); err != nil {
			continue
		}

		// Check if this is an ArgoCD Application
		if !strings.HasPrefix(app.APIVersion, "argoproj.io/") || app.Kind != "Application" {
			continue
		}

		apps = append(apps, convertV1alpha1Application(app, sourceFile))
	}

	return apps, nil
}

// ParseRawApplicationFromYAML parses YAML content and returns the typed
// v1alpha1.Application for the Application CR matching appName.
// Handles multi-document YAML files.
func ParseRawApplicationFromYAML(content []byte, appName string) (*v1alpha1.Application, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(content))

	for {
		var rawDoc any
		err := decoder.Decode(&rawDoc)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			continue
		}

		yamlBytes, err := yaml.Marshal(rawDoc)
		if err != nil {
			continue
		}

		var app v1alpha1.Application
		if err := sigsyaml.Unmarshal(yamlBytes, &app); err != nil {
			continue
		}

		if !strings.HasPrefix(app.APIVersion, "argoproj.io/") {
			continue
		}
		if app.Kind != "Application" {
			continue
		}
		if app.Name != appName {
			continue
		}

		return &app, nil
	}

	return nil, fmt.Errorf("application %q not found in YAML", appName)
}

// ParseApplicationSetsFromYAML parses ArgoCD ApplicationSet CRs from YAML content.
// It handles both single-document and multi-document YAML files.
func ParseApplicationSetsFromYAML(content []byte, sourceFile string) ([]v1alpha1.ApplicationSet, error) {
	var appSets []v1alpha1.ApplicationSet

	decoder := yaml.NewDecoder(bytes.NewReader(content))

	for {
		var rawDoc any
		err := decoder.Decode(&rawDoc)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			// Skip invalid YAML documents
			continue
		}

		yamlBytes, err := yaml.Marshal(rawDoc)
		if err != nil {
			continue
		}

		var appSet v1alpha1.ApplicationSet
		if err := sigsyaml.Unmarshal(yamlBytes, &appSet); err != nil {
			continue
		}

		// Check if this is an ArgoCD ApplicationSet
		if !strings.HasPrefix(appSet.APIVersion, "argoproj.io/") || appSet.Kind != "ApplicationSet" {
			continue
		}

		appSets = append(appSets, appSet)
	}

	return appSets, nil
}

// ParseRawApplicationSetFromYAML parses YAML content and returns the typed
// v1alpha1.ApplicationSet for the ApplicationSet CR matching name.
// Handles multi-document YAML files.
func ParseRawApplicationSetFromYAML(content []byte, name string) (*v1alpha1.ApplicationSet, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(content))

	for {
		var rawDoc any
		err := decoder.Decode(&rawDoc)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			continue
		}

		yamlBytes, err := yaml.Marshal(rawDoc)
		if err != nil {
			continue
		}

		var appSet v1alpha1.ApplicationSet
		if err := sigsyaml.Unmarshal(yamlBytes, &appSet); err != nil {
			continue
		}

		if !strings.HasPrefix(appSet.APIVersion, "argoproj.io/") {
			continue
		}
		if appSet.Kind != "ApplicationSet" {
			continue
		}
		if appSet.Name != name {
			continue
		}

		return &appSet, nil
	}

	return nil, fmt.Errorf("applicationset %q not found in YAML", name)
}

// ParsedAppSet is a helper that pairs an ApplicationSet with its source file.
type ParsedAppSet struct {
	AppSet     *v1alpha1.ApplicationSet
	SourceFile string
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
