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
	"testing"
	"time"
)

// TestSecurityScanTool_Name verifies the tool name.
func TestSecurityScanTool_Name(t *testing.T) {
	t.Parallel()

	tool := NewSecurityScanTool(nil)
	if got := tool.Name(); got != "security_scan" {
		t.Errorf("Name() = %q, want %q", got, "security_scan")
	}
}

// TestSecurityScanTool_NilStore_NoLockfiles verifies that a tool with a nil
// store returns empty findings without error.
func TestSecurityScanTool_NilStore_NoLockfiles(t *testing.T) {
	t.Parallel()

	tool := NewSecurityScanTool(nil)
	result, err := tool.Execute(context.Background(), "handle-nil", map[string]any{})
	if err != nil {
		t.Fatalf("Execute() error = %v, want nil", err)
	}

	var got map[string]any
	if err := json.Unmarshal([]byte(result), &got); err != nil {
		t.Fatalf("result is not valid JSON: %v\nresult: %s", err, result)
	}

	findings, ok := got["findings"]
	if !ok {
		t.Fatal("result missing 'findings' key")
	}
	list, ok := findings.([]any)
	if !ok {
		t.Fatalf("findings is not a list: %T %v", findings, findings)
	}
	if len(list) != 0 {
		t.Errorf("findings = %v, want empty slice", list)
	}

	fromCache, ok := got["from_cache"]
	if !ok {
		t.Fatal("result missing 'from_cache' key")
	}
	if fromCache != false {
		t.Errorf("from_cache = %v, want false", fromCache)
	}
}

// TestSecurityScanTool_CooldownCacheHit verifies that a second Execute call
// within 60 seconds returns from_cache: true.
func TestSecurityScanTool_CooldownCacheHit(t *testing.T) {
	t.Parallel()

	tool := NewSecurityScanTool(nil)
	handle := "handle-cooldown"

	// First call — must be fresh.
	first, err := tool.Execute(context.Background(), handle, map[string]any{})
	if err != nil {
		t.Fatalf("first Execute() error = %v", err)
	}
	var firstResult map[string]any
	if err := json.Unmarshal([]byte(first), &firstResult); err != nil {
		t.Fatalf("first result not valid JSON: %v", err)
	}
	if firstResult["from_cache"] != false {
		t.Errorf("first call from_cache = %v, want false", firstResult["from_cache"])
	}

	// Second call immediately — must be cached.
	second, err := tool.Execute(context.Background(), handle, map[string]any{})
	if err != nil {
		t.Fatalf("second Execute() error = %v", err)
	}
	var secondResult map[string]any
	if err := json.Unmarshal([]byte(second), &secondResult); err != nil {
		t.Fatalf("second result not valid JSON: %v", err)
	}
	if secondResult["from_cache"] != true {
		t.Errorf("second call from_cache = %v, want true", secondResult["from_cache"])
	}

	// Different handle — must not be cached.
	third, err := tool.Execute(context.Background(), "handle-different", map[string]any{})
	if err != nil {
		t.Fatalf("third Execute() error = %v", err)
	}
	var thirdResult map[string]any
	if err := json.Unmarshal([]byte(third), &thirdResult); err != nil {
		t.Fatalf("third result not valid JSON: %v", err)
	}
	if thirdResult["from_cache"] != false {
		t.Errorf("third call (different handle) from_cache = %v, want false", thirdResult["from_cache"])
	}
}

// TestSecurityScanTool_CooldownExpires verifies that after the cooldown the
// tool performs a fresh scan (from_cache: false).
func TestSecurityScanTool_CooldownExpires(t *testing.T) {
	t.Parallel()

	tool := NewSecurityScanTool(nil)
	handle := "handle-expire"

	// Seed the cache with a timestamp old enough to be expired.
	tool.mu.Lock()
	tool.lastScanTime[handle] = time.Now().Add(-61 * time.Second)
	tool.lastResult[handle] = `{"findings":[],"from_cache":false,"scanned_at":"old"}`
	tool.mu.Unlock()

	result, err := tool.Execute(context.Background(), handle, map[string]any{})
	if err != nil {
		t.Fatalf("Execute() after expiry error = %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(result), &got); err != nil {
		t.Fatalf("result not valid JSON: %v", err)
	}
	if got["from_cache"] != false {
		t.Errorf("after cooldown expiry from_cache = %v, want false", got["from_cache"])
	}
}
