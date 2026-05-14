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

	"github.com/mwigge/milliways/internal/daemon/runners"
	"github.com/mwigge/milliways/internal/history"
	"github.com/mwigge/milliways/internal/parallel"
	"github.com/mwigge/milliways/internal/security"
)

// stubMPClient implements parallel.MPClient and captures KGAdd calls.
// KGQuery returns a pre-seeded finding when the subject matches seedSubject.
// All methods are safe for concurrent use.
type stubMPClient struct {
	seedSubject string
	seedObject  string

	mu          sync.Mutex
	addedKeys   []string
	invalidated []string
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

func (m *stubMPClient) KGInvalidate(_ context.Context, subject, predicate, object string) error {
	m.mu.Lock()
	m.invalidated = append(m.invalidated, subject+"|"+predicate+"|"+object)
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

func (m *stubMPClient) invalidatedCopy() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]string, len(m.invalidated))
	copy(cp, m.invalidated)
	return cp
}

type stubCGClient struct {
	impact []string
	delay  time.Duration
}

func (c *stubCGClient) Search(context.Context, string) ([]parallel.CodeGraphResult, error) {
	return nil, nil
}

func (c *stubCGClient) Callers(context.Context, string) ([]string, error) {
	return nil, nil
}

func (c *stubCGClient) Callees(context.Context, string) ([]string, error) {
	return nil, nil
}

func (c *stubCGClient) Impact(ctx context.Context, _ string) ([]string, error) {
	if c.delay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(c.delay):
		}
	}
	return c.impact, nil
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
	if workspace, _ := result["security_workspace"].(string); workspace == "" {
		h.t.Fatalf("agent.open security_workspace empty: %v", result)
	}

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

func TestAgentOpen_BlocksStrictClientProfileBeforeSessionCreation(t *testing.T) {
	db := openSecurityMethodTestDB(t)
	workspace := t.TempDir()
	t.Setenv("MILLIWAYS_WORKSPACE_ROOT", workspace)
	writeSecurityMethodFile(t, filepath.Join(workspace, ".codex", "config.toml"), `sandbox_mode = "danger-full-access"`)
	if err := db.Security().SetWorkspaceStatus(workspace, string(security.ModeStrict), "codex"); err != nil {
		t.Fatalf("SetWorkspaceStatus: %v", err)
	}

	s := &Server{pantryDB: db}
	s.agents = NewAgentRegistry(s)
	enc, buf := newCapturingEncoder()
	s.agentOpen(enc, &Request{
		ID:     mustSecurityMethodParams(t, 1),
		Params: mustSecurityMethodParams(t, map[string]any{"agent_id": "codex"}),
	})

	resp := decodeSecurityMethodResponse(t, buf.Bytes())
	rawErr, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("agent.open succeeded; want client-profile block: %v", resp)
	}
	if msg, _ := rawErr["message"].(string); !strings.Contains(msg, "client profile blocked codex") {
		t.Fatalf("error message = %q, want client profile block; resp=%v", msg, resp)
	}
	s.agents.mu.Lock()
	sessionCount := len(s.agents.sessions)
	s.agents.mu.Unlock()
	if sessionCount != 0 {
		t.Fatalf("sessions created = %d, want 0 before blocked client open", sessionCount)
	}

	status, err := db.Security().SecurityStatus(workspace)
	if err != nil {
		t.Fatalf("SecurityStatus: %v", err)
	}
	if status.CountsBySeverity["BLOCK"] == 0 {
		t.Fatalf("profile block was not recorded in status: %#v", status.Warnings)
	}
}

func TestAgentOpen_WarnModeRecordsClientProfileWithoutBlocking(t *testing.T) {
	db := openSecurityMethodTestDB(t)
	workspace := t.TempDir()
	t.Setenv("MILLIWAYS_WORKSPACE_ROOT", workspace)
	writeSecurityMethodFile(t, filepath.Join(workspace, ".codex", "config.toml"), `approval_policy = "never"`)
	if err := db.Security().SetWorkspaceStatus(workspace, string(security.ModeWarn), ""); err != nil {
		t.Fatalf("SetWorkspaceStatus: %v", err)
	}

	s := &Server{pantryDB: db}
	s.agents = NewAgentRegistry(s)
	enc, buf := newCapturingEncoder()
	s.agentOpen(enc, &Request{
		ID:     mustSecurityMethodParams(t, 1),
		Params: mustSecurityMethodParams(t, map[string]any{"agent_id": "codex"}),
	})

	resp := decodeSecurityMethodResponse(t, buf.Bytes())
	if _, ok := resp["error"]; ok {
		t.Fatalf("agent.open returned error in warn mode: %v", resp)
	}
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("agent.open result = %T, want map; resp=%v", resp["result"], resp)
	}
	handle, _ := result["handle"].(float64)
	if handle == 0 {
		t.Fatalf("agent.open handle missing: %v", result)
	}
	t.Cleanup(func() {
		if sess, ok := s.agents.Get(AgentHandle(handle)); ok {
			sess.Close()
		}
	})

	status, err := db.Security().SecurityStatus(workspace)
	if err != nil {
		t.Fatalf("SecurityStatus: %v", err)
	}
	if status.ActiveClient != "codex" {
		t.Fatalf("ActiveClient = %q, want codex", status.ActiveClient)
	}
	if status.CountsByCategory[string(security.FindingClient)] == 0 || status.CountsBySeverity["WARN"] == 0 {
		t.Fatalf("profile warning was not recorded in warn mode: %#v", status.Warnings)
	}
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
	runners.SetBrokerPathProvider(nil)
	t.Cleanup(func() { runners.SetBrokerPathProvider(nil) })

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
	if agents[0].Enforcement.Level != runners.EnforcementPreflightOnly {
		t.Fatalf("agent enforcement = %q, want %q", agents[0].Enforcement.Level, runners.EnforcementPreflightOnly)
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

func TestAgentSend_PersistsPromptHistoryWithTurnMetadata(t *testing.T) {
	h := newAgentMethodsHarness(t, nil)
	h.openEcho()

	prompt := "remember this prompt"
	h.send("agent.send", map[string]any{"handle": h.handle, "bytes": prompt}, 3)
	_ = h.readResp()

	var entries []map[string]any
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		var err error
		entries, err = history.ReadAgentHistory(h.stateDir, "_echo", 20)
		if err == nil {
			for _, entry := range entries {
				payload, _ := entry["v"].(map[string]any)
				if payload["t"] == "prompt" {
					if _, ok := payload["text"]; ok {
						t.Fatalf("prompt history stored raw text by default: %#v", payload)
					}
					if payload["text_redacted"] != true {
						t.Fatalf("prompt history missing redaction marker: %#v", payload)
					}
					if payload["text_bytes"] != float64(len(prompt)) {
						t.Fatalf("prompt history text_bytes = %v, want %d: %#v", payload["text_bytes"], len(prompt), payload)
					}
					if payload["text_sha256"] == "" {
						t.Fatalf("prompt history missing text hash: %#v", payload)
					}
					if payload["turn_id"] == "" || payload["trace_id"] == "" || payload["session_id"] == "" {
						t.Fatalf("prompt history missing metadata: %#v", payload)
					}
					return
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("prompt history entry not found: %#v", entries)
}

func TestAgentSend_PersistsRawPromptHistoryWhenExplicitlyEnabled(t *testing.T) {
	t.Setenv("MILLIWAYS_HISTORY_RAW_CONTENT", "1")
	h := newAgentMethodsHarness(t, nil)
	h.openEcho()

	prompt := "remember this prompt exactly"
	h.send("agent.send", map[string]any{"handle": h.handle, "bytes": prompt}, 3)
	_ = h.readResp()

	var entries []map[string]any
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		var err error
		entries, err = history.ReadAgentHistory(h.stateDir, "_echo", 20)
		if err == nil {
			for _, entry := range entries {
				payload, _ := entry["v"].(map[string]any)
				if payload["t"] == "prompt" && payload["text"] == prompt {
					if payload["text_redacted"] == true {
						t.Fatalf("raw history opt-in still marked text redacted: %#v", payload)
					}
					return
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("raw prompt history entry not found: %#v", entries)
}

func TestAgentSend_CodeGraphContextInjectedOnNormalSend(t *testing.T) {
	h := newAgentMethodsHarness(t, nil)
	h.srv.testCGClient = &stubCGClient{impact: []string{"AuthHandler (internal/server/auth.go:42)"}}
	h.openEcho()

	prompt := "review internal/server/auth.go"
	h.send("agent.send", map[string]any{"handle": h.handle, "bytes": prompt}, 3)
	_ = h.readResp()

	combined := h.readStreamText(400 * time.Millisecond)
	if !strings.Contains(combined, "[codegraph context: internal/server/auth.go]") {
		t.Fatalf("expected CodeGraph context in stream; got %q", combined)
	}
	if !strings.Contains(combined, prompt) {
		t.Fatalf("expected user prompt in stream; got %q", combined)
	}
}

func TestAgentSend_CodeGraphContextTimeoutDoesNotBlockPrompt(t *testing.T) {
	t.Setenv("MILLIWAYS_CODEGRAPH_TIMEOUT", "10ms")
	h := newAgentMethodsHarness(t, nil)
	h.srv.testCGClient = &stubCGClient{
		impact: []string{"AuthHandler (internal/server/auth.go:42)"},
		delay:  200 * time.Millisecond,
	}
	h.openEcho()

	prompt := "review internal/server/auth.go"
	start := time.Now()
	h.send("agent.send", map[string]any{"handle": h.handle, "bytes": prompt}, 3)
	_ = h.readResp()
	elapsed := time.Since(start)

	combined := h.readStreamText(400 * time.Millisecond)
	if elapsed > 150*time.Millisecond {
		t.Fatalf("agent.send blocked on slow CodeGraph lookup for %s", elapsed)
	}
	if strings.Contains(combined, "[codegraph context:") {
		t.Fatalf("timed-out CodeGraph context should not be injected; got %q", combined)
	}
	if !strings.Contains(combined, prompt) {
		t.Fatalf("expected user prompt in stream despite CodeGraph timeout; got %q", combined)
	}
}

func TestAgentOpen_InvalidatesInjectedHandoff(t *testing.T) {
	mp := &stubMPClient{
		seedSubject: "handoff:_echo",
		seedObject:  "take over with this briefing",
	}
	h := newAgentMethodsHarness(t, mp)
	h.openEcho()

	combined := h.readStreamText(400 * time.Millisecond)
	if !strings.Contains(combined, "take over with this briefing") {
		t.Fatalf("expected handoff briefing in stream; got %q", combined)
	}
	invalidated := mp.invalidatedCopy()
	if len(invalidated) == 0 {
		t.Fatal("expected delivered handoff to be invalidated")
	}
	if !strings.Contains(invalidated[0], "handoff:_echo|takeover_briefing|take over with this briefing") {
		t.Fatalf("unexpected invalidation: %v", invalidated)
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
