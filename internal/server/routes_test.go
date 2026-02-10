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

func TestRequireCSRFHeader(t *testing.T) {
	// dummyHandler is the next handler that should only be reached when CSRF check passes.
	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	handler := requireCSRFHeader(dummyHandler)

	tests := []struct {
		name       string
		method     string
		header     string // value of X-Requested-With; empty means omit
		wantStatus int
		wantError  bool
	}{
		{
			name:       "GET without header succeeds",
			method:     http.MethodGet,
			header:     "",
			wantStatus: http.StatusOK,
		},
		{
			name:       "HEAD without header succeeds",
			method:     http.MethodHead,
			header:     "",
			wantStatus: http.StatusOK,
		},
		{
			name:       "OPTIONS without header succeeds",
			method:     http.MethodOptions,
			header:     "",
			wantStatus: http.StatusOK,
		},
		{
			name:       "POST without header is rejected",
			method:     http.MethodPost,
			header:     "",
			wantStatus: http.StatusForbidden,
			wantError:  true,
		},
		{
			name:       "PUT without header is rejected",
			method:     http.MethodPut,
			header:     "",
			wantStatus: http.StatusForbidden,
			wantError:  true,
		},
		{
			name:       "DELETE without header is rejected",
			method:     http.MethodDelete,
			header:     "",
			wantStatus: http.StatusForbidden,
			wantError:  true,
		},
		{
			name:       "PATCH without header is rejected",
			method:     http.MethodPatch,
			header:     "",
			wantStatus: http.StatusForbidden,
			wantError:  true,
		},
		{
			name:       "POST with correct header succeeds",
			method:     http.MethodPost,
			header:     "XMLHttpRequest",
			wantStatus: http.StatusOK,
		},
		{
			name:       "PUT with correct header succeeds",
			method:     http.MethodPut,
			header:     "XMLHttpRequest",
			wantStatus: http.StatusOK,
		},
		{
			name:       "DELETE with correct header succeeds",
			method:     http.MethodDelete,
			header:     "XMLHttpRequest",
			wantStatus: http.StatusOK,
		},
		{
			name:       "PATCH with correct header succeeds",
			method:     http.MethodPatch,
			header:     "XMLHttpRequest",
			wantStatus: http.StatusOK,
		},
		{
			name:       "POST with wrong header value is rejected",
			method:     http.MethodPost,
			header:     "fetch",
			wantStatus: http.StatusForbidden,
			wantError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/api/v1/status", nil)
			if tt.header != "" {
				req.Header.Set("X-Requested-With", tt.header)
			}
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}

			// Verify JSON content type on all responses (both success and error).
			ct := w.Header().Get("Content-Type")
			if ct != "application/json" {
				t.Errorf("Content-Type = %q, want %q", ct, "application/json")
			}

			if tt.wantError {
				var body map[string]string
				if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
					t.Fatalf("failed to decode JSON error body: %v", err)
				}
				if body["error"] == "" {
					t.Error("expected non-empty 'error' field in JSON response")
				}
			}
		})
	}
}

func TestRespondJSON(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		data       any
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
