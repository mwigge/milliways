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
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mwigge/milliways/internal/pantry"
)

// dialAndSetup connects to the test server, opens an _echo session, attaches
// a stream sidecar, and returns the session handle and stream ID.
func dialAndSetup(t *testing.T, sock string) (conn net.Conn, sidecar net.Conn, handle int64, streamID int64) {
	t.Helper()

	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	enc := json.NewEncoder(conn)
	reader := bufio.NewReader(conn)

	// agent.open
	req := map[string]any{
		"jsonrpc": "2.0",
		"method":  "agent.open",
		"params":  map[string]any{"agent_id": "_echo"},
		"id":      1,
	}
	if err := enc.Encode(req); err != nil {
		t.Fatalf("encode agent.open: %v", err)
	}
	line, err := reader.ReadBytes('\n')
	if err != nil {
		t.Fatalf("read agent.open resp: %v", err)
	}
	var openResp map[string]any
	if err := json.Unmarshal(line, &openResp); err != nil {
		t.Fatalf("decode agent.open resp: %v", err)
	}
	result, ok := openResp["result"].(map[string]any)
	if !ok {
		t.Fatalf("no result in agent.open resp: %s", string(line))
	}
	handle = int64(result["handle"].(float64))

	// agent.stream
	req2 := map[string]any{
		"jsonrpc": "2.0",
		"method":  "agent.stream",
		"params":  map[string]any{"handle": handle},
		"id":      2,
	}
	if err := enc.Encode(req2); err != nil {
		t.Fatalf("encode agent.stream: %v", err)
	}
	line2, err := reader.ReadBytes('\n')
	if err != nil {
		t.Fatalf("read agent.stream resp: %v", err)
	}
	var streamResp map[string]any
	if err := json.Unmarshal(line2, &streamResp); err != nil {
		t.Fatalf("decode agent.stream resp: %v", err)
	}
	sresult, ok := streamResp["result"].(map[string]any)
	if !ok {
		t.Fatalf("no result in agent.stream resp: %s", string(line2))
	}
	streamID = int64(sresult["stream_id"].(float64))

	// Attach sidecar.
	sidecar, err = net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("dial sidecar: %v", err)
	}
	if _, err := sidecar.Write([]byte("STREAM " + itoa(streamID) + " 0\n")); err != nil {
		t.Fatalf("write sidecar preamble: %v", err)
	}

	return conn, sidecar, handle, streamID
}

func itoa(n int64) string {
	return fmt.Sprintf("%d", n)
}

// readStreamEvents reads events from the sidecar until timeout.
func readStreamEvents(t *testing.T, sidecar net.Conn, timeout time.Duration) []map[string]any {
	t.Helper()
	if err := sidecar.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	reader := bufio.NewReader(sidecar)
	var events []map[string]any
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			break
		}
		var ev map[string]any
		if err := json.Unmarshal(line, &ev); err != nil {
			continue
		}
		events = append(events, ev)
	}
	return events
}

// TestAgentOpen_SecurityContextInjected verifies that when a CRITICAL finding
// exists in pantryDB, opening an _echo session causes the session to receive
// a priming message containing the security context block before user messages.
func TestAgentOpen_SecurityContextInjected(t *testing.T) {
	stateDir, err := os.MkdirTemp("", "mw-sectest-")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	defer os.RemoveAll(stateDir)

	sock := filepath.Join(stateDir, "sock")

	// Open a pantry DB with a security finding.
	db, err := pantry.Open(filepath.Join(stateDir, "milliways.db"))
	if err != nil {
		t.Fatalf("pantry.Open: %v", err)
	}
	defer db.Close()

	workspace, _ := os.Getwd()
	if err := db.Security().UpsertFinding(pantry.SecurityFinding{
		Workspace:        workspace,
		CVEID:            "CVE-2024-TEST",
		Severity:         "CRITICAL",
		PackageName:      "github.com/vuln/pkg",
		InstalledVersion: "v1.0.0",
		FixedInVersion:   "v1.0.1",
		Summary:          "Test vulnerability for priming injection",
		Status:           "active",
		FirstSeen:        time.Now(),
	}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	srv, err := NewServer(sock)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	srv.pantryDB = db
	go srv.Serve()
	defer srv.Close()

	time.Sleep(50 * time.Millisecond)

	conn, sidecar, handle, _ := dialAndSetup(t, sock)
	defer conn.Close()
	defer sidecar.Close()

	// Send a test message to the _echo agent so the stream is active.
	enc := json.NewEncoder(conn)
	sendReq := map[string]any{
		"jsonrpc": "2.0",
		"method":  "agent.send",
		"params": map[string]any{
			"handle": handle,
			"bytes":  "hello",
		},
		"id": 3,
	}
	if err := enc.Encode(sendReq); err != nil {
		t.Fatalf("encode agent.send: %v", err)
	}

	// Read events from stream — collect for 300ms.
	events := readStreamEvents(t, sidecar, 300*time.Millisecond)

	// Decode all b64 payloads and concatenate.
	var decoded strings.Builder
	for _, ev := range events {
		if b64, ok := ev["b64"].(string); ok {
			bs, err := base64.StdEncoding.DecodeString(b64)
			if err == nil {
				decoded.Write(bs)
			}
		}
	}
	combined := decoded.String()

	if !strings.Contains(combined, "security context") {
		t.Errorf("expected security context in stream output, got: %q", combined)
	}
	if !strings.Contains(combined, "CVE-2024-TEST") {
		t.Errorf("expected CVE-2024-TEST in stream output, got: %q", combined)
	}
}

// TestAgentOpen_SecurityContextSuppressed verifies that when security_context
// is explicitly false, no priming message is injected.
func TestAgentOpen_SecurityContextSuppressed(t *testing.T) {
	stateDir, err := os.MkdirTemp("", "mw-sectest2-")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	defer os.RemoveAll(stateDir)

	sock := filepath.Join(stateDir, "sock")

	db, err := pantry.Open(filepath.Join(stateDir, "milliways.db"))
	if err != nil {
		t.Fatalf("pantry.Open: %v", err)
	}
	defer db.Close()

	workspace, _ := os.Getwd()
	if err := db.Security().UpsertFinding(pantry.SecurityFinding{
		Workspace:        workspace,
		CVEID:            "CVE-2024-SUPPRESS",
		Severity:         "CRITICAL",
		PackageName:      "github.com/vuln/pkg",
		InstalledVersion: "v1.0.0",
		Status:           "active",
		FirstSeen:        time.Now(),
	}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	srv, err := NewServer(sock)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	srv.pantryDB = db
	go srv.Serve()
	defer srv.Close()

	time.Sleep(50 * time.Millisecond)

	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	enc := json.NewEncoder(conn)
	reader := bufio.NewReader(conn)

	secFalse := false
	req := map[string]any{
		"jsonrpc": "2.0",
		"method":  "agent.open",
		"params": map[string]any{
			"agent_id":         "_echo",
			"security_context": secFalse,
		},
		"id": 1,
	}
	if err := enc.Encode(req); err != nil {
		t.Fatalf("encode agent.open: %v", err)
	}
	line, err := reader.ReadBytes('\n')
	if err != nil {
		t.Fatalf("read agent.open resp: %v", err)
	}
	var openResp map[string]any
	if err := json.Unmarshal(line, &openResp); err != nil {
		t.Fatalf("decode resp: %v", err)
	}
	result, ok := openResp["result"].(map[string]any)
	if !ok {
		t.Fatalf("no result: %s", string(line))
	}
	handle := int64(result["handle"].(float64))

	// agent.stream
	req2 := map[string]any{
		"jsonrpc": "2.0",
		"method":  "agent.stream",
		"params":  map[string]any{"handle": handle},
		"id":      2,
	}
	if err := enc.Encode(req2); err != nil {
		t.Fatalf("encode agent.stream: %v", err)
	}
	line2, err := reader.ReadBytes('\n')
	if err != nil {
		t.Fatalf("read agent.stream resp: %v", err)
	}
	var streamResp map[string]any
	if err := json.Unmarshal(line2, &streamResp); err != nil {
		t.Fatalf("decode stream resp: %v", err)
	}
	sresult, ok := streamResp["result"].(map[string]any)
	if !ok {
		t.Fatalf("no result in stream resp: %s", string(line2))
	}
	streamID := int64(sresult["stream_id"].(float64))

	sidecar, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("dial sidecar: %v", err)
	}
	defer sidecar.Close()
	if _, err := sidecar.Write([]byte("STREAM " + itoa(streamID) + " 0\n")); err != nil {
		t.Fatalf("write sidecar preamble: %v", err)
	}

	// Send a message so there's some stream activity.
	sendReq := map[string]any{
		"jsonrpc": "2.0",
		"method":  "agent.send",
		"params":  map[string]any{"handle": handle, "bytes": "hello"},
		"id":      3,
	}
	if err := enc.Encode(sendReq); err != nil {
		t.Fatalf("encode send: %v", err)
	}

	events := readStreamEvents(t, sidecar, 300*time.Millisecond)

	var decoded strings.Builder
	for _, ev := range events {
		if b64, ok := ev["b64"].(string); ok {
			bs, err := base64.StdEncoding.DecodeString(b64)
			if err == nil {
				decoded.Write(bs)
			}
		}
	}
	combined := decoded.String()

	if strings.Contains(combined, "security context") {
		t.Errorf("expected NO security context when suppressed, got: %q", combined)
	}
	if strings.Contains(combined, "CVE-2024-SUPPRESS") {
		t.Errorf("expected NO CVE in suppressed output, got: %q", combined)
	}
}
