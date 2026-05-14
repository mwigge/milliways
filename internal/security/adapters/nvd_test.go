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

package adapters_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/mwigge/milliways/internal/security"
	"github.com/mwigge/milliways/internal/security/adapters"
)

func TestNVDEnricherAddsMetadata(t *testing.T) {
	t.Parallel()

	var gotAPIKey string
	var gotCVEID string
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/rest/json/cves/2.0" {
			t.Fatalf("path = %q, want /rest/json/cves/2.0", r.URL.Path)
		}
		gotAPIKey = r.Header.Get("apiKey")
		gotCVEID = r.URL.Query().Get("cveId")
		return jsonResponse(http.StatusOK, `{
			"vulnerabilities": [{
				"cve": {
					"id": "CVE-2024-12345",
					"descriptions": [{"lang": "en", "value": "bounds check bypass in parser"}],
					"metrics": {
						"cvssMetricV31": [{
							"cvssData": {"baseSeverity": "CRITICAL"}
						}]
					},
					"configurations": [{
						"nodes": [{
							"cpeMatch": [{
								"vulnerable": true,
								"criteria": "cpe:2.3:a:example:parser:*:*:*:*:*:*:*:*",
								"versionEndExcluding": "1.2.3"
							}]
						}]
					}]
				}
			}]
		}`), nil
	})}

	enricher := adapters.NewNVDEnricher(
		adapters.WithNVDBaseURL("https://nvd.test"),
		adapters.WithNVDAPIKey("test-key"),
		adapters.WithNVDHTTPClient(client),
	)
	findings := []security.Finding{{
		ID:          "GO-2024-0001",
		CVEID:       "CVE-2024-12345",
		PackageName: "example.com/parser",
	}}

	enriched, err := enricher.Enrich(context.Background(), findings)
	if err != nil {
		t.Fatalf("Enrich: %v", err)
	}
	if gotAPIKey != "test-key" {
		t.Fatalf("apiKey header = %q, want test-key", gotAPIKey)
	}
	if gotCVEID != "CVE-2024-12345" {
		t.Fatalf("cveId query = %q, want CVE-2024-12345", gotCVEID)
	}
	if len(enriched) != 1 {
		t.Fatalf("findings = %d, want 1", len(enriched))
	}
	got := enriched[0]
	if got.Severity != "CRITICAL" || got.Summary != "bounds check bypass in parser" || got.FixedInVersion != "1.2.3" {
		t.Fatalf("enriched finding = %#v", got)
	}
}

func TestNVDEnricherAPIErrorsReturnOriginalFindings(t *testing.T) {
	t.Parallel()

	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusInternalServerError, "temporary outage"), nil
	})}

	enricher := adapters.NewNVDEnricher(
		adapters.WithNVDBaseURL("https://nvd.test"),
		adapters.WithNVDAPIKey("test-key"),
		adapters.WithNVDHTTPClient(client),
	)
	findings := []security.Finding{{
		CVEID:    "CVE-2024-12345",
		Severity: "HIGH",
		Summary:  "scanner summary",
	}}

	enriched, err := enricher.Enrich(context.Background(), findings)
	if err != nil {
		t.Fatalf("Enrich: %v", err)
	}
	if enriched[0].Severity != "HIGH" || enriched[0].Summary != "scanner summary" {
		t.Fatalf("finding changed after API error: %#v", enriched[0])
	}
}

func TestNVDEnricherDisabledWithoutAPIKey(t *testing.T) {
	t.Parallel()

	calls := 0
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return jsonResponse(http.StatusOK, `{}`), nil
	})}

	enricher := adapters.NewNVDEnricher(
		adapters.WithNVDBaseURL("https://nvd.test"),
		adapters.WithNVDHTTPClient(client),
	)
	if enricher.Enabled() {
		t.Fatal("Enabled() = true, want false without API key")
	}

	findings := []security.Finding{{CVEID: "CVE-2024-12345", Severity: "HIGH"}}
	enriched, err := enricher.Enrich(context.Background(), findings)
	if err != nil {
		t.Fatalf("Enrich: %v", err)
	}
	if calls != 0 {
		t.Fatalf("server calls = %d, want 0", calls)
	}
	if enriched[0].Severity != "HIGH" {
		t.Fatalf("finding changed while disabled: %#v", enriched[0])
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return fn(r)
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
