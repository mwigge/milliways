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
	"context"
	"encoding/base64"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mwigge/milliways/internal/parallel"
)

// stubMPClient implements parallel.MPClient and captures KGAdd calls.
// KGQuery returns a pre-seeded finding when the subject matches seedSubject.
// All methods are safe for concurrent use.
type stubMPClient struct {
	seedSubject string
	seedObject  string

	mu        sync.Mutex
	addedKeys []string
}

func (m *stubMPClient) KGQuery(_ context.Context, subjectPrefix, _ string, _ map[string]string) ([]parallel.KGTriple, error) {
	if m.seedSubject != "" && strings.HasPrefix(subjectPrefix, m.seedSubject) {
		return []parallel.KGTriple{{
			Subject:    m.seedSubject,
			Predicate:  "has_finding",
			Object:     m.seedObject,
			Properties: map[string]string{"source": "test", "ts": "2026-01-01T00:00:00Z"},
		}}, nil
	}
	return nil, nil
}

func (m *stubMPClient) KGAdd(_ context.Context, subject, _, _ string, _ map[string]string) error {
	m.mu.Lock()
	m.addedKeys = append(m.addedKeys, subject)
	m.mu.Unlock()
	return nil
}

func (m *stubMPClient) addedKeysCopy() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]string, len(m.addedKeys))
	copy(cp, m.addedKeys)
	return cp
}

// agentMethodsHarness spins up a real Server, optionally wires a fake
// pantryDB surrogate so mempalaceClient() returns our stub, and returns
// helpers to drive the agent.* RPC surface.
//
// It does NOT use a real pantry.DB — instead it monkey-patches
// srv.pantryDB with a non-nil sentinel so the "if s.pantryDB != nil"
// guards pass, and overrides mempalaceClient to return the stub.
//
// The test infrastructure then reads stream events from the _echo sidecar
// to assert what bytes reached the session input channel.
type agentMethodsHarness struct {
	t        *testing.T
	srv      *Server
	stateDir string
	conn     net.Conn
	sidecar  net.Conn
	enc      *json.Encoder
	reader   *bufio.Reader
	handle   int64
}

// newAgentMethodsHarness creates a test server with the stub MP wired in.
// It uses the parallel_methods_test.go pattern of starting a real Server
// but replaces pantryDB with a real pantry db so guards pass.
func newAgentMethodsHarness(t *testing.T, mp parallel.MPClient) *agentMethodsHarness {
	t.Helper()
	stateDir, err := os.MkdirTemp("", "mw-agtest-")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	sock := filepath.Join(stateDir, "sock")

	srv, err := NewServer(sock)
	if err != nil {
		os.RemoveAll(stateDir)
		t.Fatalf("NewServer: %v", err)
	}

	// Install the stub mempalace override so InjectBaseline uses it.
	if mp != nil {
		srv.testMPClient = mp
	}

	go srv.Serve()
	time.Sleep(50 * time.Millisecond)

	conn, err := net.Dial("unix", sock)
	if err != nil {
		srv.Close()
		os.RemoveAll(stateDir)
		t.Fatalf("dial: %v", err)
	}
	enc := json.NewEncoder(conn)
	reader := bufio.NewReader(conn)

	h := &agentMethodsHarness{
		t:        t,
		srv:      srv,
		stateDir: stateDir,
		conn:     conn,
		enc:      enc,
		reader:   reader,
	}
	t.Cleanup(func() {
		conn.Close()
		if h.sidecar != nil {
			h.sidecar.Close()
		}
		srv.Close()
		os.RemoveAll(stateDir)
	})
	return h
}

// openEcho performs agent.open + agent.stream and attaches a sidecar.
func (h *agentMethodsHarness) openEcho() {
	h.t.Helper()
	// agent.open
	h.send("agent.open", map[string]any{"agent_id": "_echo"}, 1)
	resp := h.readResp()
	result, ok := resp["result"].(map[string]any)
	if !ok {
		h.t.Fatalf("agent.open result: %v", resp)
	}
	h.handle = int64(result["handle"].(float64))

	// agent.stream
	h.send("agent.stream", map[string]any{"handle": h.handle}, 2)
	resp2 := h.readResp()
	sresult, ok := resp2["result"].(map[string]any)
	if !ok {
		h.t.Fatalf("agent.stream result: %v", resp2)
	}
	streamID := int64(sresult["stream_id"].(float64))

	// Attach sidecar.
	var err error
	h.sidecar, err = net.Dial("unix", h.srv.socket)
	if err != nil {
		h.t.Fatalf("dial sidecar: %v", err)
	}
	if _, err := h.sidecar.Write([]byte("STREAM " + itoa(streamID) + " 0\n")); err != nil {
		h.t.Fatalf("write sidecar preamble: %v", err)
	}
}

func (h *agentMethodsHarness) send(method string, params, id any) {
	h.t.Helper()
	req := map[string]any{"jsonrpc": "2.0", "method": method}
	if params != nil {
		req["params"] = params
	}
	if id != nil {
		req["id"] = id
	}
	if err := h.enc.Encode(req); err != nil {
		h.t.Fatalf("encode %s: %v", method, err)
	}
}

func (h *agentMethodsHarness) readResp() map[string]any {
	h.t.Helper()
	line, err := h.reader.ReadBytes('\n')
	if err != nil {
		h.t.Fatalf("readResp: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(line, &m); err != nil {
		h.t.Fatalf("unmarshal resp: %v", err)
	}
	return m
}

// readStreamText collects decoded b64 text from sidecar events for up to
// timeout. Returns all decoded text concatenated.
func (h *agentMethodsHarness) readStreamText(timeout time.Duration) string {
	h.t.Helper()
	return readTextFromSidecar(h.t, h.sidecar, timeout)
}

func readTextFromSidecar(t *testing.T, sidecar net.Conn, timeout time.Duration) string {
	t.Helper()
	if err := sidecar.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	defer func() { _ = sidecar.SetReadDeadline(time.Time{}) }()

	sidecarReader := bufio.NewReader(sidecar)
	var sb strings.Builder
	for {
		line, err := sidecarReader.ReadBytes('\n')
		if err != nil {
			break
		}
		var ev map[string]any
		if json.Unmarshal(line, &ev) != nil {
			continue
		}
		if b64, ok := ev["b64"].(string); ok {
			bs, err2 := base64.StdEncoding.DecodeString(b64)
			if err2 == nil {
				sb.Write(bs)
			}
		}
	}
	return sb.String()
}

func (h *agentMethodsHarness) attachAdditionalStream(id any) net.Conn {
	h.t.Helper()
	h.send("agent.stream", map[string]any{"handle": h.handle}, id)
	resp := h.readResp()
	sresult, ok := resp["result"].(map[string]any)
	if !ok {
		h.t.Fatalf("agent.stream result: %v", resp)
	}
	streamID := int64(sresult["stream_id"].(float64))
	sidecar, err := net.Dial("unix", h.srv.socket)
	if err != nil {
		h.t.Fatalf("dial sidecar: %v", err)
	}
	if _, err := sidecar.Write([]byte("STREAM " + itoa(streamID) + " 0\n")); err != nil {
		sidecar.Close()
		h.t.Fatalf("write sidecar preamble: %v", err)
	}
	h.t.Cleanup(func() { sidecar.Close() })
	return sidecar
}

func TestAgentStream_FansOutToMultipleSubscribers(t *testing.T) {
	h := newAgentMethodsHarness(t, nil)
	h.openEcho()
	second := h.attachAdditionalStream(20)

	const prompt = "hello fanout"
	h.send("agent.send", map[string]any{"handle": h.handle, "bytes": prompt}, 21)
	_ = h.readResp()

	firstText := h.readStreamText(400 * time.Millisecond)
	secondText := readTextFromSidecar(t, second, 400*time.Millisecond)
	if !strings.Contains(firstText, prompt) {
		t.Fatalf("first subscriber missing prompt %q in %q", prompt, firstText)
	}
	if !strings.Contains(secondText, prompt) {
		t.Fatalf("second subscriber missing prompt %q in %q", prompt, secondText)
	}
}

func TestDeckSnapshotReportsSessionStateAndBuffer(t *testing.T) {
	h := newAgentMethodsHarness(t, nil)
	h.openEcho()

	const prompt = "show deck state"
	h.send("agent.send", map[string]any{"handle": h.handle, "bytes": prompt}, 30)
	_ = h.readResp()
	_ = h.readStreamText(200 * time.Millisecond)

	h.send("deck.snapshot", nil, 31)
	resp := h.readResp()
	raw, err := json.Marshal(resp["result"])
	if err != nil {
		t.Fatalf("marshal deck snapshot: %v", err)
	}
	var snap DeckSnapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatalf("unmarshal deck snapshot: %v", err)
	}
	if snap.Active != "_echo" {
		t.Fatalf("active = %q, want _echo", snap.Active)
	}
	if len(snap.Sessions) != 1 {
		t.Fatalf("sessions = %d, want 1: %#v", len(snap.Sessions), snap.Sessions)
	}
	sess := snap.Sessions[0]
	if sess.Handle != h.handle {
		t.Fatalf("handle = %d, want %d", sess.Handle, h.handle)
	}
	if sess.Status != "streaming" {
		t.Fatalf("status = %q, want streaming", sess.Status)
	}
	if sess.PromptCount != 1 {
		t.Fatalf("prompt count = %d, want 1", sess.PromptCount)
	}
	if sess.CurrentTrace == "" {
		t.Fatalf("current trace empty in deck snapshot: %#v", sess)
	}
	if sess.TTFTMS <= 0 {
		t.Fatalf("ttft_ms = %f, want > 0", sess.TTFTMS)
	}
	var sawPrompt, sawResponse bool
	for _, block := range sess.Buffer {
		if block.Kind == "prompt" && strings.Contains(block.Text, prompt) {
			sawPrompt = true
		}
		if block.Kind == "response" && strings.Contains(block.Text, prompt) {
			sawResponse = true
		}
	}
	if !sawPrompt || !sawResponse {
		t.Fatalf("buffer missing prompt/response blocks: %#v", sess.Buffer)
	}
}

func TestRecordingPusherRecordsModelInDeckSnapshot(t *testing.T) {
	sess := &AgentSession{
		AgentID: "_echo",
		Handle:  42,
		input:   make(chan []byte, 1),
	}
	pusher := &recordingPusher{sess: sess}
	pusher.Push(map[string]any{"t": "model", "model": "codex-resolved", "source": "observed"})

	snap := sess.deckSnapshot()
	if snap.Model != "codex-resolved" {
		t.Fatalf("model = %q, want codex-resolved", snap.Model)
	}
	if snap.ModelSource != "observed" {
		t.Fatalf("model source = %q, want observed", snap.ModelSource)
	}
}

func TestAgentListOverlaysObservedSessionModel(t *testing.T) {
	reg := NewAgentRegistry(nil)
	sess := &AgentSession{
		AgentID: "codex",
		Handle:  7,
		input:   make(chan []byte, 1),
	}
	sess.recordModel("gpt-5.5", "observed")
	reg.mu.Lock()
	reg.sessions[7] = sess
	reg.mu.Unlock()

	srv := &Server{
		agents: reg,
		agentsCache: []AgentInfo{{
			ID:         "codex",
			Available:  true,
			AuthStatus: "ok",
			Model:      "codex CLI default",
		}},
	}
	agents := srv.agentList()
	if len(agents) != 1 {
		t.Fatalf("agentList len = %d, want 1", len(agents))
	}
	if agents[0].Model != "gpt-5.5" {
		t.Fatalf("agent model = %q, want observed model", agents[0].Model)
	}
}

func TestTextFromDeckBlocksUsesResponseBlocksOnly(t *testing.T) {
	got := textFromDeckBlocks([]DeckBlock{
		{Kind: "prompt", Text: "ignore prompt"},
		{Kind: "thinking", Text: "ignore thinking"},
		{Kind: "response", Text: "first line\n"},
		{Kind: "response", Text: "second line"},
	}, 0)
	if got != "first line\nsecond line" {
		t.Fatalf("textFromDeckBlocks = %q", got)
	}
}

// TestAgentSend_MemPalaceBaselineInjectedOnFirstSend verifies that when
// MemPalace has a prior finding for a file path referenced in the prompt,
// the baseline block is sent to the session BEFORE the user's bytes on
// the first agent.send call in a session.
func TestAgentSend_MemPalaceBaselineInjectedOnFirstSend(t *testing.T) {
	mp := &stubMPClient{
		seedSubject: "file:internal/server/auth.go",
		seedObject:  "null deref at line 42",
	}
	h := newAgentMethodsHarness(t, mp)
	h.openEcho()

	// First send — prompt references a Go file path so InjectBaseline can match.
	prompt := "review internal/server/auth.go for issues"
	h.send("agent.send", map[string]any{"handle": h.handle, "bytes": prompt}, 3)
	_ = h.readResp()

	combined := h.readStreamText(400 * time.Millisecond)

	if !strings.Contains(combined, "[prior findings from mempalace]") {
		t.Errorf("expected MemPalace baseline block in first send; got: %q", combined)
	}
	if !strings.Contains(combined, "null deref at line 42") {
		t.Errorf("expected finding content in baseline; got: %q", combined)
	}
	// User's prompt must also arrive.
	if !strings.Contains(combined, prompt) {
		t.Errorf("expected user prompt in stream; got: %q", combined)
	}
}

// TestAgentSend_BaselineNotInjectedOnSubsequentSends verifies that the second
// (and later) sends in the same session do NOT re-inject the MemPalace baseline.
func TestAgentSend_BaselineNotInjectedOnSubsequentSends(t *testing.T) {
	mp := &stubMPClient{
		seedSubject: "file:internal/server/auth.go",
		seedObject:  "null deref at line 42",
	}
	h := newAgentMethodsHarness(t, mp)
	h.openEcho()

	// First send — baseline should be injected (same as above).
	firstPrompt := "review internal/server/auth.go"
	h.send("agent.send", map[string]any{"handle": h.handle, "bytes": firstPrompt}, 3)
	_ = h.readResp()
	firstText := h.readStreamText(400 * time.Millisecond)
	if !strings.Contains(firstText, "[prior findings from mempalace]") {
		t.Skipf("baseline not injected on first send; skipping subsequent check: %q", firstText)
	}

	// Second send — same file path, but baseline must NOT be injected again.
	secondPrompt := "follow up: also check internal/server/auth.go error handling"
	h.send("agent.send", map[string]any{"handle": h.handle, "bytes": secondPrompt}, 4)
	_ = h.readResp()
	secondText := h.readStreamText(400 * time.Millisecond)

	if strings.Contains(secondText, "[prior findings from mempalace]") {
		t.Errorf("MemPalace baseline must NOT be injected on second send; got: %q", secondText)
	}
	// The user's prompt must still arrive.
	if !strings.Contains(secondText, secondPrompt) {
		t.Errorf("expected second prompt in stream; got: %q", secondText)
	}
}

// TestAgentSend_OnTurnComplete_WritesFindings verifies that when the _echo
// agent emits an "end" event, the onTurnComplete hook fires and findings
// extracted from the response are written to MemPalace.
//
// The _echo agent pushes back whatever bytes it receives; so we send text
// that matches the finding pattern (file: description) and wait for KGAdd
// to be called.
func TestAgentSend_OnTurnComplete_WritesFindings(t *testing.T) {
	mp := &stubMPClient{}
	h := newAgentMethodsHarness(t, mp)
	h.openEcho()

	// Send text that contains a finding line (no file-path prefix needed for
	// KGQuery in this test — we just check KGAdd is called after the turn).
	findingText := "internal/pkg/foo.go: unhandled error from os.Open"
	h.send("agent.send", map[string]any{"handle": h.handle, "bytes": findingText}, 3)
	_ = h.readResp()

	// Drain stream output so the echo finishes.
	_ = h.readStreamText(400 * time.Millisecond)

	// Close the session to flush — _echo emits "end" on close.
	h.send("agent.close", map[string]any{"handle": h.handle}, 4)
	_ = h.readResp()

	// Give the onTurnComplete goroutine a moment to run.
	time.Sleep(100 * time.Millisecond)

	added := mp.addedKeysCopy()
	if len(added) == 0 {
		t.Error("expected findings to be written to MemPalace after turn complete; KGAdd was never called")
	}
	var found bool
	for _, k := range added {
		if strings.Contains(k, "internal/pkg/foo.go") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected KGAdd for internal/pkg/foo.go; addedKeys=%v", added)
	}
}
