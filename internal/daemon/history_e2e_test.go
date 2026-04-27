package daemon

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestHistoryE2E spins up a real Server, dials it via UDS, exercises the
// agent.* lifecycle with the _echo agent (which echoes back whatever bytes
// are sent), and verifies that history.append and history.get correctly
// capture and return the resulting data and chunk_end events.
func TestHistoryE2E(t *testing.T) {
	stateDir, err := os.MkdirTemp("", "milliways-e2e-")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	sock := filepath.Join(stateDir, "sock")
	defer os.RemoveAll(stateDir)

	srv, err := NewServer(sock)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	go srv.Serve()
	defer srv.Close()

	// Give the listener time to be ready.
	time.Sleep(50 * time.Millisecond)

	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	enc := json.NewEncoder(conn)
	reader := bufio.NewReader(conn)

	send := func(method string, params, id any) {
		req := map[string]any{
			"jsonrpc": "2.0",
			"method":  method,
		}
		if params != nil {
			req["params"] = params
		}
		if id != nil {
			req["id"] = id
		}
		if err := enc.Encode(req); err != nil {
			t.Fatalf("encode %s: %v", method, err)
		}
	}

	readResp := func() (map[string]any, error) {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			return nil, err
		}
		var resp map[string]any
		if err := json.Unmarshal(line, &resp); err != nil {
			return nil, err
		}
		return resp, nil
	}

	// Step 1: agent.open for _echo agent.
	send("agent.open", map[string]any{"agent_id": "_echo"}, 1)
	resp, err := readResp()
	if err != nil {
		t.Fatalf("read open: %v", err)
	}
	if errObj, ok := resp["error"].(map[string]any); ok {
		t.Fatalf("agent.open error: %v", errObj)
	}
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("open result: %v", resp)
	}
	handle, ok := result["handle"].(float64)
	if !ok {
		t.Fatalf("handle type: %v", result)
	}
	t.Logf("opened handle=%v", handle)

	// Step 2: agent.stream to get stream_id.
	send("agent.stream", map[string]any{"handle": handle}, 2)
	resp, err = readResp()
	if err != nil {
		t.Fatalf("read stream: %v", err)
	}
	if errObj, ok := resp["error"].(map[string]any); ok {
		t.Fatalf("agent.stream error: %v", errObj)
	}
	streamResult, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("stream result: %v", resp)
	}
	streamID, ok := streamResult["stream_id"].(float64)
	if !ok {
		t.Fatalf("stream_id type: %v", streamResult)
	}
	t.Logf("stream_id=%v", streamID)

	// Step 3: open sidecar connection to receive events.
	sidecar, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("sidecar dial: %v", err)
	}
	defer sidecar.Close()

	preamble := fmt.Sprintf("STREAM %d %d\n", int64(streamID), int64(0))
	if _, err := sidecar.Write([]byte(preamble)); err != nil {
		t.Fatalf("sidecar write preamble: %v", err)
	}

	// Step 4: agent.send to trigger echo output on sidecar.
	send("agent.send", map[string]any{
		"handle": handle,
		"bytes":  "hello from e2e test",
	}, 3)
	resp, err = readResp()
	if err != nil {
		t.Fatalf("read send: %v", err)
	}
	if _, ok := resp["error"].(map[string]any); ok {
		t.Fatalf("agent.send error: %v", resp)
	}

	// Step 5: read events from sidecar (set deadline so we don't block forever).
	sidecarReader := bufio.NewReader(sidecar)
	_ = sidecar.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	for i := 0; i < 3; i++ {
		line, err := sidecarReader.ReadBytes('\n')
		if err != nil {
			break
		}
		var ev map[string]any
		if json.Unmarshal(line, &ev) != nil {
			continue
		}
		if ev["t"] == "data" {
			t.Logf("sidecar received data event: %v", ev)
		}
	}
	// Clear deadline.
	_ = sidecar.SetReadDeadline(time.Time{})

	// Step 6: history.append a data event directly (simulates what recordingPusher does).
	testPayload := map[string]any{
		"t":    "data",
		"text": "hello from e2e test",
	}
	send("history.append", map[string]any{
		"agent_id": "_echo",
		"payload":  testPayload,
	}, 4)
	resp, err = readResp()
	if err != nil {
		t.Fatalf("read history.append: %v", err)
	}
	if errObj, ok := resp["error"].(map[string]any); ok {
		t.Fatalf("history.append error: %v", errObj)
	}

	// Step 7: history.append a chunk_end event.
	chunkEndPayload := map[string]any{
		"t":            "chunk_end",
		"cost_usd":      0.015,
		"input_tokens":  100,
		"output_tokens": 50,
	}
	send("history.append", map[string]any{
		"agent_id": "_echo",
		"payload":  chunkEndPayload,
	}, 5)
	resp, err = readResp()
	if err != nil {
		t.Fatalf("read history.append chunk_end: %v", err)
	}
	if errObj, ok := resp["error"].(map[string]any); ok {
		t.Fatalf("history.append chunk_end error: %v", errObj)
	}

	// Step 8: history.get and verify both entries.
	send("history.get", map[string]any{"agent_id": "_echo", "limit": 20}, 6)
	resp, err = readResp()
	if err != nil {
		t.Fatalf("read history.get: %v", err)
	}
	if errObj, ok := resp["error"].(map[string]any); ok {
		t.Fatalf("history.get error: %v", errObj)
	}
	entries, ok := resp["result"].([]any)
	if !ok {
		t.Fatalf("history.get result type: %v", resp)
	}

	if len(entries) < 2 {
		t.Fatalf("expected at least 2 history entries, got %d: %v", len(entries), entries)
	}

	var foundData, foundChunkEnd bool
	for _, e := range entries {
		entry, ok := e.(map[string]any)
		if !ok {
			continue
		}
		if v, ok := entry["v"].(map[string]any); ok {
			switch v["t"] {
			case "data":
				if text, ok := v["text"].(string); ok && text != "" {
					foundData = true
					t.Logf("history entry contains data: %q", text)
				}
			case "chunk_end":
				foundChunkEnd = true
				if cost, ok := v["cost_usd"].(float64); ok {
					t.Logf("found chunk_end with cost_usd: %v", cost)
				}
				if tokIn, ok := v["input_tokens"].(float64); ok {
					t.Logf("found chunk_end with input_tokens: %v", tokIn)
				}
				if tokOut, ok := v["output_tokens"].(float64); ok {
					t.Logf("found chunk_end with output_tokens: %v", tokOut)
				}
			}
		}
	}
	if !foundData {
		t.Errorf("expected data entry in history; got: %v", entries)
	}
	if !foundChunkEnd {
		t.Errorf("expected chunk_end entry in history; got: %v", entries)
	}

	// Step 9: close session.
	send("agent.close", map[string]any{"handle": handle}, 7)
	_, _ = readResp() // drain

	t.Log("TestHistoryE2E passed")
}

// TestHistoryE2EPayloadTooLarge verifies that history.append rejects payloads
// exceeding 1 MiB.
func TestHistoryE2EPayloadTooLarge(t *testing.T) {
	stateDir, err := os.MkdirTemp("", "milliways-e2e-")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	sock := filepath.Join(stateDir, "sock")
	defer os.RemoveAll(stateDir)

	srv, err := NewServer(sock)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
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

	// Payload just over 1 MB.
	largePayload := make([]byte, 1<<20+1)
	for i := range largePayload {
		largePayload[i] = 'x'
	}

	req := map[string]any{
		"jsonrpc": "2.0",
		"method":  "history.append",
		"params": map[string]any{
			"agent_id": "_echo",
			"payload":  string(largePayload),
		},
		"id": 1,
	}
	if err := enc.Encode(req); err != nil {
		t.Fatalf("encode: %v", err)
	}

	resp, err := readRespErr(reader)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatal("expected error response for oversized payload")
	}
	if code, ok := errObj["code"].(float64); ok && int(code) != ErrInvalidParams {
		t.Errorf("expected ErrInvalidParams (%d), got %v", ErrInvalidParams, code)
	}
	t.Log("TestHistoryE2EPayloadTooLarge passed")
}

// readRespErr is a helper that reads one NDJSON line and returns the parsed map.
func readRespErr(reader *bufio.Reader) (map[string]any, error) {
	line, err := reader.ReadBytes('\n')
	if err != nil {
		return nil, err
	}
	var resp map[string]any
	if err := json.Unmarshal(line, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// TestHistoryE2EInvalidAgentID verifies that history.append rejects unknown
// agent_ids and missing agent_ids.
func TestHistoryE2EInvalidAgentID(t *testing.T) {
	stateDir, err := os.MkdirTemp("", "milliways-e2e-")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	sock := filepath.Join(stateDir, "sock")
	defer os.RemoveAll(stateDir)

	srv, err := NewServer(sock)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
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

	sendReq := func(agentID string) {
		req := map[string]any{
			"jsonrpc": "2.0",
			"method":  "history.append",
			"params": map[string]any{
				"agent_id": agentID,
				"payload":  map[string]any{"x": 1},
			},
			"id": 1,
		}
		if err := enc.Encode(req); err != nil {
			t.Fatalf("encode: %v", err)
		}
	}

	// Empty agent_id → ErrInvalidParams.
	sendReq("")
	resp, err := readRespErr(reader)
	if err != nil {
		t.Fatalf("read empty agent_id resp: %v", err)
	}
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatal("expected error for empty agent_id")
	}
	if code, ok := errObj["code"].(float64); ok && int(code) != ErrInvalidParams {
		t.Errorf("expected ErrInvalidParams, got %v", code)
	}

	// Unknown agent_id → ErrInvalidParams.
	sendReq("rogue-agent")
	resp, err = readRespErr(reader)
	if err != nil {
		t.Fatalf("read unknown agent_id resp: %v", err)
	}
	errObj, ok = resp["error"].(map[string]any)
	if !ok {
		t.Fatal("expected error for unknown agent_id")
	}
	if code, ok := errObj["code"].(float64); ok && int(code) != ErrInvalidParams {
		t.Errorf("expected ErrInvalidParams for unknown agent, got %v", code)
	}
	t.Log("TestHistoryE2EInvalidAgentID passed")
}

// TestHistoryE2ERateLimit verifies that history.append returns ErrQuotaExceeded
// when the per-agent rate limit is exceeded.
func TestHistoryE2ERateLimit(t *testing.T) {
	stateDir, err := os.MkdirTemp("", "milliways-e2e-")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	sock := filepath.Join(stateDir, "sock")
	defer os.RemoveAll(stateDir)

	srv, err := NewServer(sock)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
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

	payload, _ := json.Marshal(map[string]any{"x": 1})
	reqTemplate := map[string]any{
		"jsonrpc": "2.0",
		"method":  "history.append",
		"params": map[string]any{
			"agent_id": "_echo",
			"payload":  string(payload),
		},
	}

	// Send all 80 calls in a batch WITHOUT waiting for responses,
	// then read responses. The rate limit is 60/min, so calls 0-59
	// should succeed and calls 60+ should fail with ErrQuotaExceeded.
	const totalCalls = 80
	for i := 0; i < totalCalls; i++ {
		req := reqTemplate
		req["id"] = i
		if err := enc.Encode(req); err != nil {
			t.Fatalf("encode: %v", err)
		}
	}

	// Read responses: count how many succeeded vs failed.
	var successCount, quotaFailCount int
	for i := 0; i < totalCalls; i++ {
		resp, err := readRespErr(reader)
		if err != nil {
			t.Fatalf("read resp %d: %v", i, err)
		}
		if errObj, ok := resp["error"].(map[string]any); ok {
			if code, ok := errObj["code"].(float64); ok && int(code) == ErrQuotaExceeded {
				quotaFailCount++
			}
		} else {
			successCount++
		}
	}

	t.Logf("successCount=%d quotaFailCount=%d", successCount, quotaFailCount)
	if successCount != 60 {
		t.Errorf("expected exactly 60 successful calls, got %d", successCount)
	}
	if quotaFailCount != 20 {
		t.Errorf("expected 20 quota failures, got %d", quotaFailCount)
	}
	t.Log("TestHistoryE2ERateLimit passed")
}

// TestHistoryQuotaCheck tests the HistoryQuota.Check method directly.
func TestHistoryQuotaCheck(t *testing.T) {
	q := NewHistoryQuota()
	stateDir, err := os.MkdirTemp("", "milliways-quota-")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	defer os.RemoveAll(stateDir)

	// Rate limit: 61st call within same window should fail.
	for i := 0; i < 61; i++ {
		err := q.Check("_echo", stateDir, 100)
		if i < 60 && err != nil {
			t.Errorf("call %d (count=%d) failed unexpectedly: %v", i, i+1, err)
		}
		if i == 60 && err == nil {
			t.Errorf("61st call should have been rejected, but it succeeded")
		}
		if i >= 60 && err != nil {
			t.Logf("call %d correctly rejected: %v", i, err)
		}
	}
}