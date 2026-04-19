package tui

import (
	"time"

	"github.com/mwigge/milliways/internal/conversation"
	"github.com/mwigge/milliways/internal/kitchen"
	"github.com/mwigge/milliways/internal/kitchen/adapter"
	"github.com/mwigge/milliways/internal/observability"
	"github.com/mwigge/milliways/internal/pantry"
	"github.com/mwigge/milliways/internal/sommelier"
)

// blockEventMsg carries a normalized adapter event routed to a specific block.
type blockEventMsg struct {
	BlockID string
	Event   adapter.Event
}

// blockRoutedMsg arrives when sommelier decision is made for a block.
type blockRoutedMsg struct {
	BlockID  string
	Decision sommelier.Decision
	Adapt    adapter.Adapter
}

// blockPIDMsg records the OS process id for a started kitchen block.
type blockPIDMsg struct {
	BlockID string
	PID     int
}

// blockDoneMsg signals dispatch completion for a block.
type blockDoneMsg struct {
	BlockID      string
	Result       kitchen.Result
	Decision     sommelier.Decision
	Conversation *conversation.Conversation
	Err          error
	Duration     time.Duration
}

// tickMsg is a tick for elapsed timers on all active blocks.
type tickMsg time.Time

// systemMonitorTickMsg triggers a process stats refresh.
type systemMonitorTickMsg time.Time

// jobsRefreshMsg carries a fresh slice of recent tickets.
type jobsRefreshMsg []pantry.Ticket

// runtimeEventMsg carries a structured runtime event into the TUI model.
type runtimeEventMsg struct {
	Event observability.Event
}

// pipelineStepMsg signals a pipeline step lifecycle change.
type pipelineStepMsg struct {
	blockID    string
	stepID     string
	status     string // "pending", "active", "done", "failed", "skipped"
	durationMs int
}

// pipelineEventMsg carries an adapter event from a pipeline step.
type pipelineEventMsg struct {
	blockID string
	stepID  string
	event   adapter.Event
}
