package main

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/mwigge/milliways/internal/rpc"
)

const maxParallelColumns = 3

func renderParallelComparison(status rpc.GroupStatusResult, consensus string, width int) string {
	if width < 60 {
		width = 60
	}
	if width > 140 {
		width = 140
	}

	var b strings.Builder
	shortID := status.GroupID
	if len(shortID) > 12 {
		shortID = shortID[:12]
	}
	fmt.Fprintf(&b, "parallel comparison %s  status %s\n", shortID, emptyDash(status.Status))
	if status.Prompt != "" {
		fmt.Fprintf(&b, "prompt: %s\n", trimDisplay(status.Prompt, width-8))
	}

	if len(status.Slots) == 0 {
		b.WriteString("no slots\n")
		return b.String()
	}

	for start := 0; start < len(status.Slots); start += maxParallelColumns {
		end := start + maxParallelColumns
		if end > len(status.Slots) {
			end = len(status.Slots)
		}
		b.WriteString(renderSlotRow(status.Slots[start:end], width))
	}

	b.WriteString(renderAgreement(status.Slots, width))
	if strings.TrimSpace(consensus) != "" {
		b.WriteByte('\n')
		b.WriteString(strings.TrimRight(consensus, "\n"))
		b.WriteByte('\n')
	}
	return b.String()
}

func renderSlotRow(slots []rpc.GroupSlotStatus, width int) string {
	if len(slots) == 0 {
		return ""
	}
	gap := 3
	colWidth := (width - gap*(len(slots)-1)) / len(slots)
	if colWidth < 18 {
		colWidth = 18
	}
	cells := make([][]string, 0, len(slots))
	maxLines := 0
	for _, slot := range slots {
		lines := renderSlotCell(slot, colWidth)
		if len(lines) > maxLines {
			maxLines = len(lines)
		}
		cells = append(cells, lines)
	}

	var b strings.Builder
	for line := 0; line < maxLines; line++ {
		for i, cell := range cells {
			if i > 0 {
				b.WriteString(strings.Repeat(" ", gap))
			}
			part := ""
			if line < len(cell) {
				part = cell[line]
			}
			b.WriteString(padDisplay(part, colWidth))
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func renderSlotCell(slot rpc.GroupSlotStatus, width int) []string {
	inner := width - 2
	if inner < 10 {
		inner = 10
	}
	title := fmt.Sprintf("%s %s", emptyDash(slot.Provider), emptyDash(slot.Status))
	meta := fmt.Sprintf("in %d out %d", slot.TokensIn, slot.TokensOut)
	if slot.Model != "" {
		meta += " · " + slot.Model
	}
	text := slot.Text
	if text == "" && slot.LastError != "" {
		text = "error: " + slot.LastError
	}
	if text == "" && slot.LastThinking != "" {
		text = "thinking: " + slot.LastThinking
	}
	if text == "" {
		text = "waiting for output"
	}

	var lines []string
	border := "┌" + strings.Repeat("─", inner) + "┐"
	lines = append(lines, border)
	lines = append(lines, "│"+padDisplay(trimDisplay(title, inner), inner)+"│")
	lines = append(lines, "│"+padDisplay(trimDisplay(meta, inner), inner)+"│")
	lines = append(lines, "├"+strings.Repeat("─", inner)+"┤")
	body := wrapParallelText(text, inner)
	if len(body) > 10 {
		body = append(body[:9], "…")
	}
	for _, line := range body {
		lines = append(lines, "│"+padDisplay(trimDisplay(line, inner), inner)+"│")
	}
	lines = append(lines, "└"+strings.Repeat("─", inner)+"┘")
	return lines
}

func renderAgreement(slots []rpc.GroupSlotStatus, width int) string {
	var b strings.Builder
	common := commonParallelLines(slots)
	unique := uniqueParallelLines(slots, common)
	b.WriteString("── agreement ")
	b.WriteString(strings.Repeat("─", maxInt(0, width-13)))
	b.WriteByte('\n')
	if len(common) == 0 {
		b.WriteString("no repeated lines yet\n")
	} else {
		limit := minInt(len(common), 5)
		for _, line := range common[:limit] {
			fmt.Fprintf(&b, "  = %s\n", trimDisplay(line, width-6))
		}
	}

	b.WriteString("── differences ")
	b.WriteString(strings.Repeat("─", maxInt(0, width-14)))
	b.WriteByte('\n')
	providers := make([]string, 0, len(unique))
	for provider := range unique {
		providers = append(providers, provider)
	}
	sort.Strings(providers)
	for _, provider := range providers {
		lines := unique[provider]
		if len(lines) == 0 {
			continue
		}
		fmt.Fprintf(&b, "  %s: %s\n", provider, trimDisplay(lines[0], width-len(provider)-5))
	}
	if len(providers) == 0 {
		b.WriteString("no provider-specific lines yet\n")
	}
	return b.String()
}

func commonParallelLines(slots []rpc.GroupSlotStatus) []string {
	counts := map[string]int{}
	for _, slot := range slots {
		seen := map[string]struct{}{}
		for _, line := range comparableLines(slot.Text) {
			seen[line] = struct{}{}
		}
		for line := range seen {
			counts[line]++
		}
	}
	var common []string
	for line, count := range counts {
		if count >= 2 {
			common = append(common, line)
		}
	}
	sort.Strings(common)
	return common
}

func uniqueParallelLines(slots []rpc.GroupSlotStatus, common []string) map[string][]string {
	commonSet := map[string]struct{}{}
	for _, line := range common {
		commonSet[line] = struct{}{}
	}
	out := map[string][]string{}
	for _, slot := range slots {
		for _, line := range comparableLines(slot.Text) {
			if _, ok := commonSet[line]; ok {
				continue
			}
			out[slot.Provider] = append(out[slot.Provider], line)
			if len(out[slot.Provider]) >= 3 {
				break
			}
		}
	}
	return out
}

func comparableLines(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.Join(strings.Fields(line), " ")
		if len(line) < 8 {
			continue
		}
		out = append(out, line)
	}
	return out
}

func wrapParallelText(text string, width int) []string {
	var out []string
	for _, paragraph := range strings.Split(strings.TrimSpace(text), "\n") {
		wrapped := wrapPlainForTerminal(paragraph, width)
		if len(wrapped) == 0 {
			continue
		}
		out = append(out, wrapped...)
	}
	if len(out) == 0 {
		return []string{""}
	}
	return out
}

func padDisplay(s string, width int) string {
	pad := width - displayWidth(s)
	if pad <= 0 {
		return s
	}
	return s + strings.Repeat(" ", pad)
}

func trimDisplay(s string, width int) string {
	if width <= 0 || displayWidth(s) <= width {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		if displayWidth(b.String()+string(r)+"…") > width {
			break
		}
		b.WriteRune(r)
	}
	b.WriteString("…")
	return b.String()
}

func emptyDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func writeParallelComparison(w io.Writer, status rpc.GroupStatusResult, consensus string, width int) {
	fmt.Fprint(w, renderParallelComparison(status, consensus, width))
}
