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

package osvapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mwigge/milliways/internal/security/osvapi"
)

func TestClientQueryBatchPostsQueriesAndParsesVulns(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotReq osvapi.BatchRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotReq); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`{"results":[{"vulns":[{"id":"OSV-1","summary":"bad lib"}]},{}]}`)); err != nil {
			t.Fatalf("write response: %v", err)
		}
	}))
	defer server.Close()

	client := osvapi.NewClient(osvapi.WithBaseURL(server.URL), osvapi.WithHTTPClient(server.Client()))
	got, err := client.QueryBatch(context.Background(), []osvapi.Query{
		{Package: osvapi.Package{Name: "stdlib", Ecosystem: "Go"}, Version: "1.21.0"},
		{Package: osvapi.Package{Name: "left-pad", Ecosystem: "npm"}, Version: "1.3.0"},
	})
	if err != nil {
		t.Fatalf("QueryBatch: %v", err)
	}

	if gotPath != "/v1/querybatch" {
		t.Fatalf("path = %q, want /v1/querybatch", gotPath)
	}
	if len(gotReq.Queries) != 2 || gotReq.Queries[0].Package.Name != "stdlib" {
		t.Fatalf("request queries = %#v", gotReq.Queries)
	}
	if len(got.Results) != 2 || len(got.Results[0].Vulns) != 1 || got.Results[0].Vulns[0].ID != "OSV-1" {
		t.Fatalf("response = %#v", got)
	}
}

func TestClientQueryBatchReturnsHTTPError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusTeapot)
	}))
	defer server.Close()

	client := osvapi.NewClient(osvapi.WithBaseURL(server.URL), osvapi.WithHTTPClient(server.Client()))
	_, err := client.QueryBatch(context.Background(), []osvapi.Query{{Commit: "abc123"}})
	if err == nil {
		t.Fatal("QueryBatch error = nil, want error")
	}
}
