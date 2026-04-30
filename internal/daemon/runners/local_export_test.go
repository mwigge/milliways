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

package runners

import (
	"net/http"
	"testing"

	"github.com/mwigge/milliways/internal/tools"
)

// withLocalToolRegistry installs `r` as the registry seen by RunLocal for
// the duration of the test. Restored automatically on test cleanup.
func withLocalToolRegistry(t *testing.T, r *tools.Registry) {
	t.Helper()
	localToolRegistryMu.Lock()
	prev := localToolRegistryOverride
	localToolRegistryOverride = r
	localToolRegistryMu.Unlock()
	t.Cleanup(func() {
		localToolRegistryMu.Lock()
		localToolRegistryOverride = prev
		localToolRegistryMu.Unlock()
	})
}

// withLocalHTTPClient swaps the per-runner HTTP client for the duration of
// the test, restoring on cleanup. Used to inject a stubbed transport
// without touching `http.DefaultTransport` (which would race with parallel
// tests in this or any other package — Code-quality B2 / Reviewer
// MEDIUM 13).
func withLocalHTTPClient(t *testing.T, c *http.Client) {
	t.Helper()
	prev := localHTTPClient
	localHTTPClient = c
	t.Cleanup(func() { localHTTPClient = prev })
}
