package tui

import (
	"context"
	"fmt"
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
	LineCode                  // Syntax-highlighted code block
	LineTool                  // Tool use notification
	LineSystem                // System message (routing, quota, etc.)
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
	ID             string
	ConversationID string
	Prompt         string
	Kitchen        string
	ProviderChain  []string
	Decision       sommelier.Decision
	State          DispatchState
	Lines          []OutputLine
	Collapsed      bool
	Focused        bool
	StartedAt      time.Time
	Duration       time.Duration
	Cost           *adapter.CostInfo
	ExitCode       int
	Rated          *bool
	Conversation   *conversation.Conversation

	// Lifecycle — not serialized.
	CancelFn      context.CancelFunc
	ActiveAdapter adapter.Adapter

	// Per-block scroll offset (lines from top of body).
	ScrollOffset int
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
	if len(b.Lines) == 0 {
		if b.State == StateRouting {
			return mutedStyle.Render("routing...")
		}
		if b.State == StateStreaming || b.State == StateRouted {
			return mutedStyle.Render("waiting for output...")
		}
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

	// Footer with cost if done.
	if b.isDone() {
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
		buf.WriteString(footer + "\n")
	}

	return buf.String()
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
