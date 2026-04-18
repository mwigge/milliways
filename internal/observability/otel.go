package observability

import (
	"context"
	"io"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutmetric"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

const instrumentationName = "github.com/mwigge/milliways/internal/observability"

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
	traceExporter, err := stdouttrace.New(stdouttrace.WithWriter(io.Discard))
	if err != nil {
		return otelState{}, err
	}
	metricExporter, err := stdoutmetric.New(stdoutmetric.WithWriter(io.Discard))
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
	}, nil

}

func newNoopOTelState() otelState {
	meter := otel.GetMeterProvider().Meter(instrumentationName)
	dispatchTotal, _ := meter.Int64Counter("milliways.dispatch.total")
	dispatchDuration, _ := meter.Float64Histogram("milliways.dispatch.duration_seconds", metric.WithUnit("s"))
	failoverTotal, _ := meter.Int64Counter("milliways.failover.total")

	return otelState{
		tracer:           trace.NewNoopTracerProvider().Tracer(instrumentationName),
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
	segmentKitchen := firstNonEmpty(evt.Provider, "unknown")

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
	fromKitchen := firstNonEmpty(
		fieldValue(evt.Fields, "from_kitchen"),
		fieldValue(evt.Fields, "from"),
		evt.Provider,
		"unknown",
	)
	toKitchen := firstNonEmpty(
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
