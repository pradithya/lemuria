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
	"testing"
)

func TestParseManifest(t *testing.T) {
	tests := []struct {
		name       string
		raw        string
		wantAPI    string
		wantKind   string
		wantName   string
		wantNS     string
		wantErr    bool
		wantRawSet bool
	}{
		{
			name:       "valid JSON manifest with all fields",
			raw:        `{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"web","namespace":"production"}}`,
			wantAPI:    "apps/v1",
			wantKind:   "Deployment",
			wantName:   "web",
			wantNS:     "production",
			wantRawSet: true,
		},
		{
			name:       "manifest without namespace",
			raw:        `{"apiVersion":"v1","kind":"Namespace","metadata":{"name":"my-ns"}}`,
			wantAPI:    "v1",
			wantKind:   "Namespace",
			wantName:   "my-ns",
			wantNS:     "",
			wantRawSet: true,
		},
		{
			name:    "invalid JSON returns error",
			raw:     `{not valid json`,
			wantErr: true,
		},
		{
			name:    "empty string returns error",
			raw:     "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseManifest(tt.raw)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseManifest() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if got.APIVersion != tt.wantAPI {
				t.Errorf("APIVersion = %q, want %q", got.APIVersion, tt.wantAPI)
			}
			if got.Kind != tt.wantKind {
				t.Errorf("Kind = %q, want %q", got.Kind, tt.wantKind)
			}
			if got.Name != tt.wantName {
				t.Errorf("Name = %q, want %q", got.Name, tt.wantName)
			}
			if got.Namespace != tt.wantNS {
				t.Errorf("Namespace = %q, want %q", got.Namespace, tt.wantNS)
			}
			if tt.wantRawSet && got.Raw != tt.raw {
				t.Errorf("Raw = %q, want %q", got.Raw, tt.raw)
			}
		})
	}
}
