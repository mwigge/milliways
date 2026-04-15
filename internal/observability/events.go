package observability

import "time"

// Event is a structured runtime event in Milliways.
type Event struct {
	ID             string
	ConversationID string
	BlockID        string
	SegmentID      string
	Kind           string
	Provider       string
	Text           string
	At             time.Time
	Fields         map[string]string
}

// Sink consumes runtime events.
type Sink interface {
	Emit(Event)
}

// NopSink discards runtime events.
type NopSink struct{}

// Emit discards the event.
func (NopSink) Emit(Event) {}

// FuncSink adapts a function to a Sink.
type FuncSink func(Event)

// Emit forwards the event to the wrapped function.
func (f FuncSink) Emit(evt Event) {
	if f != nil {
		f(evt)
	}
}

// MultiSink fans out events to multiple sinks.
type MultiSink []Sink

// Emit forwards the event to each sink.
func (m MultiSink) Emit(evt Event) {
	for _, sink := range m {
		if sink != nil {
			sink.Emit(evt)
		}
	}
}
