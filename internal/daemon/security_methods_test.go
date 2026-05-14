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

	"github.com/mwigge/milliways/internal/pantry"
)

func TestSecurityStartupScanPersistsWarningsForStatus(t *testing.T) {
	db := openSecurityMethodTestDB(t)
	workspace := t.TempDir()
	t.Setenv("MILLIWAYS_WORKSPACE_ROOT", workspace)
	writeSecurityMethodFile(t, filepath.Join(workspace, "package.json"), `{
  "scripts": {"postinstall": "node setup.mjs"}
}`)
	writeSecurityMethodFile(t, filepath.Join(workspace, "setup.mjs"), `fetch("https://getsession.org/x")`)

	s := &Server{pantryDB: db}
	enc, buf := newCapturingEncoder()
	s.securityStartupScan(enc, &Request{
		ID:     mustSecurityMethodParams(t, 1),
		Params: mustSecurityMethodParams(t, map[string]any{"workspace": workspace}),
	})

	resp := decodeSecurityMethodResponse(t, buf.Bytes())
	if _, ok := resp["error"]; ok {
		t.Fatalf("security.startup_scan returned error: %v", resp)
	}
	result := resp["result"].(map[string]any)
	if blocks, _ := result["blocks"].(float64); blocks < 1 {
		t.Fatalf("blocks = %v, want at least one block; response=%v", result["blocks"], result)
	}

	enc, buf = newCapturingEncoder()
	s.securityStatus(enc, &Request{ID: mustSecurityMethodParams(t, 2)})
	resp = decodeSecurityMethodResponse(t, buf.Bytes())
	result = resp["result"].(map[string]any)
	if posture, _ := result["posture"].(string); posture != "block" {
		t.Fatalf("posture = %q, want block; result=%v", posture, result)
	}
	if blocks, _ := result["blocks"].(float64); blocks < 1 {
		t.Fatalf("status blocks = %v, want at least one; result=%v", result["blocks"], result)
	}
}

func TestSecurityModeStoresWorkspaceMode(t *testing.T) {
	db := openSecurityMethodTestDB(t)
	workspace := t.TempDir()
	t.Setenv("MILLIWAYS_WORKSPACE_ROOT", workspace)

	s := &Server{pantryDB: db}
	enc, buf := newCapturingEncoder()
	s.securityMode(enc, &Request{
		ID:     mustSecurityMethodParams(t, 1),
		Params: mustSecurityMethodParams(t, map[string]any{"mode": "strict"}),
	})

	resp := decodeSecurityMethodResponse(t, buf.Bytes())
	if _, ok := resp["error"]; ok {
		t.Fatalf("security.mode returned error: %v", resp)
	}
	result := resp["result"].(map[string]any)
	if mode, _ := result["mode"].(string); mode != "strict" {
		t.Fatalf("mode = %q, want strict; result=%v", mode, result)
	}
}

func openSecurityMethodTestDB(t *testing.T) *pantry.DB {
	t.Helper()
	db, err := pantry.Open(filepath.Join(t.TempDir(), "milliways.db"))
	if err != nil {
		t.Fatalf("pantry.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func writeSecurityMethodFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func mustSecurityMethodParams(t *testing.T, params any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("Marshal params: %v", err)
	}
	return b
}

func decodeSecurityMethodResponse(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var resp map[string]any
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("Unmarshal response %q: %v", string(data), err)
	}
	return resp
}
