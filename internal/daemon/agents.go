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
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mwigge/milliways/internal/daemon/observability"
	"github.com/mwigge/milliways/internal/daemon/runners"
	"github.com/mwigge/milliways/internal/history"
)

// Agent session lifecycle (per agent-domain/spec.md):
//
//   1. agent.open({agent_id})     -> {handle, pty_size}
//   2. agent.stream({handle})     -> {stream_id, output_offset}
//      Client opens sidecar, daemon pumps runner output.
//   3. agent.send({handle, bytes}) -> ok
//      Bytes go to the runner's input channel.
//   4. agent.close({handle})      -> ok
//
// agent_ids prefixed with `_` are reserved internal demo/test agents.
// Real runner ids (claude, codex, minimax, copilot) currently return
// agent_not_implemented pending TASK-1.4 (full runner lift).

// AgentHandle is the daemon-allocated identifier for an open session.
type AgentHandle int64

// AgentSession is one open agent. Bytes arrive on input via agent.send;
// runner-emitted bytes are pushed to stream via Stream.Push().
type AgentSession struct {
	Handle            AgentHandle
	AgentID           string
	SessionID         string
	SecurityWorkspace string
	stream            *Stream // first subscriber stream; nil until agent.stream is called
	streams           map[int64]*Stream
	server            *Server

	mu              sync.Mutex
	ctx             context.Context
	cancel          context.CancelFunc
	input           chan []byte
	closed          atomic.Bool
	streamReady     chan struct{} // closed when the first stream is attached
	streamReadyOnce sync.Once

	// responseBuf is a rolling buffer of the runner's most recent
	// emitted text (decoded from `{"t":"data","b64":...}` events).
	// Capped at responseBufCap; oldest bytes are dropped on overflow.
	// Feeds the apply.extract RPC.
	respMu      sync.Mutex
	responseBuf []byte

	// firstSendDone is 0 until the first agent.send runs context injection,
	// then atomically set to 1 so subsequent sends skip injection.
	firstSendDone atomic.Uint32

	// onTurnComplete is called once per completed turn (when an "end" event
	// arrives from the runner). The argument is the final response text.
	// nil means no hook is installed.
	onTurnComplete func(finalText string)

	stateMu        sync.Mutex
	status         string
	promptCount    int
	turnCount      int
	inputTokens    int
	outputTokens   int
	costUSD        float64
	currentTrace   string
	currentTurnID  string
	turnSpanClosed bool
	lastTrace      string
	startedAt      time.Time
	firstTokenAt   time.Time
	latencyMS      float64
	ttftMS         float64
	tokenRate      float64
	errorCount     int
	model          string
	modelSource    string
	lastThinking   string
	lastError      string
	lastPrompt     string
	lastUpdated    time.Time
	buffer         []DeckBlock
}

// responseBufCap is the per-session response-buffer capacity. 64 KiB is
// enough for several screens of typical agent output and keeps memory
// bounded even with many concurrent sessions.
const responseBufCap = 64 * 1024

const deckBufferCap = 80

// DeckBlock is one prompt/status/output block retained for deck and central
// panel rendering clients.
type DeckBlock struct {
	Kind string    `json:"kind"`
	Text string    `json:"text"`
	At   time.Time `json:"at"`
}

// DeckSessionSnapshot is the daemon-backed status for one open agent session.
type DeckSessionSnapshot struct {
	AgentID           string      `json:"agent_id"`
	Handle            int64       `json:"handle"`
	SessionID         string      `json:"session_id,omitempty"`
	SecurityWorkspace string      `json:"security_workspace,omitempty"`
	Status            string      `json:"status"`
	PromptCount       int         `json:"prompt_count"`
	TurnCount         int         `json:"turn_count"`
	InputTokens       int         `json:"input_tokens"`
	OutputTokens      int         `json:"output_tokens"`
	TotalTokens       int         `json:"total_tokens"`
	CostUSD           float64     `json:"cost_usd"`
	CurrentTrace      string      `json:"current_trace,omitempty"`
	CurrentTurnID     string      `json:"current_turn_id,omitempty"`
	LastTrace         string      `json:"last_trace,omitempty"`
	LatencyMS         float64     `json:"latency_ms,omitempty"`
	TTFTMS            float64     `json:"ttft_ms,omitempty"`
	TokenRate         float64     `json:"token_rate,omitempty"`
	ErrorCount        int         `json:"error_count,omitempty"`
	QueueDepth        int         `json:"queue_depth,omitempty"`
	Model             string      `json:"model,omitempty"`
	ModelSource       string      `json:"model_source,omitempty"`
	LastThinking      string      `json:"last_thinking,omitempty"`
	LastError         string      `json:"last_error,omitempty"`
	LastPrompt        string      `json:"last_prompt,omitempty"`
	LastUpdated       time.Time   `json:"last_updated,omitempty"`
	Buffer            []DeckBlock `json:"buffer,omitempty"`
}

// DeckSnapshot is the daemon-backed deck state returned by deck.snapshot.
type DeckSnapshot struct {
	Active   string                `json:"active"`
	Sessions []DeckSessionSnapshot `json:"sessions"`
}

// recordingPusher wraps a Stream so any `{"t":"data","b64":...}` event
// pushed by a runner is also captured (decoded) into the session's
// rolling response buffer. This is the hook the apply.extract method
// reads.
type recordingPusher struct {
	stream *Stream
	sess   *AgentSession
}

func (p *recordingPusher) Push(event any) {
	if p.sess == nil {
		if p.stream != nil {
			p.stream.Push(event)
		}
		return
	}
	p.sess.pushEvent(event)
	m, ok := event.(map[string]any)
	if !ok {
		return
	}
	t, _ := m["t"].(string)
	switch t {
	case "model":
		model, _ := m["model"].(string)
		source, _ := m["source"].(string)
		p.sess.recordModel(model, source)
	case "thinking":
		b64, _ := m["b64"].(string)
		if b64 == "" {
			return
		}
		bs, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			return
		}
		p.sess.recordThinking(string(bs))
	case "data":
		b64, _ := m["b64"].(string)
		if b64 == "" {
			return
		}
		bs, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			return
		}
		p.sess.recordResponse(bs)
		p.sess.recordData(string(bs))
		p.sess.appendHistory(map[string]any{"t": "data", "text": string(bs)})
	case "chunk_end", "end", "err":
		payload := make(map[string]any)
		payload["t"] = t
		if v, ok := m["cost_usd"]; ok {
			payload["cost_usd"] = v
		}
		if v, ok := m["input_tokens"]; ok {
			payload["input_tokens"] = v
		}
		if v, ok := m["output_tokens"]; ok {
			payload["output_tokens"] = v
		}
		if v, ok := m["total_tokens"]; ok {
			payload["total_tokens"] = v
		}
		if v, ok := m["msg"]; ok {
			payload["msg"] = v
		}
		switch t {
		case "chunk_end":
			p.sess.recordChunkEnd(intFromEvent(m["input_tokens"]), intFromEvent(m["output_tokens"]), floatFromEvent(m["cost_usd"]))
		case "err":
			msg, _ := m["msg"].(string)
			p.sess.recordError(msg)
		case "end":
			p.sess.recordIdle()
		}
		p.sess.appendHistory(payload)
		// Fire onTurnComplete once per completed turn when the runner signals "end".
		if t == "end" {
			if hook := p.sess.onTurnComplete; hook != nil {
				finalText := p.sess.snapshotResponse()
				go hook(finalText)
			}
		}
	default:
		// ignore others
	}
}

func intFromEvent(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}

func floatFromEvent(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	default:
		return 0
	}
}

// AgentRegistry holds all open sessions for the lifetime of the daemon.
type AgentRegistry struct {
	mu       sync.Mutex
	next     atomic.Int64
	sessions map[AgentHandle]*AgentSession
	server   *Server // back-reference for stream allocation

	// metrics is the observer the runners push tokens_in / tokens_out /
	// cost_usd / error_count into. Resolved lazily from server.metrics
	// at session-start time so callers that construct the registry
	// before the metrics store is opened still see the observer once it
	// becomes available. May be nil in tests; runners tolerate nil.
	metrics runners.MetricsObserver
}

// NewAgentRegistry returns an empty registry. The metrics observer is
// pulled from s (s.metrics implements runners.MetricsObserver) so per-
// runner observations land in the daemon's five-tier rollup.
func NewAgentRegistry(s *Server) *AgentRegistry {
	r := &AgentRegistry{
		sessions: make(map[AgentHandle]*AgentSession),
		server:   s,
	}
	if s != nil && s.metrics != nil {
		r.metrics = s.metrics
	}
	return r
}

// metricsObserver returns the registry's MetricsObserver, falling back
// to the server's current metrics store. The fallback handles the
// NewAgentRegistry-runs-before-NewServer-finishes-wiring case in
// server.go (the registry is created before the metrics store is
// opened in NewServer).
func (r *AgentRegistry) metricsObserver() runners.MetricsObserver {
	if r == nil {
		return nil
	}
	if r.metrics != nil {
		return r.metrics
	}
	if r.server != nil && r.server.metrics != nil {
		return r.server.metrics
	}
	return nil
}

// Open allocates a new session for agent_id. For reserved/known ids the
// session goroutine is started; for unknown ids returns an error.
func (r *AgentRegistry) Open(agentID string) (*AgentSession, error) {
	handle := AgentHandle(r.next.Add(1))
	ctx, cancel := context.WithCancel(context.Background())
	sess := &AgentSession{
		Handle:      handle,
		AgentID:     agentID,
		SessionID:   fmt.Sprintf("%s-%d", agentID, handle),
		server:      r.server,
		ctx:         ctx,
		cancel:      cancel,
		input:       make(chan []byte, 16),
		streamReady: make(chan struct{}),
		streams:     make(map[int64]*Stream),
		status:      "idle",
		// firstSendDone starts at zero (atomic.Uint32 zero value) — first send
		// will CAS it to 1 and perform context injection exactly once.
	}
	r.mu.Lock()
	r.sessions[handle] = sess
	r.mu.Unlock()

	mo := r.metricsObserver()
	switch agentID {
	case "_echo":
		go runEcho(sess)
	case "claude":
		go runClaude(sess, mo)
	case "codex":
		go runCodex(sess, mo)
	case "copilot":
		go runCopilot(sess, mo)
	case "minimax":
		go runMiniMax(sess, mo)
	case "local":
		go runLocal(sess, mo)
	case "gemini":
		go runGemini(sess, mo)
	case "pool":
		go runPool(sess, mo)
	default:
		// Unknown / not yet lifted.
		cancel()
		r.mu.Lock()
		delete(r.sessions, handle)
		r.mu.Unlock()
		return nil, fmt.Errorf("agent_not_implemented: %s", agentID)
	}
	return sess, nil
}

// runClaude waits for the sidecar to attach, then hands the session's
// input channel + stream to runners.RunClaude. Each agent.send call
// triggers one `claude --print --output-format stream-json --verbose`
// subprocess; the session stays open across sends and ends only when
// the registry closes the input channel. `metrics` may be nil; runners
// tolerate that and skip observation.
func runClaude(sess *AgentSession, metrics runners.MetricsObserver) {
	stream := waitForStream(sess)
	if stream == nil {
		return
	}
	runners.RunClaude(sess.ctx, sess.input, &recordingPusher{stream: stream, sess: sess}, metrics)
	sess.closeStreams()
	slog.Debug("claude session ended", "handle", sess.Handle)
}

// runCodex waits for the sidecar to attach, then hands the session's
// input channel + stream to runners.RunCodex. Each agent.send call
// triggers one `codex exec --json` subprocess; the session stays open
// across sends and ends only when the registry closes the input channel.
// `metrics` may be nil.
func runCodex(sess *AgentSession, metrics runners.MetricsObserver) {
	stream := waitForStream(sess)
	if stream == nil {
		return
	}
	runners.RunCodex(sess.ctx, sess.input, &recordingPusher{stream: stream, sess: sess}, metrics)
	sess.closeStreams()
	slog.Debug("codex session ended", "handle", sess.Handle)
}

// runCopilot waits for the sidecar to attach, then hands the session's
// input channel + stream to runners.RunCopilot. Each agent.send call
// triggers one `copilot -p <prompt>` subprocess; the session stays open
// across sends and ends only when the registry closes the input channel.
// `metrics` may be nil.
func runCopilot(sess *AgentSession, metrics runners.MetricsObserver) {
	stream := waitForStream(sess)
	if stream == nil {
		return
	}
	runners.RunCopilot(sess.ctx, sess.input, &recordingPusher{stream: stream, sess: sess}, metrics)
	sess.closeStreams()
	slog.Debug("copilot session ended", "handle", sess.Handle)
}

// runMiniMax waits for the sidecar to attach, then hands the session's
// input channel + stream to runners.RunMiniMax. Each agent.send call
// triggers one MiniMax chat completion HTTP request (stream:true). The
// session stays open across sends and ends only when the registry closes
// the input channel. `metrics` may be nil.
func runMiniMax(sess *AgentSession, metrics runners.MetricsObserver) {
	stream := waitForStream(sess)
	if stream == nil {
		return
	}
	runners.RunMiniMax(sess.ctx, sess.input, &recordingPusher{stream: stream, sess: sess}, metrics)
	sess.closeStreams()
	slog.Debug("minimax session ended", "handle", sess.Handle)
}

func runLocal(sess *AgentSession, metrics runners.MetricsObserver) {
	stream := waitForStream(sess)
	if stream == nil {
		return
	}
	runners.RunLocal(sess.ctx, sess.input, &recordingPusher{stream: stream, sess: sess}, metrics)
	sess.closeStreams()
	slog.Debug("local session ended", "handle", sess.Handle)
}

// runGemini waits for the sidecar to attach, then hands the session's
// input channel + stream to runners.RunGemini. Each agent.send call
// triggers one `gemini -p <prompt> -y` subprocess; the session stays open
// across sends and ends only when the registry closes the input channel.
// `metrics` may be nil.
func runGemini(sess *AgentSession, metrics runners.MetricsObserver) {
	stream := waitForStream(sess)
	if stream == nil {
		return
	}
	runners.RunGemini(sess.ctx, sess.input, &recordingPusher{stream: stream, sess: sess}, metrics)
	sess.closeStreams()
	slog.Debug("gemini session ended", "handle", sess.Handle)
}

// runPool waits for the sidecar to attach, then hands the session's
// input channel + stream to runners.RunPool. Each agent.send call
// triggers one `pool exec -p <prompt> --unsafe-auto-allow` subprocess;
// the session stays open across sends and ends only when the registry
// closes the input channel. `metrics` may be nil.
func runPool(sess *AgentSession, metrics runners.MetricsObserver) {
	stream := waitForStream(sess)
	if stream == nil {
		return
	}
	runners.RunPool(sess.ctx, sess.input, &recordingPusher{stream: stream, sess: sess}, metrics)
	sess.closeStreams()
	slog.Debug("pool session ended", "handle", sess.Handle)
}

// waitForStream blocks until a stream is attached (AttachStream closes
// streamReady) or the session is closed. Returns nil when the session
// closes before a stream attaches. Replaces the previous busy-wait loop.
func waitForStream(sess *AgentSession) *Stream {
	select {
	case <-sess.streamReady:
		sess.mu.Lock()
		s := sess.stream
		sess.mu.Unlock()
		return s
	case <-sess.ctx.Done():
		return nil
	case <-closedWhenDone(sess):
		return nil
	}
}

// closedWhenDone returns a channel that is readable once sess.closed is true.
// We cannot block directly on an atomic.Bool, so we poll with a small ticker
// only as a fallback — the common path exits via streamReady.
func closedWhenDone(sess *AgentSession) <-chan struct{} {
	ch := make(chan struct{})
	go func() {
		for !sess.closed.Load() {
			// 1 ms sleep is negligible; this path only fires when the
			// session closes before the client ever calls agent.stream.
			select {
			case <-sess.streamReady:
				// Stream arrived — the select in waitForStream will
				// handle it; nothing to do here.
				return
			default:
			}
			// Brief yield so we don't spin at 100% in the rare gap.
			select {
			case <-time.After(time.Millisecond):
			}
		}
		close(ch)
	}()
	return ch
}

// Get returns the session for handle.
func (r *AgentRegistry) Get(handle AgentHandle) (*AgentSession, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.sessions[handle]
	return s, ok
}

// Close terminates the session and removes it from the registry.
func (r *AgentRegistry) Close(handle AgentHandle) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if s, ok := r.sessions[handle]; ok {
		if s.closed.CompareAndSwap(false, true) {
			s.cancel()
			close(s.input)
			s.closeStreams()
		}
		delete(r.sessions, handle)
	}
}

// AttachStream allocates a fresh subscriber stream for the session. The first
// subscriber unblocks the runner; later subscribers receive the same future
// events via session fanout.
func (s *AgentSession) AttachStream(srv *Server) *Stream {
	stream := srv.streams.Allocate()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stream == nil {
		s.stream = stream
		s.server = srv
		s.streamReadyOnce.Do(func() { close(s.streamReady) })
	}
	if s.streams == nil {
		s.streams = make(map[int64]*Stream)
	}
	s.streams[stream.ID] = stream
	return stream
}

func (s *AgentSession) pushEvent(event any) {
	s.mu.Lock()
	streams := make([]*Stream, 0, len(s.streams))
	for _, stream := range s.streams {
		streams = append(streams, stream)
	}
	s.mu.Unlock()
	for _, stream := range streams {
		stream.Push(event)
	}
}

func (s *AgentSession) closeStreams() {
	s.mu.Lock()
	streams := make([]*Stream, 0, len(s.streams))
	for _, stream := range s.streams {
		streams = append(streams, stream)
	}
	s.streams = make(map[int64]*Stream)
	s.stream = nil
	s.mu.Unlock()
	for _, stream := range streams {
		stream.Close()
	}
}

// recordResponse appends the runner's emitted bytes to the session's
// rolling response buffer. Anything beyond responseBufCap is dropped
// from the front (oldest first).
func (s *AgentSession) recordResponse(b []byte) {
	if len(b) == 0 {
		return
	}
	s.respMu.Lock()
	defer s.respMu.Unlock()
	s.responseBuf = append(s.responseBuf, b...)
	if len(s.responseBuf) > responseBufCap {
		s.responseBuf = s.responseBuf[len(s.responseBuf)-responseBufCap:]
	}
}

func (s *AgentSession) recordPrompt(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	now := time.Now()
	s.status = "thinking"
	s.promptCount++
	s.currentTrace = observability.NewTraceID()
	s.currentTurnID = fmt.Sprintf("%s-%d", s.SessionID, s.promptCount)
	s.turnSpanClosed = false
	s.startedAt = now
	s.firstTokenAt = time.Time{}
	s.latencyMS = 0
	s.ttftMS = 0
	s.tokenRate = 0
	s.lastPrompt = text
	s.lastError = ""
	s.lastUpdated = now
	s.appendDeckBlockLocked("prompt", text)
}

func (s *AgentSession) appendHistory(payload map[string]any) {
	if s == nil || s.server == nil || s.server.socket == "" || payload == nil {
		return
	}
	payload = sanitizeHistoryPayload(payload)
	s.stateMu.Lock()
	payload["agent_id"] = s.AgentID
	payload["session_id"] = s.SessionID
	payload["turn_id"] = s.currentTurnID
	payload["trace_id"] = s.currentTrace
	payload["model"] = s.model
	payload["model_source"] = s.modelSource
	s.stateMu.Unlock()

	stateDir := filepath.Dir(s.server.socket)
	go func(agent string, dir string, pl any) {
		if err := history.AppendAgentHistory(dir, agent, pl, history.DefaultMaxLines); err != nil {
			slog.Debug("append history", "err", err, "agent", agent)
		}
	}(s.AgentID, stateDir, payload)
}

func sanitizeHistoryPayload(payload map[string]any) map[string]any {
	out := make(map[string]any, len(payload)+4)
	for k, v := range payload {
		out[k] = v
	}
	if historyRawContentEnabled() {
		return out
	}
	eventType, _ := out["t"].(string)
	switch eventType {
	case "prompt", "context":
		text, ok := out["text"].(string)
		if !ok || text == "" {
			return out
		}
		sum := sha256.Sum256([]byte(text))
		delete(out, "text")
		out["text_redacted"] = true
		out["text_bytes"] = len(text)
		out["text_sha256"] = fmt.Sprintf("%x", sum[:])
	}
	return out
}

func historyRawContentEnabled() bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv("MILLIWAYS_HISTORY_RAW_CONTENT")))
	return value == "1" || value == "true" || value == "yes" || value == "on"
}

func (s *AgentSession) pushTurnSpanLocked(status, errMsg string, inputTokens, outputTokens int, costUSD float64) {
	if s == nil || s.server == nil || s.currentTrace == "" || s.turnSpanClosed {
		return
	}
	s.turnSpanClosed = true
	durationMS := 0.0
	if !s.startedAt.IsZero() {
		durationMS = float64(time.Since(s.startedAt).Microseconds()) / 1000.0
	}
	attrs := map[string]any{
		"agent_id":      s.AgentID,
		"session_id":    s.SessionID,
		"turn_id":       s.currentTurnID,
		"input_tokens":  inputTokens,
		"output_tokens": outputTokens,
		"total_tokens":  inputTokens + outputTokens,
		"cost_usd":      costUSD,
	}
	if s.model != "" {
		attrs["model"] = s.model
	}
	if errMsg != "" {
		attrs["error"] = errMsg
	}
	s.server.spans.Push(observability.Span{
		TraceID:    s.currentTrace,
		SpanID:     observability.NewSpanID(),
		Name:       "agent.turn",
		StartTS:    s.startedAt,
		DurationMS: durationMS,
		Status:     status,
		Attributes: attrs,
	})
}

func (s *AgentSession) recordThinking(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	s.status = "thinking"
	s.lastThinking = text
	s.lastUpdated = time.Now()
	s.appendDeckBlockLocked("thinking", text)
}

func (s *AgentSession) recordData(text string) {
	if text == "" {
		return
	}
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	now := time.Now()
	if s.firstTokenAt.IsZero() {
		s.firstTokenAt = now
		if !s.startedAt.IsZero() {
			s.ttftMS = float64(now.Sub(s.startedAt).Microseconds()) / 1000.0
		}
	}
	s.status = "streaming"
	s.lastUpdated = now
	s.appendDeckBlockLocked("response", text)
}

func (s *AgentSession) recordChunkEnd(inputTokens, outputTokens int, costUSD float64) {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	now := time.Now()
	s.status = "idle"
	s.inputTokens += inputTokens
	s.outputTokens += outputTokens
	s.costUSD += costUSD
	s.turnCount++
	if !s.startedAt.IsZero() {
		duration := now.Sub(s.startedAt)
		s.latencyMS = float64(duration.Microseconds()) / 1000.0
		if outputTokens > 0 && duration > 0 {
			s.tokenRate = float64(outputTokens) / duration.Seconds()
		}
	}
	if s.currentTrace != "" {
		s.lastTrace = s.currentTrace
	}
	s.lastUpdated = now
	s.pushTurnSpanLocked("ok", "", inputTokens, outputTokens, costUSD)
}

func (s *AgentSession) recordError(msg string) {
	msg = strings.TrimSpace(msg)
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	s.status = "error"
	s.errorCount++
	s.lastError = msg
	s.lastUpdated = time.Now()
	s.appendDeckBlockLocked("error", msg)
	s.pushTurnSpanLocked("error", msg, 0, 0, 0)
}

func (s *AgentSession) recordIdle() {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if s.status != "error" {
		s.status = "idle"
	}
	s.lastUpdated = time.Now()
}

func (s *AgentSession) appendDeckBlockLocked(kind, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	s.buffer = append(s.buffer, DeckBlock{Kind: kind, Text: text, At: time.Now()})
	if over := len(s.buffer) - deckBufferCap; over > 0 {
		s.buffer = s.buffer[over:]
	}
}

func (s *AgentSession) recordModel(model, source string) {
	model = strings.TrimSpace(model)
	source = strings.TrimSpace(source)
	if model == "" {
		return
	}
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	s.model = model
	s.modelSource = source
	s.lastUpdated = time.Now()
}

func (s *AgentSession) deckSnapshot() DeckSessionSnapshot {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	status := s.status
	if status == "" {
		status = "idle"
	}
	buffer := append([]DeckBlock(nil), s.buffer...)
	return DeckSessionSnapshot{
		AgentID:           s.AgentID,
		Handle:            int64(s.Handle),
		SessionID:         s.SessionID,
		SecurityWorkspace: s.SecurityWorkspace,
		Status:            status,
		PromptCount:       s.promptCount,
		TurnCount:         s.turnCount,
		InputTokens:       s.inputTokens,
		OutputTokens:      s.outputTokens,
		TotalTokens:       s.inputTokens + s.outputTokens,
		CostUSD:           s.costUSD,
		CurrentTrace:      s.currentTrace,
		CurrentTurnID:     s.currentTurnID,
		LastTrace:         s.lastTrace,
		LatencyMS:         s.latencyMS,
		TTFTMS:            s.ttftMS,
		TokenRate:         s.tokenRate,
		ErrorCount:        s.errorCount,
		QueueDepth:        len(s.input),
		Model:             s.model,
		ModelSource:       s.modelSource,
		LastThinking:      s.lastThinking,
		LastError:         s.lastError,
		LastPrompt:        s.lastPrompt,
		LastUpdated:       s.lastUpdated,
		Buffer:            buffer,
	}
}

// snapshotResponse returns a copy of the rolling response buffer.
func (s *AgentSession) snapshotResponse() string {
	s.respMu.Lock()
	defer s.respMu.Unlock()
	return string(s.responseBuf)
}

func (r *AgentRegistry) DeckSnapshot(active string) DeckSnapshot {
	r.mu.Lock()
	sessions := make([]*AgentSession, 0, len(r.sessions))
	for _, sess := range r.sessions {
		sessions = append(sessions, sess)
	}
	r.mu.Unlock()

	out := DeckSnapshot{Active: active}
	for _, sess := range sessions {
		out.Sessions = append(out.Sessions, sess.deckSnapshot())
	}
	return out
}

func (r *AgentRegistry) SessionModels() map[string]string {
	r.mu.Lock()
	sessions := make([]*AgentSession, 0, len(r.sessions))
	for _, sess := range r.sessions {
		sessions = append(sessions, sess)
	}
	r.mu.Unlock()

	out := make(map[string]string)
	for _, sess := range sessions {
		snap := sess.deckSnapshot()
		if snap.Model != "" {
			out[snap.AgentID] = snap.Model
		}
	}
	return out
}

// Send writes bytes to the session's input channel. Returns an error if
// the session is closed.
func (s *AgentSession) Send(bytes []byte) error {
	if s.closed.Load() {
		return fmt.Errorf("session closed")
	}
	// Make a copy so callers can reuse the slice.
	cp := make([]byte, len(bytes))
	copy(cp, bytes)
	select {
	case s.input <- cp:
		return nil
	default:
		return fmt.Errorf("agent input buffer full")
	}
}

// runEcho is the `_echo` reserved agent's run loop. Every batch of input
// bytes is wrapped in `{"t":"data","b64":...}` and pushed back via the
// stream — verifies the agent.open/send/stream/close pattern end-to-end
// without needing a real runner subprocess.
//
// Events are routed through recordingPusher so that onTurnComplete and
// response-buffer recording work identically to real runner sessions.
func runEcho(sess *AgentSession) {
	stream := waitForStream(sess)
	if stream == nil {
		return
	}
	pusher := &recordingPusher{stream: stream, sess: sess}
	for bytes := range sess.input {
		pusher.Push(map[string]any{
			"t":   "data",
			"b64": base64.StdEncoding.EncodeToString(bytes),
		})
	}
	pusher.Push(map[string]any{"t": "end"})
	sess.closeStreams()
	slog.Debug("echo session ended", "handle", sess.Handle)
}
