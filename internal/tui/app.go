package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mwigge/milliways/internal/conversation"
	"github.com/mwigge/milliways/internal/kitchen/adapter"
	"github.com/mwigge/milliways/internal/observability"
	"github.com/mwigge/milliways/internal/orchestrator"
	"github.com/mwigge/milliways/internal/pantry"
	"github.com/mwigge/milliways/internal/pipeline"
)

// ProviderFactory creates provider adapters for orchestrated dispatch.
type ProviderFactory = orchestrator.ProviderFactory

// ConversationRecorder records completed conversations outside the TUI package.
type ConversationRecorder func(prompt string, duration float64, exitCode int, conv *conversation.Conversation)

// ConversationReplayer restores conversation state and activity from persisted runtime data.
type ConversationReplayer func(conversationID, blockID, prompt string, exitCode int) (*conversation.Conversation, []observability.Event, error)

// Model is the main Bubble Tea application model for the Milliways TUI.
type Model struct {
	input      textinput.Model
	output     viewport.Model
	width      int
	height     int
	renderMode RenderMode

	// Block-based dispatch.
	blocks        []Block
	focusedIdx    int
	maxConcurrent int
	activeCount   int
	blockCounter  int

	providerFactory ProviderFactory
	hydrator        orchestrator.ContextHydrator
	sink            observability.Sink
	recorder        ConversationRecorder
	replayer        ConversationReplayer
	prog            **tea.Program

	history    []string
	historyIdx int
	ready      bool

	// Jobs panel (async tickets from pantry).
	jobTickets  []pantry.Ticket
	ticketStore *pantry.TicketStore

	// Dialogue overlay.
	overlayInput  textinput.Model
	overlayActive bool
	overlayMode   OverlayMode

	// Task queue for overflow beyond maxConcurrent.
	queue taskQueue

	// Command palette state.
	palette PaletteState
	// Fuzzy history search state.
	search SearchState

	// Pipeline orchestration support.
	planner        *pipeline.Planner
	adapterFactory pipeline.AdapterFactory

	// Kitchen status for status bar.
	kitchenStates []KitchenState

	// Structured runtime activity for transparency.
	runtimeEvents []observability.Event

	// Run target chooser state.
	runTargets          []RunTargetOption
	runTargetSelected   int
	pendingPrompt       string
	pendingKitchenForce string
}

// NewModel creates the TUI model.
func NewModel(store *pantry.TicketStore) Model {
	ti := textinput.New()
	ti.Placeholder = "Type a task... (@kitchen to force, Ctrl+D to exit)"
	ti.Focus()
	ti.CharLimit = 500
	ti.Width = 80
	ti.Prompt = "▶ "
	ti.PromptStyle = promptStyle
	ti.TextStyle = inputStyle

	vp := viewport.New(80, 20)
	vp.SetContent("")

	return Model{
		input:         ti,
		output:        vp,
		historyIdx:    -1,
		ticketStore:   store,
		prog:          new(*tea.Program),
		maxConcurrent: defaultMaxConcurrent,
	}
}

// NewAdapterModel creates the TUI model with adapter-based dispatch.
func NewAdapterModel(providerFactory ProviderFactory, hydrator orchestrator.ContextHydrator, sink observability.Sink, recorder ConversationRecorder, replayer ConversationReplayer, store *pantry.TicketStore) Model {
	m := NewModel(store)
	m.providerFactory = providerFactory
	m.hydrator = hydrator
	m.sink = sink
	m.recorder = recorder
	m.replayer = replayer
	return m
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, jobsRefreshCmd(m.ticketStore))
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.output.Width = msg.Width - 30
		m.output.Height = msg.Height - 6
		m.input.Width = msg.Width - 4
		m.ready = true

	case tea.KeyMsg:
		cmds = append(cmds, m.handleKey(msg)...)

	case blockRoutedMsg:
		if b := m.findBlock(msg.BlockID); b != nil {
			b.State = StateRouted
			b.ActiveAdapter = msg.Adapt
			b.Kitchen = msg.Decision.Kitchen
			b.Decision = msg.Decision
			b.ConversationID = msg.BlockID
			if msg.Decision.Kitchen != "" && !containsProvider(b.ProviderChain, msg.Decision.Kitchen) {
				b.ProviderChain = append(b.ProviderChain, msg.Decision.Kitchen)
			}
		}

	case blockEventMsg:
		if b := m.findBlock(msg.BlockID); b != nil {
			b.AppendEvent(msg.Event)

			switch msg.Event.Type {
			case adapter.EventText, adapter.EventCodeBlock:
				if b.State == StateRouted {
					b.State = StateStreaming
				}
			case adapter.EventQuestion:
				b.State = StateAwaiting
				m.focusedIdx = m.blockIndex(msg.BlockID)
				m.overlayActive = true
				m.overlayMode = OverlayQuestion
				m.overlayInput = textinput.New()
				m.overlayInput.Placeholder = msg.Event.Text
				m.overlayInput.Focus()
				cmds = append(cmds, textinput.Blink)
			case adapter.EventConfirm:
				b.State = StateConfirming
				m.focusedIdx = m.blockIndex(msg.BlockID)
			}
		}

	case blockDoneMsg:
		if b := m.findBlock(msg.BlockID); b != nil {
			exitCode := msg.Result.ExitCode
			if msg.Err != nil {
				if exitCode == 0 {
					exitCode = 1
				}
			}

			var cost *adapter.CostInfo
			if msg.Duration > 0 {
				cost = &adapter.CostInfo{DurationMs: int(msg.Duration.Milliseconds())}
			}
			b.Complete(exitCode, cost)
			if msg.Conversation != nil {
				b.Conversation = msg.Conversation
				if b.ConversationID == "" {
					b.ConversationID = msg.Conversation.ID
				}
			}
			if m.recorder != nil && msg.Conversation != nil {
				m.recorder(b.Prompt, b.elapsed().Seconds(), exitCode, msg.Conversation)
			}
			m.activeCount--
			if m.activeCount < 0 {
				m.activeCount = 0
			}

			// Dequeue next if we have capacity.
			if task, ok := m.queue.Dequeue(); ok && m.activeCount < m.maxConcurrent {
				_, cmd := m.startBlockDispatch(task.Prompt, task.KitchenForce)
				cmds = append(cmds, cmd)
			}
		}

	case tickMsg:
		hasActive := false
		for _, b := range m.blocks {
			if b.IsActive() {
				hasActive = true
				break
			}
		}
		if hasActive {
			cmds = append(cmds, tickCmd())
		}

	case pipelineStepMsg:
		if b := m.findBlock(msg.blockID); b != nil {
			// Store pipeline steps as system lines for visibility.
			b.AppendEvent(adapter.Event{
				Type:    adapter.EventText,
				Kitchen: "pipeline",
				Text:    fmt.Sprintf("[step:%s] %s", msg.stepID, msg.status),
			})
		}

	case pipelineEventMsg:
		if b := m.findBlock(msg.blockID); b != nil {
			evt := msg.event
			evt.Kitchen = fmt.Sprintf("[%s] %s", msg.stepID, evt.Kitchen)
			b.AppendEvent(evt)
		}

	case jobsRefreshMsg:
		m.jobTickets = []pantry.Ticket(msg)
		return m, tea.Batch(scheduleJobsRefresh(m.ticketStore))

	case runtimeEventMsg:
		m.runtimeEvents = append(m.runtimeEvents, msg.Event)
		if len(m.runtimeEvents) > 100 {
			m.runtimeEvents = append([]observability.Event(nil), m.runtimeEvents[len(m.runtimeEvents)-100:]...)
		}
	}

	// Update input or overlay.
	var inputCmd tea.Cmd
	if m.overlayActive {
		if m.overlayMode != OverlayRunIn {
			m.overlayInput, inputCmd = m.overlayInput.Update(msg)
		}

		// Live-filter palette/search as user types.
		if m.overlayMode == OverlayPalette {
			query := m.overlayInput.Value()
			m.palette.Query = query
			m.palette.Matches = FilterPalette(query)
			if m.palette.Selected >= len(m.palette.Matches) {
				m.palette.Selected = 0
			}
		}
		if m.overlayMode == OverlaySearch {
			query := m.overlayInput.Value()
			m.search.Query = query
			entries := BuildHistoryFromBlocks(m.blocks)
			for i := len(m.history) - 1; i >= 0; i-- {
				entries = append(entries, HistoryEntry{Prompt: m.history[i]})
			}
			m.search.Matches = FilterHistory(entries, query)
			if m.search.Selected >= len(m.search.Matches) {
				m.search.Selected = 0
			}
		}
	} else {
		m.input, inputCmd = m.input.Update(msg)

		// Detect `/` at start to open command palette.
		val := m.input.Value()
		if val == "/" {
			m.input.SetValue("")
			m.palette = PaletteState{
				Active:  true,
				Matches: FilterPalette(""),
			}
			m.overlayActive = true
			m.overlayMode = OverlayPalette
			m.overlayInput = textinput.New()
			m.overlayInput.Placeholder = "command..."
			m.overlayInput.Focus()
			cmds = append(cmds, textinput.Blink)
		}
	}
	cmds = append(cmds, inputCmd)

	// Update viewport.
	var vpCmd tea.Cmd
	m.output, vpCmd = m.output.Update(msg)
	cmds = append(cmds, vpCmd)

	return m, tea.Batch(cmds...)
}

// handleKey processes key messages and returns commands.
func (m *Model) handleKey(msg tea.KeyMsg) []tea.Cmd {
	var cmds []tea.Cmd

	switch msg.String() {
	case "ctrl+d":
		return []tea.Cmd{tea.Quit}

	case "ctrl+c":
		// Cancel focused block if active; otherwise quit.
		if b := m.focusedBlock(); b != nil && b.IsActive() {
			if b.CancelFn != nil {
				b.CancelFn()
			}
			b.State = StateCancelled
			return nil
		}
		return []tea.Cmd{tea.Quit}

	case "enter":
		if m.overlayActive && m.overlayMode == OverlayRunIn {
			return m.handleRunTargetSelection()
		}
		// Palette selection.
		if m.overlayActive && m.overlayMode == OverlayPalette {
			command := resolvePaletteCommand(strings.TrimSpace(m.overlayInput.Value()), "")
			if command == "" && m.palette.Selected >= 0 && m.palette.Selected < len(m.palette.Matches) {
				command = m.palette.Matches[m.palette.Selected].Command
			}
			if command != "" {
				cmd := m.executePaletteCommand(command)
				m.overlayActive = false
				m.overlayMode = OverlayNone
				m.palette.Active = false
				m.input.Focus()
				if cmd != nil {
					return []tea.Cmd{cmd}
				}
			}
			return nil
		}
		// Search selection.
		if m.overlayActive && m.overlayMode == OverlaySearch {
			if m.search.Selected >= 0 && m.search.Selected < len(m.search.Matches) {
				m.input.SetValue(m.search.Matches[m.search.Selected].Prompt)
			}
			m.overlayActive = false
			m.overlayMode = OverlayNone
			m.search.Active = false
			m.input.Focus()
			return nil
		}
		if m.overlayActive {
			return []tea.Cmd{m.submitOverlay()}
		}
		prompt := m.input.Value()
		if prompt == "" {
			return nil
		}

		// Parse @kitchen prefix.
		kitchenForce, cleanPrompt := parseKitchenForce(prompt)
		if kitchenForce == "" && !strings.HasPrefix(prompt, "!pipeline ") {
			m.openRunTargetChooser(cleanPrompt)
			m.input.SetValue("")
			return nil
		}
		m.history = append(m.history, prompt)
		m.historyIdx = -1
		m.input.SetValue("")

		// Check for !pipeline prefix.
		if strings.HasPrefix(prompt, "!pipeline ") && m.planner != nil {
			pipelinePrompt := strings.TrimPrefix(prompt, "!pipeline ")
			blockID, _ := m.startBlockDispatch(pipelinePrompt, "pipeline")
			ctx, cancel := context.WithCancel(context.Background())
			if b := m.findBlock(blockID); b != nil {
				b.CancelFn = cancel
			} else {
				cancel()
			}
			return []tea.Cmd{
				m.startPipelineBlockDispatch(ctx, blockID, pipelinePrompt),
				tickCmd(),
			}
		}

		// Concurrent dispatch: start immediately or queue.
		if m.activeCount < m.maxConcurrent {
			_, cmd := m.startBlockDispatch(cleanPrompt, kitchenForce)
			return []tea.Cmd{cmd}
		}

		// Queue overflow.
		ok := m.queue.Enqueue(QueuedTask{
			Prompt:       cleanPrompt,
			KitchenForce: kitchenForce,
			QueuedAt:     time.Now(),
		})
		if ok {
			if b := m.focusedBlock(); b != nil {
				b.AppendEvent(adapter.Event{
					Type:    adapter.EventText,
					Kitchen: "milliways",
					Text:    fmt.Sprintf("[queued] position %d", m.queue.Len()),
				})
			}
		} else {
			// Queue full — append system message to last block.
			if b := m.focusedBlock(); b != nil {
				b.AppendEvent(adapter.Event{
					Type:    adapter.EventText,
					Kitchen: "milliways",
					Text:    "[queue full] cannot queue more tasks (max 20)",
				})
			}
		}
		return nil

	case "y":
		if b := m.focusedBlock(); b != nil && b.State == StateConfirming {
			b.State = StateStreaming
			if b.ActiveAdapter != nil {
				_ = b.ActiveAdapter.Send(context.Background(), "y")
			}
			return nil
		}

	case "n":
		if b := m.focusedBlock(); b != nil && b.State == StateConfirming {
			b.State = StateStreaming
			if b.ActiveAdapter != nil {
				_ = b.ActiveAdapter.Send(context.Background(), "n")
			}
			return nil
		}

	case "ctrl+i":
		if b := m.focusedBlock(); b != nil && b.State == StateStreaming && !m.overlayActive {
			m.overlayActive = true
			m.overlayMode = OverlayContextInject
			m.overlayInput = textinput.New()
			m.overlayInput.Placeholder = "+ context:"
			m.overlayInput.Focus()
			return []tea.Cmd{textinput.Blink}
		}

	case "ctrl+f":
		if !m.overlayActive && m.hasCompletedBlocks() {
			m.overlayActive = true
			m.overlayMode = OverlayFeedback
			return nil
		}

	case "ctrl+s":
		if !m.overlayActive {
			m.overlayActive = true
			m.overlayMode = OverlaySummary
			return nil
		}

	case "g":
		if m.overlayActive && m.overlayMode == OverlayFeedback {
			m.rateLastDispatch(true)
			m.overlayActive = false
			m.overlayMode = OverlayNone
			return nil
		}
	case "b":
		if m.overlayActive && m.overlayMode == OverlayFeedback {
			m.rateLastDispatch(false)
			m.overlayActive = false
			m.overlayMode = OverlayNone
			return nil
		}
	case "s":
		if m.overlayActive && m.overlayMode == OverlayFeedback {
			m.overlayActive = false
			m.overlayMode = OverlayNone
			return nil
		}
	case "q":
		if m.overlayActive && m.overlayMode == OverlaySummary {
			m.overlayActive = false
			m.overlayMode = OverlayNone
			return nil
		}

	case "c":
		// Toggle collapse on focused block.
		if !m.overlayActive {
			if b := m.focusedBlock(); b != nil {
				b.ToggleCollapse()
			}
			return nil
		}

	case "tab":
		// Cycle focus to next block.
		if len(m.blocks) > 0 && !m.overlayActive {
			m.focusedIdx = (m.focusedIdx + 1) % len(m.blocks)
			return nil
		}

	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		// Jump to block N.
		idx := int(msg.String()[0]-'0') - 1
		if idx < len(m.blocks) && !m.overlayActive {
			m.focusedIdx = idx
			return nil
		}

	case "ctrl+r":
		// Fuzzy history search.
		if !m.overlayActive {
			entries := BuildHistoryFromBlocks(m.blocks)
			// Also add from command history.
			for i := len(m.history) - 1; i >= 0; i-- {
				entries = append(entries, HistoryEntry{Prompt: m.history[i]})
			}
			m.search = SearchState{
				Active:  true,
				Matches: entries,
			}
			m.overlayActive = true
			m.overlayMode = OverlaySearch
			m.overlayInput = textinput.New()
			m.overlayInput.Placeholder = "search history..."
			m.overlayInput.Focus()
			return []tea.Cmd{textinput.Blink}
		}

	case "ctrl+g":
		if m.renderMode == RenderRaw {
			m.renderMode = RenderGlamour
		} else {
			m.renderMode = RenderRaw
		}

	case "up":
		if m.overlayActive && m.overlayMode == OverlayRunIn {
			m.moveRunTargetSelection(-1)
			return nil
		}
		// In palette/search, navigate up.
		if m.overlayActive && m.overlayMode == OverlayPalette {
			if m.palette.Selected > 0 {
				m.palette.Selected--
			}
			return nil
		}
		if m.overlayActive && m.overlayMode == OverlaySearch {
			if m.search.Selected > 0 {
				m.search.Selected--
			}
			return nil
		}
		if !m.overlayActive && len(m.history) > 0 && m.historyIdx < len(m.history)-1 {
			m.historyIdx++
			m.input.SetValue(m.history[len(m.history)-1-m.historyIdx])
		}
	case "down":
		if m.overlayActive && m.overlayMode == OverlayRunIn {
			m.moveRunTargetSelection(1)
			return nil
		}
		// In palette/search, navigate down.
		if m.overlayActive && m.overlayMode == OverlayPalette {
			if m.palette.Selected < len(m.palette.Matches)-1 {
				m.palette.Selected++
			}
			return nil
		}
		if m.overlayActive && m.overlayMode == OverlaySearch {
			if m.search.Selected < len(m.search.Matches)-1 {
				m.search.Selected++
			}
			return nil
		}
		if !m.overlayActive {
			if m.historyIdx > 0 {
				m.historyIdx--
				m.input.SetValue(m.history[len(m.history)-1-m.historyIdx])
			} else {
				m.historyIdx = -1
				m.input.SetValue("")
			}
		}

	case "pgup":
		// Scroll focused block up.
		if !m.overlayActive {
			if b := m.focusedBlock(); b != nil {
				b.ScrollUp(5)
			}
			return nil
		}
	case "pgdown":
		// Scroll focused block down.
		if !m.overlayActive {
			if b := m.focusedBlock(); b != nil {
				b.ScrollDown(5)
			}
			return nil
		}

	case "esc":
		// Close any overlay.
		if m.overlayActive {
			m.overlayActive = false
			m.overlayMode = OverlayNone
			m.palette.Active = false
			m.search.Active = false
			m.pendingPrompt = ""
			m.pendingKitchenForce = ""
			m.input.Focus()
			return nil
		}
	}

	return cmds
}

func (m *Model) openRunTargetChooser(prompt string) {
	m.pendingPrompt = prompt
	m.pendingKitchenForce = ""
	m.runTargets = buildRunTargetOptions(m.kitchenStates)
	m.runTargetSelected = 0
	m.overlayActive = true
	m.overlayMode = OverlayRunIn
	m.input.Blur()
}

func (m *Model) moveRunTargetSelection(delta int) {
	if len(m.runTargets) == 0 || delta == 0 {
		return
	}
	idx := m.runTargetSelected
	for range len(m.runTargets) {
		idx = (idx + delta + len(m.runTargets)) % len(m.runTargets)
		if m.runTargets[idx].Selectable {
			m.runTargetSelected = idx
			return
		}
	}
}

func (m *Model) handleRunTargetSelection() []tea.Cmd {
	if len(m.runTargets) == 0 {
		m.overlayActive = false
		m.overlayMode = OverlayNone
		m.input.Focus()
		return nil
	}
	choice := m.runTargets[m.runTargetSelected]
	if !choice.Selectable {
		return nil
	}
	prompt := m.pendingPrompt
	kitchenForce := choice.Kitchen
	rawPrompt := prompt
	if kitchenForce != "" {
		rawPrompt = "@" + kitchenForce + " " + prompt
	}
	m.history = append(m.history, rawPrompt)
	m.historyIdx = -1
	m.overlayActive = false
	m.overlayMode = OverlayNone
	m.input.Focus()
	m.pendingPrompt = ""
	m.pendingKitchenForce = ""

	if m.activeCount < m.maxConcurrent {
		_, cmd := m.startBlockDispatch(prompt, kitchenForce)
		return []tea.Cmd{cmd}
	}

	ok := m.queue.Enqueue(QueuedTask{
		Prompt:       prompt,
		KitchenForce: kitchenForce,
		QueuedAt:     time.Now(),
	})
	if ok {
		if b := m.focusedBlock(); b != nil {
			b.AppendEvent(adapter.Event{
				Type:    adapter.EventText,
				Kitchen: "milliways",
				Text:    fmt.Sprintf("[queued] position %d", m.queue.Len()),
			})
		}
	} else {
		if b := m.focusedBlock(); b != nil {
			b.AppendEvent(adapter.Event{
				Type:    adapter.EventText,
				Kitchen: "milliways",
				Text:    "[queue full] cannot queue more tasks (max 20)",
			})
		}
	}
	return nil
}

// RunOpts configures the TUI run.
type RunOpts struct {
	ResumeSession string // session name to resume ("" = no resume, "last" = resume last)
	SessionName   string // named session ("" = use "last")
	KitchenStates []KitchenState
}

// Run starts the TUI with adapter-based dispatch.
func Run(providerFactory ProviderFactory, hydrator orchestrator.ContextHydrator, sink observability.Sink, recorder ConversationRecorder, replayer ConversationReplayer, store *pantry.TicketStore) error {
	return RunWithOpts(providerFactory, hydrator, sink, recorder, replayer, store, RunOpts{})
}

// RunWithOpts starts the TUI with adapter-based dispatch and options.
func RunWithOpts(providerFactory ProviderFactory, hydrator orchestrator.ContextHydrator, sink observability.Sink, recorder ConversationRecorder, replayer ConversationReplayer, store *pantry.TicketStore, opts RunOpts) error {
	m := NewAdapterModel(providerFactory, hydrator, sink, recorder, replayer, store)
	m.SetKitchenStates(opts.KitchenStates)

	// Resume session if requested.
	sessionName := opts.SessionName
	if sessionName == "" {
		sessionName = "last"
	}
	if opts.ResumeSession != "" {
		blocks, events, loadErr := m.loadSession(opts.ResumeSession)
		if loadErr == nil && len(blocks) > 0 {
			m.blocks = blocks
			m.runtimeEvents = events
			m.focusedIdx = len(blocks) - 1
		}
	}

	p := tea.NewProgram(
		m,
		tea.WithAltScreen(),
	)
	*m.prog = p
	finalModel, err := p.Run()

	// Auto-save session on clean exit.
	if fm, ok := finalModel.(Model); ok && len(fm.blocks) > 0 {
		_ = SaveSession(sessionName, fm.blocks)
	}

	return err
}

func (m *Model) loadSession(name string) ([]Block, []observability.Event, error) {
	blocks, err := LoadSession(name)
	if err != nil {
		return nil, nil, err
	}

	var events []observability.Event
	for i := range blocks {
		if blocks[i].ConversationID == "" || m.replayer == nil {
			continue
		}
		conv, replayEvents, replayErr := m.replayer(blocks[i].ConversationID, blocks[i].ID, blocks[i].Prompt, blocks[i].ExitCode)
		if replayErr != nil {
			continue
		}
		if conv != nil {
			blocks[i].Conversation = conv
			if len(blocks[i].ProviderChain) == 0 {
				for _, seg := range conv.Segments {
					if seg.Provider != "" && !containsProvider(blocks[i].ProviderChain, seg.Provider) {
						blocks[i].ProviderChain = append(blocks[i].ProviderChain, seg.Provider)
					}
				}
			}
		}
		events = append(events, replayEvents...)
	}

	return blocks, events, nil
}

// SetKitchenStates updates the status bar kitchen states.
func (m *Model) SetKitchenStates(states []KitchenState) {
	m.kitchenStates = states
}

// SetPipelineSupport enables pipeline orchestration on the model.
func (m *Model) SetPipelineSupport(planner *pipeline.Planner, factory pipeline.AdapterFactory) {
	m.planner = planner
	m.adapterFactory = factory
}

// SetMaxConcurrent sets the maximum number of concurrent dispatches.
func (m *Model) SetMaxConcurrent(n int) {
	if n < 1 {
		n = 1
	}
	m.maxConcurrent = n
}

// --- Block helpers ---

func (m *Model) nextBlockID() string {
	m.blockCounter++
	return fmt.Sprintf("b%d", m.blockCounter)
}

func (m *Model) findBlock(id string) *Block {
	for i := range m.blocks {
		if m.blocks[i].ID == id {
			return &m.blocks[i]
		}
	}
	return nil
}

func (m *Model) blockIndex(id string) int {
	for i, b := range m.blocks {
		if b.ID == id {
			return i
		}
	}
	return m.focusedIdx
}

func (m *Model) focusedBlock() *Block {
	if m.focusedIdx >= 0 && m.focusedIdx < len(m.blocks) {
		return &m.blocks[m.focusedIdx]
	}
	return nil
}

func containsProvider(chain []string, provider string) bool {
	for _, name := range chain {
		if name == provider {
			return true
		}
	}
	return false
}

// executePaletteCommand runs a palette command and returns an optional tea.Cmd.
func (m *Model) executePaletteCommand(command string) tea.Cmd {
	command = strings.TrimSpace(command)
	switch {
	case command == "switch":
		m.appendCommandFeedback("/switch", "usage: /switch <kitchen>")
		return nil
	case strings.HasPrefix(command, "switch "):
		m.handleSwitchCommand(strings.TrimSpace(strings.TrimPrefix(command, "switch ")))
		return nil
	case command == "kitchens":
		m.appendCommandFeedback("/kitchens", formatKitchenStates(m.kitchenStates))
		return nil
	}

	switch command {
	case "cancel":
		if b := m.focusedBlock(); b != nil && b.IsActive() {
			if b.CancelFn != nil {
				b.CancelFn()
			}
			b.State = StateCancelled
		}
	case "collapse":
		if b := m.focusedBlock(); b != nil {
			b.Collapsed = true
		}
	case "expand":
		if b := m.focusedBlock(); b != nil {
			b.Collapsed = false
		}
	case "collapse all":
		for i := range m.blocks {
			m.blocks[i].Collapsed = true
		}
	case "expand all":
		for i := range m.blocks {
			m.blocks[i].Collapsed = false
		}
	case "summary":
		m.overlayActive = true
		m.overlayMode = OverlaySummary
	case "history":
		entries := BuildHistoryFromBlocks(m.blocks)
		for i := len(m.history) - 1; i >= 0; i-- {
			entries = append(entries, HistoryEntry{Prompt: m.history[i]})
		}
		m.search = SearchState{Active: true, Matches: entries}
		m.overlayActive = true
		m.overlayMode = OverlaySearch
		m.overlayInput = textinput.New()
		m.overlayInput.Placeholder = "search history..."
		m.overlayInput.Focus()
		return textinput.Blink
	case "session save":
		if err := SaveLastSession(m.blocks); err != nil {
			if b := m.focusedBlock(); b != nil {
				b.AppendEvent(adapter.Event{
					Type:    adapter.EventText,
					Kitchen: "milliways",
					Text:    "[session save] error: " + err.Error(),
				})
			}
		} else {
			if b := m.focusedBlock(); b != nil {
				b.AppendEvent(adapter.Event{
					Type:    adapter.EventText,
					Kitchen: "milliways",
					Text:    "[session save] saved to last.json",
				})
			}
		}
	case "session load":
		blocks, events, err := m.loadSession("last")
		if err != nil {
			if b := m.focusedBlock(); b != nil {
				b.AppendEvent(adapter.Event{
					Type:    adapter.EventText,
					Kitchen: "milliways",
					Text:    "[session load] error: " + err.Error(),
				})
			}
		} else {
			m.blocks = append(m.blocks, blocks...)
			m.runtimeEvents = append(m.runtimeEvents, events...)
			if b := m.focusedBlock(); b != nil {
				b.AppendEvent(adapter.Event{
					Type:    adapter.EventText,
					Kitchen: "milliways",
					Text:    fmt.Sprintf("[session load] loaded %d blocks", len(blocks)),
				})
			}
		}
	case "status", "report":
		// These would normally trigger external commands — placeholder.
		if b := m.focusedBlock(); b != nil {
			b.AppendEvent(adapter.Event{
				Type:    adapter.EventText,
				Kitchen: "milliways",
				Text:    "[" + command + "] not yet implemented in TUI palette",
			})
		}
	}
	return nil
}

func (m *Model) handleSwitchCommand(kitchen string) {
	kitchen = strings.TrimSpace(kitchen)
	if kitchen == "" {
		m.appendCommandFeedback("/switch", "usage: /switch <kitchen>")
		return
	}

	state, ok := findKitchenState(m.kitchenStates, kitchen)
	if !ok {
		m.appendCommandFeedback("/switch "+kitchen, fmt.Sprintf("kitchen %q is unavailable. Ready kitchens: %s", kitchen, formatReadyKitchens(m.kitchenStates)))
		return
	}
	if state.Status != "ready" {
		m.appendCommandFeedback("/switch "+kitchen, fmt.Sprintf("kitchen %q is unavailable (%s). Ready kitchens: %s", kitchen, kitchenAvailabilityLabel(state), formatReadyKitchens(m.kitchenStates)))
		return
	}

	b := m.focusedBlock()
	if b == nil {
		m.appendCommandFeedback("/switch "+kitchen, fmt.Sprintf("cannot switch to %q: no focused block", kitchen))
		return
	}
	if b.Conversation == nil {
		b.appendSystemLine(fmt.Sprintf("cannot switch to %q: focused block has no active conversation", kitchen))
		return
	}
	active := b.Conversation.ActiveSegment()
	if active == nil {
		b.appendSystemLine(fmt.Sprintf("cannot switch to %q: conversation has no active segment", kitchen))
		return
	}

	fromKitchen := active.Provider
	b.Conversation.EndActiveSegment(conversation.SegmentDone, "user_switch")
	segment := b.Conversation.StartSegment(kitchen)
	b.ContinuationPrompt = conversation.BuildContinuationPrompt(conversation.ContinueInput{
		Conversation: b.Conversation,
		NextProvider: kitchen,
		Reason:       "user requested",
	})
	b.Conversation.AppendTurn(conversation.RoleSystem, "milliways", fmt.Sprintf("Prepared continuation payload for user-requested switch from %s to %s.\n%s", fromKitchen, kitchen, b.ContinuationPrompt))
	b.Kitchen = kitchen
	if !containsProvider(b.ProviderChain, kitchen) {
		b.ProviderChain = append(b.ProviderChain, kitchen)
	}
	b.appendSystemLine(fmt.Sprintf("switch executed: %s -> %s (%s)", fromKitchen, kitchen, "user requested"))
	m.appendRuntimeEvent(observability.Event{
		ID:             fmt.Sprintf("switch-%s-%d", b.ID, time.Now().UnixNano()),
		ConversationID: b.Conversation.ID,
		BlockID:        b.ID,
		SegmentID:      segment.ID,
		Kind:           "switch",
		Provider:       kitchen,
		Text:           fmt.Sprintf("switch %s -> %s (user requested)", fromKitchen, kitchen),
		At:             time.Now(),
		Fields: map[string]string{
			"from":   fromKitchen,
			"to":     kitchen,
			"reason": "user requested",
		},
	})
}

func (m *Model) appendCommandFeedback(prompt, text string) {
	block := Block{
		ID:        m.nextBlockID(),
		Prompt:    prompt,
		Kitchen:   "milliways",
		State:     StateRouted,
		StartedAt: time.Now(),
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		block.appendSystemLine(line)
	}
	m.blocks = append(m.blocks, block)
	m.focusedIdx = len(m.blocks) - 1
}

func (m *Model) appendRuntimeEvent(event observability.Event) {
	m.runtimeEvents = append(m.runtimeEvents, event)
	if len(m.runtimeEvents) > 100 {
		m.runtimeEvents = append([]observability.Event(nil), m.runtimeEvents[len(m.runtimeEvents)-100:]...)
	}
	if m.sink != nil {
		m.sink.Emit(event)
	}
}

func resolvePaletteCommand(input, fallback string) string {
	input = strings.TrimSpace(input)
	if input == "" {
		return fallback
	}
	if input == "kitchens" || input == "switch" || strings.HasPrefix(input, "switch ") {
		return input
	}
	for _, item := range paletteItems {
		if input == item.Command {
			return input
		}
	}
	return fallback
}

func findKitchenState(states []KitchenState, name string) (KitchenState, bool) {
	for _, state := range states {
		if state.Name == name {
			return state, true
		}
	}
	return KitchenState{}, false
}

func formatReadyKitchens(states []KitchenState) string {
	var ready []string
	for _, state := range states {
		if state.Status == "ready" {
			ready = append(ready, state.Name)
		}
	}
	if len(ready) == 0 {
		return "none"
	}
	return strings.Join(ready, ", ")
}

func formatKitchenStates(states []KitchenState) string {
	if len(states) == 0 {
		return "Kitchens: none available"
	}
	parts := make([]string, 0, len(states))
	for _, state := range states {
		parts = append(parts, fmt.Sprintf("%s [%s]", state.Name, kitchenAvailabilityLabel(state)))
	}
	return "Kitchens: " + strings.Join(parts, ", ")
}

func kitchenAvailabilityLabel(state KitchenState) string {
	switch state.Status {
	case "exhausted":
		if state.ResetsAt != "" {
			return "exhausted until " + state.ResetsAt
		}
		return "exhausted"
	case "warning":
		return fmt.Sprintf("warning %.0f%%", state.UsageRatio*100)
	default:
		return state.Status
	}
}

func (m *Model) hasCompletedBlocks() bool {
	for _, b := range m.blocks {
		if b.isDone() {
			return true
		}
	}
	return false
}

// parseKitchenForce extracts @kitchen prefix from a prompt.
func parseKitchenForce(prompt string) (kitchenForce, cleanPrompt string) {
	if strings.HasPrefix(prompt, "@") {
		parts := strings.SplitN(prompt, " ", 2)
		kitchenForce = strings.TrimPrefix(parts[0], "@")
		if len(parts) > 1 {
			cleanPrompt = parts[1]
		}
		return kitchenForce, cleanPrompt
	}
	return "", prompt
}

// truncateQueue shortens a string for queue display.
func truncateQueue(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-1]) + "…"
}
