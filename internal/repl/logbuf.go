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

package repl

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"time"
)

const maxLogRecords = 500

// LogRecord is a single captured slog entry.
type LogRecord struct {
	At      time.Time
	Level   slog.Level
	Message string
	Attrs   map[string]any
}

// LogBuffer is a thread-safe ring buffer for log records.
type LogBuffer struct {
	mu  sync.Mutex
	buf []LogRecord
}

func (b *LogBuffer) append(r LogRecord) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf = append(b.buf, r)
	if len(b.buf) > maxLogRecords {
		b.buf = b.buf[len(b.buf)-maxLogRecords:]
	}
}

// Recent returns the last n records (or all if n <= 0 or n > len).
func (b *LogBuffer) Recent(n int) []LogRecord {
	b.mu.Lock()
	defer b.mu.Unlock()
	if n <= 0 || n >= len(b.buf) {
		out := make([]LogRecord, len(b.buf))
		copy(out, b.buf)
		return out
	}
	out := make([]LogRecord, n)
	copy(out, b.buf[len(b.buf)-n:])
	return out
}

// ReplLogHandler is a slog.Handler that fans out to stderr JSON and LogBuffer.
type ReplLogHandler struct {
	buf  *LogBuffer
	next slog.Handler
}

// NewReplLogHandler creates a handler that captures logs in memory and writes JSON to stderr.
func NewReplLogHandler() *ReplLogHandler {
	return &ReplLogHandler{
		buf:  &LogBuffer{},
		next: slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}),
	}
}

// Buffer returns the underlying LogBuffer.
func (h *ReplLogHandler) Buffer() *LogBuffer { return h.buf }

// Enabled reports whether the handler handles records at the given level.
func (h *ReplLogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

// Handle captures the record into the buffer and forwards to stderr JSON.
func (h *ReplLogHandler) Handle(ctx context.Context, r slog.Record) error {
	attrs := make(map[string]any, r.NumAttrs())
	r.Attrs(func(a slog.Attr) bool {
		attrs[a.Key] = a.Value.Any()
		return true
	})
	h.buf.append(LogRecord{
		At:      r.Time,
		Level:   r.Level,
		Message: r.Message,
		Attrs:   attrs,
	})
	return h.next.Handle(ctx, r)
}

// WithAttrs returns a new handler with the given attributes pre-attached.
func (h *ReplLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &ReplLogHandler{buf: h.buf, next: h.next.WithAttrs(attrs)}
}

// WithGroup returns a new handler scoped to the given group name.
func (h *ReplLogHandler) WithGroup(name string) slog.Handler {
	return &ReplLogHandler{buf: h.buf, next: h.next.WithGroup(name)}
}
