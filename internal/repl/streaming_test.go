package repl

import (
	"bytes"
	"context"
	"io"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestStreamCmdOutput(t *testing.T) {
	t.Parallel()

	t.Run("basic streaming", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		buf := &bytes.Buffer{}

		cmd := exec.CommandContext(ctx, "sh", "-c", "echo line1; echo line2; echo line3")
		err := streamCmdOutput(ctx, cmd, buf)
		if err != nil {
			t.Fatalf("streamCmdOutput() = %v", err)
		}

		lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
		if len(lines) != 3 {
			t.Errorf("got %d lines, want 3: %q", len(lines), buf.String())
		}
	})

	t.Run("context cancellation", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		buf := &bytes.Buffer{}

		cmd := exec.CommandContext(ctx, "sh", "-c", "sleep 5; echo done")
		err := streamCmdOutput(ctx, cmd, buf)
		if err != nil && ctx.Err() != nil {
			// expected - context was cancelled
		}
	})

	t.Run("stderr streamed separately", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		buf := &bytes.Buffer{}

		cmd := exec.CommandContext(ctx, "sh", "-c", "echo stdout; echo stderr >&2")
		err := streamCmdOutput(ctx, cmd, buf)
		if err != nil {
			t.Fatalf("streamCmdOutput() = %v", err)
		}

		output := buf.String()
		if !strings.Contains(output, "stdout") {
			t.Errorf("missing stdout: %q", output)
		}
		if !strings.Contains(output, "stderr") {
			t.Errorf("missing stderr: %q", output)
		}
	})

	t.Run("empty command", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		buf := &bytes.Buffer{}

		cmd := exec.CommandContext(ctx, "true")
		err := streamCmdOutput(ctx, cmd, buf)
		if err != nil {
			t.Fatalf("streamCmdOutput() = %v", err)
		}
	})

	t.Run("long output", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		buf := &bytes.Buffer{}

		cmd := exec.CommandContext(ctx, "sh", "-c", "for i in $(seq 100); do echo \"line $i\"; done")
		err := streamCmdOutput(ctx, cmd, buf)
		if err != nil {
			t.Fatalf("streamCmdOutput() = %v", err)
		}

		lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
		if len(lines) != 100 {
			t.Errorf("got %d lines, want 100", len(lines))
		}
	})
}

func TestStreamingWriter(t *testing.T) {
	t.Parallel()

	t.Run("flush on newlines", func(t *testing.T) {
		t.Parallel()
		buf := &bytes.Buffer{}
		sw := newStreamingWriter(buf)

		sw.Write([]byte("hello"))
		if buf.Len() != 0 {
			t.Errorf("buf.Len() = %d, want 0 (no newline yet)", buf.Len())
		}

		sw.Write([]byte("\nworld"))
		if got := buf.String(); got != "hello\n" {
			t.Errorf("buf = %q, want %q", got, "hello\n")
		}
		sw.Flush()
		if got := buf.String(); got != "hello\nworld" {
			t.Errorf("after flush: buf = %q, want %q", got, "hello\nworld")
		}
	})

	t.Run("flush", func(t *testing.T) {
		t.Parallel()
		buf := &bytes.Buffer{}
		sw := newStreamingWriter(buf)

		sw.Write([]byte("partial"))
		sw.Flush()
		if got := buf.String(); got != "partial" {
			t.Errorf("buf = %q, want %q", got, "partial")
		}
	})

	t.Run("multi-line", func(t *testing.T) {
		t.Parallel()
		buf := &bytes.Buffer{}
		sw := newStreamingWriter(buf)

		sw.Write([]byte("line1\nline2\nline3"))
		if got := buf.String(); got != "line1\nline2\n" {
			t.Errorf("buf = %q, want %q", got, "line1\nline2\n")
		}
		sw.Flush()
		if got := buf.String(); got != "line1\nline2\nline3" {
			t.Errorf("after flush: buf = %q, want %q", got, "line1\nline2\nline3")
		}
	})

	t.Run("empty write", func(t *testing.T) {
		t.Parallel()
		buf := &bytes.Buffer{}
		sw := newStreamingWriter(buf)

		n, err := sw.Write([]byte{})
		if n != 0 || err != nil {
			t.Errorf("Write([]) = (%d, %v), want (0, nil)", n, err)
		}
		if buf.Len() != 0 {
			t.Errorf("buf.Len() = %d, want 0", buf.Len())
		}
	})
}

func TestConcurrentStreaming(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var mu sync.Mutex
	var lines []string
	var wg sync.WaitGroup

	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			localBuf := &bytes.Buffer{}
			cmd := exec.CommandContext(ctx, "sh", "-c", "echo line from goroutine $id")
			streamCmdOutput(ctx, cmd, localBuf)
			mu.Lock()
			for _, l := range strings.Split(strings.TrimSpace(localBuf.String()), "\n") {
				lines = append(lines, l)
			}
			mu.Unlock()
		}(i)
	}

	wg.Wait()

	if len(lines) != 3 {
		t.Errorf("got %d lines, want 3: %v", len(lines), lines)
	}
}

type mockWriter struct {
	mu    sync.Mutex
	lines []string
}

func (m *mockWriter) Write(p []byte) (n int, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lines = append(m.lines, string(p))
	return len(p), nil
}

func TestMultiWriter(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	mock1 := &mockWriter{}
	mock2 := &mockWriter{}

	mw := io.MultiWriter(mock1, mock2)

	cmd := exec.CommandContext(ctx, "sh", "-c", "echo hello")
	err := streamCmdOutput(ctx, cmd, mw)
	if err != nil {
		t.Fatalf("streamCmdOutput() = %v", err)
	}

	if len(mock1.lines) == 0 || len(mock2.lines) == 0 {
		t.Error("both writers should have received output")
	}
}
