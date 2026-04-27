package daemon

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/mwigge/milliways/internal/daemon/runners"
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

	mu     sync.Mutex
	input  chan []byte
	closed atomic.Bool
}

// AgentRegistry holds all open sessions for the lifetime of the daemon.
type AgentRegistry struct {
	mu       sync.Mutex
	next     atomic.Int64
	sessions map[AgentHandle]*AgentSession
	server   *Server // back-reference for stream allocation
}

// NewAgentRegistry returns an empty registry.
func NewAgentRegistry(s *Server) *AgentRegistry {
	return &AgentRegistry{
		sessions: make(map[AgentHandle]*AgentSession),
		server:   s,
	}
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

	switch agentID {
	case "_echo":
		go runEcho(sess)
	case "claude":
		go runClaude(sess)
	case "codex":
		go runCodex(sess)
	case "copilot":
		go runCopilot(sess)
	case "minimax":
		go runMiniMax(sess)
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
// the registry closes the input channel.
func runClaude(sess *AgentSession) {
	stream := waitForStream(sess)
	if stream == nil {
		return
	}
	runners.RunClaude(context.Background(), sess.input, stream)
	stream.Close()
	slog.Debug("claude session ended", "handle", sess.Handle)
}

// runCodex waits for the sidecar to attach, then hands the session's
// input channel + stream to runners.RunCodex. Each agent.send call
// triggers one `codex exec --json` subprocess; the session stays open
// across sends and ends only when the registry closes the input channel.
func runCodex(sess *AgentSession) {
	stream := waitForStream(sess)
	if stream == nil {
		return
	}
	runners.RunCodex(context.Background(), sess.input, stream)
	stream.Close()
	slog.Debug("codex session ended", "handle", sess.Handle)
}

// runCopilot waits for the sidecar to attach, then hands the session's
// input channel + stream to runners.RunCopilot. Each agent.send call
// triggers one `copilot -p <prompt>` subprocess; the session stays open
// across sends and ends only when the registry closes the input channel.
func runCopilot(sess *AgentSession) {
	stream := waitForStream(sess)
	if stream == nil {
		return
	}
	runners.RunCopilot(context.Background(), sess.input, stream)
	stream.Close()
	slog.Debug("copilot session ended", "handle", sess.Handle)
}

// runMiniMax waits for the sidecar to attach, then hands the session's
// input channel + stream to runners.RunMiniMax. Each agent.send call
// triggers one MiniMax chat completion HTTP request (stream:true). The
// session stays open across sends and ends only when the registry closes
// the input channel.
func runMiniMax(sess *AgentSession) {
	stream := waitForStream(sess)
	if stream == nil {
		return
	}
	runners.RunMiniMax(context.Background(), sess.input, stream)
	stream.Close()
	slog.Debug("minimax session ended", "handle", sess.Handle)
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
	}
	return s.stream
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
	}
	if sess.stream != nil {
		sess.stream.Push(map[string]any{"t": "end"})
		sess.stream.Close()
	}
	slog.Debug("echo session ended", "handle", sess.Handle)
}
