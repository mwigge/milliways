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
	"context"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func setupDelegateTestState(t *testing.T) (sdkmetric.Reader, *tracetest.InMemoryExporter) {
	t.Helper()
	resetOTelForTest(t)

	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	meter := mp.Meter(instrumentationName)

	dispatchTotal, _ := meter.Int64Counter("milliways.dispatch.total")
	dispatchDuration, _ := meter.Float64Histogram("milliways.dispatch.duration_seconds")
	failoverTotal, _ := meter.Int64Counter("milliways.failover.total")
	pass, rework, fail, err := registerDelegateOutcomeCounters(meter)
	if err != nil {
		t.Fatalf("registerDelegateOutcomeCounters: %v", err)
	}

	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))

	otelInit = func() (otelState, error) {
		return otelState{
			tracer:                tp.Tracer(instrumentationName),
			meter:                 meter,
			dispatchTotal:         dispatchTotal,
			dispatchDuration:      dispatchDuration,
			failoverTotal:         failoverTotal,
			delegateOutcomePass:   pass,
			delegateOutcomeRework: rework,
			delegateOutcomeFail:   fail,
		}, nil
	}

	return reader, exp
}

func collectCounters(t *testing.T, reader sdkmetric.Reader) map[string]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	out := make(map[string]int64)
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if sum, ok := m.Data.(metricdata.Sum[int64]); ok {
				var total int64
				for _, dp := range sum.DataPoints {
					total += dp.Value
				}
				out[m.Name] = total
			}
		}
	}
	return out
}

func TestRecordDelegateOutcomeIncrementsPassCounter(t *testing.T) {
	reader, _ := setupDelegateTestState(t)

	ctx := context.Background()
	RecordDelegateOutcome(ctx, "pass")
	RecordDelegateOutcome(ctx, "pass")

	counts := collectCounters(t, reader)
	if got := counts["milliways.delegate.outcome.pass"]; got != 2 {
		t.Errorf("pass counter = %d, want 2", got)
	}
	if got := counts["milliways.delegate.outcome.rework"]; got != 0 {
		t.Errorf("rework counter = %d, want 0", got)
	}
	if got := counts["milliways.delegate.outcome.fail"]; got != 0 {
		t.Errorf("fail counter = %d, want 0", got)
	}
}

func TestRecordDelegateOutcomeIncrementsReworkCounter(t *testing.T) {
	reader, _ := setupDelegateTestState(t)

	RecordDelegateOutcome(context.Background(), "rework")

	counts := collectCounters(t, reader)
	if got := counts["milliways.delegate.outcome.rework"]; got != 1 {
		t.Errorf("rework counter = %d, want 1", got)
	}
	if got := counts["milliways.delegate.outcome.pass"]; got != 0 {
		t.Errorf("pass counter = %d, want 0", got)
	}
}

func TestRecordDelegateOutcomeIncrementsFailCounter(t *testing.T) {
	reader, _ := setupDelegateTestState(t)

	RecordDelegateOutcome(context.Background(), "fail")

	counts := collectCounters(t, reader)
	if got := counts["milliways.delegate.outcome.fail"]; got != 1 {
		t.Errorf("fail counter = %d, want 1", got)
	}
}

func TestRecordDelegateOutcomeIgnoresUnknown(t *testing.T) {
	reader, _ := setupDelegateTestState(t)

	ctx := context.Background()
	RecordDelegateOutcome(ctx, "unknown")
	RecordDelegateOutcome(ctx, "")
	RecordDelegateOutcome(ctx, "PASS")

	counts := collectCounters(t, reader)
	total := counts["milliways.delegate.outcome.pass"] +
		counts["milliways.delegate.outcome.rework"] +
		counts["milliways.delegate.outcome.fail"]
	if total != 0 {
		t.Errorf("total counters = %d, want 0 for unknown outcomes", total)
	}
}

func TestRecordDelegateOutcomeHandlesNilContext(t *testing.T) {
	setupDelegateTestState(t)
	// Must not panic.
	RecordDelegateOutcome(context.Background(), "pass")
	RecordDelegateOutcome(context.Background(), "rework")
	RecordDelegateOutcome(context.Background(), "fail")
}

func TestStartAgentDelegateSpanSetsOutcomeAttribute(t *testing.T) {
	_, exp := setupDelegateTestState(t)

	ctx := context.Background()
	_, span := StartAgentDelegateSpan(ctx, "session-1", "coder-go", "/repo", 100, "pass")
	span.End()

	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("got %d recorded spans, want 1", len(spans))
	}

	found := false
	for _, attr := range spans[0].Attributes {
		if string(attr.Key) == AttrDelegateOutcome && attr.Value.AsString() == "pass" {
			found = true
		}
	}
	if !found {
		t.Errorf("span missing %s=pass attribute; recorded attrs: %v", AttrDelegateOutcome, spans[0].Attributes)
	}
}
