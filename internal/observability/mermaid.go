package observability

import (
	"fmt"
	"sort"
	"strings"
)

// GenerateTimeline renders trace events as a Mermaid timeline diagram.
func GenerateTimeline(events []AgentTraceEvent) string {
	sorted := sortedTraceEvents(events)
	lines := []string{"timeline", fmt.Sprintf("    title Agent Session %s", traceSessionID(sorted))}
	for _, event := range sorted {
		lines = append(lines, fmt.Sprintf("    %s : %s: %s", mermaidTimestamp(event), event.Type, traceDescription(event)))
	}
	return strings.Join(lines, "\n")
}

// GenerateCallGraph renders trace events as a Mermaid flowchart.
func GenerateCallGraph(events []AgentTraceEvent) string {
	sorted := sortedTraceEvents(events)
	type edge struct{ from, to string }

	nodeIDs := map[string]string{}
	nodeLabels := make([]string, 0)
	edges := make([]edge, 0, len(sorted))
	addNode := func(label string) string {
		if id, ok := nodeIDs[label]; ok {
			return id
		}
		id := fmt.Sprintf("N%d", len(nodeIDs))
		nodeIDs[label] = id
		nodeLabels = append(nodeLabels, label)
		return id
	}

	previous := "Orchestrator"
	addNode(previous)
	for _, event := range sorted {
		from := traceEdgeSource(event, previous)
		to := traceEdgeTarget(event)
		addNode(from)
		addNode(to)
		edges = append(edges, edge{from: from, to: to})
		previous = to
	}

	lines := []string{"flowchart TD"}
	for _, label := range nodeLabels {
		lines = append(lines, fmt.Sprintf("    %s[%s]", nodeIDs[label], escapeMermaidLabel(label)))
	}
	for _, edge := range edges {
		lines = append(lines, fmt.Sprintf("    %s --> %s", nodeIDs[edge.from], nodeIDs[edge.to]))
	}
	if len(edges) == 0 {
		lines = append(lines, "    N0[Orchestrator]")
	}
	return strings.Join(lines, "\n")
}

// GenerateDecisionTree renders decision events as a Mermaid flowchart.
func GenerateDecisionTree(events []AgentTraceEvent) string {
	sorted := sortedTraceEvents(events)
	lines := []string{"flowchart TD"}
	decisionCount := 0
	for _, event := range sorted {
		options, ok := traceStringList(event.Data, "options")
		if !ok || len(options) == 0 {
			continue
		}
		choice, _ := traceString(event.Data, "choice")
		decisionID := fmt.Sprintf("D%d", decisionCount)
		lines = append(lines, fmt.Sprintf("    %s{%s}", decisionID, escapeMermaidLabel(traceDescription(event))))
		for index, option := range options {
			optionID := fmt.Sprintf("D%dO%d", decisionCount, index)
			label := option
			if choice != "" && choice == option {
				label += " ✓"
			}
			lines = append(lines, fmt.Sprintf("    %s -->|%s| %s[%s]", decisionID, escapeMermaidLabel(option), optionID, escapeMermaidLabel(label)))
		}
		decisionCount++
	}
	if decisionCount == 0 {
		lines = append(lines, "    D0[No decision events]")
	}
	return strings.Join(lines, "\n")
}

func sortedTraceEvents(events []AgentTraceEvent) []AgentTraceEvent {
	cloned := append([]AgentTraceEvent(nil), events...)
	sort.SliceStable(cloned, func(i, j int) bool {
		return cloned[i].Timestamp.Before(cloned[j].Timestamp)
	})
	return cloned
}

func traceSessionID(events []AgentTraceEvent) string {
	for _, event := range events {
		if event.SessionID != "" {
			return event.SessionID
		}
	}
	return "unknown"
}

func mermaidTimestamp(event AgentTraceEvent) string {
	if event.Timestamp.IsZero() {
		return "unknown"
	}
	return event.Timestamp.UTC().Format("2006-01-02 15:04:05Z")
}

func traceDescription(event AgentTraceEvent) string {
	if event.Description != "" {
		return escapeMermaidLabel(event.Description)
	}
	return escapeMermaidLabel(event.Type)
}

func traceEdgeSource(event AgentTraceEvent, fallback string) string {
	if from, ok := traceString(event.Data, "from"); ok {
		return from
	}
	if event.Parent != "" {
		return event.Parent
	}
	if event.Actor != "" {
		return event.Actor
	}
	return fallback
}

func traceEdgeTarget(event AgentTraceEvent) string {
	if to, ok := traceString(event.Data, "to"); ok {
		return to
	}
	if toolName, ok := traceString(event.Data, "tool_name"); ok {
		return "tool: " + toolName
	}
	if strings.Contains(event.Type, "tool") {
		return "tool: " + traceDescription(event)
	}
	if strings.Contains(event.Type, "delegate") {
		return "delegate: " + traceDescription(event)
	}
	return fmt.Sprintf("%s: %s", event.Type, traceDescription(event))
}

func escapeMermaidLabel(value string) string {
	replacer := strings.NewReplacer("\n", " ", "\r", " ", "[", "(", "]", ")", "\"", "'")
	return replacer.Replace(value)
}

func traceString(data map[string]any, key string) (string, bool) {
	if len(data) == 0 {
		return "", false
	}
	raw, ok := data[key]
	if !ok {
		return "", false
	}
	value, ok := raw.(string)
	if !ok {
		return "", false
	}
	if strings.TrimSpace(value) == "" {
		return "", false
	}
	return value, true
}

func traceStringList(data map[string]any, key string) ([]string, bool) {
	if len(data) == 0 {
		return nil, false
	}
	raw, ok := data[key]
	if !ok {
		return nil, false
	}
	rawList, ok := raw.([]any)
	if !ok {
		stringList, ok := raw.([]string)
		if ok && len(stringList) > 0 {
			return stringList, true
		}
		return nil, false
	}
	value := make([]string, 0, len(rawList))
	for _, item := range rawList {
		text, ok := item.(string)
		if !ok {
			return nil, false
		}
		value = append(value, text)
	}
	if len(value) == 0 {
		return nil, false
	}
	return value, true
}
