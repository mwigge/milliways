package tui

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mwigge/milliways/internal/kitchen"
	"github.com/mwigge/milliways/internal/kitchen/adapter"
	"github.com/mwigge/milliways/internal/ledger"
	"github.com/mwigge/milliways/internal/observability"
	"github.com/mwigge/milliways/internal/orchestrator"
	"github.com/mwigge/milliways/internal/pantry"
	"github.com/mwigge/milliways/internal/pipeline"
	"github.com/mwigge/milliways/internal/sommelier"
)

const defaultMaxConcurrent = 4

// startBlockDispatch creates a new Block for the prompt and starts the adapter dispatch.
// Returns the block ID and tea.Cmd to run.
func (m *Model) startBlockDispatch(prompt, kitchenForce string) (string, tea.Cmd) {
	blockID := m.nextBlockID()

	ctx, cancel := context.WithCancel(context.Background())

	block := Block{
		ID:        blockID,
		Prompt:    prompt,
		Kitchen:   kitchenForce,
		State:     StateRouting,
		StartedAt: time.Now(),
		CancelFn:  cancel,
	}

	m.blocks = append(m.blocks, block)
	m.focusedIdx = len(m.blocks) - 1
	m.activeCount++

	return blockID, tea.Batch(
		m.adapterDispatchCmd(ctx, blockID, prompt, kitchenForce),
		tickCmd(),
	)
}

// adapterDispatchCmd returns a tea.Cmd that routes, then streams adapter events for a block.
func (m *Model) adapterDispatchCmd(ctx context.Context, blockID, prompt, kitchenForce string) tea.Cmd {
	return func() tea.Msg {
		if m.providerFactory == nil {
			return blockDoneMsg{
				BlockID: blockID,
				Result:  kitchen.Result{ExitCode: 1},
				Err:     fmt.Errorf("provider factory is nil"),
			}
		}

		sink := m.sink
		var sinks observability.MultiSink
		if m.pdb != nil {
			sinks = append(sinks, ledger.NewLedgerSink(m.pdb))
		}
		if m.prog != nil && *m.prog != nil {
			sinks = append(sinks, observability.FuncSink(func(evt observability.Event) {
				(*m.prog).Send(runtimeEventMsg{Event: evt})
			}))
		}
		if len(sinks) > 0 {
			if m.sink != nil {
				sinks = append([]observability.Sink{m.sink}, sinks...)
			}
			sink = sinks
		}
		orch := orchestrator.Orchestrator{
			Factory: m.providerFactory,
			Hydrate: m.hydrator,
			Sink:    sink,
		}
		var lastCost *adapter.CostInfo
		exitCode := 0
		var lastDecision sommelier.Decision

		conv, err := orch.Run(ctx, orchestrator.RunRequest{
			ConversationID: blockID,
			BlockID:        blockID,
			Prompt:         prompt,
			KitchenForce:   kitchenForce,
		}, func(res orchestrator.RouteResult) {
			lastDecision = res.Decision
			if m.prog != nil && *m.prog != nil {
				(*m.prog).Send(blockRoutedMsg{
					BlockID:  blockID,
					Decision: res.Decision,
					Adapt:    res.Adapter,
				})
			}
		}, func(evt adapter.Event) {
			if evt.Type == adapter.EventCost {
				lastCost = evt.Cost
			}
			if evt.Type == adapter.EventDone {
				exitCode = evt.ExitCode
				return
			}
			if m.prog != nil && *m.prog != nil {
				(*m.prog).Send(blockEventMsg{BlockID: blockID, Event: evt})
			}
		})
		if err != nil {
			return blockDoneMsg{
				BlockID:      blockID,
				Result:       kitchen.Result{ExitCode: 1},
				Decision:     lastDecision,
				Conversation: conv,
				Err:          err,
			}
		}

		result := kitchen.Result{ExitCode: exitCode}
		var dur time.Duration
		if lastCost != nil {
			dur = time.Duration(lastCost.DurationMs) * time.Millisecond
		}

		return blockDoneMsg{
			BlockID:      blockID,
			Result:       result,
			Decision:     lastDecision,
			Conversation: conv,
			Duration:     dur,
		}
	}
}

// startPipelineBlockDispatch plans and executes a multi-kitchen pipeline in a block.
func (m *Model) startPipelineBlockDispatch(ctx context.Context, blockID, prompt string) tea.Cmd {
	return func() tea.Msg {
		pipe, err := m.planner.Plan(ctx, prompt)
		if err != nil {
			return blockDoneMsg{
				BlockID: blockID,
				Result:  kitchen.Result{ExitCode: 1},
				Err:     err,
			}
		}

		if pipe == nil {
			return blockDoneMsg{
				BlockID: blockID,
				Result:  kitchen.Result{ExitCode: 0},
			}
		}

		// Notify TUI of pipeline steps.
		if m.prog != nil && *m.prog != nil {
			for _, step := range pipe.Steps {
				(*m.prog).Send(pipelineStepMsg{
					blockID: blockID,
					stepID:  step.ID,
					status:  string(pipeline.StatusPending),
				})
			}
		}

		onEvent := func(stepID string, evt adapter.Event) {
			if m.prog != nil && *m.prog != nil {
				(*m.prog).Send(pipelineEventMsg{blockID: blockID, stepID: stepID, event: evt})
			}
		}

		summarizeStep := pipe.StepByID("summarize")

		wrappedFactory := func(fctx context.Context, kitchenName string) (adapter.Adapter, error) {
			return m.adapterFactory(fctx, kitchenName)
		}

		executor := pipeline.NewExecutor(wrappedFactory, onEvent, func(stepID string, status pipeline.StepStatus) {
			msg := pipelineStepMsg{blockID: blockID, stepID: stepID, status: string(status)}
			if m.prog != nil && *m.prog != nil {
				(*m.prog).Send(msg)
			}

			if summarizeStep != nil && stepID != "summarize" && status == pipeline.StatusDone || status == pipeline.StatusFailed {
				allDone := true
				var fanOutSteps []*pipeline.Step
				for _, s := range pipe.Steps {
					if s.ID == "summarize" {
						continue
					}
					fanOutSteps = append(fanOutSteps, s)
					if s.Status == pipeline.StatusPending || s.Status == pipeline.StatusActive {
						allDone = false
					}
				}
				if allDone && summarizeStep.Prompt == "" {
					summarizeStep.Prompt = pipeline.BuildSummarizePrompt(prompt, fanOutSteps)
				}
			}
		})

		start := time.Now()
		execErr := executor.Run(ctx, pipe)
		dur := time.Since(start)

		exitCode := 0
		if execErr != nil || pipe.Status == pipeline.StatusFailed {
			exitCode = 1
		}

		return blockDoneMsg{
			BlockID:  blockID,
			Result:   kitchen.Result{ExitCode: exitCode},
			Decision: sommelier.Decision{Kitchen: "pipeline", Tier: "pipeline"},
			Err:      execErr,
			Duration: dur,
		}
	}
}

// submitOverlay handles overlay input submission — routes to the focused block's adapter.
func (m *Model) submitOverlay() tea.Cmd {
	value := m.overlayInput.Value()
	m.overlayActive = false
	m.overlayInput.Blur()

	b := m.focusedBlock()

	switch m.overlayMode {
	case OverlayQuestion:
		if b != nil {
			b.State = StateStreaming
			if b.ActiveAdapter != nil {
				if err := b.ActiveAdapter.Send(context.Background(), value); err != nil {
					b.AppendEvent(adapter.Event{
						Type:    adapter.EventText,
						Kitchen: "milliways",
						Text:    "[milliways] kitchen does not support dialogue — auto-answered",
					})
				}
			}
		}
	case OverlayContextInject:
		if b != nil && b.ActiveAdapter != nil {
			if err := b.ActiveAdapter.Send(context.Background(), value); err != nil {
				b.AppendEvent(adapter.Event{
					Type:    adapter.EventText,
					Kitchen: "milliways",
					Text:    "[milliways] kitchen does not support context injection",
				})
			}
		}
		if b != nil {
			b.AppendEvent(adapter.Event{
				Type:    adapter.EventText,
				Kitchen: "milliways",
				Text:    "[+context] " + value,
			})
		}
	}

	m.overlayMode = OverlayNone
	return nil
}

func tickCmd() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// jobsRefreshCmd fetches recent tickets and returns a jobsRefreshMsg.
func jobsRefreshCmd(store *pantry.TicketStore) tea.Cmd {
	return func() tea.Msg {
		if store == nil {
			return jobsRefreshMsg(nil)
		}
		tickets, err := store.ListRecent(8)
		if err != nil {
			return jobsRefreshMsg(nil)
		}
		return jobsRefreshMsg(tickets)
	}
}

// scheduleJobsRefresh returns a command that fires jobsRefreshCmd after 5 s.
func scheduleJobsRefresh(store *pantry.TicketStore) tea.Cmd {
	return tea.Tick(5*time.Second, func(_ time.Time) tea.Msg {
		if store == nil {
			return jobsRefreshMsg(nil)
		}
		tickets, err := store.ListRecent(8)
		if err != nil {
			return jobsRefreshMsg(nil)
		}
		return jobsRefreshMsg(tickets)
	})
}
