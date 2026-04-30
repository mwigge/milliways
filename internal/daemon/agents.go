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
	"encoding/base64"
	"fmt"
	"log/slog"
	"path/filepath"
	"sync"
	"sync/atomic"

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
	Handle  AgentHandle
	AgentID string
	stream  *Stream // nil until agent.stream is called
	server  *Server

	mu     sync.Mutex
	input  chan []byte
	closed atomic.Bool

	// responseBuf is a rolling buffer of the runner's most recent
	// emitted text (decoded from `{"t":"data","b64":...}` events).
	// Capped at responseBufCap; oldest bytes are dropped on overflow.
	// Feeds the apply.extract RPC.
	respMu      sync.Mutex
	responseBuf []byte
}

// responseBufCap is the per-session response-buffer capacity. 64 KiB is
// enough for several screens of typical agent output and keeps memory
// bounded even with many concurrent sessions.
const responseBufCap = 64 * 1024

// recordingPusher wraps a Stream so any `{"t":"data","b64":...}` event
// pushed by a runner is also captured (decoded) into the session's
// rolling response buffer. This is the hook the apply.extract method
// reads.
type recordingPusher struct {
	stream *Stream
	sess   *AgentSession
}

func (p *recordingPusher) Push(event any) {
	if p.stream != nil {
		p.stream.Push(event)
	}
	if p.sess == nil {
		return
	}
	m, ok := event.(map[string]any)
	if !ok {
		return
	}
	t, _ := m["t"].(string)
	switch t {
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
		// Append to per-agent history asynchronously if we have a server/state dir.
		if p.sess.server != nil {
			stateDir := filepath.Dir(p.sess.server.socket)
			payload := map[string]any{"t": "data", "text": string(bs)}
			go func(agent string, dir string, pl any) {
				if err := history.AppendAgentHistory(dir, agent, pl, history.DefaultMaxLines); err != nil {
					slog.Debug("append history", "err", err, "agent", agent)
				}
			}(p.sess.AgentID, stateDir, payload)
		}
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
		if v, ok := m["msg"]; ok {
			payload["msg"] = v
		}
		if p.sess.server != nil {
			stateDir := filepath.Dir(p.sess.server.socket)
			go func(agent string, dir string, pl any) {
				if err := history.AppendAgentHistory(dir, agent, pl, history.DefaultMaxLines); err != nil {
					slog.Debug("append history", "err", err, "agent", agent)
				}
			}(p.sess.AgentID, stateDir, payload)
		}
	default:
		// ignore others
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
	sess := &AgentSession{
		Handle:  handle,
		AgentID: agentID,
		input:   make(chan []byte, 16),
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
	runners.RunClaude(context.Background(), sess.input, &recordingPusher{stream: stream, sess: sess}, metrics)
	stream.Close()
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
	runners.RunCodex(context.Background(), sess.input, &recordingPusher{stream: stream, sess: sess}, metrics)
	stream.Close()
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
	runners.RunCopilot(context.Background(), sess.input, &recordingPusher{stream: stream, sess: sess}, metrics)
	stream.Close()
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
	runners.RunMiniMax(context.Background(), sess.input, &recordingPusher{stream: stream, sess: sess}, metrics)
	stream.Close()
	slog.Debug("minimax session ended", "handle", sess.Handle)
}

func runLocal(sess *AgentSession, metrics runners.MetricsObserver) {
	stream := waitForStream(sess)
	if stream == nil {
		return
	}
	runners.RunLocal(context.Background(), sess.input, &recordingPusher{stream: stream, sess: sess}, metrics)
	stream.Close()
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
	runners.RunGemini(context.Background(), sess.input, &recordingPusher{stream: stream, sess: sess}, metrics)
	stream.Close()
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
	runners.RunPool(context.Background(), sess.input, &recordingPusher{stream: stream, sess: sess}, metrics)
	stream.Close()
	slog.Debug("pool session ended", "handle", sess.Handle)
}

// waitForStream blocks until sess.stream is non-nil or the session is
// closed. Returns nil if the session closed before a stream attached.
func waitForStream(sess *AgentSession) *Stream {
	for {
		sess.mu.Lock()
		s := sess.stream
		sess.mu.Unlock()
		if s != nil {
			return s
		}
		if sess.closed.Load() {
			return nil
		}
	}
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
			close(s.input)
		}
		delete(r.sessions, handle)
	}
}

// AttachStream allocates a Stream for the session if it does not have one,
// or returns the existing one. Idempotent. The Server must be set on the
// registry before calling.
func (s *AgentSession) AttachStream(srv *Server) *Stream {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stream == nil {
		s.stream = srv.streams.Allocate()
		s.server = srv
	}
	return s.stream
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

// snapshotResponse returns a copy of the rolling response buffer.
func (s *AgentSession) snapshotResponse() string {
	s.respMu.Lock()
	defer s.respMu.Unlock()
	return string(s.responseBuf)
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
func runEcho(sess *AgentSession) {
	for bytes := range sess.input {
		// Wait until the client subscribes (stream allocated).
		var stream *Stream
		for stream == nil {
			sess.mu.Lock()
			stream = sess.stream
			sess.mu.Unlock()
			if stream != nil {
				break
			}
			// busy-wait briefly; in production we'd notify on a chan
			// when stream is attached. Acceptable for the echo demo.
			if sess.closed.Load() {
				return
			}
		}
		stream.Push(map[string]any{
			"t":   "data",
			"b64": base64.StdEncoding.EncodeToString(bytes),
		})
		sess.recordResponse(bytes)
	}
	if sess.stream != nil {
		sess.stream.Push(map[string]any{"t": "end"})
		sess.stream.Close()
	}
	slog.Debug("echo session ended", "handle", sess.Handle)
}
