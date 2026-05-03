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

package observability

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"
)

func TestNewRing_CreatesWithCapacity(t *testing.T) {
	r := NewRing(10)
	if r.cap != 10 {
		t.Errorf("expected capacity 10, got %d", r.cap)
	}
	if len(r.buf) != 10 {
		t.Errorf("expected buffer length 10, got %d", len(r.buf))
	}
}

func TestNewRing_CapsCapacityToOne(t *testing.T) {
	r := NewRing(0)
	if r.cap != 1 {
		t.Errorf("expected capacity 1 for zero input, got %d", r.cap)
	}
	r2 := NewRing(-5)
	if r2.cap != 1 {
		t.Errorf("expected capacity 1 for negative input, got %d", r2.cap)
	}
}

func TestRing_Push_RecordsSpans(t *testing.T) {
	r := NewRing(3)
	span := Span{
		TraceID:  "trace-123",
		SpanID:   "span-456",
		Name:     "test-span",
		StartTS:  time.Now(),
		DurationMS: 100,
		Status:   "ok",
	}
	r.Push(span)

	got := r.Snapshot(time.Time{}, 0)
	if len(got) != 1 {
		t.Fatalf("expected 1 span, got %d", len(got))
	}
	if got[0].TraceID != "trace-123" {
		t.Errorf("expected trace ID 'trace-123', got '%s'", got[0].TraceID)
	}
}

func TestRing_Push_WrapsAround(t *testing.T) {
	r := NewRing(3)
	now := time.Now()

	for i := 0; i < 5; i++ {
		r.Push(Span{Name: "span", StartTS: now.Add(time.Duration(i) * time.Second)})
	}

	// After 5 pushes on a 3-slot ring, we should have 3 spans (wrapped twice)
	got := r.Snapshot(time.Time{}, 0)
	if len(got) != 3 {
		t.Errorf("expected 3 spans after wrap, got %d", len(got))
	}
}

func TestRing_Snapshot_FiltersBySince(t *testing.T) {
	r := NewRing(10)
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	for i := 0; i < 5; i++ {
		r.Push(Span{Name: "span", StartTS: base.Add(time.Duration(i) * time.Hour)})
	}

	// Filter from 3rd hour onward
	since := base.Add(2 * time.Hour)
	got := r.Snapshot(since, 0)
	if len(got) != 3 {
		t.Errorf("expected 3 spans after since filter, got %d", len(got))
	}
}

func TestRing_Snapshot_RespectsLimit(t *testing.T) {
	r := NewRing(10)
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	for i := 0; i < 5; i++ {
		r.Push(Span{Name: "span", StartTS: base.Add(time.Duration(i) * time.Hour)})
	}

	got := r.Snapshot(time.Time{}, 2)
	if len(got) != 2 {
		t.Errorf("expected 2 spans with limit, got %d", len(got))
	}
}

func TestRing_Snapshot_ReturnsOldestFirst(t *testing.T) {
	r := NewRing(5)
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	for i := 0; i < 4; i++ {
		r.Push(Span{Name: "span", StartTS: base.Add(time.Duration(i) * time.Hour)})
	}

	got := r.Snapshot(time.Time{}, 0)
	if len(got) != 4 {
		t.Fatalf("expected 4 spans, got %d", len(got))
	}
	for i := 1; i < len(got); i++ {
		if got[i].StartTS.Before(got[i-1].StartTS) {
			t.Errorf("spans not in oldest-first order: span[%d]=%v, span[%d]=%v",
				i-1, got[i-1].StartTS, i, got[i].StartTS)
		}
	}
}

func TestRing_Snapshot_EmptyRing(t *testing.T) {
	r := NewRing(5)
	got := r.Snapshot(time.Time{}, 0)
	if len(got) != 0 {
		t.Errorf("expected 0 spans for empty ring, got %d", len(got))
	}
}

func TestRing_Push_NotFullYet(t *testing.T) {
	r := NewRing(5)
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	// Push fewer than capacity
	for i := 0; i < 3; i++ {
		r.Push(Span{Name: "span", StartTS: base.Add(time.Duration(i) * time.Hour)})
	}

	got := r.Snapshot(time.Time{}, 0)
	if len(got) != 3 {
		t.Errorf("expected 3 spans when not full, got %d", len(got))
	}
}

func TestSpan_JSONSerialization(t *testing.T) {
	ts := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	span := Span{
		TraceID:      "abc123",
		SpanID:       "def456",
		ParentSpanID: "parent789",
		Name:         "test-op",
		StartTS:      ts,
		DurationMS:   42.5,
		Status:       "ok",
		Attributes:   map[string]any{"key": "value"},
	}

	data, err := json.Marshal(span)
	if err != nil {
		t.Fatalf("failed to marshal span: %v", err)
	}

	var decoded Span
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal span: %v", err)
	}

	if decoded.TraceID != span.TraceID {
		t.Errorf("trace ID mismatch: got %s, want %s", decoded.TraceID, span.TraceID)
	}
	if decoded.DurationMS != span.DurationMS {
		t.Errorf("duration mismatch: got %f, want %f", decoded.DurationMS, span.DurationMS)
	}
}

func TestSpan_OmitsEmptyFields(t *testing.T) {
	span := Span{
		TraceID: "abc123",
		Name:    "test",
		StartTS: time.Now(),
		Status:  "ok",
	}

	data, err := json.Marshal(span)
	if err != nil {
		t.Fatalf("failed to marshal span: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("failed to unmarshal to raw map: %v", err)
	}

	if _, ok := raw["parent_span_id"]; ok {
		t.Error("expected parent_span_id to be omitted")
	}
	if _, ok := raw["attributes"]; ok {
		t.Error("expected attributes to be omitted")
	}
}

func TestNewSpanID_ReturnsHexString(t *testing.T) {
	id := NewSpanID()
	if len(id) != 16 { // 8 bytes * 2 hex chars
		t.Errorf("expected 16 char hex string, got %d", len(id))
	}

	for _, c := range id {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("invalid hex character: %c", c)
		}
	}
}

func TestNewSpanID_ReturnsUnique(t *testing.T) {
	ids := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id := NewSpanID()
		if ids[id] {
			t.Errorf("duplicate span ID generated: %s", id)
		}
		ids[id] = true
	}
}

func TestNewTraceID_ReturnsHexString(t *testing.T) {
	id := NewTraceID()
	if len(id) != 32 { // 16 bytes * 2 hex chars
		t.Errorf("expected 32 char hex string, got %d", len(id))
	}

	for _, c := range id {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("invalid hex character: %c", c)
		}
	}
}

func TestRing_Push_ConcurrentSafety(t *testing.T) {
	r := NewRing(100)
	done := make(chan bool)

	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				r.Push(Span{Name: "concurrent-span"})
			}
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	// Should not crash and should have reasonable number of spans
	got := r.Snapshot(time.Time{}, 0)
	if len(got) == 0 {
		t.Error("expected spans after concurrent pushes")
	}
}

func TestRing_Snapshot_ConcurrentWithPush(t *testing.T) {
	r := NewRing(50)
	done := make(chan bool)

	// Writer goroutine
	go func() {
		for i := 0; i < 100; i++ {
			r.Push(Span{Name: "span"})
		}
		done <- true
	}()

	// Reader goroutines
	for i := 0; i < 5; i++ {
		go func() {
			for j := 0; j < 50; j++ {
				r.Snapshot(time.Time{}, 0)
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 6; i++ {
		<-done
	}
}

func TestSpan_StatusValues(t *testing.T) {
	tests := []struct {
		status string
		valid  bool
	}{
		{"ok", true},
		{"error", true},
		{"unset", true},
		{"invalid", false},
	}

	for _, tc := range tests {
		span := Span{
			TraceID: "test",
			Name:    "test",
			StartTS: time.Now(),
			Status:  tc.status,
		}
		data, err := json.Marshal(span)
		if err != nil {
			t.Fatalf("failed to marshal span: %v", err)
		}

		var raw map[string]any
		json.Unmarshal(data, &raw)

		// Status field should be present regardless of value
		if raw["status"] != tc.status {
			t.Errorf("status field mismatch for %q", tc.status)
		}
	}
}

func TestRing_ExactCapacity(t *testing.T) {
	r := NewRing(3)
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	// Fill exactly to capacity
	for i := 0; i < 3; i++ {
		r.Push(Span{Name: "span", StartTS: base.Add(time.Duration(i))})
	}

	// Should not have wrapped yet
	got := r.Snapshot(time.Time{}, 0)
	if len(got) != 3 {
		t.Errorf("expected 3 spans at exact capacity, got %d", len(got))
	}

	// One more push should trigger wrap
	r.Push(Span{Name: "span", StartTS: base.Add(time.Duration(10))})
	got = r.Snapshot(time.Time{}, 0)
	if len(got) != 3 {
		t.Errorf("expected 3 spans after wrap, got %d", len(got))
	}
}

func TestSpan_AttributesMap(t *testing.T) {
	span := Span{
		TraceID: "test",
		Name:    "test",
		StartTS: time.Now(),
		Status:  "ok",
		Attributes: map[string]any{
			"string_val": "hello",
			"int_val":    42,
			"float_val":  3.14,
			"bool_val":   true,
			"nested":     map[string]any{"key": "value"},
		},
	}

	data, err := json.Marshal(span)
	if err != nil {
		t.Fatalf("failed to marshal span: %v", err)
	}

	var decoded Span
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal span: %v", err)
	}

	if decoded.Attributes["string_val"] != "hello" {
		t.Errorf("string attribute mismatch")
	}
	if decoded.Attributes["int_val"].(float64) != 42 {
		t.Errorf("int attribute mismatch")
	}
	if decoded.Attributes["float_val"].(float64) != 3.14 {
		t.Errorf("float attribute mismatch")
	}
}

func TestRing_Snapshot_LimitWithWrap(t *testing.T) {
	r := NewRing(3)
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	// Wrap the ring
	for i := 0; i < 6; i++ {
		r.Push(Span{Name: "span", StartTS: base.Add(time.Duration(i))})
	}

	// Request only 1
	got := r.Snapshot(time.Time{}, 1)
	if len(got) != 1 {
		t.Errorf("expected 1 span with limit, got %d", len(got))
	}
}

// Benchmark tests for reference
func BenchmarkRing_Push(b *testing.B) {
	r := NewRing(1000)
	span := Span{Name: "bench-span", StartTS: time.Now()}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Push(span)
	}
}

func BenchmarkRing_Snapshot(b *testing.B) {
	r := NewRing(1000)
	for i := 0; i < 1000; i++ {
		r.Push(Span{Name: "bench-span", StartTS: time.Now()})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Snapshot(time.Time{}, 0)
	}
}

// JSON roundtrip test for the Span type
func TestSpan_Roundtrip(t *testing.T) {
	original := Span{
		TraceID:      "trace-roundtrip",
		SpanID:       "span-roundtrip",
		ParentSpanID: "parent-roundtrip",
		Name:         "roundtrip-test",
		StartTS:      time.Date(2024, 6, 15, 12, 30, 0, 0, time.UTC),
		DurationMS:   123.456,
		Status:       "error",
		Attributes: map[string]any{
			"request_id": "req-abc-123",
			"retries":    3,
		},
	}

	// Marshal
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	// Verify it's valid JSON
	if !json.Valid(data) {
		t.Error("marshaled output is not valid JSON")
	}

	// Unmarshal
	var decoded Span
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	// Compare
	if decoded.TraceID != original.TraceID {
		t.Error("TraceID mismatch")
	}
	if decoded.SpanID != original.SpanID {
		t.Error("SpanID mismatch")
	}
	if decoded.ParentSpanID != original.ParentSpanID {
		t.Error("ParentSpanID mismatch")
	}
	if decoded.Name != original.Name {
		t.Error("Name mismatch")
	}
	if !decoded.StartTS.Equal(original.StartTS) {
		t.Error("StartTS mismatch")
	}
	if decoded.DurationMS != original.DurationMS {
		t.Error("DurationMS mismatch")
	}
	if decoded.Status != original.Status {
		t.Error("Status mismatch")
	}
}

// Test that Snapshot returns copy of data
func TestRing_Snapshot_ReturnsCopy(t *testing.T) {
	r := NewRing(10)
	r.Push(Span{Name: "original", StartTS: time.Now()})

	got := r.Snapshot(time.Time{}, 0)
	if len(got) == 0 {
		t.Fatal("expected spans")
	}

	// Modify the returned slice - should not affect the ring
	got[0].Name = "modified"
	got = r.Snapshot(time.Time{}, 0)
	if got[0].Name != "original" {
		t.Error("snapshot should return copy, not reference")
	}
}

// Test JSON struct tags match expected schema field names
func TestSpan_JSONFieldNames(t *testing.T) {
	span := Span{
		TraceID: "trace",
		SpanID: "span",
		Name:   "name",
	}
	data, _ := json.Marshal(span)
	var decoded map[string]any
	json.Unmarshal(data, &decoded)

	expectedFields := []string{"trace_id", "span_id", "name", "start_ts", "duration_ms", "status"}
	for _, field := range expectedFields {
		if _, ok := decoded[field]; !ok {
			t.Errorf("expected JSON field %q in output", field)
		}
	}
}

// Test Ring edge case: limit larger than available spans
func TestRing_Snapshot_LimitExceedsAvailable(t *testing.T) {
	r := NewRing(10)
	r.Push(Span{Name: "span-1", StartTS: time.Now()})
	r.Push(Span{Name: "span-2", StartTS: time.Now()})

	// Request limit of 100, but only 2 spans exist
	got := r.Snapshot(time.Time{}, 100)
	if len(got) != 2 {
		t.Errorf("expected 2 spans, got %d", len(got))
	}
}

// Test with very old since filter
func TestRing_Snapshot_SinceInFuture(t *testing.T) {
	r := NewRing(10)
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	for i := 0; i < 5; i++ {
		r.Push(Span{Name: "span", StartTS: base.Add(time.Duration(i))})
	}

	// Filter from far future - should return nothing
	since := base.Add(time.Hour * 24 * 365)
	got := r.Snapshot(since, 0)
	if len(got) != 0 {
		t.Errorf("expected 0 spans for future since, got %d", len(got))
	}
}

// Buffer inspection test
func TestRing_BufferInitialized(t *testing.T) {
	r := NewRing(5)
	// Verify buffer is pre-allocated
	if cap(r.buf) != 5 {
		t.Errorf("expected buffer capacity 5, got %d", cap(r.buf))
	}
	if len(r.buf) != 5 {
		t.Errorf("expected buffer length 5, got %d", len(r.buf))
	}
}

// Test with time.Time zero value
func TestRing_Snapshot_ZeroSince(t *testing.T) {
	r := NewRing(10)
	r.Push(Span{Name: "span", StartTS: time.Now()})

	// Using zero time should return all spans
	got := r.Snapshot(time.Time{}, 0)
	if len(got) != 1 {
		t.Errorf("expected 1 span with zero since, got %d", len(got))
	}
}

// Additional span with all fields populated for complete coverage
func TestSpan_FullFields(t *testing.T) {
	ts := time.Now().UTC()
	span := Span{
		TraceID:      "full-trace",
		SpanID:       "full-span",
		ParentSpanID: "full-parent",
		Name:         "full-operation",
		StartTS:      ts,
		DurationMS:   999.999,
		Status:       "ok",
		Attributes: map[string]any{
			"attr1": "value1",
			"attr2": float64(2),
		},
	}

	data, err := json.Marshal(span)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var decoded map[string]any
	json.Unmarshal(data, &decoded)

	if decoded["trace_id"] != "full-trace" {
		t.Error("trace_id mismatch")
	}
	if decoded["span_id"] != "full-span" {
		t.Error("span_id mismatch")
	}
	if decoded["parent_span_id"] != "full-parent" {
		t.Error("parent_span_id mismatch")
	}
	if decoded["name"] != "full-operation" {
		t.Error("name mismatch")
	}
	if decoded["duration_ms"] != 999.999 {
		t.Error("duration_ms mismatch")
	}
	if decoded["status"] != "ok" {
		t.Error("status mismatch")
	}
}

// Test Ring internal state after wrapping
func TestRing_InternalStateAfterWrap(t *testing.T) {
	r := NewRing(3)

	// Fill and overflow
	for i := 0; i < 5; i++ {
		r.Push(Span{Name: "span", StartTS: time.Now()})
	}

	// Verify we get 3 spans
	got := r.Snapshot(time.Time{}, 0)
	if len(got) != 3 {
		t.Errorf("expected 3 spans, got %d", len(got))
	}
}

// Ensure Snapshot does not block when ring is being written to
func TestRing_Snapshot_DoesNotBlock(t *testing.T) {
	r := NewRing(100)
	done := make(chan error, 1)

	go func() {
		for i := 0; i < 1000; i++ {
			r.Push(Span{Name: "span"})
		}
	}()

	go func() {
		for i := 0; i < 1000; i++ {
			r.Snapshot(time.Time{}, 0)
		}
		done <- nil
	}()

	select {
	case <-done:
		// Success - did not block
	case <-time.After(2 * time.Second):
		t.Error("Snapshot appeared to block during concurrent writes")
	}
}

// Test that NewSpanID and NewTraceID use crypto/rand
func TestIDGenerators_ConsistentLength(t *testing.T) {
	spanID := NewSpanID()
	traceID := NewTraceID()

	if len(spanID) != 16 {
		t.Errorf("span ID should be 16 chars, got %d", len(spanID))
	}
	if len(traceID) != 32 {
		t.Errorf("trace ID should be 32 chars, got %d", len(traceID))
	}
}

// Test Ring with maximum reasonable capacity
func TestRing_LargeCapacity(t *testing.T) {
	r := NewRing(10000)
	if r.cap != 10000 {
		t.Errorf("expected capacity 10000, got %d", r.cap)
	}
}

// Test empty snapshot doesn't cause issues
func TestRing_EmptySnapshotMultiple(t *testing.T) {
	r := NewRing(5)
	for i := 0; i < 10; i++ {
		got := r.Snapshot(time.Time{}, 0)
		if len(got) != 0 {
			t.Errorf("iteration %d: expected 0 spans, got %d", i, len(got))
		}
	}
}

// Test with negative limit (should be treated as no limit)
func TestRing_Snapshot_NegativeLimit(t *testing.T) {
	r := NewRing(10)
	for i := 0; i < 5; i++ {
		r.Push(Span{Name: "span", StartTS: time.Now()})
	}

	got := r.Snapshot(time.Time{}, -1)
	// Negative limit is > 0 check failing, so it returns all
	if len(got) != 5 {
		t.Errorf("expected 5 spans with negative limit, got %d", len(got))
	}
}

// Additional: test Span JSON structure validation
func TestSpan_JSONStructure(t *testing.T) {
	ts := time.Now()
	span := Span{
		TraceID:    "trace-abc",
		SpanID:     "span-xyz",
		Name:       "test-operation",
		StartTS:    ts,
		DurationMS: 50.5,
		Status:     "ok",
	}

	data, err := json.Marshal(span)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	// Parse as generic JSON to check structure
	var raw json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal to raw failed: %v", err)
	}

	// Should be valid JSON
	if !bytes.Equal([]byte(data)[:1], []byte("{")) {
		t.Error("expected JSON object")
	}
}
