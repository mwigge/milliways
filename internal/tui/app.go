package tui

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mwigge/milliways/internal/conversation"
	"github.com/mwigge/milliways/internal/kitchen"
	"github.com/mwigge/milliways/internal/kitchen/adapter"
	"github.com/mwigge/milliways/internal/maitre"
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

type costAccumulator struct {
	Calls, InputToks, OutputToks, CacheRead, CacheWrite int
	TotalUSD                                            float64
}

func (a *costAccumulator) add(c *adapter.CostInfo) {
	if c == nil {
		return
	}

	a.Calls++
	a.InputToks += c.InputTokens
	a.OutputToks += c.OutputTokens
	a.CacheRead += c.CacheRead
	a.CacheWrite += c.CacheWrite
	a.TotalUSD += c.USD
}

type routingEntry struct {
	Kitchen string
	Tier    string
	Reason  string
	Signals map[string]float64
	At      time.Time
}

type procInfo struct {
	PID   int
	CPU   float64
	MemMB float64
	Exe   string
}

type diffFile struct {
	Path     string
	Status   string
	Selected bool
}

type compareResult struct {
	Kitchen string
	Output  string
	Percent float64
	Done    bool
	Error   string
}

// Model is the main Bubble Tea application model for the Milliways TUI.
type Model struct {
	input      textarea.Model
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
	sidePanelIdx  int

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
	jobTickets             []pantry.Ticket
	ticketStore            *pantry.TicketStore
	costByKitchen          map[string]costAccumulator
	costTotalUSD           float64
	routingHistory         []routingEntry
	procStats              map[string]procInfo
	mu                     *sync.Mutex
	snippetIndex           []snippet
	snippetFilter          string
	snippetSelected        int
	changedFiles           []diffFile
	diffSelected           int
	compareResults         map[string][]compareResult
	activeCompareID        string
	compareSelected        int
	compareSelectedKitchen string
	openSpecChanges        []openSpecChange
	openSpecCourses        []openSpecCourse
	openSpecStatusMessage  string
	openSpecExpanded       bool
	openSpecSelected       int
	openSpecCourseSelected int

	// DB access for ledger sink.
	pdb *pantry.DB

	// Dialogue overlay.
	overlayInput  textinput.Model
	overlayActive bool
	overlayMode   OverlayMode
	vimMode       VimMode
	mouse         mouseState

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
	projectState  ProjectState
	recentRepos   RecentRepos

	// Structured runtime activity for transparency.
	runtimeEvents []observability.Event
	renderedLines []string
	configPath    string

	// Run target chooser state.
	runTargets          []RunTargetOption
	runTargetSelected   int
	pendingPrompt       string
	pendingKitchenForce string
}

// NewModel creates the TUI model.
func NewModel(store *pantry.TicketStore) Model {
	ti := textarea.New()
	ti.Placeholder = "Type a task... (@kitchen to force, Ctrl+D to exit)"
	ti.Focus()
	ti.CharLimit = 500
	ti.SetWidth(80)
	ti.SetHeight(8)
	ti.Prompt = "▶ "
	ti.ShowLineNumbers = false
	ti.KeyMap.InsertNewline = key.NewBinding(
		key.WithKeys("shift+enter"),
		key.WithHelp("shift+enter", "insert newline"),
	)
	focusedStyle, blurredStyle := textarea.DefaultStyles()
	focusedStyle.Base = inputStyle
	focusedStyle.Text = inputStyle
	focusedStyle.Prompt = promptStyle
	focusedStyle.Placeholder = mutedStyle
	blurredStyle.Base = inputStyle
	blurredStyle.Text = inputStyle
	blurredStyle.Prompt = promptStyle
	blurredStyle.Placeholder = mutedStyle
	ti.FocusedStyle = focusedStyle
	ti.BlurredStyle = blurredStyle

	vp := viewport.New(80, 20)
	vp.SetContent("")
	initialSnippets := cloneSnippets(defaultSnippets)

	return Model{
		input:                  ti,
		output:                 vp,
		historyIdx:             -1,
		ticketStore:            store,
		costByKitchen:          make(map[string]costAccumulator),
		procStats:              make(map[string]procInfo),
		prog:                   new(*tea.Program),
		mu:                     &sync.Mutex{},
		maxConcurrent:          defaultMaxConcurrent,
		snippetIndex:           initialSnippets,
		changedFiles:           []diffFile{},
		diffSelected:           0,
		compareResults:         map[string][]compareResult{},
		activeCompareID:        "",
		compareSelected:        0,
		vimMode:                VimInsert,
		openSpecChanges:        []openSpecChange{},
		openSpecCourses:        []openSpecCourse{},
		openSpecSelected:       0,
		openSpecCourseSelected: 0,
	}
}

// NewAdapterModel creates the TUI model with adapter-based dispatch.
func NewAdapterModel(providerFactory ProviderFactory, hydrator orchestrator.ContextHydrator, sink observability.Sink, recorder ConversationRecorder, replayer ConversationReplayer, store *pantry.TicketStore, pdb *pantry.DB) Model {
	m := NewModel(store)
	m.providerFactory = providerFactory
	m.hydrator = hydrator
	m.sink = sink
	m.recorder = recorder
	m.replayer = replayer
	m.pdb = pdb
	return m
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(jobsRefreshCmd(m.ticketStore), m.startSystemMonitor(), initialOpenSpecRefreshCmd(), scheduleOpenSpecRefresh())
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	skipInputUpdate := false

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.output.Width = msg.Width - 30
		m.output.Height = msg.Height - 6
		m.input.SetWidth(msg.Width - 4)
		m.ready = true

	case tea.KeyMsg:
		// Route arrow keys to side panel when in panel mode OR when no overlay is active.
		// During overlays (palette, search), the overlay itself handles arrow keys.
		inPanelMode := m.vimMode == VimNormal || (m.overlayActive && m.overlayMode == OverlayPanel)
		skipInputUpdate = inPanelMode && isSidePanelKey(m.sidePanelIdx, msg, inPanelMode)
		cmds = append(cmds, m.handleKey(msg)...)

	case tea.MouseMsg:
		cmds = append(cmds, m.handleMouse(msg))

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
		entry := routingEntry{
			Kitchen: msg.Decision.Kitchen,
			Tier:    msg.Decision.Tier,
			Reason:  msg.Decision.Reason,
			Signals: cloneSignalScores(msg.Decision.SignalScores),
			At:      time.Now(),
		}
		m.routingHistory = append([]routingEntry{entry}, m.routingHistory...)
		if len(m.routingHistory) > 20 {
			m.routingHistory = m.routingHistory[:20]
		}

	case blockEventMsg:
		if msg.Event.Type == adapter.EventCost && msg.Event.Cost != nil {
			kitchen := msg.Event.Kitchen
			if m.costByKitchen == nil {
				m.costByKitchen = make(map[string]costAccumulator)
			}
			acc := m.costByKitchen[kitchen]
			acc.add(msg.Event.Cost)
			m.costByKitchen[kitchen] = acc
			m.costTotalUSD += msg.Event.Cost.USD
		}
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

	case blockPIDMsg:
		for i := range m.blocks {
			if m.blocks[i].ID == msg.BlockID {
				m.blocks[i].PID = msg.PID
				break
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
			// Preserve the actual measured duration, not a recalculated wall-clock value.
			if msg.Duration > 0 {
				b.Duration = msg.Duration
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
			m.accumulateCompareResult(b, msg)
			m.refreshChangedFiles()
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

	case systemMonitorTickMsg:
		m.refreshProcStats()
		cmds = append(cmds, m.startSystemMonitor())

	case openSpecRefreshMsg:
		_ = m.refreshOpenSpecData()
		cmds = append(cmds, scheduleOpenSpecRefresh())
	}

	// Update input or overlay.
	var inputCmd tea.Cmd
	if skipInputUpdate {
		inputCmd = nil
	} else if m.overlayActive {
		if m.overlayMode != OverlayRunIn && m.overlayMode != OverlayPanel {
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
	m.refreshRenderedLines()

	// Update viewport.
	var vpCmd tea.Cmd
	m.output, vpCmd = m.output.Update(msg)
	cmds = append(cmds, vpCmd)

	return m, tea.Batch(cmds...)
}

func cloneSignalScores(in map[string]float64) map[string]float64 {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]float64, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func (m Model) startSystemMonitor() tea.Cmd {
	return tea.Tick(3*time.Second, func(t time.Time) tea.Msg {
		return systemMonitorTickMsg(t)
	})
}

func (m *Model) refreshProcStats() {
	if m.procStats == nil {
		m.procStats = make(map[string]procInfo)
	}

	activeKitchens := make(map[string]bool)
	for _, block := range m.blocks {
		if block.PID <= 0 || block.isDone() {
			continue
		}
		if stats, err := fetchProcStats(block.PID); err == nil {
			m.procStats[block.Kitchen] = stats
			activeKitchens[block.Kitchen] = true
		}
	}

	for kitchen := range m.procStats {
		if !activeKitchens[kitchen] {
			delete(m.procStats, kitchen)
		}
	}
}

func fetchProcStats(pid int) (procInfo, error) {
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "%cpu=,%mem=,comm=").Output()
	if err != nil {
		return procInfo{}, err
	}
	return parseProcStatsOutput(pid, string(out))
}

func parseProcStatsOutput(pid int, output string) (procInfo, error) {
	parts := strings.Fields(strings.TrimSpace(output))
	if len(parts) < 3 {
		return procInfo{}, fmt.Errorf("ps output unexpected: %q", output)
	}

	cpu, err := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	if err != nil {
		return procInfo{}, fmt.Errorf("parse cpu for pid %d: %w", pid, err)
	}
	mem, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if err != nil {
		return procInfo{}, fmt.Errorf("parse memory for pid %d: %w", pid, err)
	}

	return procInfo{
		PID:   pid,
		CPU:   cpu,
		MemMB: mem * 1024 / 100,
		Exe:   strings.TrimSpace(parts[2]),
	}, nil
}

// refreshChangedFiles updates the session diff panel from git state.
func (m *Model) refreshChangedFiles() {
	n := m.activeCount
	if n == 0 {
		n = 1
	}

	cmd := exec.Command("git", "diff", "--name-status", "HEAD~"+strconv.Itoa(n), "HEAD")
	out, err := cmd.Output()
	if err != nil {
		cmd = exec.Command("git", "diff", "--name-status")
		out, err = cmd.Output()
		if err != nil {
			m.changedFiles = []diffFile{}
			m.diffSelected = 0
			return
		}
	}

	changed := parseDiffNameOutput(string(out))

	cmd = exec.Command("git", "ls-files", "--others", "--exclude-standard")
	if untrackedOut, err := cmd.Output(); err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(untrackedOut)), "\n") {
			if line == "" {
				continue
			}
			changed = append(changed, diffFile{Path: line, Status: "??"})
		}
	}

	m.diffSelected = 0
	m.changedFiles = changed
}

func parseDiffNameOutput(output string) []diffFile {
	if strings.TrimSpace(output) == "" {
		return nil
	}

	files := make([]diffFile, 0)
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}

		parts := strings.SplitN(line, " ", 2)
		status := "M"
		path := line
		if len(parts) == 2 {
			status = parts[0]
			path = parts[1]
		}

		files = append(files, diffFile{Path: path, Status: status})
	}

	return files
}

// handleKey processes key messages and returns commands.
func (m *Model) handleKey(msg tea.KeyMsg) []tea.Cmd {
	var cmds []tea.Cmd

	switch msg.Type {
	case tea.KeyCtrlRight, tea.KeyCtrlJ:
		m.advanceSidePanel()
		return nil
	case tea.KeyCtrlLeft, tea.KeyCtrlK:
		m.rewindSidePanel()
		return nil
	// On Mac, Cmd+] / Cmd+[ send Alt+]/Alt+[ in most terminal emulators.
	// We treat these the same as Ctrl+J / Ctrl+K for panel cycling.
	case tea.KeyRunes:
		if msg.Alt && len(msg.Runes) == 1 {
			switch msg.Runes[0] {
			case ']':
				m.advanceSidePanel()
				return nil
			case '[':
				m.rewindSidePanel()
				return nil
			}
		}
		if m.vimMode == VimNormal && !msg.Alt && len(msg.Runes) == 1 {
			switch msg.Runes[0] {
			case 'i':
				m.setInsertMode()
				return nil
			case 'l', 'j':
				m.advanceSidePanel()
				return nil
			case 'h', 'k':
				m.rewindSidePanel()
				return nil
			}
		}
	}

	switch msg.String() {
	case "ctrl+d":
		if !m.overlayActive && m.sidePanelIdx == int(SidePanelDiff) {
			if m.diffSelected < len(m.changedFiles)-1 {
				m.diffSelected++
			}
			return nil
		}
		return []tea.Cmd{tea.Quit}

	case "ctrl+u":
		if !m.overlayActive && m.sidePanelIdx == int(SidePanelDiff) {
			if m.diffSelected > 0 {
				m.diffSelected--
			}
			return nil
		}
		if !m.overlayActive && m.vimMode == VimInsert {
			m.input.SetValue("")
			return nil
		}

	case "ctrl+a":
		if !m.overlayActive && m.vimMode == VimInsert {
			m.input.SetCursor(0)
			return nil
		}

	case "ctrl+e":
		if !m.overlayActive && m.vimMode == VimInsert {
			m.input.SetCursor(len(m.input.Value()))
			return nil
		}

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

	case "alt+enter":
		if m.overlayActive {
			return nil
		}
		prompt := strings.TrimSpace(m.input.Value())
		if prompt == "" {
			return nil
		}
		m.history = append(m.history, prompt)
		m.historyIdx = -1
		m.input.SetValue("")
		return m.startCompareDispatch(prompt)

	case "enter":
		if !m.overlayActive && msg.Alt {
			prompt := strings.TrimSpace(m.input.Value())
			if prompt == "" {
				return nil
			}
			m.history = append(m.history, prompt)
			m.historyIdx = -1
			m.input.SetValue("")
			return m.startCompareDispatch(prompt)
		}
		if m.overlayActive && m.overlayMode == OverlayRunIn {
			return m.handleRunTargetSelection()
		}
		if !m.overlayActive && m.sidePanelIdx == int(SidePanelSnippets) {
			if len(m.snippetIndex) == 0 {
				return nil
			}
			if m.snippetSelected < 0 {
				m.snippetSelected = 0
			}
			if m.snippetSelected >= len(m.snippetIndex) {
				m.snippetSelected = len(m.snippetIndex) - 1
			}
			selected := m.snippetIndex[m.snippetSelected]
			m.input.SetValue(snippetBodyForInput(selected.Body))
			m.sidePanelIdx = int(SidePanelLedger)
			return nil
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
				m.vimMode = VimInsert
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
			m.vimMode = VimInsert
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
			m.setInsertMode()
			return nil
		}
	case "b":
		if m.overlayActive && m.overlayMode == OverlayFeedback {
			m.rateLastDispatch(false)
			m.setInsertMode()
			return nil
		}
	case "s":
		if m.overlayActive && m.overlayMode == OverlayFeedback {
			m.setInsertMode()
			return nil
		}
	case "q":
		if m.overlayActive && m.overlayMode == OverlaySummary {
			m.setInsertMode()
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
		if !m.overlayActive && m.sidePanelIdx == int(SidePanelSnippets) {
			m.refreshSnippetIndex()
			if m.snippetSelected < len(m.snippetIndex)-1 {
				m.snippetSelected++
			}
			return nil
		}
		// Cycle focus to next block.
		if len(m.blocks) > 0 && !m.overlayActive {
			m.focusedIdx = (m.focusedIdx + 1) % len(m.blocks)
			return nil
		}

	case "shift+tab":
		if !m.overlayActive && m.sidePanelIdx == int(SidePanelSnippets) {
			m.refreshSnippetIndex()
			if m.snippetSelected > 0 {
				m.snippetSelected--
			}
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
		if !m.overlayActive && m.sidePanelIdx == int(SidePanelCompare) {
			if m.compareSelected > 0 {
				m.compareSelected--
			}
			m.syncCompareSelection()
			return nil
		}
		if !m.overlayActive && m.sidePanelIdx == int(SidePanelDiff) {
			if m.diffSelected > 0 {
				m.diffSelected--
			}
			return nil
		}
		if !m.overlayActive && m.sidePanelIdx == int(SidePanelSnippets) {
			m.refreshSnippetIndex()
			if m.snippetSelected > 0 {
				m.snippetSelected--
			}
			return nil
		}
		// In panel mode, navigate courses within the OpenSpec panel.
		if m.overlayActive && m.overlayMode == OverlayPanel && m.sidePanelIdx == int(SidePanelOpenSpec) {
			if m.openSpecCourseSelected > 0 {
				m.openSpecCourseSelected--
			}
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
		if !m.overlayActive && m.sidePanelIdx == int(SidePanelCompare) {
			results := m.activeCompareResults()
			if m.compareSelected < len(results)-1 {
				m.compareSelected++
			}
			m.syncCompareSelection()
			return nil
		}
		if !m.overlayActive && m.sidePanelIdx == int(SidePanelDiff) {
			if m.diffSelected < len(m.changedFiles)-1 {
				m.diffSelected++
			}
			return nil
		}
		if !m.overlayActive && m.sidePanelIdx == int(SidePanelSnippets) {
			m.refreshSnippetIndex()
			if m.snippetSelected < len(m.snippetIndex)-1 {
				m.snippetSelected++
			}
			return nil
		}
		// In panel mode, navigate courses within the OpenSpec panel.
		if m.overlayActive && m.overlayMode == OverlayPanel && m.sidePanelIdx == int(SidePanelOpenSpec) {
			if m.openSpecCourseSelected < len(m.openSpecCourses)-1 {
				m.openSpecCourseSelected++
			}
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

	case "ctrl+o":
		// Toggle panel navigation mode.
		if m.overlayActive && m.overlayMode == OverlayPanel {
			m.setInsertMode()
			return nil
		}
		if !m.overlayActive {
			m.setNormalMode()
			return nil
		}

	case "esc":
		if m.vimMode == VimNormal {
			m.setInsertMode()
			return nil
		}
		if m.overlayActive {
			m.vimMode = VimInsert
			m.overlayActive = false
			m.overlayMode = OverlayNone
			m.palette.Active = false
			m.search.Active = false
			m.pendingPrompt = ""
			m.pendingKitchenForce = ""
			m.input.Focus()
			return nil
		}
		m.setNormalMode()
		return nil

	case "backspace", "ctrl+h":
		if !m.overlayActive && m.sidePanelIdx == int(SidePanelSnippets) && m.snippetFilter != "" {
			m.snippetFilter = trimLastRune(m.snippetFilter)
			m.refreshSnippetIndex()
			m.snippetSelected = 0
			return nil
		}
	}

	if !m.overlayActive && m.sidePanelIdx == int(SidePanelSnippets) && msg.Type == tea.KeyRunes {
		m.snippetFilter += string(msg.Runes)
		m.refreshSnippetIndex()
		m.snippetSelected = 0
		return nil
	}

	return cmds
}

func (m *Model) advanceSidePanel() {
	m.sidePanelIdx = (m.sidePanelIdx + 1) % int(sidePanelCount)
	m.refreshSnippetIndexOnEntry()
	m.refreshDiffPanelOnEntry()
}

func (m *Model) rewindSidePanel() {
	m.sidePanelIdx--
	if m.sidePanelIdx < 0 {
		m.sidePanelIdx = int(sidePanelCount) - 1
	}
	m.refreshSnippetIndexOnEntry()
	m.refreshDiffPanelOnEntry()
}

func (m *Model) setInsertMode() {
	m.vimMode = VimInsert
	m.overlayActive = false
	m.overlayMode = OverlayNone
	m.input.Focus()
}

func (m *Model) setNormalMode() {
	m.vimMode = VimNormal
	m.overlayActive = false
	m.overlayMode = OverlayNone
	m.input.Blur()
}

func (m *Model) refreshRenderedLines() {
	m.renderedLines = buildRenderedLines(m.blocks)
}

func (m *Model) refreshSnippetIndexOnEntry() {
	if m.sidePanelIdx != int(SidePanelSnippets) {
		return
	}
	m.refreshSnippetIndex()
	m.snippetSelected = 0
}

func (m *Model) refreshDiffPanelOnEntry() {
	if m.sidePanelIdx != int(SidePanelDiff) {
		return
	}
	if len(m.changedFiles) == 0 {
		m.refreshChangedFiles()
	}
	if len(m.changedFiles) == 0 {
		m.diffSelected = 0
		return
	}
	if m.diffSelected >= len(m.changedFiles) {
		m.diffSelected = len(m.changedFiles) - 1
	}
	if m.diffSelected < 0 {
		m.diffSelected = 0
	}
}

func (m *Model) refreshSnippetIndex() {
	all := loadAllSnippets()
	if m.snippetFilter == "" {
		m.snippetIndex = all
	} else {
		m.snippetIndex = filterSnippets(all, m.snippetFilter)
	}
	if len(m.snippetIndex) == 0 {
		m.snippetSelected = 0
		return
	}
	if m.snippetSelected >= len(m.snippetIndex) {
		m.snippetSelected = len(m.snippetIndex) - 1
	}
	if m.snippetSelected < 0 {
		m.snippetSelected = 0
	}
}

func isSnippetPanelKey(msg tea.KeyMsg) bool {
	switch msg.String() {
	case "up", "down", "enter", "tab", "shift+tab", "backspace", "ctrl+h":
		return true
	default:
		return msg.Type == tea.KeyRunes
	}
}

func trimLastRune(value string) string {
	runes := []rune(value)
	if len(runes) == 0 {
		return ""
	}
	return string(runes[:len(runes)-1])
}

func snippetBodyForInput(body string) string {
	return body
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
	ProjectState  ProjectState // detected project context (optional)
	ConfigPath    string
}

// Run starts the TUI with adapter-based dispatch.
func Run(providerFactory ProviderFactory, hydrator orchestrator.ContextHydrator, sink observability.Sink, recorder ConversationRecorder, replayer ConversationReplayer, store *pantry.TicketStore, pdb *pantry.DB) error {
	return RunWithOpts(providerFactory, hydrator, sink, recorder, replayer, store, pdb, RunOpts{})
}

// RunWithOpts starts the TUI with adapter-based dispatch and options.
func RunWithOpts(providerFactory ProviderFactory, hydrator orchestrator.ContextHydrator, sink observability.Sink, recorder ConversationRecorder, replayer ConversationReplayer, store *pantry.TicketStore, pdb *pantry.DB, opts RunOpts) error {
	m := NewAdapterModel(providerFactory, hydrator, sink, recorder, replayer, store, pdb)
	m.configPath = opts.ConfigPath
	m.SetKitchenStates(opts.KitchenStates)
	if opts.ProjectState.RepoRoot != "" {
		m.SetProjectState(opts.ProjectState)
	}

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
		tea.WithMouseAllMotion(),
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

// SetProjectState updates the active project context for the TUI.
func (m *Model) SetProjectState(state ProjectState) {
	m.projectState = state
	m.AddRecentRepo(state.RepoName)
}

// AddRecentRepo records a repository as accessed in this session.
func (m *Model) AddRecentRepo(repoName string) {
	m.recentRepos.Add(repoName)
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

func (m *Model) activeCompareResults() []compareResult {
	if m.activeCompareID == "" {
		return nil
	}
	return m.compareResults[m.activeCompareID]
}

func (m *Model) startCompareDispatch(prompt string) []tea.Cmd {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return nil
	}

	compareID := fmt.Sprintf("compare-%d", time.Now().UnixNano())
	kitchens := m.compareKitchenNames()
	if len(kitchens) == 0 {
		return nil
	}

	if m.compareResults == nil {
		m.compareResults = make(map[string][]compareResult)
	}

	results := make([]compareResult, 0, len(kitchens))
	cmds := make([]tea.Cmd, 0, len(kitchens))
	for _, kitchen := range kitchens {
		results = append(results, compareResult{Kitchen: kitchen})
		blockID, cmd := m.startBlockDispatch(prompt, kitchen)
		if b := m.findBlock(blockID); b != nil {
			b.comparePrompt = compareID
		}
		cmds = append(cmds, cmd)
	}

	m.compareResults[compareID] = results
	m.activeCompareID = compareID
	m.compareSelected = 0
	m.syncCompareSelection()

	return cmds
}

func (m *Model) compareKitchenNames() []string {
	seen := make(map[string]struct{})
	names := make([]string, 0, len(m.kitchenStates))
	for _, state := range m.kitchenStates {
		if state.Name == "" {
			continue
		}
		if state.Status == "disabled" || state.Status == "not-installed" {
			continue
		}
		if _, ok := seen[state.Name]; ok {
			continue
		}
		seen[state.Name] = struct{}{}
		names = append(names, state.Name)
	}
	if len(names) == 0 {
		for _, name := range []string{"claude", "codex", "opencode", "gemini"} {
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func (m *Model) accumulateCompareResult(block *Block, msg blockDoneMsg) {
	if block == nil || block.comparePrompt == "" {
		return
	}

	result := compareResult{
		Kitchen: block.Kitchen,
		Done:    msg.Err == nil && msg.Result.ExitCode == 0,
		Error:   compareErrorText(msg),
		Output:  strings.TrimSpace(msg.Result.Output),
	}
	if result.Output == "" {
		result.Output = blockCompareOutput(block)
	}

	existing := append([]compareResult(nil), m.compareResults[block.comparePrompt]...)
	updated := false
	for i := range existing {
		if existing[i].Kitchen != block.Kitchen {
			continue
		}
		existing[i].Done = result.Done
		existing[i].Error = result.Error
		existing[i].Output = result.Output
		updated = true
		break
	}
	if !updated {
		existing = append(existing, result)
	}
	recalculateCompareProgress(existing)
	m.compareResults[block.comparePrompt] = existing
	if m.activeCompareID == "" {
		m.activeCompareID = block.comparePrompt
	}
	m.syncCompareSelection()
}

func compareErrorText(msg blockDoneMsg) string {
	if msg.Err != nil {
		return msg.Err.Error()
	}
	if msg.Result.ExitCode != 0 {
		return fmt.Sprintf("exit code %d", msg.Result.ExitCode)
	}
	return ""
}

func blockCompareOutput(block *Block) string {
	if block == nil {
		return ""
	}
	parts := make([]string, 0, len(block.Lines))
	for _, line := range block.Lines {
		text := strings.TrimSpace(line.Text)
		if text == "" {
			continue
		}
		parts = append(parts, text)
	}
	return strings.Join(parts, "\n")
}

func recalculateCompareProgress(results []compareResult) {
	if len(results) == 0 {
		return
	}
	completed := 0
	for _, result := range results {
		if result.Done || result.Error != "" || result.Output != "" {
			completed++
		}
	}
	percent := float64(completed) / float64(len(results)) * 100
	for i := range results {
		results[i].Percent = percent
	}
}

func (m *Model) syncCompareSelection() {
	results := m.activeCompareResults()
	if len(results) == 0 {
		m.compareSelected = 0
		m.compareSelectedKitchen = ""
		return
	}
	if m.compareSelected < 0 {
		m.compareSelected = 0
	}
	if m.compareSelected >= len(results) {
		m.compareSelected = len(results) - 1
	}
	m.compareSelectedKitchen = results[m.compareSelected].Kitchen
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
	case command == "project":
		m.appendCommandFeedback("/project", m.HandleProjectCommand())
		return nil
	case command == "palace":
		m.appendCommandFeedback("/palace", m.HandlePalaceCommand(""))
		return nil
	case command == "codegraph":
		m.appendCommandFeedback("/codegraph", m.HandleCodeGraphCommand(""))
		return nil
	case command == "switch":
		m.appendCommandFeedback("/switch", "usage: /switch <kitchen>")
		return nil
	case command == "stick":
		m.handleStickCommand()
		return nil
	case command == "back":
		m.handleBackCommand()
		return nil
	case strings.HasPrefix(command, "switch "):
		m.handleSwitchCommand(strings.TrimSpace(strings.TrimPrefix(command, "switch ")))
		return nil
	case command == "kitchens":
		m.appendCommandFeedback("/kitchens", formatKitchenStates(m.kitchenStates))
		return nil
	case command == "repos":
		m.appendCommandFeedback("/repos", RenderReposList(m.recentRepos.List(), m.projectState.RepoName))
		return nil
	case command == "login":
		m.handleLoginCommand("")
		return nil
	case strings.HasPrefix(command, "login "):
		kitchen := strings.TrimSpace(strings.TrimPrefix(command, "login "))
		m.handleLoginCommand(kitchen)
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
	m.executeSwitch(kitchen, "user requested")
}

// handleLoginCommand handles /login and /login <kitchen> palette commands.
func (m *Model) handleLoginCommand(kitchen string) {
	configPath := strings.TrimSpace(m.configPath)
	if configPath == "" {
		configPath = maitre.DefaultConfigPath()
	}

	cfg, err := maitre.LoadConfig(configPath)
	if err != nil {
		m.appendCommandFeedback("/login", fmt.Sprintf("failed to load config: %v", err))
		return
	}

	if kitchen == "" {
		health := maitre.Diagnose(buildRegistry(cfg))
		sort.Slice(health, func(i, j int) bool {
			return health[i].Name < health[j].Name
		})

		lines := []string{
			"Kitchen      Status              Auth Method           Action",
			"───────      ──────              ───────────           ──────",
		}
		for _, h := range health {
			lines = append(lines, fmt.Sprintf("%-12s %s %-18s %-21s %s",
				h.Name,
				h.Status.Symbol(),
				h.Status,
				authMethodForKitchen(h.Name),
				loginActionForKitchen(h),
			))
		}
		m.appendCommandFeedback("/login", strings.Join(lines, "\n"))
		return
	}

	result := captureLoginOutput(kitchen)
	m.appendCommandFeedback("/login "+kitchen, result)
}

func (m *Model) executeSwitch(kitchen, reason string) {
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
	segment := b.Conversation.StartSegment(kitchen, nil)
	b.ContinuationPrompt = conversation.BuildContinuationPrompt(conversation.ContinueInput{
		Conversation: b.Conversation,
		NextProvider: kitchen,
		Reason:       "user requested",
	})
	b.Conversation.AppendTurn(conversation.RoleSystem, "milliways", fmt.Sprintf("Prepared continuation payload for switch from %s to %s (%s).\n%s", fromKitchen, kitchen, reason, b.ContinuationPrompt))
	b.Kitchen = kitchen
	if !containsProvider(b.ProviderChain, kitchen) {
		b.ProviderChain = append(b.ProviderChain, kitchen)
	}
	b.appendSystemLine(formatSwitchSystemLine(fromKitchen, kitchen, reason))
	m.appendRuntimeEvent(observability.Event{
		ID:             fmt.Sprintf("switch-%s-%d", b.ID, time.Now().UnixNano()),
		ConversationID: b.Conversation.ID,
		BlockID:        b.ID,
		SegmentID:      segment.ID,
		Kind:           "switch",
		Provider:       kitchen,
		Text:           formatSwitchRuntimeText(fromKitchen, kitchen, reason),
		At:             time.Now(),
		Fields: map[string]string{
			"from":   fromKitchen,
			"to":     kitchen,
			"reason": reason,
		},
	})
}

func (m *Model) handleStickCommand() {
	b := m.focusedBlock()
	if b == nil {
		m.appendCommandFeedback("/stick", "cannot toggle sticky mode: no focused block")
		return
	}
	if b.Conversation == nil {
		b.appendSystemLine("cannot toggle sticky mode: focused block has no active conversation")
		return
	}
	kitchen := strings.TrimSpace(b.Kitchen)
	if kitchen == "" {
		b.appendSystemLine("cannot toggle sticky mode: focused block has no current kitchen")
		return
	}

	if b.Conversation.Memory.StickyKitchen == kitchen {
		b.Conversation.Memory.StickyKitchen = ""
		b.appendSystemLine("sticky mode off")
		return
	}

	b.Conversation.Memory.StickyKitchen = kitchen
	b.appendSystemLine(fmt.Sprintf("sticky mode enabled for kitchen %q", kitchen))
}

func (m *Model) handleBackCommand() {
	targetKitchen, ok := m.mostRecentSwitchSource()
	if !ok {
		m.appendCommandFeedback("/back", "no prior switch found to reverse")
		return
	}

	beforeBlockCount := len(m.blocks)

	m.executeSwitch(targetKitchen, "reversing most recent switch")

	if len(m.blocks) != beforeBlockCount {
		return
	}

	b := m.focusedBlock()
	if b == nil || b.Kitchen != targetKitchen {
		return
	}
}

func formatSwitchSystemLine(fromKitchen, toKitchen, reason string) string {
	return fmt.Sprintf("switch: %s -> %s | reason: %s | Use /back to return", fromKitchen, toKitchen, reason)
}

func formatSwitchRuntimeText(fromKitchen, toKitchen, reason string) string {
	return fmt.Sprintf("switch %s -> %s (%s)", fromKitchen, toKitchen, reason)
}

func (m *Model) mostRecentSwitchSource() (string, bool) {
	for i := len(m.runtimeEvents) - 1; i >= 0; i-- {
		event := m.runtimeEvents[i]
		if event.Kind != "switch" {
			continue
		}
		fromKitchen := strings.TrimSpace(event.Fields["from"])
		if fromKitchen == "" {
			continue
		}
		return fromKitchen, true
	}
	return "", false
}

func (m *Model) appendCommandFeedback(prompt, text string) {
	now := time.Now()
	block := Block{
		ID:        m.nextBlockID(),
		Prompt:    prompt,
		Kitchen:   "milliways",
		State:     StateDone,
		StartedAt: now,
		Duration:  1 * time.Second, // stable value so elapsed() doesn't drift
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
	m.refreshRenderedLines()
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
	if input == "back" || input == "codegraph" || input == "kitchens" || input == "login" || input == "palace" || input == "project" || input == "repos" || input == "stick" || input == "switch" || strings.HasPrefix(input, "login ") || strings.HasPrefix(input, "switch ") {
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

// captureLoginOutput runs maitre.LoginKitchen and captures stdout/stderr.
func captureLoginOutput(kitchen string) string {
	originalStdout := os.Stdout
	originalStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		return fmt.Sprintf("Error: creating login output pipe: %v", err)
	}

	os.Stdout = w
	os.Stderr = w
	loginErr := maitre.LoginKitchen(kitchen)
	_ = w.Close()
	os.Stdout = originalStdout
	os.Stderr = originalStderr

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	_ = r.Close()

	output := strings.TrimSpace(buf.String())
	if loginErr != nil {
		if output == "" {
			return fmt.Sprintf("Error: %v", loginErr)
		}
		return output + fmt.Sprintf("\nError: %v", loginErr)
	}
	if output == "" {
		return fmt.Sprintf("Login command completed for %s.", kitchen)
	}
	return output
}

func authMethodForKitchen(name string) string {
	switch name {
	case "claude", "gemini":
		return "Browser OAuth"
	case "opencode":
		return "Interactive TUI"
	case "minimax":
		return "API key (carte.yaml)"
	case "groq":
		return "Env var (GROQ_API_KEY)"
	case "ollama":
		return "None"
	case "aider", "cline":
		return "Env var (ANTHROPIC_API_KEY)"
	case "goose":
		return "Env var (GOOSE_API_KEY)"
	default:
		return "Unknown"
	}
}

func loginActionForKitchen(h maitre.KitchenHealth) string {
	switch h.Status {
	case kitchen.Ready:
		return "ready"
	case kitchen.Disabled:
		return "(disabled in carte.yaml)"
	case kitchen.NotInstalled:
		if h.InstallCmd != "" {
			return h.InstallCmd
		}
		return fmt.Sprintf("milliways setup %s", h.Name)
	case kitchen.NeedsAuth:
		return fmt.Sprintf("milliways login %s", h.Name)
	default:
		return "check configuration"
	}
}

func buildRegistry(cfg *maitre.Config) *kitchen.Registry {
	reg := kitchen.NewRegistry()

	installCmds := map[string]string{
		"claude":   "brew install claude",
		"opencode": "brew install opencode",
		"gemini":   "npm install -g @google/gemini-cli",
		"aider":    "pip install aider-chat",
		"goose":    "brew install goose",
		"cline":    "npm install -g cline",
	}

	authCmds := map[string]string{
		"claude":   "claude (interactive login)",
		"opencode": "none (uses Ollama)",
		"gemini":   "gcloud auth login",
		"aider":    "set ANTHROPIC_API_KEY or OPENAI_API_KEY",
		"goose":    "goose configure",
		"cline":    "cline --login",
	}

	for name, kc := range cfg.Kitchens {
		if kc.HTTPClient != nil {
			httpKitchen, err := adapter.NewHTTPKitchen(name, adapter.HTTPKitchenConfig{
				BaseURL:        kc.HTTPClient.BaseURL,
				AuthKey:        kc.HTTPClient.AuthKey,
				AuthType:       kc.HTTPClient.AuthType,
				Model:          kc.HTTPClient.Model,
				Stations:       kc.HTTPClient.Stations,
				Tier:           kitchen.ParseCostTier(kc.HTTPClient.Tier),
				ResponseFormat: kc.HTTPClient.ResponseFormat,
				Timeout:        time.Duration(kc.HTTPClient.Timeout) * time.Second,
			}, kc.Stations, kitchen.ParseCostTier(kc.CostTier))
			if err != nil {
				continue
			}
			reg.Register(httpKitchen)
			continue
		}

		reg.Register(kitchen.NewGeneric(kitchen.GenericConfig{
			Name:       name,
			Cmd:        kc.Cmd,
			Args:       kc.Args,
			Stations:   kc.Stations,
			Tier:       kitchen.ParseCostTier(kc.CostTier),
			Enabled:    kc.IsEnabled(),
			InstallCmd: installCmds[name],
			AuthCmd:    authCmds[name],
		}))
	}

	return reg
}
