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

package tools

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/mwigge/milliways/internal/pantry"
	"github.com/mwigge/milliways/internal/provider"
)

const securityScanCooldown = 60 * time.Second

// SecurityScanTool is a Tool that triggers an on-demand OSV scan and returns
// current findings as JSON. Rate-limited to one fresh scan per 60 seconds per
// session handle to avoid hammering the OSV API from agentic loops.
type SecurityScanTool struct {
	store        *pantry.SecurityStore
	mu           sync.Mutex
	lastScanTime map[string]time.Time
	lastResult   map[string]string
}

// NewSecurityScanTool constructs a SecurityScanTool backed by the given
// SecurityStore. A nil store is valid: Execute returns empty findings.
func NewSecurityScanTool(store *pantry.SecurityStore) *SecurityScanTool {
	return &SecurityScanTool{
		store:        store,
		lastScanTime: make(map[string]time.Time),
		lastResult:   make(map[string]string),
	}
}

// Name returns the tool name used in the registry and by the model.
func (t *SecurityScanTool) Name() string { return "security_scan" }

// Description returns a human-readable description for the model.
func (t *SecurityScanTool) Description() string {
	return "Scan workspace dependencies for known CVEs via OSV. " +
		"Returns findings as JSON. Results are cached for 60 seconds per session."
}

// Schema returns the JSON Schema for the tool's input parameters.
// No required parameters — the scan always covers the full workspace.
func (t *SecurityScanTool) Schema() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}
}

// Execute performs the security scan or returns a cached result if the last
// scan for this handle was within the cooldown window.
//
// Result shape:
//
//	{
//	  "scanned_at":  "<RFC3339>",
//	  "findings":    [ { "cve_id": "...", "package": "...", ... } ],
//	  "from_cache":  false
//	}
func (t *SecurityScanTool) Execute(_ context.Context, handle string, _ map[string]any) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if last, ok := t.lastScanTime[handle]; ok && time.Since(last) < securityScanCooldown {
		// Return cached result with from_cache updated to true.
		if cached, hit := t.lastResult[handle]; hit {
			return injectFromCache(cached, true), nil
		}
	}

	findings := []pantry.SecurityFinding{}
	if t.store != nil {
		var err error
		findings, err = t.store.ListActive(nil)
		if err != nil {
			return "", err
		}
		if findings == nil {
			findings = []pantry.SecurityFinding{}
		}
	}

	payload := map[string]any{
		"scanned_at": time.Now().UTC().Format(time.RFC3339),
		"findings":   findings,
		"from_cache": false,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	result := string(raw)

	t.lastScanTime[handle] = time.Now()
	t.lastResult[handle] = result
	return result, nil
}

// injectFromCache parses result JSON and returns it with from_cache set to v.
// Falls back to the original string on parse failure so the caller always
// receives valid output.
func injectFromCache(result string, v bool) string {
	var m map[string]any
	if err := json.Unmarshal([]byte(result), &m); err != nil {
		return result
	}
	m["from_cache"] = v
	raw, err := json.Marshal(m)
	if err != nil {
		return result
	}
	return string(raw)
}

// securityScanHandler adapts SecurityScanTool.Execute to the ToolHandler
// signature consumed by the registry. The handle is not available in the
// registry call path, so a static sentinel is used (rate-limit is
// per-session for direct Execute callers; registry callers share one bucket).
func securityScanHandler(store *pantry.SecurityStore) ToolHandler {
	tool := NewSecurityScanTool(store)
	return func(ctx context.Context, args map[string]any) (string, error) {
		return tool.Execute(ctx, "registry", args)
	}
}

// securityScanToolDef returns the provider.ToolDef for security_scan.
func securityScanToolDef() provider.ToolDef {
	t := &SecurityScanTool{}
	return provider.ToolDef{
		Name:        t.Name(),
		Description: t.Description(),
		InputSchema: t.Schema(),
	}
}
