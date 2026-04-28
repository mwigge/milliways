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

package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mwigge/milliways/internal/history"
)

// TestHistoryRPC simulates appending history via history.append and reading
// it back via history.get through the internal helpers. Uses a temp dir as
// the server state dir to avoid touching the real runtime state.
func TestHistoryRPC(t *testing.T) {
	dir, err := os.MkdirTemp("", "milliways-state-")
	if err != nil {
		t.Fatalf("tmpdir: %v", err)
	}
	defer os.RemoveAll(dir)

	agent := "_test-agent"
	p := map[string]any{"hello": "world"}
	if err := history.AppendAgentHistory(dir, agent, p, 100); err != nil {
		t.Fatalf("append: %v", err)
	}

	// ensure file exists
	fpath := filepath.Join(dir, "history", agent+".ndjson")
	if _, err := os.Stat(fpath); err != nil {
		t.Fatalf("stat: %v", err)
	}

	// read back
	res, err := history.ReadAgentHistory(dir, agent, -1)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(res) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(res))
	}
	// verify shape
	if _, ok := res[0]["t"]; !ok {
		t.Fatalf("missing t field")
	}
	if v, ok := res[0]["v"].(map[string]any); !ok || v["hello"] != "world" {
		b, _ := json.MarshalIndent(res, "", "  ")
		t.Fatalf("unexpected payload: %s", b)
	}

	// append a chunk_end event
	ce := map[string]any{"t": "chunk_end", "cost_usd": 0.012}
	if err := history.AppendAgentHistory(dir, agent, ce, 100); err != nil {
		t.Fatalf("append chunk_end: %v", err)
	}
	res2, err := history.ReadAgentHistory(dir, agent, -1)
	if err != nil {
		t.Fatalf("read2: %v", err)
	}
	if len(res2) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(res2))
	}
	// trimming: append many entries and verify trimming keeps last N
	for i := 0; i < 150; i++ {
		_ = history.AppendAgentHistory(dir, agent, map[string]any{"i": i}, 100)
	}
	res3, err := history.ReadAgentHistory(dir, agent, -1)
	if err != nil {
		t.Fatalf("read3: %v", err)
	}
	if len(res3) != 100 {
		t.Fatalf("expected trim to 100 lines, got %d", len(res3))
	}

	// check timestamps are present and increasing
	var last int64
	for _, e := range res3 {
		if tval, ok := e["t"].(float64); ok {
			if int64(tval) < last {
				t.Fatalf("non-monotonic time: %d < %d", int64(tval), last)
			}
			last = int64(tval)
		} else {
			t.Fatalf("t not number")
		}
	}

	// small sleep to ensure filesystem timestamps differ for manual inspection
	t.Logf("history file path: %s", fpath)
	t.Log("TestHistoryRPC passed")
}
