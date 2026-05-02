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
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestOTelSinkTracksAndClearsSegmentState(t *testing.T) {
	resetOTelForTest(t)

	var initCalls atomic.Int32
	otelInit = func() (otelState, error) {
		initCalls.Add(1)
		provider := sdktrace.NewTracerProvider()
		meterProvider := sdkmetric.NewMeterProvider()
		meter := meterProvider.Meter("test")

		dispatchTotal, err := meter.Int64Counter("milliways.dispatch.total")
		if err != nil {
			return otelState{}, err
		}
		dispatchDuration, err := meter.Float64Histogram("milliways.dispatch.duration_seconds")
		if err != nil {
			return otelState{}, err
		}
		failoverTotal, err := meter.Int64Counter("milliways.failover.total")
		if err != nil {
			return otelState{}, err
		}

		return otelState{
			tracer:           provider.Tracer("test"),
			meter:            meter,
			dispatchTotal:    dispatchTotal,
			dispatchDuration: dispatchDuration,
			failoverTotal:    failoverTotal,
		}, nil
	}

	sink, err := NewOTelSink()
	if err != nil {
		t.Fatalf("NewOTelSink: %v", err)
	}
	otelSink, ok := sink.(*OTelSink)
	if !ok {
		t.Fatalf("sink type = %T, want *OTelSink", sink)
	}

	startedAt := time.Date(2026, time.April, 18, 12, 0, 0, 0, time.UTC)
	otelSink.Emit(Event{
		ConversationID: "conv-1",
		BlockID:        "block-1",
		SegmentID:      "segment-1",
		Kind:           "segment_start",
		Provider:       "claude",
		At:             startedAt,
		Fields: map[string]string{
			"tier": "cloud",
		},
	})

	if got := initCalls.Load(); got != 1 {
		t.Fatalf("otel init calls = %d, want 1", got)
	}
	if _, ok := otelSink.segments.Load("segment-1"); !ok {
		t.Fatal("expected segment state to be stored")
	}

	otelSink.Emit(Event{
		ConversationID: "conv-1",
		BlockID:        "block-1",
		SegmentID:      "segment-1",
		Kind:           "segment_end",
		Provider:       "claude",
		At:             startedAt.Add(2 * time.Second),
		Fields: map[string]string{
			"status": "done",
		},
	})

	if _, ok := otelSink.segments.Load("segment-1"); ok {
		t.Fatal("expected segment state to be cleared after segment_end")
	}

	otelSink.Emit(Event{
		Kind: "switch",
		Fields: map[string]string{
			"from": "claude",
			"to":   "gemini",
		},
	})
	if got := initCalls.Load(); got != 1 {
		t.Fatalf("otel init calls after switch = %d, want 1", got)
	}
}

func TestOTelSinkFallsBackWhenInitFails(t *testing.T) {
	resetOTelForTest(t)

	wantErr := errors.New("boom")
	otelInit = func() (otelState, error) {
		return otelState{}, wantErr
	}

	sink, err := NewOTelSink()
	if err != nil {
		t.Fatalf("NewOTelSink: %v", err)
	}

	if err := MustOtel(); !errors.Is(err, wantErr) {
		t.Fatalf("MustOtel error = %v, want %v", err, wantErr)
	}

	sink.Emit(Event{Kind: "segment_start", SegmentID: "segment-1", At: time.Now()})
	sink.Emit(Event{Kind: "segment_end", SegmentID: "segment-1", At: time.Now()})
}

func TestDefaultOTelInitUsesOTLPWhenEndpointSet(t *testing.T) {
	// t.Setenv requires no t.Parallel — env mutation is process-wide.

	// Use an unreachable endpoint to exercise the OTLP code path.
	// OTLP HTTP exporters connect lazily (on flush), so init must succeed.
	t.Setenv("MILLIWAYS_OTEL_ENDPOINT", "http://127.0.0.1:14318")

	state, err := defaultOTelInit()
	if err != nil {
		t.Fatalf("defaultOTelInit with OTLP endpoint: %v", err)
	}
	if state.tracerProvider == nil {
		t.Fatal("expected non-nil tracerProvider when OTLP endpoint is set")
	}
	if state.meterProvider == nil {
		t.Fatal("expected non-nil meterProvider when OTLP endpoint is set")
	}
	if state.dispatchTotal == nil {
		t.Fatal("expected non-nil dispatchTotal counter")
	}
	// Verify the OTLP code path was taken by inspecting the exported kind.
	if got := state.exporterKind; got != "otlp" {
		t.Errorf("exporterKind = %q, want %q", got, "otlp")
	}
}

func TestDefaultOTelInitUsesStdoutWhenNoEndpoint(t *testing.T) {
	// Ensure the env var is absent for this test.
	t.Setenv("MILLIWAYS_OTEL_ENDPOINT", "")

	state, err := defaultOTelInit()
	if err != nil {
		t.Fatalf("defaultOTelInit without endpoint: %v", err)
	}
	if state.tracerProvider == nil {
		t.Fatal("expected non-nil tracerProvider for stdout path")
	}
	if state.meterProvider == nil {
		t.Fatal("expected non-nil meterProvider for stdout path")
	}
	if got := state.exporterKind; got != "stdout" {
		t.Errorf("exporterKind = %q, want %q", got, "stdout")
	}
}

func resetOTelForTest(t *testing.T) {
	t.Helper()

	otelOnce = sync.Once{}
	otelGlobalState = newNoopOTelState()
	otelInitErr = nil
	otelInit = defaultOTelInit
	t.Cleanup(func() {
		otelOnce = sync.Once{}
		otelGlobalState = newNoopOTelState()
		otelInitErr = nil
		otelInit = defaultOTelInit
	})
}
