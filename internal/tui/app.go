package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mwigge/milliways/internal/kitchen"
	"github.com/mwigge/milliways/internal/pantry"
	"github.com/mwigge/milliways/internal/sommelier"
)

// DispatchFunc is the callback Milliways TUI uses to dispatch a task.
type DispatchFunc func(ctx context.Context, prompt, kitchenForce string) (kitchen.Result, sommelier.Decision, error)

// Model is the main Bubble Tea application model for the Milliways TUI.
type Model struct {
	input       textinput.Model
	output      viewport.Model
	width       int
	height      int
	outputLines []string
	ledgerLog   []ledgerLine
	processMap  processState
	dispatching bool
	cancelFn    context.CancelFunc
	dispatchFn  DispatchFunc
	history      []string
	historyIdx   int
	ready        bool
	jobTickets   []pantry.Ticket  // nil = panel unavailable
	ticketStore  *pantry.TicketStore
}

type ledgerLine struct {
	time    string
	kitchen string
	dur     string
	status  string
}

type processState struct {
	active    bool
	kitchen   string
	risk      string
	status    string // "streaming", "done", "failed"
	elapsed   time.Duration
	startedAt time.Time
}

// Line received from kitchen during streaming
type lineMsg string

// Dispatch completed
type dispatchDoneMsg struct {
	result   kitchen.Result
	decision sommelier.Decision
	err      error
	duration time.Duration
}

// Tick for elapsed timer
type tickMsg time.Time

// jobsRefreshMsg carries a fresh slice of recent tickets.
type jobsRefreshMsg []pantry.Ticket

// NewModel creates the TUI model.
// store may be nil — if so, the jobs panel renders "Jobs unavailable".
func NewModel(dispatchFn DispatchFunc, store *pantry.TicketStore) Model {
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
		input:       ti,
		output:      vp,
		dispatchFn:  dispatchFn,
		historyIdx:  -1,
		ticketStore: store,
	}
}

// jobsRefreshCmd fetches recent tickets and returns a jobsRefreshMsg.
// If store is nil the command is a no-op (returns nil slice).
func jobsRefreshCmd(store *pantry.TicketStore) tea.Cmd {
	return func() tea.Msg {
		if store == nil {
			return jobsRefreshMsg(nil)
		}
		tickets, err := store.ListRecent(jobsPanelMaxRows)
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
		tickets, err := store.ListRecent(jobsPanelMaxRows)
		if err != nil {
			return jobsRefreshMsg(nil)
		}
		return jobsRefreshMsg(tickets)
	})
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
		m.output.Width = msg.Width - 30  // leave room for side panels
		m.output.Height = msg.Height - 6 // leave room for input + borders
		m.input.Width = msg.Width - 4
		m.ready = true

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+d":
			return m, tea.Quit
		case "ctrl+c":
			if m.dispatching {
				if m.cancelFn != nil {
					m.cancelFn()
				}
				m.dispatching = false
				m.processMap.status = "cancelled"
				return m, nil
			}
			return m, tea.Quit
		case "enter":
			if m.dispatching || m.input.Value() == "" {
				return m, nil
			}
			return m, m.startDispatch()
		case "up":
			if len(m.history) > 0 && m.historyIdx < len(m.history)-1 {
				m.historyIdx++
				m.input.SetValue(m.history[len(m.history)-1-m.historyIdx])
			}
		case "down":
			if m.historyIdx > 0 {
				m.historyIdx--
				m.input.SetValue(m.history[len(m.history)-1-m.historyIdx])
			} else {
				m.historyIdx = -1
				m.input.SetValue("")
			}
		}

	case lineMsg:
		m.outputLines = append(m.outputLines, string(msg))
		m.output.SetContent(strings.Join(m.outputLines, "\n"))
		m.output.GotoBottom()

	case dispatchDoneMsg:
		m.dispatching = false
		m.processMap.status = "done"
		if msg.err != nil {
			m.processMap.status = "failed"
		}
		m.processMap.elapsed = msg.duration
		m.ledgerLog = append(m.ledgerLog, ledgerLine{
			time:    time.Now().Format("15:04"),
			kitchen: msg.decision.Kitchen,
			dur:     fmt.Sprintf("%.1fs", msg.duration.Seconds()),
			status:  m.processMap.status,
		})

	case tickMsg:
		if m.dispatching {
			m.processMap.elapsed = time.Since(m.processMap.startedAt)
			return m, tickCmd()
		}

	case jobsRefreshMsg:
		m.jobTickets = []pantry.Ticket(msg)
		return m, tea.Batch(scheduleJobsRefresh(m.ticketStore))
	}

	var inputCmd tea.Cmd
	m.input, inputCmd = m.input.Update(msg)
	cmds = append(cmds, inputCmd)

	var vpCmd tea.Cmd
	m.output, vpCmd = m.output.Update(msg)
	cmds = append(cmds, vpCmd)

	return m, tea.Batch(cmds...)
}

func (m *Model) startDispatch() tea.Cmd {
	prompt := m.input.Value()
	m.history = append(m.history, prompt)
	m.historyIdx = -1
	m.input.SetValue("")
	m.outputLines = nil
	m.output.SetContent("")
	m.dispatching = true

	// Parse @kitchen prefix
	kitchenForce := ""
	if strings.HasPrefix(prompt, "@") {
		parts := strings.SplitN(prompt, " ", 2)
		kitchenForce = strings.TrimPrefix(parts[0], "@")
		if len(parts) > 1 {
			prompt = parts[1]
		}
	}

	m.processMap = processState{
		active:    true,
		kitchen:   kitchenForce,
		status:    "routing",
		startedAt: time.Now(),
	}

	ctx, cancel := context.WithCancel(context.Background())
	m.cancelFn = cancel

	return tea.Batch(
		func() tea.Msg {
			start := time.Now()
			result, decision, err := m.dispatchFn(ctx, prompt, kitchenForce)
			dur := time.Since(start)
			return dispatchDoneMsg{result: result, decision: decision, err: err, duration: dur}
		},
		tickCmd(),
	)
}

func tickCmd() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m Model) View() string {
	if !m.ready {
		return "Loading..."
	}

	// Process map (top-right)
	processMap := m.renderProcessMap()

	// Output viewport (center)
	outputPanel := panelBorder.
		Width(m.width - 28).
		Height(m.height - 6).
		Render(m.output.View())

	// Ledger panel (right)
	ledgerPanel := panelBorder.
		Width(24).
		Height((m.height - 6) / 2).
		Render(m.renderLedger())

	// Jobs panel (right, below ledger)
	jobsPanel := RenderJobsPanel(m.jobTickets, 24)

	// Combine output + process map + ledger + jobs
	mainArea := lipgloss.JoinHorizontal(lipgloss.Top,
		outputPanel,
		lipgloss.JoinVertical(lipgloss.Left, processMap, ledgerPanel, jobsPanel),
	)

	// Input at bottom
	inputBar := panelBorder.Width(m.width - 2).Render(m.input.View())

	// Title
	title := titleStyle.Width(m.width).Render("Milliways — The Restaurant at the End of the Universe")

	return lipgloss.JoinVertical(lipgloss.Left, title, mainArea, inputBar)
}

func (m Model) renderProcessMap() string {
	if !m.processMap.active {
		return panelBorder.Width(24).Height(6).Render(mutedStyle.Render("No active dispatch"))
	}

	lines := []string{}
	if m.processMap.kitchen != "" {
		lines = append(lines, KitchenBadge(m.processMap.kitchen))
	}
	lines = append(lines, StatusIcon(m.processMap.status)+" "+m.processMap.status)
	lines = append(lines, mutedStyle.Render(fmt.Sprintf("%.1fs", m.processMap.elapsed.Seconds())))

	if m.processMap.risk != "" {
		lines = append(lines, "Risk: "+m.processMap.risk)
	}

	return panelBorder.Width(24).Height(6).Render(strings.Join(lines, "\n"))
}

func (m Model) renderLedger() string {
	if len(m.ledgerLog) == 0 {
		return mutedStyle.Render("No dispatches yet")
	}

	lines := []string{mutedStyle.Render("Ledger")}
	start := 0
	if len(m.ledgerLog) > 8 {
		start = len(m.ledgerLog) - 8
	}

	for _, l := range m.ledgerLog[start:] {
		lines = append(lines, fmt.Sprintf("%s %s %s %s",
			mutedStyle.Render(l.time),
			KitchenBadge(l.kitchen),
			l.dur,
			StatusIcon(l.status),
		))
	}
	return strings.Join(lines, "\n")
}

// Run starts the TUI with an optional ticket store for the jobs panel.
// Pass nil for store to disable jobs panel.
func Run(dispatchFn DispatchFunc, store *pantry.TicketStore) error {
	p := tea.NewProgram(
		NewModel(dispatchFn, store),
		tea.WithAltScreen(),
	)
	_, err := p.Run()
	return err
}
