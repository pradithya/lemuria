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

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRespondJSON(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		data       interface{}
		wantStatus int
	}{
		{
			name:       "ok response",
			status:     http.StatusOK,
			data:       map[string]string{"status": "healthy"},
			wantStatus: http.StatusOK,
		},
		{
			name:       "error response",
			status:     http.StatusInternalServerError,
			data:       map[string]string{"error": "something failed"},
			wantStatus: http.StatusInternalServerError,
		},
		{
			name:       "not found",
			status:     http.StatusNotFound,
			data:       map[string]string{"error": "not found"},
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			respondJSON(w, tt.status, tt.data)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}

			ct := w.Header().Get("Content-Type")
			if ct != "application/json" {
				t.Errorf("Content-Type = %q, want %q", ct, "application/json")
			}

			var got map[string]string
			if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}

			expected := tt.data.(map[string]string)
			for k, v := range expected {
				if got[k] != v {
					t.Errorf("response[%q] = %q, want %q", k, got[k], v)
				}
			}
		})
	}
}
