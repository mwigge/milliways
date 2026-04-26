package tui

import (
	"fmt"
	"strings"

	"github.com/mwigge/milliways/internal/observability"
)

type tracePanelView int

const (
	tracePanelEvents tracePanelView = iota
	tracePanelTimeline
	tracePanelGraph
	tracePanelViewCount
)

func (m Model) renderTracePanel(width, height int) string {
	innerWidth := max(1, width-4)
	sessions, sessionEvents, selectedSession, loadErr := m.loadSelectedTraceSession()
	lines := []string{fmt.Sprintf("View: %s", m.currentTracePanelViewName())}
	if loadErr != nil {
		lines = append(lines, truncate(loadErr.Error(), innerWidth))
	}
	if len(sessions) > 0 {
		lines = append(lines, truncate(fmt.Sprintf("Session: %s (%d/%d)", selectedSession, m.traceSessionIndex(len(sessions))+1, len(sessions)), innerWidth))
	}

	switch m.tracePanelView {
	case tracePanelTimeline:
		lines = append(lines, m.traceTimelineLines(innerWidth, max(1, height-len(lines)-1), sessionEvents)...)
	case tracePanelGraph:
		lines = append(lines, m.traceGraphLines(innerWidth, max(1, height-len(lines)-1), selectedSession, sessionEvents)...)
	default:
		lines = append(lines, m.traceEventLines(innerWidth, max(1, height-len(lines)-1), sessionEvents)...)
	}

	footer := "[tab] switch view"
	if len(sessions) > 1 {
		footer += "  [↑↓] session"
	}
	lines = append(lines, "", mutedStyle.Render(footer))
	if height > 0 && len(lines) > height {
		lines = lines[:height]
	}
	return strings.Join(lines, "\n")
}

func (m Model) currentTracePanelViewName() string {
	switch m.tracePanelView {
	case tracePanelTimeline:
		return "Timeline"
	case tracePanelGraph:
		return "Graph"
	default:
		return "Events"
	}
}

func (m *Model) advanceTracePanelView() {
	m.tracePanelView = (m.tracePanelView + 1) % tracePanelViewCount
}

func (m *Model) moveTraceSessionSelection(delta int) {
	if delta == 0 {
		return
	}
	sessions, err := observability.ListTraceSessions()
	if err != nil || len(sessions) == 0 {
		m.traceSessionSelected = 0
		return
	}
	m.traceSessionSelected = (m.traceSessionIndex(len(sessions)) + delta + len(sessions)) % len(sessions)
}

func (m Model) traceSessionIndex(count int) int {
	if count <= 0 {
		return 0
	}
	if m.traceSessionSelected < 0 {
		return 0
	}
	if m.traceSessionSelected >= count {
		return count - 1
	}
	return m.traceSessionSelected
}

func (m Model) loadSelectedTraceSession() ([]string, []observability.AgentTraceEvent, string, error) {
	sessions, err := observability.ListTraceSessions()
	if err != nil {
		return nil, nil, "", err
	}
	if len(sessions) == 0 {
		return nil, normalizeRuntimeEvents(m.runtimeEvents), "", nil
	}
	selected := sessions[m.traceSessionIndex(len(sessions))]
	events, err := observability.ReadTraceEvents(selected)
	if err != nil {
		return sessions, nil, selected, err
	}
	return sessions, events, selected, nil
}

func (m Model) traceEventLines(width, limit int, sessionEvents []observability.AgentTraceEvent) []string {
	if len(sessionEvents) > 0 {
		lines := make([]string, 0, min(limit, len(sessionEvents)))
		for _, event := range recentAgentTraceEvents(sessionEvents, limit) {
			line := fmt.Sprintf("%s [%s] %s", event.Timestamp.Format("15:04:05"), event.Type, traceEventText(event))
			lines = append(lines, truncate(line, width))
		}
		return lines
	}
	if len(m.runtimeEvents) == 0 {
		return []string{mutedStyle.Render("(no trace events)")}
	}
	lines := make([]string, 0, min(limit, len(m.runtimeEvents)))
	for _, event := range recentTraceEvents(m.runtimeEvents, limit) {
		line := fmt.Sprintf("%s [%s] %s", event.At.Format("15:04:05"), event.Kind, event.Text)
		if strings.TrimSpace(event.Text) == "" {
			line = fmt.Sprintf("%s [%s] %s", event.At.Format("15:04:05"), event.Kind, event.Provider)
		}
		lines = append(lines, truncate(line, width))
	}
	return lines
}

func (m Model) traceTimelineLines(width, limit int, events []observability.AgentTraceEvent) []string {
	if len(events) == 0 {
		return []string{mutedStyle.Render("(no timeline available)")}
	}
	lines := make([]string, 0, min(limit, len(events)+1))
	for _, event := range recentAgentTraceEvents(events, limit) {
		lines = append(lines, truncate(fmt.Sprintf("├─ %s %s", event.Timestamp.Format("15:04:05"), event.Type+": "+event.Description), width))
	}
	lines = append(lines, mutedStyle.Render("Mermaid source via: milliways trace diagram <session-id>"))
	return lines
}

func (m Model) traceGraphLines(width, limit int, sessionID string, events []observability.AgentTraceEvent) []string {
	if len(events) == 0 {
		return []string{mutedStyle.Render("(no graph available)")}
	}
	mermaid := strings.Split(observability.GenerateCallGraph(events), "\n")
	lines := []string{mutedStyle.Render("Mermaid call graph")}
	for _, line := range mermaid {
		lines = append(lines, truncate(line, width))
		if len(lines) >= limit {
			break
		}
	}
	if len(lines) == 1 && sessionID != "" {
		lines = append(lines, mutedStyle.Render("milliways trace diagram "+sessionID+" --graph"))
	}
	return lines
}

func traceEventText(event observability.AgentTraceEvent) string {
	if strings.TrimSpace(event.Description) != "" {
		return event.Description
	}
	if toolName, ok := event.Data["tool_name"].(string); ok && strings.TrimSpace(toolName) != "" {
		return toolName
	}
	if toolName, ok := event.Data["tool"].(string); ok && strings.TrimSpace(toolName) != "" {
		return toolName
	}
	return event.Type
}

func normalizeRuntimeEvents(events []observability.Event) []observability.AgentTraceEvent {
	out := make([]observability.AgentTraceEvent, 0, len(events))
	for _, event := range events {
		out = append(out, observability.AgentTraceEvent{
			SessionID:   event.ConversationID,
			Timestamp:   event.At,
			Type:        event.Kind,
			Description: event.Text,
			Actor:       event.Provider,
			Data:        stringMapToAny(event.Fields),
		})
	}
	return out
}

func stringMapToAny(fields map[string]string) map[string]any {
	if len(fields) == 0 {
		return nil
	}
	out := make(map[string]any, len(fields))
	for key, value := range fields {
		out[key] = value
	}
	return out
}

func recentTraceEvents(events []observability.Event, limit int) []observability.Event {
	if len(events) <= limit {
		return events
	}
	return events[len(events)-limit:]
}

func recentAgentTraceEvents(events []observability.AgentTraceEvent, limit int) []observability.AgentTraceEvent {
	if len(events) <= limit {
		return events
	}
	return events[len(events)-limit:]
}
