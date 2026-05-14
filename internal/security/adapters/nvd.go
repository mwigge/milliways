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

package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/mwigge/milliways/internal/security"
)

const defaultNVDBaseURL = "https://services.nvd.nist.gov"

// EnrichmentAdapter adds optional metadata to existing scanner findings.
type EnrichmentAdapter interface {
	Name() string
	Enabled() bool
	Enrich(ctx context.Context, findings []security.Finding) ([]security.Finding, error)
}

// NVDOption configures NVD CVE enrichment.
type NVDOption func(*NVDEnricher)

// WithNVDBaseURL overrides the NVD API base URL.
func WithNVDBaseURL(baseURL string) NVDOption {
	return func(e *NVDEnricher) {
		e.baseURL = strings.TrimRight(baseURL, "/")
	}
}

// WithNVDAPIKey configures the NVD API key. Without a key, enrichment is
// disabled and Enrich returns the original findings without network access.
func WithNVDAPIKey(apiKey string) NVDOption {
	return func(e *NVDEnricher) {
		e.apiKey = strings.TrimSpace(apiKey)
	}
}

// WithNVDHTTPClient injects the HTTP client used for NVD API calls.
func WithNVDHTTPClient(client *http.Client) NVDOption {
	return func(e *NVDEnricher) {
		if client != nil {
			e.httpClient = client
		}
	}
}

// NVDEnricher enriches CVE findings with NVD CVE API metadata. It is not wired
// into default scanner execution; callers must explicitly configure it.
type NVDEnricher struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// NewNVDEnricher returns an optional NVD metadata enricher.
func NewNVDEnricher(opts ...NVDOption) *NVDEnricher {
	e := &NVDEnricher{
		baseURL:    defaultNVDBaseURL,
		httpClient: http.DefaultClient,
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

func (e *NVDEnricher) Name() string {
	return "nvd"
}

func (e *NVDEnricher) Enabled() bool {
	return e != nil && e.apiKey != "" && e.baseURL != "" && e.httpClient != nil
}

func (e *NVDEnricher) Enrich(ctx context.Context, findings []security.Finding) ([]security.Finding, error) {
	out := append([]security.Finding(nil), findings...)
	if !e.Enabled() {
		return out, nil
	}

	cache := make(map[string]nvdCVE, len(out))
	for i := range out {
		cveID := strings.TrimSpace(out[i].CVEID)
		if cveID == "" {
			continue
		}
		cve, ok := cache[cveID]
		if !ok {
			var err error
			cve, err = e.lookupCVE(ctx, cveID)
			if err != nil {
				continue
			}
			cache[cveID] = cve
		}
		applyNVDMetadata(&out[i], cve)
	}
	return out, nil
}

func (e *NVDEnricher) lookupCVE(ctx context.Context, cveID string) (nvdCVE, error) {
	endpoint, err := url.Parse(e.baseURL + "/rest/json/cves/2.0")
	if err != nil {
		return nvdCVE{}, fmt.Errorf("build nvd cve url: %w", err)
	}
	query := endpoint.Query()
	query.Set("cveId", cveID)
	endpoint.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nvdCVE{}, fmt.Errorf("build nvd cve request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("apiKey", e.apiKey)

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nvdCVE{}, fmt.Errorf("get nvd cve: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return nvdCVE{}, fmt.Errorf("nvd cve status %d", resp.StatusCode)
	}

	var parsed nvdResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nvdCVE{}, fmt.Errorf("decode nvd cve response: %w", err)
	}
	for _, item := range parsed.Vulnerabilities {
		if strings.EqualFold(item.CVE.ID, cveID) {
			return item.CVE, nil
		}
	}
	return nvdCVE{}, fmt.Errorf("nvd cve %s not found", cveID)
}

func applyNVDMetadata(f *security.Finding, cve nvdCVE) {
	if f.Severity == "" || strings.EqualFold(f.Severity, "UNKNOWN") {
		if severity := cve.severity(); severity != "" {
			f.Severity = severity
		}
	}
	if f.Summary == "" {
		f.Summary = cve.summary()
	}
	if f.FixedInVersion == "" {
		f.FixedInVersion = cve.fixedVersion()
	}
}

type nvdResponse struct {
	Vulnerabilities []struct {
		CVE nvdCVE `json:"cve"`
	} `json:"vulnerabilities"`
}

type nvdCVE struct {
	ID             string           `json:"id"`
	Descriptions   []nvdDescription `json:"descriptions"`
	Metrics        nvdMetrics       `json:"metrics"`
	Configurations []nvdConfig      `json:"configurations"`
}

type nvdDescription struct {
	Lang  string `json:"lang"`
	Value string `json:"value"`
}

type nvdMetrics struct {
	CVSSMetricV40 []nvdCVSSMetric `json:"cvssMetricV40"`
	CVSSMetricV31 []nvdCVSSMetric `json:"cvssMetricV31"`
	CVSSMetricV30 []nvdCVSSMetric `json:"cvssMetricV30"`
	CVSSMetricV2  []nvdCVSSMetric `json:"cvssMetricV2"`
}

type nvdCVSSMetric struct {
	CVSSData struct {
		BaseSeverity string `json:"baseSeverity"`
	} `json:"cvssData"`
	BaseSeverity string `json:"baseSeverity"`
}

type nvdConfig struct {
	Nodes []nvdNode `json:"nodes"`
}

type nvdNode struct {
	Nodes    []nvdNode     `json:"nodes"`
	CPEMatch []nvdCPEMatch `json:"cpeMatch"`
}

type nvdCPEMatch struct {
	Vulnerable          bool   `json:"vulnerable"`
	VersionEndExcluding string `json:"versionEndExcluding"`
}

func (c nvdCVE) summary() string {
	for _, desc := range c.Descriptions {
		if strings.EqualFold(desc.Lang, "en") && strings.TrimSpace(desc.Value) != "" {
			return strings.TrimSpace(desc.Value)
		}
	}
	for _, desc := range c.Descriptions {
		if strings.TrimSpace(desc.Value) != "" {
			return strings.TrimSpace(desc.Value)
		}
	}
	return ""
}

func (c nvdCVE) severity() string {
	for _, metrics := range [][]nvdCVSSMetric{
		c.Metrics.CVSSMetricV40,
		c.Metrics.CVSSMetricV31,
		c.Metrics.CVSSMetricV30,
		c.Metrics.CVSSMetricV2,
	} {
		for _, metric := range metrics {
			severity := fallback(metric.CVSSData.BaseSeverity, metric.BaseSeverity)
			if severity != "" {
				return normaliseSeverity(severity)
			}
		}
	}
	return ""
}

func (c nvdCVE) fixedVersion() string {
	for _, config := range c.Configurations {
		if fixed := fixedVersionFromNodes(config.Nodes); fixed != "" {
			return fixed
		}
	}
	return ""
}

func fixedVersionFromNodes(nodes []nvdNode) string {
	for _, node := range nodes {
		for _, match := range node.CPEMatch {
			if !match.Vulnerable {
				continue
			}
			if match.VersionEndExcluding != "" {
				return match.VersionEndExcluding
			}
		}
		if fixed := fixedVersionFromNodes(node.Nodes); fixed != "" {
			return fixed
		}
	}
	return ""
}
