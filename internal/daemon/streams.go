package daemon

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// Streaming protocol primitives. Per term-daemon-rpc/spec.md (Decisions
// derived from the architect review):
//
//   1. A unary RPC like status.subscribe allocates a stream_id and returns
//      {stream_id, output_offset:0}.
//   2. The client opens a SECOND connection to the same UDS and writes a
//      one-line preamble: `STREAM <id> <last_seen_offset>\n`.
//   3. The daemon attaches the connection to the registered stream,
//      replays any bytes from last_seen_offset, then takes over live emit.
//   4. Reservations expire after 5 seconds if no sidecar attaches.
//   5. Per-stream output ring is bounded; on overflow we discard oldest
//      bytes and emit a warning line on next attach.
//
// One Stream = one subscriber. Concurrent subscribers should each open
// their own status.subscribe (different stream_id).

const (
	defaultRingSize     = 256 * 1024
	streamAttachTimeout = 5 * time.Second
)

// StreamRegistry holds the daemon's active streams. Stream ids are
// monotonic int64s allocated by Allocate(); ids are never reused.
type StreamRegistry struct {
	mu      sync.Mutex
	next    atomic.Int64
	streams map[int64]*Stream
}

// NewStreamRegistry returns an empty registry.
func NewStreamRegistry() *StreamRegistry {
	return &StreamRegistry{streams: make(map[int64]*Stream)}
}

// Allocate registers a new stream and starts the 5s attach timer. The
// caller is responsible for calling Push() and (eventually) Close() on the
// returned Stream.
func (r *StreamRegistry) Allocate() *Stream {
	id := r.next.Add(1)
	s := &Stream{
		ID:       id,
		registry: r,
		ring:     make([]byte, 0, defaultRingSize),
	}
	r.mu.Lock()
	r.streams[id] = s
	r.mu.Unlock()
	s.timer = time.AfterFunc(streamAttachTimeout, func() {
		s.attachOnce.Do(func() {
			slog.Debug("stream attach timeout", "id", id)
			s.timeoutFlag.Store(true)
			r.Remove(id)
			s.markClosed()
		})
	})
	return s
}

// Get returns the Stream registered under id, or false if absent or expired.
func (r *StreamRegistry) Get(id int64) (*Stream, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.streams[id]
	return s, ok
}

// Remove unregisters the stream. Idempotent.
func (r *StreamRegistry) Remove(id int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.streams, id)
}

// Stream is a single subscriber channel. Events go in via Push (any
// JSON-encodable value) and out via the attached sidecar connection.
// Without a sidecar attached, events accumulate in `ring` (bounded);
// overflow drops oldest bytes and surfaces as a replay_truncated warning
// on next attach.
type Stream struct {
	ID       int64
	registry *StreamRegistry

	mu     sync.Mutex
	conn   net.Conn // active sidecar or nil
	ring   []byte
	offset int64 // total bytes emitted since stream creation
	closed bool

	attachOnce  sync.Once
	timer       *time.Timer
	timeoutFlag atomic.Bool
}

// Attach binds a sidecar connection. Replays any bytes in the ring from
// lastSeenOffset, emits a replay_truncated warning if the offset is older
// than the ring's window, then takes over live emission. Returns an error
// if the stream is already attached or the reservation has expired.
func (s *Stream) Attach(conn net.Conn, lastSeenOffset int64) error {
	already := true
	s.attachOnce.Do(func() { already = false })
	if already {
		if s.timeoutFlag.Load() {
			return fmt.Errorf("stream %d attach timeout (reservation expired)", s.ID)
		}
		return fmt.Errorf("stream %d already attached", s.ID)
	}
	if s.timer != nil {
		s.timer.Stop()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return fmt.Errorf("stream %d already closed", s.ID)
	}
	s.conn = conn

	// Replay missed bytes.
	dropped := s.offset - int64(len(s.ring))
	if lastSeenOffset < s.offset {
		if lastSeenOffset < dropped {
			truncatedBytes := dropped - lastSeenOffset
			warn := fmt.Sprintf(
				`{"t":"warn","code":%d,"msg":"replay_truncated","dropped_bytes":%d}`+"\n",
				ErrReplayTruncated, truncatedBytes,
			)
			conn.Write([]byte(warn))
			lastSeenOffset = dropped
		}
		start := int(lastSeenOffset - dropped)
		if start < len(s.ring) {
			conn.Write(s.ring[start:])
		}
	}
	return nil
}

// Push emits an NDJSON event. Encoded with a trailing newline. If a
// sidecar is attached, writes immediately; ring stores a copy for replay.
func (s *Stream) Push(event any) {
	line, err := json.Marshal(event)
	if err != nil {
		slog.Debug("stream push encode err", "err", err, "id", s.ID)
		return
	}
	line = append(line, '\n')

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	if s.conn != nil {
		if _, err := s.conn.Write(line); err != nil {
			slog.Debug("stream sidecar write err — dropping conn", "err", err, "id", s.ID)
			s.conn.Close()
			s.conn = nil
		}
	}
	// Ring storage with eviction.
	free := cap(s.ring) - len(s.ring)
	if free < len(line) {
		drop := len(line) - free
		if drop > len(s.ring) {
			drop = len(s.ring)
		}
		copy(s.ring, s.ring[drop:])
		s.ring = s.ring[:len(s.ring)-drop]
	}
	s.ring = append(s.ring, line...)
	s.offset += int64(len(line))
}

// Close terminates the stream and removes it from the registry. Idempotent.
func (s *Stream) Close() {
	s.mu.Lock()
	if !s.closed {
		s.closed = true
		if s.conn != nil {
			s.conn.Close()
			s.conn = nil
		}
	}
	s.mu.Unlock()
	s.registry.Remove(s.ID)
}

func (s *Stream) markClosed() {
	s.mu.Lock()
	s.closed = true
	if s.conn != nil {
		s.conn.Close()
		s.conn = nil
	}
	s.mu.Unlock()
}
