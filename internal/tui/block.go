package tui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/mwigge/milliways/internal/conversation"
	"github.com/mwigge/milliways/internal/kitchen/adapter"
	"github.com/mwigge/milliways/internal/sommelier"
)

// LineType classifies the kind of output line in a block.
type LineType int

const (
	LineText   LineType = iota // Plain text from kitchen
	LineCode                   // Syntax-highlighted code block
	LineTool                   // Tool use notification
	LineSystem                 // System message (routing, quota, etc.)
)

// OutputLine is a single line of kitchen output within a block.
type OutputLine struct {
	Kitchen  string   `json:"kitchen"`
	Type     LineType `json:"type"`
	Text     string   `json:"text"`
	Language string   `json:"language,omitempty"` // for LineCode
}

// RenderMode controls how viewport content is displayed.
type RenderMode int

const (
	RenderRaw     RenderMode = iota // Raw markdown + syntax-highlighted code
	RenderGlamour                   // Full glamour-rendered markdown
)

// Block represents a single dispatch lifecycle in the TUI.
// Each Block owns its state, output, and adapter — enabling concurrent dispatch.
type Block struct {
	ID                 string
	ConversationID     string
	Prompt             string
	Kitchen            string
	PID                int
	ContinuationPrompt string
	ProviderChain      []string
	Decision           sommelier.Decision
	State              DispatchState
	Lines              []OutputLine
	Collapsed          bool
	Focused            bool
	StartedAt          time.Time
	Duration           time.Duration
	Cost               *adapter.CostInfo
	ExitCode           int
	Rated              *bool
	Conversation       *conversation.Conversation

	// Lifecycle — not serialized.
	CancelFn      context.CancelFunc
	ActiveAdapter adapter.Adapter

	// Per-block scroll offset (lines from top of body).
	ScrollOffset int
}

func (b *Block) appendSystemLine(text string) {
	if b == nil || strings.TrimSpace(text) == "" {
		return
	}
	b.Lines = append(b.Lines, OutputLine{
		Kitchen: "milliways",
		Type:    LineSystem,
		Text:    text,
	})
}

// Block border styles.
var (
	blockBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#374151"))

	focusedBlockBorder = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("#8B5CF6"))

	doneBlockBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#10B981"))

	failedBlockBorder = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("#EF4444"))
)

// RenderHeader returns the block header: prompt + kitchen badge + state icon + duration.
func (b *Block) RenderHeader() string {
	prompt := b.Prompt
	if len(prompt) > 50 {
		prompt = prompt[:47] + "..."
	}

	parts := []string{promptStyle.Render("▶ " + prompt)}

	if b.Kitchen != "" {
		parts = append(parts, KitchenBadge(b.Kitchen))
	}
	if len(b.ProviderChain) > 1 {
		parts = append(parts, mutedStyle.Render(strings.Join(b.ProviderChain, " -> ")))
	}

	parts = append(parts, stateIcon(b.State))

	elapsed := b.elapsed()
	if elapsed > 0 {
		parts = append(parts, mutedStyle.Render(fmt.Sprintf("%.0fs", elapsed.Seconds())))
	}

	return strings.Join(parts, "  ")
}

// RenderSeparator returns a horizontal line for between header and body.
func (b *Block) RenderSeparator(width int) string {
	w := width - 4
	if w < 10 {
		w = 10
	}
	if w > 80 {
		w = 80
	}
	return mutedStyle.Render(strings.Repeat("─", w))
}

// RenderBody renders the output lines with kitchen prefixes.
// When maxLines > 0, applies ScrollOffset to show a window of maxLines.
func (b *Block) RenderBody(width int, mode RenderMode) string {
	sections := make([]string, 0, 3)
	if telemetry := b.renderTelemetry(); telemetry != "" {
		sections = append(sections, telemetry)
	}

	output := b.renderOutput(mode)
	if output != "" {
		sections = append(sections, output)
	} else if len(b.Lines) == 0 && (b.State == StateDone || b.State == StateFailed) {
		// Silent failure: block completed (done/failed) but captured no output.
		sections = append(sections, mutedStyle.Render("(no output)"))
	} else {
		placeholder := b.renderPlaceholder()
		if placeholder != "" {
			sections = append(sections, placeholder)
		}
	}

	if footer := b.renderFooter(); footer != "" {
		sections = append(sections, footer)
	}

	return strings.Join(sections, "\n")
}

// RenderBodyWindow renders a scrollable window of the body.
// maxLines is the visible height; ScrollOffset determines where to start.
func (b *Block) RenderBodyWindow(width, maxLines int, mode RenderMode) string {
	full := b.RenderBody(width, mode)
	if maxLines <= 0 {
		return full
	}

	lines := strings.Split(full, "\n")
	totalLines := len(lines)

	// Clamp scroll offset.
	if b.ScrollOffset > totalLines-maxLines {
		b.ScrollOffset = totalLines - maxLines
	}
	if b.ScrollOffset < 0 {
		b.ScrollOffset = 0
	}

	if totalLines <= maxLines {
		return full
	}

	end := b.ScrollOffset + maxLines
	if end > totalLines {
		end = totalLines
	}

	windowed := lines[b.ScrollOffset:end]
	result := strings.Join(windowed, "\n")

	// Show scroll indicator if not at bottom.
	if end < totalLines {
		result += "\n" + mutedStyle.Render(fmt.Sprintf("  ↓ %d more lines", totalLines-end))
	}

	return result
}

// ScrollDown moves the scroll offset down by n lines.
func (b *Block) ScrollDown(n int) {
	b.ScrollOffset += n
}

// ScrollUp moves the scroll offset up by n lines.
func (b *Block) ScrollUp(n int) {
	b.ScrollOffset -= n
	if b.ScrollOffset < 0 {
		b.ScrollOffset = 0
	}
}

// Render returns the full block view (header + separator + body) or header-only if collapsed.
// maxBodyLines controls the body window size; 0 means unlimited.
func (b *Block) Render(width, maxBodyLines int, mode RenderMode) string {
	border := b.borderStyle()

	if b.Collapsed {
		return border.Width(width).Render(b.RenderHeader())
	}

	var body string
	if maxBodyLines > 0 {
		body = b.RenderBodyWindow(width, maxBodyLines, mode)
	} else {
		body = b.RenderBody(width, mode)
	}

	content := b.RenderHeader() + "\n" +
		b.RenderSeparator(width) + "\n" +
		body

	return border.Width(width).Render(content)
}

// AppendEvent adds an adapter event to this block's output.
func (b *Block) AppendEvent(event adapter.Event) {
	switch event.Type {
	case adapter.EventText:
		b.Lines = append(b.Lines, OutputLine{
			Kitchen: event.Kitchen,
			Type:    LineText,
			Text:    event.Text,
		})
	case adapter.EventCodeBlock:
		b.Lines = append(b.Lines, OutputLine{
			Kitchen:  event.Kitchen,
			Type:     LineCode,
			Text:     event.Code,
			Language: event.Language,
		})
	case adapter.EventToolUse:
		label := event.ToolName
		if event.ToolStatus != "" {
			label += " (" + event.ToolStatus + ")"
		}
		b.Lines = append(b.Lines, OutputLine{
			Kitchen: event.Kitchen,
			Type:    LineTool,
			Text:    label,
		})
	case adapter.EventError, adapter.EventRateLimit:
		text := event.Text
		if event.RateLimit != nil {
			text = fmt.Sprintf("rate limit: %s (resets %s)", event.RateLimit.Status, event.RateLimit.ResetsAt.Format("15:04"))
		}
		b.Lines = append(b.Lines, OutputLine{
			Kitchen: event.Kitchen,
			Type:    LineSystem,
			Text:    text,
		})
	}
}

// Complete marks the block as done.
func (b *Block) Complete(exitCode int, cost *adapter.CostInfo) {
	if exitCode == 0 {
		b.State = StateDone
	} else {
		b.State = StateFailed
	}
	b.ExitCode = exitCode
	b.Duration = time.Since(b.StartedAt)
	b.Cost = cost
}

// ToggleCollapse toggles the collapsed state.
func (b *Block) ToggleCollapse() {
	b.Collapsed = !b.Collapsed
}

// IsActive returns true if the block is in an active dispatch state.
func (b *Block) IsActive() bool {
	switch b.State {
	case StateRouting, StateRouted, StateStreaming, StateAwaiting, StateConfirming:
		return true
	default:
		return false
	}
}

func (b *Block) isDone() bool {
	return b.State == StateDone || b.State == StateFailed || b.State == StateCancelled
}

func (b *Block) elapsed() time.Duration {
	if b.Duration > 0 {
		return b.Duration
	}
	if b.StartedAt.IsZero() {
		return 0
	}
	return time.Since(b.StartedAt)
}

func (b *Block) renderTelemetry() string {
	rows := make([]string, 0, 5)

	if session := b.renderSessionTelemetry(); session != "" {
		rows = append(rows, session)
	}
	if usage := b.renderUsageTelemetry(); usage != "" {
		rows = append(rows, usage)
	}
	if runtime := b.renderRuntimeTelemetry(); runtime != "" {
		rows = append(rows, runtime)
	}
	if context := b.renderContextTelemetry(); context != "" {
		rows = append(rows, context)
	}
	if mcp := b.renderMCPTelemetry(); mcp != "" {
		rows = append(rows, mcp)
	}

	return strings.Join(rows, "\n")
}

func (b *Block) renderSessionTelemetry() string {
	parts := make([]string, 0, 4)
	if started := b.sessionStart(); !started.IsZero() {
		parts = append(parts, "New session - "+started.Format("2006-01-02 15:04:05 MST"))
	}
	if elapsed := b.elapsed().Round(time.Second); elapsed > 0 {
		parts = append(parts, "elapsed "+elapsed.String())
	}
	if b.Kitchen != "" {
		parts = append(parts, "kitchen "+b.Kitchen)
	}
	if sticky := b.stickyKitchen(); sticky != "" {
		parts = append(parts, "sticky "+sticky)
	}
	if len(parts) == 0 {
		return ""
	}
	return renderTelemetrySection("Session", parts)
}

func (b *Block) renderUsageTelemetry() string {
	if b.Cost == nil {
		return ""
	}
	parts := make([]string, 0, 4)
	if b.Cost.InputTokens > 0 {
		parts = append(parts, fmt.Sprintf("%d in", b.Cost.InputTokens))
	}
	if b.Cost.OutputTokens > 0 {
		parts = append(parts, fmt.Sprintf("%d out", b.Cost.OutputTokens))
	}
	if b.Cost.USD > 0 {
		parts = append(parts, fmt.Sprintf("$%.2f", b.Cost.USD))
	}
	if len(parts) == 0 {
		return ""
	}
	return renderTelemetrySection("Usage", parts)
}

func (b *Block) renderRuntimeTelemetry() string {
	parts := make([]string, 0, 3)
	if switches, ok := b.switchCount(); ok {
		parts = append(parts, pluralizeCount(switches, "switch"))
	}
	if segments, ok := b.segmentCount(); ok {
		parts = append(parts, pluralizeCount(segments, "segment"))
	}
	if checkpoints, ok := b.checkpointCount(); ok {
		parts = append(parts, pluralizeCount(checkpoints, "checkpoint"))
	}
	if len(parts) == 0 {
		return ""
	}
	return renderTelemetrySection("Progress", parts)
}

func (b *Block) renderContextTelemetry() string {
	parts := make([]string, 0, 2)
	if b.ContinuationPrompt != "" {
		parts = append(parts, "continuation ready")
	}
	if b.hasContextBundle() {
		parts = append(parts, "bundle restored")
	}
	if len(parts) == 0 {
		return ""
	}
	return renderTelemetrySection("Context", parts)
}

func (b *Block) renderMCPTelemetry() string {
	parts := make([]string, 0, 3)
	if envConfigured("MILLIWAYS_MEMPALACE_MCP_CMD") {
		parts = append(parts, "MemPalace configured")
	}
	if envConfigured("MILLIWAYS_CODEGRAPH_MCP_CMD") {
		parts = append(parts, "CodeGraph configured")
	}
	parts = append(parts, "task-queue unknown")
	if len(parts) == 0 {
		return ""
	}
	return renderTelemetrySection("MCP", parts)
}

func (b *Block) renderOutput(mode RenderMode) string {
	if len(b.Lines) == 0 {
		return ""
	}

	var buf strings.Builder
	for _, line := range b.Lines {
		prefix := kitchenPrefix(line.Kitchen)

		switch line.Type {
		case LineCode:
			code := line.Text
			if mode == RenderRaw && line.Language != "" {
				code = highlightCode(code, line.Language)
			}
			for _, codeLine := range strings.Split(code, "\n") {
				buf.WriteString(prefix + codeLine + "\n")
			}
		case LineTool:
			buf.WriteString(prefix + mutedStyle.Render("⚙ "+line.Text) + "\n")
		case LineSystem:
			buf.WriteString(mutedStyle.Render("[milliways] "+line.Text) + "\n")
		default:
			buf.WriteString(prefix + line.Text + "\n")
		}
	}

	return strings.TrimRight(buf.String(), "\n")
}

func (b *Block) renderPlaceholder() string {
	switch b.State {
	case StateRouting:
		return mutedStyle.Render("routing...")
	case StateStreaming, StateRouted:
		return mutedStyle.Render("waiting for output...")
	default:
		return ""
	}
}

func (b *Block) renderFooter() string {
	if !b.isDone() {
		return ""
	}
	status := "done"
	icon := successStyle.Render("✓")
	if b.ExitCode != 0 {
		status = "failed"
		icon = failureStyle.Render("✗")
	}
	footer := fmt.Sprintf("%s %s  %s  %.1fs", icon, b.Kitchen, status, b.elapsed().Seconds())
	if b.Cost != nil && b.Cost.USD > 0 {
		footer += fmt.Sprintf("  $%.2f", b.Cost.USD)
	}
	return footer
}

func (b *Block) sessionStart() time.Time {
	if !b.StartedAt.IsZero() {
		return b.StartedAt
	}
	if b.Conversation != nil {
		return b.Conversation.CreatedAt
	}
	return time.Time{}
}

func (b *Block) stickyKitchen() string {
	if b.Conversation == nil {
		return ""
	}
	return b.Conversation.Memory.StickyKitchen
}

func (b *Block) hasContextBundle() bool {
	if b.Conversation == nil {
		return false
	}
	ctx := b.Conversation.Context
	return ctx.CodeGraphText != "" || ctx.MemPalaceText != "" || len(ctx.SpecRefs) > 0 || ctx.InvalidatedMemoryCount > 0
}

func (b *Block) switchCount() (int, bool) {
	if len(b.ProviderChain) > 0 {
		return maxInt(len(b.ProviderChain)-1, 0), true
	}
	if b.Conversation != nil && len(b.Conversation.Segments) > 0 {
		return maxInt(len(b.Conversation.Segments)-1, 0), true
	}
	return 0, false
}

func (b *Block) segmentCount() (int, bool) {
	if b.Conversation != nil && len(b.Conversation.Segments) > 0 {
		return len(b.Conversation.Segments), true
	}
	return 0, false
}

func (b *Block) checkpointCount() (int, bool) {
	if b.Conversation != nil {
		return len(b.Conversation.Checkpoints), true
	}
	return 0, false
}

func envConfigured(name string) bool {
	value, ok := os.LookupEnv(name)
	return ok && strings.TrimSpace(value) != ""
}

func pluralizeCount(count int, noun string) string {
	if count == 1 {
		return fmt.Sprintf("%d %s", count, noun)
	}
	return fmt.Sprintf("%d %ss", count, noun)
}

func renderTelemetrySection(label string, parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	return label + " │ " + strings.Join(parts, " · ")
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// kitchenPrefix returns a color-coded [kitchen] prefix string.
func kitchenPrefix(name string) string {
	if name == "" {
		return ""
	}
	return KitchenBadge(name) + " "
}

func (b *Block) borderStyle() lipgloss.Style {
	if b.Focused {
		return focusedBlockBorder
	}
	switch b.State {
	case StateDone:
		return doneBlockBorder
	case StateFailed:
		return failedBlockBorder
	default:
		return blockBorder
	}
}
