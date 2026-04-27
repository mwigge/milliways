package daemon

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mwigge/milliways/internal/daemon/observability"
	"github.com/mwigge/milliways/internal/daemon/runners"
)

// Protocol version exposed via ping. Bump major when breaking; minor for
// forward-compatible additions.
const (
	ProtoMajor = 0
	ProtoMinor = 1
)

// Server is the milliwaysd JSON-RPC 2.0 server. Newline-delimited framing.
// One goroutine per connection. Sidecar connections (for streaming) are
// detected by a `STREAM <id> <offset>` first line.
type Server struct {
	socket   string
	listener net.Listener
	closing  atomic.Bool
	wg       sync.WaitGroup

	streams *StreamRegistry

	// Background broadcaster lifecycle.
	bgCtx    context.Context
	bgCancel context.CancelFunc

	// Status broadcaster.
	statusMu          sync.Mutex
	statusSubscribers map[int64]*Stream

	// Cached runner probes — populated at startup, served by agent.list.
	agentsCache []AgentInfo

	// In-memory span ring — populated by every dispatch, served by
	// observability.spans.
	spans *observability.Ring

	// Agent session registry — populated by agent.open, drained by
	// agent.send / agent.stream / agent.close.
	agents *AgentRegistry
}

// NewServer binds a UDS at socket with mode 0600. Removes any stale socket
// from a previous unclean exit. Starts the status broadcaster.
func NewServer(socket string) (*Server, error) {
	_ = os.Remove(socket)
	l, err := net.Listen("unix", socket)
	if err != nil {
		return nil, fmt.Errorf("listen %s: %w", socket, err)
	}
	if err := os.Chmod(socket, 0o600); err != nil {
		l.Close()
		return nil, fmt.Errorf("chmod %s: %w", socket, err)
	}
	bgCtx, bgCancel := context.WithCancel(context.Background())
	s := &Server{
		socket:            socket,
		listener:          l,
		streams:           NewStreamRegistry(),
		bgCtx:             bgCtx,
		bgCancel:          bgCancel,
		statusSubscribers: make(map[int64]*Stream),
		spans:             observability.NewRing(1000),
	}
	s.agents = NewAgentRegistry(s)
	// Probe runners once at startup and cache the result. This populates
	// agent.list's auth_status without per-call subprocess churn.
	probeCtx, probeCancel := context.WithTimeout(bgCtx, 10*time.Second)
	defer probeCancel()
	for _, info := range runners.Probe(probeCtx) {
		s.agentsCache = append(s.agentsCache, AgentInfo{
			ID:         info.ID,
			Available:  info.Available,
			AuthStatus: info.AuthStatus,
			Model:      info.Model,
		})
	}
	slog.Info("runners probed", "n", len(s.agentsCache))

	go s.statusBroadcaster()
	return s, nil
}

// Serve accepts connections until Shutdown is called.
func (s *Server) Serve() error {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			if s.closing.Load() {
				return nil
			}
			return err
		}
		s.wg.Add(1)
		go s.handle(conn)
	}
}

// handle reads from conn until close. The first line determines the
// connection's role:
//   - JSON object (starts with `{`) → JSON-RPC request loop.
//   - `STREAM <id> <offset>` → sidecar attach for an existing stream.
func (s *Server) handle(conn net.Conn) {
	defer s.wg.Done()
	defer conn.Close()

	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	enc := json.NewEncoder(conn)

	if !scanner.Scan() {
		return
	}
	first := scanner.Bytes()

	if bytes.HasPrefix(first, []byte("STREAM ")) {
		s.handleSidecar(conn, first)
		return
	}

	// JSON-RPC: process the first line, then loop for any subsequent ones.
	s.processLine(enc, first)
	for scanner.Scan() {
		s.processLine(enc, scanner.Bytes())
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		slog.Debug("scan err", "err", err)
	}
}

func (s *Server) processLine(enc *json.Encoder, line []byte) {
	var req Request
	if err := json.Unmarshal(line, &req); err != nil {
		slog.Debug("malformed jsonrpc envelope", "err", err)
		writeError(enc, nil, ErrInvalidParams, "invalid JSON-RPC envelope")
		return
	}
	s.dispatch(enc, &req)
}

// handleSidecar parses the STREAM preamble, attaches the connection to the
// referenced stream, and blocks reading the conn until either side closes.
func (s *Server) handleSidecar(conn net.Conn, preamble []byte) {
	var streamID, lastOffset int64
	if _, err := fmt.Sscanf(string(preamble), "STREAM %d %d", &streamID, &lastOffset); err != nil {
		conn.Write([]byte(fmt.Sprintf(
			`{"t":"err","code":%d,"msg":"invalid stream attach line"}`+"\n",
			ErrInvalidParams)))
		return
	}
	stream, ok := s.streams.Get(streamID)
	if !ok {
		conn.Write([]byte(fmt.Sprintf(
			`{"t":"err","code":%d,"msg":"stream_not_found_or_attach_timeout"}`+"\n",
			ErrStreamAttachTimeout)))
		return
	}
	if err := stream.Attach(conn, lastOffset); err != nil {
		slog.Debug("stream attach err", "err", err, "stream_id", streamID)
		return
	}
	slog.Debug("sidecar attached", "stream_id", streamID, "last_offset", lastOffset)
	// Block until the conn closes. We don't expect more bytes from the
	// sidecar — it's server-push-only.
	io.Copy(io.Discard, conn)
}

// statusBroadcaster ticks at 1 Hz and pushes a Status snapshot to every
// active status.subscribe stream. Real broadcasts on actual state changes
// land when TASK-1.4 lifts the session/quota state.
func (s *Server) statusBroadcaster() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.bgCtx.Done():
			return
		case <-ticker.C:
			s.broadcastStatus()
		}
	}
}

func (s *Server) broadcastStatus() {
	s.statusMu.Lock()
	subs := make([]*Stream, 0, len(s.statusSubscribers))
	for _, sub := range s.statusSubscribers {
		subs = append(subs, sub)
	}
	s.statusMu.Unlock()
	if len(subs) == 0 {
		return
	}
	snapshot := s.buildStatus()
	for _, sub := range subs {
		sub.Push(map[string]any{"t": "data", "snapshot": snapshot})
	}
}

func (s *Server) registerStatusSubscriber(stream *Stream) {
	s.statusMu.Lock()
	s.statusSubscribers[stream.ID] = stream
	s.statusMu.Unlock()
}

func (s *Server) unregisterStatusSubscriber(id int64) {
	s.statusMu.Lock()
	delete(s.statusSubscribers, id)
	s.statusMu.Unlock()
}

// Shutdown stops accepting new connections and waits for active handlers to
// drain. Idempotent.
func (s *Server) Shutdown() {
	if !s.closing.CompareAndSwap(false, true) {
		return
	}
	s.bgCancel()
	s.listener.Close()
	s.wg.Wait()
}

// Close shuts the server down and removes the socket file.
func (s *Server) Close() error {
	s.Shutdown()
	return os.Remove(s.socket)
}
