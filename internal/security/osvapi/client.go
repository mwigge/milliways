// Copyright 2024 The milliways Authors
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

// Package osvapi provides a small OSV API client foundation for dependency
// queries. It is intentionally not wired into the daemon yet.
package osvapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const defaultBaseURL = "https://api.osv.dev"

// Client queries the OSV API.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// Option configures a Client.
type Option func(*Client)

// WithBaseURL overrides the OSV API base URL.
func WithBaseURL(baseURL string) Option {
	return func(c *Client) {
		c.baseURL = strings.TrimRight(baseURL, "/")
	}
}

// WithHTTPClient injects the HTTP client used for API calls.
func WithHTTPClient(client *http.Client) Option {
	return func(c *Client) {
		if client != nil {
			c.httpClient = client
		}
	}
}

// NewClient returns a Client configured for api.osv.dev by default.
func NewClient(opts ...Option) *Client {
	c := &Client{
		baseURL:    defaultBaseURL,
		httpClient: http.DefaultClient,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Package identifies a package query target.
type Package struct {
	Name      string `json:"name,omitempty"`
	Ecosystem string `json:"ecosystem,omitempty"`
	PURL      string `json:"purl,omitempty"`
}

// Query is one OSV querybatch request item.
type Query struct {
	Commit  string  `json:"commit,omitempty"`
	Version string  `json:"version,omitempty"`
	Package Package `json:"package,omitempty"`
}

// BatchRequest is the JSON request body for /v1/querybatch.
type BatchRequest struct {
	Queries []Query `json:"queries"`
}

// Vulnerability is the subset of OSV vulnerability data currently needed by
// downstream scanner foundations.
type Vulnerability struct {
	ID      string   `json:"id"`
	Aliases []string `json:"aliases,omitempty"`
	Summary string   `json:"summary,omitempty"`
}

// BatchResult is one response item corresponding to one query.
type BatchResult struct {
	Vulns []Vulnerability `json:"vulns,omitempty"`
}

// BatchResponse is the JSON response body from /v1/querybatch.
type BatchResponse struct {
	Results []BatchResult `json:"results"`
}

// QueryBatch posts queries to OSV's /v1/querybatch endpoint.
func (c *Client) QueryBatch(ctx context.Context, queries []Query) (BatchResponse, error) {
	body, err := json.Marshal(BatchRequest{Queries: queries})
	if err != nil {
		return BatchResponse{}, fmt.Errorf("encode osv querybatch: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/querybatch", bytes.NewReader(body))
	if err != nil {
		return BatchResponse{}, fmt.Errorf("build osv querybatch request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return BatchResponse{}, fmt.Errorf("post osv querybatch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return BatchResponse{}, fmt.Errorf("osv querybatch status %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}

	var parsed BatchResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return BatchResponse{}, fmt.Errorf("decode osv querybatch response: %w", err)
	}
	return parsed, nil
}
