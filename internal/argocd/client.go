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
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/org/lemuria/internal/config"
	"github.com/org/lemuria/internal/metrics"
)

// Client wraps the Argo CD REST API client.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// NewClient creates a new Argo CD API client.
func NewClient(cfg config.ArgoCDConfig) (*Client, error) {
	baseURL := strings.TrimSuffix(cfg.ServerURL, "/")

	transport := http.DefaultTransport.(*http.Transport).Clone()
	if cfg.Insecure {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	return &Client{
		baseURL: baseURL,
		token:   cfg.Token,
		httpClient: &http.Client{
			Timeout:   30 * time.Second,
			Transport: transport,
		},
	}, nil
}

// request performs an HTTP request to the Argo CD API.
func (c *Client) request(ctx context.Context, method, path string, query url.Values, body io.Reader) (*http.Response, error) {
	start := time.Now()

	u, err := url.Parse(c.baseURL + path)
	if err != nil {
		return nil, fmt.Errorf("parsing URL: %w", err)
	}

	if query != nil {
		u.RawQuery = query.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)

	statusCode := 0
	if resp != nil {
		statusCode = resp.StatusCode
	}

	normalizedPath := metrics.NormalizePath(path)
	metrics.RecordArgoCDRequest(method, normalizedPath, statusCode)
	metrics.ObserveArgoCDRequestDuration(method, start)

	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}

	return resp, nil
}

// get performs a GET request and decodes the JSON response.
func (c *Client) get(ctx context.Context, path string, query url.Values, result any) error {
	resp, err := c.request(ctx, http.MethodGet, path, query, nil)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return fmt.Errorf("decoding response: %w", err)
		}
	}

	return nil
}

// post performs a POST request with JSON body.
func (c *Client) post(ctx context.Context, path string, query url.Values, payload, result any) error {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("encoding payload: %w", err)
		}
		body = bytes.NewReader(data)
	}

	resp, err := c.request(ctx, http.MethodPost, path, query, body)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return fmt.Errorf("decoding response: %w", err)
		}
	}

	return nil
}

// put performs a PUT request with JSON body.
func (c *Client) put(ctx context.Context, path string, query url.Values, payload, result any) error {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("encoding payload: %w", err)
		}
		body = bytes.NewReader(data)
	}

	resp, err := c.request(ctx, http.MethodPut, path, query, body)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return fmt.Errorf("decoding response: %w", err)
		}
	}

	return nil
}

// delete performs a DELETE request.
func (c *Client) delete(ctx context.Context, path string, query url.Values) error {
	resp, err := c.request(ctx, http.MethodDelete, path, query, nil)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	return nil
}

// IsNotFound returns true if the error indicates a 404 Not Found response.
// The error format is controlled by our own get/put/delete helpers which use
// fmt.Errorf("API error (status %d): ..."), so the string match is stable.
func IsNotFound(err error) bool {
	return err != nil && strings.Contains(err.Error(), "API error (status 404)")
}

// Version returns the Argo CD server version.
func (c *Client) Version(ctx context.Context) (string, error) {
	var resp struct {
		Version string `json:"Version"`
	}
	if err := c.get(ctx, "/api/version", nil, &resp); err != nil {
		return "", err
	}
	return resp.Version, nil
}
