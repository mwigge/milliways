package adapter

import "time"

// EventType enumerates the kinds of events a kitchen adapter can emit.
type EventType int

const (
	EventText      EventType = iota // Plain text line from kitchen
	EventCodeBlock                  // Fenced code block (language + content)
	EventToolUse                    // Kitchen invoked a tool (name + status)
	EventQuestion                   // Kitchen needs free-text answer
	EventConfirm                    // Kitchen needs y/N confirmation
	EventCost                       // Cost/usage data from kitchen
	EventRateLimit                  // Rate limit or quota signal
	EventError                      // Kitchen-side error
	EventDone                       // Kitchen finished (carries exit code)
)

// String returns the event type name.
func (t EventType) String() string {
	switch t {
	case EventText:
		return "text"
	case EventCodeBlock:
		return "code_block"
	case EventToolUse:
		return "tool_use"
	case EventQuestion:
		return "question"
	case EventConfirm:
		return "confirm"
	case EventCost:
		return "cost"
	case EventRateLimit:
		return "rate_limit"
	case EventError:
		return "error"
	case EventDone:
		return "done"
	default:
		return "unknown"
	}
}

// Event is a single normalized event from any kitchen adapter.
type Event struct {
	Type       EventType
	Kitchen    string
	Text       string // for Text, Question, Confirm, Error
	Language   string // for CodeBlock — e.g. "go", "python"
	Code       string // for CodeBlock — the code content
	ToolName   string // for ToolUse — e.g. "Edit", "Bash"
	ToolStatus string // for ToolUse — "started", "done", "failed"
	Cost       *CostInfo
	RateLimit  *RateLimitInfo
	ExitCode   int // for EventDone
}

// CostInfo contains cost and token usage data from a kitchen.
type CostInfo struct {
	USD          float64
	InputTokens  int
	OutputTokens int
	CacheRead    int
	CacheWrite   int
	DurationMs   int
}

// RateLimitInfo contains rate limit status from a kitchen.
type RateLimitInfo struct {
	Status        string    // "allowed", "exhausted", "warning"
	ResetsAt      time.Time // when quota resets
	Kitchen       string    // which kitchen is affected
	IsExhaustion  bool
	RawText       string
	DetectionKind string // structured, assistant_text, stdout_text, stderr_text
}
