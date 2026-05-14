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

package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type securityRPCCall struct {
	Method string
	Params map[string]any
}

func startSecurityRPCTestServer(t *testing.T, results map[string]any) (string, <-chan securityRPCCall) {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "mw.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	calls := make(chan securityRPCCall, 8)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go handleSecurityRPCTestConn(conn, results, calls)
		}
	}()
	return sock, calls
}

func handleSecurityRPCTestConn(conn net.Conn, results map[string]any, calls chan<- securityRPCCall) {
	defer conn.Close()
	scanner := bufio.NewScanner(conn)
	enc := json.NewEncoder(conn)
	for scanner.Scan() {
		var req struct {
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
			ID     int64           `json:"id"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			return
		}
		params := map[string]any{}
		if len(req.Params) > 0 {
			_ = json.Unmarshal(req.Params, &params)
		}
		calls <- securityRPCCall{Method: req.Method, Params: params}

		result, ok := results[req.Method]
		if !ok {
			_ = enc.Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"error": map[string]any{
					"code":    -32601,
					"message": "method not found",
				},
			})
			continue
		}
		_ = enc.Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"result":  result,
		})
	}
}

func TestRunSecurityHelpIncludesSecureSurface(t *testing.T) {
	var stdout bytes.Buffer
	if rc := runSecurity([]string{"help"}, &stdout, &bytes.Buffer{}); rc != 0 {
		t.Fatalf("expected rc=0, got %d", rc)
	}
	for _, want := range []string{"startup-scan", "warnings", "mode", "harden npm", "quarantine", "rules list|update"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("help missing %q; got:\n%s", want, stdout.String())
		}
	}
}

func TestRunSecurityScanUsesExistingRPC(t *testing.T) {
	sock, calls := startSecurityRPCTestServer(t, map[string]any{
		"security.scan": map[string]any{"findings": []any{}},
	})

	var stdout bytes.Buffer
	if rc := runSecurity([]string{"scan"}, &stdout, &bytes.Buffer{}, sock); rc != 0 {
		t.Fatalf("expected rc=0, got %d; stdout:\n%s", rc, stdout.String())
	}
	call := <-calls
	if call.Method != "security.scan" {
		t.Fatalf("expected security.scan, got %q", call.Method)
	}
	if !strings.Contains(stdout.String(), "0 finding(s)") {
		t.Errorf("scan output should summarize findings; got:\n%s", stdout.String())
	}
}

func TestRunSecurityStartupScanPassesStrict(t *testing.T) {
	sock, calls := startSecurityRPCTestServer(t, map[string]any{
		"security.startup_scan": map[string]any{"warnings": []any{"x"}},
	})

	var stdout bytes.Buffer
	if rc := runSecurity([]string{"startup-scan", "--strict"}, &stdout, &bytes.Buffer{}, sock); rc != 0 {
		t.Fatalf("expected rc=0, got %d; stdout:\n%s", rc, stdout.String())
	}
	call := <-calls
	if call.Method != "security.startup_scan" {
		t.Fatalf("expected security.startup_scan, got %q", call.Method)
	}
	if strict, _ := call.Params["strict"].(bool); !strict {
		t.Fatalf("expected strict=true params, got %#v", call.Params)
	}
	if !strings.Contains(stdout.String(), "1 warning(s)") {
		t.Errorf("startup-scan output should summarize warnings; got:\n%s", stdout.String())
	}
}

func TestRunSecurityModeValidatesAndCallsRPC(t *testing.T) {
	sock, calls := startSecurityRPCTestServer(t, map[string]any{
		"security.mode": map[string]any{"mode": "strict"},
	})

	var stdout bytes.Buffer
	if rc := runSecurity([]string{"mode", "strict"}, &stdout, &bytes.Buffer{}, sock); rc != 0 {
		t.Fatalf("expected rc=0, got %d", rc)
	}
	call := <-calls
	if call.Method != "security.mode" {
		t.Fatalf("expected security.mode, got %q", call.Method)
	}
	if mode, _ := call.Params["mode"].(string); mode != "strict" {
		t.Fatalf("expected mode=strict params, got %#v", call.Params)
	}

	var stderr bytes.Buffer
	if rc := runSecurity([]string{"mode", "panic"}, &bytes.Buffer{}, &stderr, sock); rc == 0 {
		t.Fatalf("expected invalid mode to fail")
	}
	if !strings.Contains(stderr.String(), "invalid mode") {
		t.Errorf("expected invalid mode error, got:\n%s", stderr.String())
	}
}

func TestRunSecurityQuarantineDefaultsToDryRun(t *testing.T) {
	sock, calls := startSecurityRPCTestServer(t, map[string]any{
		"security.quarantine": map[string]any{"actions": []any{"move"}},
	})

	var stdout bytes.Buffer
	if rc := runSecurity([]string{"quarantine"}, &stdout, &bytes.Buffer{}, sock); rc != 0 {
		t.Fatalf("expected rc=0, got %d", rc)
	}
	call := <-calls
	if call.Method != "security.quarantine" {
		t.Fatalf("expected security.quarantine, got %q", call.Method)
	}
	if dryRun, _ := call.Params["dry_run"].(bool); !dryRun {
		t.Fatalf("expected dry_run=true params, got %#v", call.Params)
	}
	if apply, _ := call.Params["apply"].(bool); apply {
		t.Fatalf("expected apply=false params, got %#v", call.Params)
	}
	if !strings.Contains(stdout.String(), "1 action(s)") {
		t.Errorf("quarantine output should summarize actions; got:\n%s", stdout.String())
	}
}

func TestRunSecurityStatusRendersExtendedFields(t *testing.T) {
	sock, _ := startSecurityRPCTestServer(t, map[string]any{
		"security.status": map[string]any{
			"installed":               false,
			"enabled":                 true,
			"mode":                    "warn",
			"posture":                 "warn",
			"warning_count":           2,
			"block_count":             1,
			"last_startup_scan_at":    "2026-05-14T10:00:00Z",
			"last_dependency_scan_at": "2026-05-14T10:02:00Z",
		},
	})

	var stdout bytes.Buffer
	if rc := runSecurity([]string{"status"}, &stdout, &bytes.Buffer{}, sock); rc != 0 {
		t.Fatalf("expected rc=0, got %d", rc)
	}
	for _, want := range []string{"mode: warn", "posture: WARN", "warnings: 2  blocks: 1", "last startup scan", "last dependency scan"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("status missing %q; got:\n%s", want, stdout.String())
		}
	}
}

func TestRunSecurityHardenNPMDryRunDoesNotWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".npmrc")
	var stdout bytes.Buffer
	if rc := runSecurity([]string{"harden", "npm", "--path", path}, &stdout, &bytes.Buffer{}); rc != 0 {
		t.Fatalf("expected rc=0, got %d", rc)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("dry-run should not create %s", path)
	}
	if !strings.Contains(stdout.String(), "ignore-scripts=true") {
		t.Errorf("dry-run should preview npm settings; got:\n%s", stdout.String())
	}
}

func TestRunSecurityHardenNPMApplyAppendsMissingSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".npmrc")
	if err := os.WriteFile(path, []byte("audit=false\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	var stdout bytes.Buffer
	if rc := runSecurity([]string{"harden", "npm", "--apply", "--path", path}, &stdout, &bytes.Buffer{}); rc != 0 {
		t.Fatalf("expected rc=0, got %d", rc)
	}
	gotBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	got := string(gotBytes)
	for _, want := range []string{"audit=false", "ignore-scripts=true", "fund=false", "package-lock=true"} {
		if !strings.Contains(got, want) {
			t.Errorf(".npmrc missing %q; got:\n%s", want, got)
		}
	}
	if strings.Contains(got, "audit=true") {
		t.Errorf("harden should not override an existing audit setting; got:\n%s", got)
	}
}
