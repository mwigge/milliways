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
	"fmt"
	"os"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutmetric"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

const instrumentationName = "github.com/mwigge/milliways/internal/observability"

const (
	// SpanProviderSend wraps provider request/response work.
	SpanProviderSend = "milliways.provider.send"
	// SpanMemorySearch wraps MemPalace search work.
	SpanMemorySearch = "milliways.memory.search"
	// SpanMemoryWrite wraps MemPalace write work.
	SpanMemoryWrite = "milliways.memory.write"
	// SpanSessionCompact wraps session compaction work.
	SpanSessionCompact = "milliways.session.compact"
	// SpanToolPrefix prefixes tool spans.
	SpanToolPrefix = "milliways.tool."
	// SpanHookPrefix prefixes hook spans.
	SpanHookPrefix = "milliways.hook."
	// SpanAgentThink wraps agent reasoning spans.
	SpanAgentThink = "agent.think"
	// SpanAgentDelegate wraps agent delegation spans.
	SpanAgentDelegate = "agent.delegate"
	// SpanAgentTool wraps agent tool usage spans.
	SpanAgentTool = "agent.tool"
	// SpanAgentObserve wraps agent observation spans.
	SpanAgentObserve = "agent.observe"
	// SpanAgentDecide wraps agent decision spans.
	SpanAgentDecide = "agent.decide"
	// AttrAgentID identifies the agent trace session.
	AttrAgentID = "ai.agent.id"
	// AttrAgentModel identifies the active model.
	AttrAgentModel = "ai.agent.model"
	// AttrAgentTier identifies the active execution tier.
	AttrAgentTier = "ai.agent.tier"
	// AttrAgentReasoning stores a reasoning summary.
	AttrAgentReasoning = "ai.agent.reasoning"
	// AttrDelegateAgent identifies the delegated agent.
	AttrDelegateAgent = "ai.delegate.agent"
	// AttrDelegateTask identifies the delegated task.
	AttrDelegateTask = "ai.delegate.task"
	// AttrDelegateDur stores delegation duration in milliseconds.
	AttrDelegateDur = "ai.delegate.duration_ms"
	// AttrDelegateOutcome stores the delegation outcome.
	AttrDelegateOutcome = "ai.delegate.outcome"
	// AttrToolName identifies the tool used.
	AttrToolName = "ai.tool.name"
	// AttrToolDur stores tool duration in milliseconds.
	AttrToolDur = "ai.tool.duration_ms"
	// AttrToolBlocked indicates whether a tool call was blocked.
	AttrToolBlocked = "ai.tool.blocked"
	// AttrObserveType identifies the observation type.
	AttrObserveType = "ai.observe.type"
	// AttrDecisionChoice identifies the chosen decision.
	AttrDecisionChoice = "ai.decision.choice"
	// AttrDecisionOptions stores available decision options.
	AttrDecisionOptions = "ai.decision.options"
)

var (
	otelOnce        sync.Once
	otelGlobalState = newNoopOTelState()
	otelInitErr     error
	otelInit        = defaultOTelInit
)

type otelState struct {
	tracerProvider   *sdktrace.TracerProvider
	meterProvider    *sdkmetric.MeterProvider
	tracer           trace.Tracer
	meter            metric.Meter
	dispatchTotal    metric.Int64Counter
	dispatchDuration metric.Float64Histogram
	failoverTotal    metric.Int64Counter
	// exporterKind records which exporter backend was selected: "otlp" or "stdout".
	exporterKind string
}

type segmentState struct {
	span      trace.Span
	startedAt time.Time
	kitchen   string
}

// OTelSink emits orchestrator events to an OpenTelemetry pipeline.
type OTelSink struct {
	segments sync.Map
}

var _ Sink = (*OTelSink)(nil)

// NewOTelSink returns a sink that lazily initializes OpenTelemetry on first use.
func NewOTelSink() (Sink, error) {
	return &OTelSink{}, nil
}

// MustOtel initializes the OpenTelemetry pipeline once and returns the init result.
func MustOtel() error {
	otelOnce.Do(func() {
		state, err := otelInit()
		if err != nil {
			otelGlobalState = newNoopOTelState()
			otelInitErr = err
			return
		}
		otelGlobalState = state
	})
	return otelInitErr
}

// Emit maps runtime events into OpenTelemetry spans and metrics.
func (s *OTelSink) Emit(evt Event) {
	if s == nil {
		return
	}

	_ = MustOtel()
	state := otelGlobalState

	switch evt.Kind {
	case "segment_start":
		s.handleSegmentStart(state, evt)
	case "segment_end":
		s.handleSegmentEnd(state, evt)
	case "failover", "switch":
		s.handleFailover(state, evt)
	}

}

func defaultOTelInit() (otelState, error) {
	endpoint := os.Getenv("MILLIWAYS_OTEL_ENDPOINT")
	if endpoint != "" {
		return defaultOTelInitOTLP(endpoint)
	}
	return defaultOTelInitStdout()
}

// defaultOTelInitOTLP configures OTLP HTTP exporters targeting endpoint.
// The OTLP HTTP exporter appends /v1/traces and /v1/metrics automatically.
// Connections are established lazily on the first flush, so init succeeds
// even when the collector is not yet reachable.
func defaultOTelInitOTLP(endpoint string) (otelState, error) {
	ctx := context.Background()

	traceExporter, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(endpoint))
	if err != nil {
		return otelState{}, fmt.Errorf("otlp trace exporter: %w", err)
	}
	metricExporter, err := otlpmetrichttp.New(ctx, otlpmetrichttp.WithEndpointURL(endpoint))
	if err != nil {
		return otelState{}, fmt.Errorf("otlp metric exporter: %w", err)
	}

	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(traceExporter))
	reader := sdkmetric.NewPeriodicReader(metricExporter)
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

	otel.SetTracerProvider(tracerProvider)
	otel.SetMeterProvider(meterProvider)

	meter := otel.GetMeterProvider().Meter(instrumentationName)
	dispatchTotal, err := meter.Int64Counter("milliways.dispatch.total")
	if err != nil {
		return otelState{}, err
	}
	dispatchDuration, err := meter.Float64Histogram(
		"milliways.dispatch.duration_seconds",
		metric.WithUnit("s"),
	)
	if err != nil {
		return otelState{}, err
	}
	failoverTotal, err := meter.Int64Counter("milliways.failover.total")
	if err != nil {
		return otelState{}, err
	}

	return otelState{
		tracerProvider:   tracerProvider,
		meterProvider:    meterProvider,
		tracer:           otel.GetTracerProvider().Tracer(instrumentationName),
		meter:            meter,
		dispatchTotal:    dispatchTotal,
		dispatchDuration: dispatchDuration,
		failoverTotal:    failoverTotal,
		exporterKind:     "otlp",
	}, nil
}

// defaultOTelInitStdout configures stdout exporters (the original behaviour).
func defaultOTelInitStdout() (otelState, error) {
	traceExporter, err := stdouttrace.New(stdouttrace.WithWriter(os.Stdout))
	if err != nil {
		return otelState{}, err
	}
	metricExporter, err := stdoutmetric.New(stdoutmetric.WithWriter(os.Stdout))
	if err != nil {
		return otelState{}, err
	}

	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(traceExporter))
	reader := sdkmetric.NewPeriodicReader(metricExporter)
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

	otel.SetTracerProvider(tracerProvider)
	otel.SetMeterProvider(meterProvider)

	meter := otel.GetMeterProvider().Meter(instrumentationName)
	dispatchTotal, err := meter.Int64Counter("milliways.dispatch.total")
	if err != nil {
		return otelState{}, err
	}
	dispatchDuration, err := meter.Float64Histogram(
		"milliways.dispatch.duration_seconds",
		metric.WithUnit("s"),
	)
	if err != nil {
		return otelState{}, err
	}
	failoverTotal, err := meter.Int64Counter("milliways.failover.total")
	if err != nil {
		return otelState{}, err
	}

	return otelState{
		tracerProvider:   tracerProvider,
		meterProvider:    meterProvider,
		tracer:           otel.GetTracerProvider().Tracer(instrumentationName),
		meter:            meter,
		dispatchTotal:    dispatchTotal,
		dispatchDuration: dispatchDuration,
		failoverTotal:    failoverTotal,
		exporterKind:     "stdout",
	}, nil
}

func newNoopOTelState() otelState {
	meter := otel.GetMeterProvider().Meter(instrumentationName)
	dispatchTotal, _ := meter.Int64Counter("milliways.dispatch.total")
	dispatchDuration, _ := meter.Float64Histogram("milliways.dispatch.duration_seconds", metric.WithUnit("s"))
	failoverTotal, _ := meter.Int64Counter("milliways.failover.total")

	return otelState{
		tracer:           noop.NewTracerProvider().Tracer(instrumentationName),
		meter:            meter,
		dispatchTotal:    dispatchTotal,
		dispatchDuration: dispatchDuration,
		failoverTotal:    failoverTotal,
	}
}

func (s *OTelSink) handleSegmentStart(state otelState, evt Event) {
	ctx := context.Background()
	attrs := segmentAttributes(evt)
	if tier := fieldValue(evt.Fields, "tier"); tier != "" {
		attrs = append(attrs, attribute.String("tier", tier))
	}
	_, span := state.tracer.Start(ctx, "segment", trace.WithTimestamp(evt.At), trace.WithAttributes(attrs...))
	s.segments.Store(evt.SegmentID, segmentState{
		span:      span,
		startedAt: evt.At,
		kitchen:   evt.Provider,
	})
}

func (s *OTelSink) handleSegmentEnd(state otelState, evt Event) {
	ctx := context.Background()
	status := fieldOr(evt.Fields, "status", "unknown")
	segmentKitchen := otelFirstNonEmpty(evt.Provider, "unknown")

	if value, ok := s.segments.LoadAndDelete(evt.SegmentID); ok {
		seg, ok := value.(segmentState)
		if ok {
			if seg.kitchen != "" {
				segmentKitchen = seg.kitchen
			}
			if !seg.startedAt.IsZero() && !evt.At.IsZero() {
				durationSeconds := evt.At.Sub(seg.startedAt).Seconds()
				if durationSeconds < 0 {
					durationSeconds = 0
				}
				state.dispatchDuration.Record(
					ctx,
					durationSeconds,
					metric.WithAttributes(attribute.String("kitchen", segmentKitchen)),
				)
			}
			seg.span.SetAttributes(attribute.String("exit_code", status))
			seg.span.End(trace.WithTimestamp(evt.At))
		}
	}

	state.dispatchTotal.Add(ctx, 1, metric.WithAttributes(
		attribute.String("kitchen", segmentKitchen),
		attribute.String("exit_code", status),
	))
}

func (s *OTelSink) handleFailover(state otelState, evt Event) {
	fromKitchen := otelFirstNonEmpty(
		fieldValue(evt.Fields, "from_kitchen"),
		fieldValue(evt.Fields, "from"),
		evt.Provider,
		"unknown",
	)
	toKitchen := otelFirstNonEmpty(
		fieldValue(evt.Fields, "to_kitchen"),
		fieldValue(evt.Fields, "to"),
		"unknown",
	)
	state.failoverTotal.Add(context.Background(), 1, metric.WithAttributes(
		attribute.String("from_kitchen", fromKitchen),
		attribute.String("to_kitchen", toKitchen),
	))
}

func segmentAttributes(evt Event) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("conversation_id", evt.ConversationID),
		attribute.String("block_id", evt.BlockID),
		attribute.String("segment_id", evt.SegmentID),
		attribute.String("kitchen", evt.Provider),
		attribute.String("kind", evt.Kind),
	}
}

func fieldValue(fields map[string]string, key string) string {
	if fields == nil {
		return ""
	}
	return fields[key]
}

func fieldOr(fields map[string]string, key, fallback string) string {
	if value := fieldValue(fields, key); value != "" {
		return value
	}
	return fallback
}

func otelFirstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// StartSpan starts a named span on the shared tracer.
func StartSpan(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	if ctx == nil {
		ctx = context.Background()
	}
	_ = MustOtel()
	return otelGlobalState.tracer.Start(ctx, name, trace.WithAttributes(attrs...))
}

// SpanFromCtx returns the current span from context.
func SpanFromCtx(ctx context.Context) trace.Span {
	if ctx == nil {
		ctx = context.Background()
	}
	return trace.SpanFromContext(ctx)
}

// AddEvent adds an event to the current span if one is present.
func AddEvent(ctx context.Context, name string, attrs ...attribute.KeyValue) {
	span := SpanFromCtx(ctx)
	if span == nil {
		return
	}
	span.AddEvent(name, trace.WithAttributes(attrs...))
}

// StartProviderSendSpan starts a provider send span.
func StartProviderSendSpan(ctx context.Context, model string, inputTokens, outputTokens int) (context.Context, trace.Span) {
	return StartSpan(ctx, SpanProviderSend,
		attribute.String("model", model),
		attribute.Int("tokens.input", inputTokens),
		attribute.Int("tokens.output", outputTokens),
	)
}

// StartMemorySearchSpan starts a MemPalace search span.
func StartMemorySearchSpan(ctx context.Context, query string, limit int) (context.Context, trace.Span) {
	return StartSpan(ctx, SpanMemorySearch,
		attribute.String("query", query),
		attribute.Int("limit", limit),
	)
}

// StartMemoryWriteSpan starts a MemPalace write span.
func StartMemoryWriteSpan(ctx context.Context, wing, room string) (context.Context, trace.Span) {
	return StartSpan(ctx, SpanMemoryWrite,
		attribute.String("wing", wing),
		attribute.String("room", room),
	)
}

// StartToolSpan starts a tool execution span.
func StartToolSpan(ctx context.Context, toolName string) (context.Context, trace.Span) {
	return StartSpan(ctx, SpanToolPrefix+toolName, attribute.String("tool.name", toolName))
}

// StartHookSpan starts a hook span.
func StartHookSpan(ctx context.Context, operation string, blocked bool) (context.Context, trace.Span) {
	return StartSpan(ctx, SpanHookPrefix+operation, attribute.Bool("hook.blocked", blocked))
}

// StartSessionCompactSpan starts a compaction span.
func StartSessionCompactSpan(ctx context.Context, sessionID string) (context.Context, trace.Span) {
	return StartSpan(ctx, SpanSessionCompact, attribute.String("session.id", sessionID))
}

// StartAgentThinkSpan starts an agent reasoning span.
func StartAgentThinkSpan(ctx context.Context, sessionID, reasoning string) (context.Context, trace.Span) {
	return StartSpan(ctx, SpanAgentThink,
		attribute.String(AttrAgentID, sessionID),
		attribute.String(AttrAgentReasoning, reasoning),
	)
}

// StartAgentDelegateSpan starts an agent delegation span.
func StartAgentDelegateSpan(ctx context.Context, sessionID, agent, task string, durMS int, outcome string) (context.Context, trace.Span) {
	return StartSpan(ctx, SpanAgentDelegate,
		attribute.String(AttrAgentID, sessionID),
		attribute.String(AttrDelegateAgent, agent),
		attribute.String(AttrDelegateTask, task),
		attribute.Int(AttrDelegateDur, durMS),
		attribute.String(AttrDelegateOutcome, outcome),
	)
}

// StartAgentToolSpan starts an agent tool usage span.
func StartAgentToolSpan(ctx context.Context, sessionID, toolName string, durMS int, blocked bool) (context.Context, trace.Span) {
	return StartSpan(ctx, SpanAgentTool,
		attribute.String(AttrAgentID, sessionID),
		attribute.String(AttrToolName, toolName),
		attribute.Int(AttrToolDur, durMS),
		attribute.Bool(AttrToolBlocked, blocked),
	)
}

// StartAgentObserveSpan starts an agent observation span.
func StartAgentObserveSpan(ctx context.Context, sessionID, obsType, content string) (context.Context, trace.Span) {
	return StartSpan(ctx, SpanAgentObserve,
		attribute.String(AttrAgentID, sessionID),
		attribute.String(AttrObserveType, obsType),
		attribute.String("content", content),
	)
}

// StartAgentDecideSpan starts an agent decision span.
func StartAgentDecideSpan(ctx context.Context, sessionID string, options []string, choice string) (context.Context, trace.Span) {
	return StartSpan(ctx, SpanAgentDecide,
		attribute.String(AttrAgentID, sessionID),
		attribute.StringSlice(AttrDecisionOptions, append([]string(nil), options...)),
		attribute.String(AttrDecisionChoice, choice),
	)
}

// Shutdown flushes and stops the shared providers.
func Shutdown(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	var err error
	if otelGlobalState.meterProvider != nil {
		if shutdownErr := otelGlobalState.meterProvider.Shutdown(ctx); shutdownErr != nil {
			err = shutdownErr
		}
	}
	if otelGlobalState.tracerProvider != nil {
		if shutdownErr := otelGlobalState.tracerProvider.Shutdown(ctx); shutdownErr != nil && err == nil {
			err = shutdownErr
		}
	}
	return err
}
