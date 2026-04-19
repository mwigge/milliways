package dispatch

// TODO: Add go.uber.org/goleak.VerifyTestMain when the dependency is adopted.
// This package spawns goroutines in DispatchAsync; all are joined via Wait()
// in each test, but goleak would catch any future leak regressions.

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mwigge/milliways/internal/conversation"
	"github.com/mwigge/milliways/internal/kitchen"
	"github.com/mwigge/milliways/internal/pantry"
	"github.com/mwigge/milliways/internal/substrate"
)

func openTestPantry(t *testing.T) *pantry.DB {
	t.Helper()
	pdb, err := pantry.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = pdb.Close() })
	return pdb
}

func TestDispatchAsync(t *testing.T) {
	t.Parallel()
	pdb := openTestPantry(t)
	d := NewAsyncDispatcher(pdb)

	k := kitchen.NewGeneric(kitchen.GenericConfig{Name: "echo-test", Cmd: "echo", Enabled: true})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ticketID, err := d.DispatchAsync(ctx, k, "hello async")
	if err != nil {
		t.Fatalf("DispatchAsync: %v", err)
	}
	if ticketID == "" {
		t.Fatal("expected non-empty ticket ID")
	}
	if len(ticketID) < 4 {
		t.Errorf("ticket ID too short: %q", ticketID)
	}

	// Wait for background goroutine to complete
	d.Wait()

	// Verify ticket was created and updated
	ticket, err := pdb.Tickets().Get(ticketID)
	if err != nil {
		t.Fatalf("Get ticket: %v", err)
	}
	if ticket == nil {
		t.Fatal("ticket not found")
	}
	if ticket.Status != "complete" {
		t.Errorf("expected status 'complete', got %q", ticket.Status)
	}
	if ticket.Kitchen != "echo-test" {
		t.Errorf("expected kitchen 'echo-test', got %q", ticket.Kitchen)
	}

	// Verify ledger entry was written
	total, _ := pdb.Ledger().Total()
	if total != 1 {
		t.Errorf("expected 1 ledger entry, got %d", total)
	}
}

func TestDispatchAsync_TicketList(t *testing.T) {
	t.Parallel()
	pdb := openTestPantry(t)
	d := NewAsyncDispatcher(pdb)

	k := kitchen.NewGeneric(kitchen.GenericConfig{Name: "echo-test", Cmd: "echo", Enabled: true})

	ctx := context.Background()
	_, _ = d.DispatchAsync(ctx, k, "task one")
	_, _ = d.DispatchAsync(ctx, k, "task two")

	d.Wait()

	tickets, err := pdb.Tickets().List("")
	if err != nil {
		t.Fatal(err)
	}
	if len(tickets) != 2 {
		t.Errorf("expected 2 tickets, got %d", len(tickets))
	}
}

func TestDispatchDetached_ReturnsNotImplemented(t *testing.T) {
	t.Parallel()
	pdb := openTestPantry(t)
	d := NewAsyncDispatcher(pdb)

	k := kitchen.NewGeneric(kitchen.GenericConfig{Name: "echo-test", Cmd: "echo", Enabled: true})

	_, err := d.DispatchDetached(k, "detached task")
	if err == nil {
		t.Fatal("expected error from DispatchDetached, got nil")
	}
	if !strings.Contains(err.Error(), "not yet implemented") {
		t.Errorf("expected 'not yet implemented' error, got: %v", err)
	}
}

func TestDispatchAsync_LogsStructuredSubstrateWarnings(t *testing.T) {
	pdb := openTestPantry(t)
	d := NewAsyncDispatcher(pdb)
	capture := installTestLogger(t)

	writer := &stubConvWriter{
		beginErr:        errors.New("begin failed"),
		startSegmentErr: errors.New("start segment failed"),
		appendTurnErr:   errors.New("append turn failed"),
		endSegmentErr:   errors.New("end segment failed"),
		finishErr:       errors.New("finish failed"),
	}
	d.WithSubstrateClient(func() convWriter { return writer })

	ticketID, err := d.DispatchAsync(context.Background(), stubKitchen{
		name:   "stub-kitchen",
		result: kitchen.Result{ExitCode: 0, Output: "hello"},
	}, "hello async")
	if err != nil {
		t.Fatalf("DispatchAsync: %v", err)
	}

	d.Wait()

	assertLogRecord(t, capture.records(), slog.LevelWarn, "async substrate begin", ticketID, "begin failed")
	assertLogRecord(t, capture.records(), slog.LevelWarn, "async substrate start segment", ticketID, "start segment failed")
	assertLogRecord(t, capture.records(), slog.LevelWarn, "async substrate append turn", ticketID, "append turn failed")
	assertLogRecord(t, capture.records(), slog.LevelWarn, "async substrate end segment", ticketID, "end segment failed")
	assertLogRecord(t, capture.records(), slog.LevelWarn, "async substrate finish", ticketID, "finish failed")
	assertAttrValue(t, capture.records(), "async substrate end segment", "segment_status", "done")
	assertAttrValue(t, capture.records(), "async substrate finish", "conversation_status", "done")
}

func TestDispatchAsync_LogsStructuredCheckpointWarning(t *testing.T) {
	pdb := openTestPantry(t)
	d := NewAsyncDispatcher(pdb)
	capture := installTestLogger(t)

	writer := &stubConvWriter{
		checkpointErr: errors.New("checkpoint failed"),
	}
	d.WithSubstrateClient(func() convWriter { return writer })

	ticketID, err := d.DispatchAsync(context.Background(), stubKitchen{
		name:   "stub-kitchen",
		result: kitchen.Result{ExitCode: 2, Output: "partial"},
	}, "hello async")
	if err != nil {
		t.Fatalf("DispatchAsync: %v", err)
	}

	d.Wait()

	assertLogRecord(t, capture.records(), slog.LevelWarn, "async substrate checkpoint on exhaustion", ticketID, "checkpoint failed")
	for _, record := range capture.records() {
		if record.Message == "async substrate end segment" {
			t.Fatal("did not expect end segment log on exhaustion path")
		}
	}
}

func TestDispatchAsync_RecoversKitchenExecPanic(t *testing.T) {
	t.Parallel()
	pdb := openTestPantry(t)
	d := NewAsyncDispatcher(pdb)

	ticketID, err := d.DispatchAsync(context.Background(), panicKitchen{name: "panic-kitchen"}, "hello async")
	if err != nil {
		t.Fatalf("DispatchAsync: %v", err)
	}

	waitForDispatcher(t, d)

	ticket, err := pdb.Tickets().Get(ticketID)
	if err != nil {
		t.Fatalf("Get ticket: %v", err)
	}
	if ticket == nil {
		t.Fatal("ticket not found")
	}
	if ticket.Status != "failed" {
		t.Fatalf("expected status failed, got %q", ticket.Status)
	}
	if ticket.ExitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", ticket.ExitCode)
	}
}

func TestDispatchAsync_RecoversOuterGoroutinePanic(t *testing.T) {
	pdb := openTestPantry(t)
	d := NewAsyncDispatcher(pdb)
	capture := installTestLogger(t)
	d.WithSubstrateClient(func() convWriter { return panicConvWriter{} })

	ticketID, err := d.DispatchAsync(context.Background(), stubKitchen{
		name:   "stub-kitchen",
		result: kitchen.Result{ExitCode: 0, Output: "hello"},
	}, "hello async")
	if err != nil {
		t.Fatalf("DispatchAsync: %v", err)
	}

	waitForDispatcher(t, d)

	ticket, err := pdb.Tickets().Get(ticketID)
	if err != nil {
		t.Fatalf("Get ticket: %v", err)
	}
	if ticket == nil {
		t.Fatal("ticket not found")
	}
	if ticket.Status != "panicked" {
		t.Fatalf("expected status panicked, got %q", ticket.Status)
	}
	if ticket.ExitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", ticket.ExitCode)
	}
	assertAttrValue(t, capture.records(), "dispatch async goroutine panicked", "ticket", ticketID)
	assertAttrValue(t, capture.records(), "dispatch async goroutine panicked", "panic", "substrate begin panic")
}

type stubKitchen struct {
	name   string
	result kitchen.Result
	err    error
}

func (k stubKitchen) Name() string { return k.name }

func (k stubKitchen) Exec(_ context.Context, _ kitchen.Task) (kitchen.Result, error) {
	return k.result, k.err
}

func (k stubKitchen) Stations() []string { return nil }

func (k stubKitchen) CostTier() kitchen.CostTier { return kitchen.Free }

func (k stubKitchen) Status() kitchen.Status { return kitchen.Ready }

type panicKitchen struct {
	name string
}

func (k panicKitchen) Name() string { return k.name }

func (k panicKitchen) Exec(context.Context, kitchen.Task) (kitchen.Result, error) {
	panic("kitchen exec panic")
}

func (k panicKitchen) Stations() []string { return nil }

func (k panicKitchen) CostTier() kitchen.CostTier { return kitchen.Free }

func (k panicKitchen) Status() kitchen.Status { return kitchen.Ready }

type stubConvWriter struct {
	beginErr        error
	startSegmentErr error
	appendTurnErr   error
	endSegmentErr   error
	checkpointErr   error
	finishErr       error
}

func (w *stubConvWriter) Begin(context.Context, string, string, string, string) error {
	return w.beginErr
}

func (w *stubConvWriter) StartSegment(context.Context, string, *conversation.RepoContext) error {
	return w.startSegmentErr
}

func (w *stubConvWriter) AppendTurn(context.Context, conversation.TurnRole, string, string, []string, []conversation.ProjectRef) error {
	return w.appendTurnErr
}

func (w *stubConvWriter) EndSegment(context.Context, string, string) error { return w.endSegmentErr }

func (w *stubConvWriter) CheckpointOnExhaustion(context.Context, string) (substrate.CheckpointResponse, error) {
	return substrate.CheckpointResponse{}, w.checkpointErr
}

func (w *stubConvWriter) Finish(context.Context, string, string) error { return w.finishErr }

type panicConvWriter struct{}

func (panicConvWriter) Begin(context.Context, string, string, string, string) error {
	panic("substrate begin panic")
}

func (panicConvWriter) StartSegment(context.Context, string, *conversation.RepoContext) error {
	return nil
}

func (panicConvWriter) AppendTurn(context.Context, conversation.TurnRole, string, string, []string, []conversation.ProjectRef) error {
	return nil
}

func (panicConvWriter) EndSegment(context.Context, string, string) error { return nil }

func (panicConvWriter) CheckpointOnExhaustion(context.Context, string) (substrate.CheckpointResponse, error) {
	return substrate.CheckpointResponse{}, nil
}

func (panicConvWriter) Finish(context.Context, string, string) error { return nil }

var testLoggerMu sync.Mutex

type testLogRecord struct {
	Level   slog.Level
	Message string
	Attrs   map[string]any
}

type testLogCapture struct {
	mu      sync.Mutex
	entries []testLogRecord
}

func installTestLogger(t *testing.T) *testLogCapture {
	t.Helper()
	testLoggerMu.Lock()
	capture := &testLogCapture{}
	previous := slog.Default()
	slog.SetDefault(slog.New(capture))
	t.Cleanup(func() {
		slog.SetDefault(previous)
		testLoggerMu.Unlock()
	})
	return capture
}

func (c *testLogCapture) Enabled(context.Context, slog.Level) bool { return true }

func (c *testLogCapture) Handle(_ context.Context, record slog.Record) error {
	attrs := make(map[string]any, record.NumAttrs())
	record.Attrs(func(attr slog.Attr) bool {
		attrs[attr.Key] = attr.Value.Any()
		return true
	})

	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = append(c.entries, testLogRecord{
		Level:   record.Level,
		Message: record.Message,
		Attrs:   attrs,
	})
	return nil
}

func (c *testLogCapture) WithAttrs(_ []slog.Attr) slog.Handler { return c }

func (c *testLogCapture) WithGroup(string) slog.Handler { return c }

func (c *testLogCapture) records() []testLogRecord {
	c.mu.Lock()
	defer c.mu.Unlock()
	clone := make([]testLogRecord, len(c.entries))
	copy(clone, c.entries)
	return clone
}

func assertLogRecord(t *testing.T, records []testLogRecord, level slog.Level, message, ticketID, errText string) {
	t.Helper()
	for _, record := range records {
		if record.Level != level || record.Message != message {
			continue
		}
		if got, ok := record.Attrs["ticket"]; !ok || got != ticketID {
			t.Fatalf("log %q ticket = %v, want %q", message, got, ticketID)
		}
		if got := record.Attrs["err"]; got == nil || !strings.Contains(got.(error).Error(), errText) {
			t.Fatalf("log %q err = %v, want substring %q", message, got, errText)
		}
		return
	}
	t.Fatalf("log %q not found in %+v", message, records)
}

func assertAttrValue(t *testing.T, records []testLogRecord, message, key string, want any) {
	t.Helper()
	for _, record := range records {
		if record.Message != message {
			continue
		}
		if got := record.Attrs[key]; got != want {
			t.Fatalf("log %q attr %q = %v, want %v", message, key, got, want)
		}
		return
	}
	t.Fatalf("log %q not found", message)
}

func waitForDispatcher(t *testing.T, d *AsyncDispatcher) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		d.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for async dispatcher")
	}
}
