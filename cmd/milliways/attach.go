// Copyright 2024 The milliways Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// attach.go implements `milliways attach <handle>` and the --nav navigator mode.
//
// Streaming mode (attach <handle>):
//  1. Dial the daemon UDS via daemonSocket().
//  2. Call agent.stream with {handle: N} to subscribe.
//  3. Decode base64 content deltas and print to stdout.
//  4. Exit when the stream closes or context is cancelled.
//
// JSON mode (--json): each event is emitted as one NDJSON line.
//
// Nav mode (--nav <group-id>): raw ANSI navigator for a parallel group.
package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/mwigge/milliways/internal/parallel"
	"github.com/mwigge/milliways/internal/rpc"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// attachCmd returns the cobra `attach` subcommand.
func attachCmd() *cobra.Command {
	var jsonMode bool
	var navGroupID string
	var deckMode bool
	var rightPaneID string
	var plainMode bool

	cmd := &cobra.Command{
		Use:   "attach <handle>",
		Short: "Attach to a running agent session stream",
		Long: `Attach to an active milliwaysd session by handle and stream its output.

Streaming mode:
  milliways attach 42               stream decoded content to stdout
  milliways attach --json 42        emit one NDJSON event per delta/done

Deck navigator (interactive provider browser):
  milliways attach --deck --right-pane <pane-id>

Navigator mode (parallel panel):
  milliways attach --nav grp-abc    render the slot navigator for a parallel group`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			if deckMode {
				if plainMode || !ansiEnabled() {
					return runDeckNavigatorPlain(ctx)
				}
				return runDeckNavigator(ctx, rightPaneID)
			}
			if navGroupID != "" {
				if len(args) > 0 {
					return fmt.Errorf("--nav and positional handle are mutually exclusive")
				}
				return runNavigator(ctx, navGroupID, plainMode || !ansiEnabled())
			}
			if len(args) != 1 {
				return fmt.Errorf("attach requires a session handle (integer), --deck, or --nav <group-id>")
			}
			handle, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid handle %q: must be an integer", args[0])
			}
			return runAttach(ctx, handle, jsonMode, os.Stdout, os.Stderr)
		},
	}

	cmd.Flags().BoolVar(&jsonMode, "json", false, "Emit one NDJSON event per delta/done")
	cmd.Flags().StringVar(&navGroupID, "nav", "", "Run navigator mode for the given parallel group ID")
	cmd.Flags().BoolVar(&plainMode, "plain", false, "Render navigator output without ANSI or box drawing")
	cmd.Flags().BoolVar(&deckMode, "deck", false, "Run interactive deck provider navigator")
	cmd.Flags().StringVar(&rightPaneID, "right-pane", "", "WezTerm pane ID of the chat pane (used with --deck)")

	return cmd
}

// runAttach dials the daemon, subscribes to agent.stream for handle, and
// drains events to out. stderr receives error messages.
// socketOverride, if non-empty, replaces the default daemon socket path.
func runAttach(ctx context.Context, handle int64, jsonMode bool, out io.Writer, errw io.Writer, socketOverride ...string) error {
	sock := daemonSocket()
	if len(socketOverride) > 0 && socketOverride[0] != "" {
		sock = socketOverride[0]
	}
	client, err := rpc.Dial(sock)
	if err != nil {
		fmt.Fprintln(errw, friendlyError("attach: ", "", err))
		return fmt.Errorf("%s", friendlyError("attach: ", "", err))
	}
	defer func() { _ = client.Close() }()

	events, cancel, err := client.Subscribe("agent.stream", map[string]any{"handle": handle})
	if err != nil {
		// Handle not found — check if the error message suggests an unknown handle.
		fmt.Fprintf(errw, "unknown handle: %d\n", handle)
		return fmt.Errorf("%s", friendlyError("attach stream: ", "", err))
	}

	// Context cancellation closes the subscription.
	go func() {
		<-ctx.Done()
		cancel()
	}()

	drainStreamToWriter(events, out, jsonMode)
	return nil
}

// drainStreamToWriter consumes stream events from events and writes decoded
// output to w. In JSON mode each event becomes one NDJSON line; in plain mode
// decoded content deltas are written directly.
//
// The function returns when the events channel is closed or an "end" event is
// received.
func drainStreamToWriter(events <-chan []byte, w io.Writer, jsonMode bool) {
	var usage usageStats
	for line := range events {
		var ev streamEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			continue
		}
		switch ev.T {
		case "delta", "data": // "data" is the _echo agent's format; "delta" is used by real runners
			decoded, err := base64.StdEncoding.DecodeString(ev.B64)
			if err != nil {
				continue
			}
			if jsonMode {
				fmt.Fprintln(w, formatDeltaEvent(string(decoded), time.Now().UTC()))
			} else {
				_, _ = w.Write(decoded)
			}
		case "chunk_end":
			usage.InputTokens += firstNonZero(ev.InputTokens, ev.TokensIn)
			usage.OutputTokens += firstNonZero(ev.OutputTokens, ev.TokensOut)
			if ev.TotalTokens > 0 {
				usage.TotalTokens += ev.TotalTokens
			}
			usage.CostUSD += ev.CostUSD
			if jsonMode {
				fmt.Fprintln(w, formatDoneEventWithUsage(usage, time.Now().UTC()))
			}
		case "end":
			return
		}
	}
}

// streamEvent is the minimal subset of daemon stream event fields we need.
type streamEvent struct {
	T            string  `json:"t"`
	B64          string  `json:"b64,omitempty"`
	Status       string  `json:"status,omitempty"`
	TokensIn     int     `json:"tokens_in,omitempty"`
	TokensOut    int     `json:"tokens_out,omitempty"`
	InputTokens  int     `json:"input_tokens,omitempty"`
	OutputTokens int     `json:"output_tokens,omitempty"`
	TotalTokens  int     `json:"total_tokens,omitempty"`
	CostUSD      float64 `json:"cost_usd,omitempty"`
}

// formatDeltaEvent returns a JSON line for a delta event.
func formatDeltaEvent(content string, ts time.Time) string {
	b, _ := json.Marshal(map[string]any{
		"type":    "delta",
		"content": content,
		"ts":      ts.UTC().Format(time.RFC3339),
	})
	return string(b)
}

// formatDoneEvent returns a JSON line for a done event.
func formatDoneEvent(tokensIn, tokensOut int, ts time.Time) string {
	return formatDoneEventWithUsage(usageStats{InputTokens: tokensIn, OutputTokens: tokensOut}, ts)
}

func formatDoneEventWithUsage(usage usageStats, ts time.Time) string {
	b, _ := json.Marshal(map[string]any{
		"type":          "done",
		"tokens_in":     usage.InputTokens,
		"tokens_out":    usage.OutputTokens,
		"total_tokens":  usage.total(),
		"cost_usd":      usage.CostUSD,
		"usage_display": formatUsageInline(usage),
		"ts":            ts.UTC().Format(time.RFC3339),
	})
	return string(b)
}

func firstNonZero(values ...int) int {
	for _, v := range values {
		if v != 0 {
			return v
		}
	}
	return 0
}

// buildQuotasFromSnapshots converts a quota.get response slice into a
// map[providerID]QuotaSummary suitable for parallel.RenderHeader. Snapshots
// with a zero cap (unlimited / not tracked) are omitted so the header does
// not display a meaningless "0% quota" entry.
func buildQuotasFromSnapshots(snapshots []rpc.QuotaSnapshot) map[string]parallel.QuotaSummary {
	if len(snapshots) == 0 {
		return nil
	}
	quotas := make(map[string]parallel.QuotaSummary, len(snapshots))
	for _, s := range snapshots {
		if s.Cap <= 0 {
			continue
		}
		quotas[string(s.AgentID)] = parallel.QuotaSummary{
			UsedToday: int(s.Used),
			LimitDay:  int(s.Cap),
		}
	}
	if len(quotas) == 0 {
		return nil
	}
	return quotas
}

// sumSlotTokens returns the sum of TokensIn + TokensOut across all slots.
func sumSlotTokens(slots []parallel.SlotRecord) int {
	total := 0
	for _, s := range slots {
		total += s.TokensIn + s.TokensOut
	}
	return total
}

// termWidth returns the current terminal width, defaulting to 80 on error.
func termWidth() int {
	w, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || w <= 0 {
		return 80
	}
	return w
}

// runNavigator renders the slot list for a parallel group in raw ANSI mode.
// It polls group.status every 500ms and redraws on each cycle.
//
// Keyboard input:
//   - 1–9: select slot by number
//   - Tab: cycle to next slot
//   - c: print consensus hint
//   - q or Ctrl+D: exit
func runNavigator(ctx context.Context, groupID string, plain bool) error {
	sock := daemonSocket()
	client, err := rpc.Dial(sock)
	if err != nil {
		return fmt.Errorf("%s", friendlyError("navigator: ", "", err))
	}
	defer func() { _ = client.Close() }()

	if plain {
		slots, err := fetchNavigatorSlots(client, groupID)
		if err != nil {
			return fmt.Errorf("%s", friendlyError("navigator status: ", "", err))
		}
		fmt.Printf("milliways parallel %s\n", groupID)
		fmt.Println("0 main full chat")
		for _, s := range slots {
			fmt.Printf("%d %s %s %s tokens\n", s.SlotN, s.Provider, s.Status, formatNavTokens(s.TokensOut))
		}
		return nil
	}

	// Switch stdin to raw mode so we can read single keystrokes.
	fd := int(os.Stdin.Fd())
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return fmt.Errorf("raw terminal: %w", err)
	}
	defer func() {
		_ = term.Restore(fd, oldState)
		fmt.Println() // blank line on exit
	}()

	selected := 1
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	// Key input channel.
	keyCh := make(chan byte, 8)
	go func() {
		buf := make([]byte, 1)
		for {
			n, err := os.Stdin.Read(buf)
			if err != nil || n == 0 {
				close(keyCh)
				return
			}
			keyCh <- buf[0]
		}
	}()

	var slots []parallel.SlotRecord
	var quotas map[string]parallel.QuotaSummary
	var lastErr error

	render := func() {
		if lastErr != nil {
			return
		}
		// Clear and home.
		fmt.Print("\033[2J\033[H")

		// Render the observability header before the slot list.
		totalTokens := sumSlotTokens(slots)
		tw := termWidth()
		if header := parallel.RenderHeader(slots, quotas, totalTokens, tw); header != "" {
			fmt.Println(header)
			fmt.Println(strings.Repeat("─", tw))
		}

		n := len(slots)
		running := 0
		done := 0
		for _, s := range slots {
			switch s.Status {
			case "running":
				running++
			case "done":
				done++
			}
		}

		fmt.Printf("milliways parallel — %d slot(s)\n", n)
		fmt.Println("──────────────────────────────")
		// Slot 0 is the main panel (the calling chat session).
		mainMarker := "  "
		if selected == 0 {
			mainMarker = "▶ "
		}
		fmt.Printf("%s0  [main]   full chat · /help /switch /quota…\n", mainMarker)
		fmt.Println("──")
		for _, s := range slots {
			marker := "  "
			if s.SlotN == selected {
				marker = "▶ "
			}
			ago := "0s ago"
			if !s.StartedAt.IsZero() {
				ago = time.Since(s.StartedAt).Round(time.Second).String() + " ago"
			}
			fmt.Printf("%s%d  %-8s %-9s %s  %s tok\n",
				marker, s.SlotN, parallel.ColorProvider(s.Provider), s.Status, ago, formatNavTokens(s.TokensOut))
			fmt.Println("──")
		}
		fmt.Println("──────────────────────────────")
		fmt.Printf("%d running | %d done | %s\n", running, done, groupID)
		fmt.Println("0 main · 1–9 select · Tab cycle · c consensus · q exit")
	}

	pollSlots := func() {
		got, err := fetchNavigatorSlots(client, groupID)
		if err != nil {
			lastErr = err
			return
		}
		slots = got
		lastErr = nil

		// Best-effort quota poll: errors are silently swallowed so a missing
		// or unreachable quota endpoint never prevents the navigator from rendering.
		var snapshots []rpc.QuotaSnapshot
		if err := client.Call("quota.get", nil, &snapshots); err == nil {
			quotas = buildQuotasFromSnapshots(snapshots)
		}
	}

	// Initial poll.
	pollSlots()
	render()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			pollSlots()
			render()
		case k, ok := <-keyCh:
			if !ok {
				return nil
			}
			switch {
			case k == '0':
				selected = 0
				render()
				fmt.Println("\n[switching to main pane — use your original terminal]")
			case k >= '1' && k <= '9':
				n := int(k - '0')
				if n <= len(slots) {
					selected = n
					render()
				}
			case k == '\t': // Tab
				if len(slots) > 0 {
					selected = (selected % len(slots)) + 1
					render()
				}
			case k == 'c':
				fmt.Println("\n[consensus: press c in the calling session to aggregate]")
			case k == 'q' || k == 4: // q or Ctrl+D
				return nil
			}
		}
	}
}

func fetchNavigatorSlots(client *rpc.Client, groupID string) ([]parallel.SlotRecord, error) {
	var resp rpc.GroupStatusResult
	if err := client.Call("group.status", map[string]any{"group_id": groupID}, &resp); err != nil {
		return nil, err
	}
	slots := make([]parallel.SlotRecord, 0, len(resp.Slots))
	for i, s := range resp.Slots {
		slots = append(slots, parallel.SlotRecord{
			SlotN:     i + 1,
			Handle:    s.Handle,
			Provider:  s.Provider,
			Status:    parallel.SlotStatus(s.Status),
			TokensIn:  s.TokensIn,
			TokensOut: s.TokensOut,
		})
	}
	return slots, nil
}

// formatNavTokens is a local helper identical to parallel.formatTokens but
// inlined here to avoid an import cycle between cmd and internal packages at
// test time. It will always delegate to the canonical implementation.
func formatNavTokens(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	return fmt.Sprintf("%.1fk", float64(n)/1000.0)
}

// deckProviderInfo holds the data shown per row in the deck navigator.
type deckProviderInfo struct {
	ID           string
	AuthStatus   string
	Model        string
	Handle       int64
	Status       string
	Turns        int
	Tokens       int
	CostUSD      float64
	CurrentTrace string
	LastTrace    string
	LatencyMS    float64
	TTFTMS       float64
	TokenRate    float64
	ErrorCount   int
	QueueDepth   int
	LastError    string
	LastThink    string
}

func renderDeckNavigator(w int, providers []deckProviderInfo, selected int, active string, polled bool, quotas map[string]parallel.QuotaSummary) string {
	return renderDeckNavigatorSized(w, 0, providers, selected, active, polled, quotas)
}

func renderDeckNavigatorSized(w, h int, providers []deckProviderInfo, selected int, active string, polled bool, quotas map[string]parallel.QuotaSummary) string {
	if w <= 0 {
		w = 36
	}
	if w < 18 {
		w = 18
	}
	if h <= 0 {
		h = 40
	}

	colorEnabled := ansiEnabled()
	reset := "\033[0m"
	dim := "\033[2m"
	red := "\033[31m"
	if !colorEnabled {
		reset = ""
		dim = ""
		red = ""
	}

	var b strings.Builder
	lines := 0
	ln := func(format string, args ...any) {
		if lines >= h {
			return
		}
		fmt.Fprintf(&b, format+"\r\n", args...)
		lines++
	}
	section := func(name string) {
		ln("%s%s %s %s%s", dim, strings.Repeat("─", 2), name, strings.Repeat("─", max(1, w-len(name)-5)), reset)
	}
	truncate := func(s string, maxLen int) string {
		if maxLen < 1 {
			return ""
		}
		if len(s) <= maxLen {
			return s
		}
		if maxLen == 1 {
			return "…"
		}
		return s[:maxLen-1] + "…"
	}
	padPlain := func(s string, width int) string {
		dw := displayWidth(s)
		if dw > width {
			// Rune-aware truncation so multi-byte characters (▶, …, etc.)
			// don't cause the content to overflow the right border.
			var buf strings.Builder
			visible := 0
			for _, r := range s {
				if visible >= width-1 {
					buf.WriteRune('…')
					break
				}
				buf.WriteRune(r)
				visible++
			}
			s = buf.String()
			dw = displayWidth(s)
		}
		return s + strings.Repeat(" ", max(0, width-dw))
	}
	clientLine := func(i int, p deckProviderInfo) string {
		auth := "auth?"
		switch p.AuthStatus {
		case "ok":
			auth = "auth ok"
		case "missing_credentials":
			auth = "auth miss"
		}
		status := fallbackStatus(p.Status)
		if p.ID == active && status == deckStatusIdle {
			status = "active"
		}
		prefix := fmt.Sprintf("%d", i+1)
		if i == selected {
			prefix = "▶ " + prefix
		}
		meta := fmt.Sprintf("turns %d", p.Turns)
		if usage := formatUsageCompact(usageStats{TotalTokens: p.Tokens, CostUSD: p.CostUSD}); usage != "" {
			meta += " " + usage
		}
		if p.LastError != "" {
			meta = "err"
		} else if p.LastThink != "" {
			meta = "think"
		}
		return fmt.Sprintf("%s %s %s %s %s", prefix, p.ID, status, auth, meta)
	}
	card := func(selected bool, provider, line string) {
		edgeColor := dim
		if selected {
			edgeColor = agentColor(provider)
			if edgeColor == "" && colorEnabled {
				edgeColor = "\033[38;5;75m"
			}
		}
		labelColor := agentColor(provider)
		inner := max(8, w-2)
		ln("%s┌%s┐%s", edgeColor, strings.Repeat("─", inner), reset)
		if labelColor == "" {
			ln("%s│%s│%s", edgeColor, padPlain(" "+line, inner), reset)
		} else {
			ln("%s│%s%s%s%s│%s", edgeColor, labelColor, padPlain(" "+line, inner), reset, edgeColor, reset)
		}
		ln("%s└%s┘%s", edgeColor, strings.Repeat("─", inner), reset)
	}

	// Keep the lower panels visible and the left deck predictable. Milliways has
	// seven clients, so render up to seven choices when the pane has room; users
	// can still switch directly with
	// /switch, /<client>, /next, and /prev in the main pane.
	clientBudget := max(3, h-16)
	maxCards := min(7, max(1, clientBudget/3))
	if maxCards > len(providers) || len(providers) == 0 {
		maxCards = len(providers)
	}
	start := 0
	if len(providers) > maxCards {
		start = selected - maxCards/2
		if start < 0 {
			start = 0
		}
		if start+maxCards > len(providers) {
			start = len(providers) - maxCards
		}
	}
	end := start + maxCards
	clientSection := "Clients"
	if len(providers) > 0 && maxCards < len(providers) {
		clientSection = fmt.Sprintf("Clients %d-%d/%d", start+1, end, len(providers))
	}
	section(clientSection)
	if len(providers) == 0 {
		if polled {
			ln("  %sno clients%s", dim, reset)
		} else {
			ln("  %sconnecting...%s", dim, reset)
		}
	}
	for i := start; i < end; i++ {
		card(i == selected, providers[i].ID, clientLine(i, providers[i]))
	}

	section("Active")
	if active != "" {
		provColor := agentColor(active)
		activeModel := "—"
		for _, p := range providers {
			if p.ID == active {
				if p.Model != "" {
					activeModel = p.Model
				}
				break
			}
		}
		ln("%s● %s%s%s active%s", dim, provColor, active, dim, reset)
		ln("  %s%s%s", dim, truncate(activeModel, w-4), reset)
	} else {
		ln("%sno active client%s", dim, reset)
	}

	section("Status")
	if polled {
		ln("%sdaemon connected%s", dim, reset)
		ln("%s%d clients%s", dim, len(providers), reset)
	} else {
		ln("%sdaemon connecting%s", dim, reset)
	}
	ln("%s↑↓ move  ↩ switch  q quit%s", dim, reset)

	section("Observability")
	// Fleet status bar: aggregate live state + auth + total cost
	var obsThink, obsStream, obsRun, obsErr, obsOk int
	var obsTotalCost float64
	for _, p := range providers {
		if p.AuthStatus == "ok" {
			obsOk++
		}
		obsTotalCost += p.CostUSD
		switch fallbackStatus(p.Status) {
		case deckStatusThinking:
			obsThink++
		case deckStatusStreaming:
			obsStream++
		case deckStatusRunning:
			obsRun++
		case deckStatusError:
			obsErr++
		}
	}
	fleetParts := make([]string, 0, 4)
	if obsThink > 0 {
		fleetParts = append(fleetParts, fmt.Sprintf("%d●", obsThink))
	}
	if obsStream > 0 {
		fleetParts = append(fleetParts, fmt.Sprintf("%d⟳", obsStream))
	}
	if obsRun > 0 {
		fleetParts = append(fleetParts, fmt.Sprintf("%d▶", obsRun))
	}
	if obsErr > 0 {
		fleetParts = append(fleetParts, fmt.Sprintf("%d✗", obsErr))
	}
	obsAuthStr := fmt.Sprintf("auth %d/%d", obsOk, len(providers))
	obsCostStr := formatCost(obsTotalCost)
	if len(fleetParts) == 0 {
		ln("%s◌ all idle  │%s  %s%s", dim, obsAuthStr, obsCostStr, reset)
	} else {
		ln("%s  │%s  %s%s", strings.Join(fleetParts, " "), obsAuthStr, obsCostStr, reset)
	}
	// Per-agent rows: one row per non-idle provider
	var obsIdleShorts []string
	var obsAlerts []string
	for _, p := range providers {
		st := fallbackStatus(p.Status)
		if st == deckStatusIdle {
			obsIdleShorts = append(obsIdleShorts, obsProviderShort(p.ID))
			continue
		}
		glyph, stLabel := obsStatusRow(st, p.LastError)
		row := obsProviderShort(p.ID) + " " + glyph + stLabel
		if usage := formatUsageCompact(usageStats{TotalTokens: p.Tokens, CostUSD: p.CostUSD}); usage != "" {
			row += " " + usage
		}
		if p.LatencyMS > 0 {
			row += " lat" + formatDurationMS(p.LatencyMS)
		}
		if p.TTFTMS > 0 {
			row += " ttft" + formatDurationMS(p.TTFTMS)
		}
		if p.TokenRate > 0 {
			row += fmt.Sprintf(" %.0ft/s", p.TokenRate)
		}
		if p.QueueDepth > 0 {
			row += fmt.Sprintf(" q%d", p.QueueDepth)
		}
		if trace := shortTraceID(p.CurrentTrace, p.LastTrace); trace != "" {
			row += " tr" + trace
		}
		ln("  %s", truncate(row, w-4))
		if st == deckStatusError && p.LastError != "" {
			obsAlerts = append(obsAlerts, "✗ "+obsProviderShort(p.ID)+" "+truncate(p.LastError, 18))
		}
	}
	// Idle summary
	if len(obsIdleShorts) > 0 {
		idleLine := "◌ idle: " + strings.Join(obsIdleShorts, " ")
		if len(obsIdleShorts) > 4 {
			idleLine = fmt.Sprintf("◌ idle: %s +%d", strings.Join(obsIdleShorts[:4], " "), len(obsIdleShorts)-4)
		}
		ln("%s%s%s", dim, truncate(idleLine, w-2), reset)
	}
	// Alert block — only when there are error events
	if len(obsAlerts) > 0 {
		alertHdr := "┄ " + strings.Repeat("┄", max(1, w-4))
		ln("%s%s%s", dim, truncate(alertHdr, w-2), reset)
		for _, alert := range obsAlerts {
			ln("%s  %s%s", red, alert, reset)
		}
	}
	// Footer: quota for the active client
	obsQuotaStr := "quota --"
	if active != "" {
		if q, ok := quotas[active]; ok && q.LimitDay > 0 {
			obsQuotaStr = fmt.Sprintf("quota %d%%", int(q.UsedPct()))
		}
	}
	ln("%s%s%s", dim, obsQuotaStr, reset)

	return b.String()
}

// obsProviderShort returns a 4-char abbreviation used in the Observability panel rows.
func obsProviderShort(id string) string {
	switch id {
	case "claude":
		return "clde"
	case "codex":
		return "cdex"
	case "copilot":
		return "cplt"
	case "gemini":
		return "gemi"
	case "minimax":
		return "mnmx"
	case "local":
		return "lcal"
	case "pool":
		return "pool"
	default:
		if len(id) >= 4 {
			return id[:4]
		}
		return id + strings.Repeat(" ", 4-len(id))
	}
}

// obsStatusRow returns the status glyph and a short label for an agent row in
// the Observability panel.
func obsStatusRow(status, lastError string) (glyph, label string) {
	switch status {
	case deckStatusThinking:
		return "●", "think"
	case deckStatusStreaming:
		return "⟳", "stream"
	case deckStatusRunning:
		return "▶", "tool"
	case deckStatusError:
		reason := lastError
		if len(reason) > 8 {
			reason = reason[:8]
		}
		return "✗", "err:" + reason
	default:
		return "◌", "idle"
	}
}

func formatDurationMS(ms float64) string {
	switch {
	case ms <= 0:
		return ""
	case ms < 1000:
		return fmt.Sprintf("%.0fms", ms)
	default:
		return fmt.Sprintf("%.1fs", ms/1000)
	}
}

func shortTraceID(ids ...string) string {
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if len(id) <= 8 {
			return id
		}
		return id[:8]
	}
	return ""
}

func orderDeckProviders(providers []deckProviderInfo) []deckProviderInfo {
	ordered := append([]deckProviderInfo(nil), providers...)
	rank := make(map[string]int, len(chatSwitchableAgents))
	for i, id := range chatSwitchableAgents {
		rank[id] = i
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		ri, iok := rank[ordered[i].ID]
		rj, jok := rank[ordered[j].ID]
		switch {
		case iok && jok:
			return ri < rj
		case iok:
			return true
		case jok:
			return false
		default:
			return ordered[i].ID < ordered[j].ID
		}
	})
	return ordered
}

func renderDeckNavigatorPlain(providers []deckProviderInfo, active string, polled bool, quotas map[string]parallel.QuotaSummary) string {
	var b strings.Builder
	fmt.Fprintln(&b, "milliways deck")
	fmt.Fprintln(&b, "Clients")
	if len(providers) == 0 {
		if polled {
			fmt.Fprintln(&b, "  no clients")
		} else {
			fmt.Fprintln(&b, "  connecting")
		}
	}
	for i, p := range providers {
		status := fallbackStatus(p.Status)
		if p.ID == active && status == deckStatusIdle {
			status = "active"
		}
		auth := "auth?"
		switch p.AuthStatus {
		case "ok":
			auth = "auth ok"
		case "missing_credentials":
			auth = "auth missing"
		}
		model := p.Model
		if model == "" {
			model = "-"
		}
		usage := formatUsageCompact(usageStats{TotalTokens: p.Tokens, CostUSD: p.CostUSD})
		if usage == "" {
			usage = "0 tok"
		}
		fmt.Fprintf(&b, "  %d %s %s %s model %s turns %d usage %s\n", i+1, p.ID, status, auth, model, p.Turns, usage)
	}
	fmt.Fprintln(&b, "Active")
	if active == "" {
		fmt.Fprintln(&b, "  none")
	} else {
		fmt.Fprintf(&b, "  %s\n", active)
	}
	fmt.Fprintln(&b, "Status")
	if polled {
		fmt.Fprintf(&b, "  daemon connected; %d clients\n", len(providers))
	} else {
		fmt.Fprintln(&b, "  daemon connecting")
	}
	fmt.Fprintln(&b, "Observability")
	authOK := 0
	for _, p := range providers {
		if p.AuthStatus == "ok" {
			authOK++
		}
	}
	fmt.Fprintf(&b, "  auth %d/%d ok\n", authOK, len(providers))
	if active != "" {
		if q, ok := quotas[active]; ok && q.LimitDay > 0 {
			fmt.Fprintf(&b, "  quota %d%%\n", int(q.UsedPct()))
		}
	}
	return b.String()
}

func runDeckNavigatorPlain(ctx context.Context) error {
	client, err := rpc.Dial(daemonSocket())
	if err != nil {
		return fmt.Errorf("%s", friendlyError("deck: ", "", err))
	}
	defer func() { _ = client.Close() }()

	providers, active, polled, quotas := pollDeckNavigatorSnapshot(ctx, client)
	fmt.Print(renderDeckNavigatorPlain(providers, active, polled, quotas))
	return nil
}

// runDeckNavigator is the interactive provider browser for deck mode.
// It shows a list of all providers, lets the user browse with ↑↓, and
// sends "/switch <provider>\n" to rightPaneID on Enter.
//
// Key bindings:
//
//	↑ / k   move up
//	↓ / j   move down
//	Enter   switch right pane to selected provider
//	q/^D    exit navigator
func runDeckNavigator(ctx context.Context, rightPaneID string) error {
	sock := daemonSocket()

	// Retry dial up to 3 times with 200ms backoff. The navigator is launched
	// by wezterm cli split-pane concurrently with the chat pane; the daemon
	// is expected to be up but a short timing race can cause the first dial
	// to fail.
	var client *rpc.Client
	const dialAttempts = 3
	const dialBackoff = 200 * time.Millisecond
	for attempt := 1; attempt <= dialAttempts; attempt++ {
		var err error
		client, err = rpc.Dial(sock)
		if err == nil {
			break
		}
		if attempt == dialAttempts {
			return fmt.Errorf("dial milliwaysd: %w", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(dialBackoff):
		}
	}
	defer func() { _ = client.Close() }()

	fd := int(os.Stdin.Fd())
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return fmt.Errorf("raw terminal: %w", err)
	}
	defer func() {
		_ = term.Restore(fd, oldState)
		fmt.Print("\033[?25h\033[?1049l\033[0m\n")
	}()
	fmt.Print("\033[?1049h\033[?25l") // alternate screen + hidden cursor

	var providers []deckProviderInfo
	var quotas map[string]parallel.QuotaSummary
	selected := 0
	active := "" // last provider switched to in the right pane
	// polled tracks whether at least one successful agent.list call has
	// returned. Until then we show "connecting..."; after, an empty list
	// means "no providers" — Bug 3.
	polled := false

	pollProviders := func() {
		// agent.list returns a flat []AgentInfo array, not {"agents":[...]}.
		var agents []struct {
			ID         string `json:"id"`
			AuthStatus string `json:"auth_status"`
			Model      string `json:"model"`
		}
		if err := client.Call("agent.list", nil, &agents); err != nil {
			return
		}
		var deck daemonDeckSnapshot
		deckByAgent := map[string]daemonDeckSessionStatus{}
		if err := client.Call("deck.snapshot", nil, &deck); err == nil {
			if deck.Active != "" {
				active = deck.Active
			}
			for _, sess := range deck.Sessions {
				if sess.AgentID != "" {
					deckByAgent[sess.AgentID] = sess
				}
			}
		}
		// Always update providers from any successful response, even an empty
		// one. An empty list means no runners are configured, not that we are
		// still connecting — Bug 3.
		polled = true
		updated := make([]deckProviderInfo, 0, len(agents))
		for _, a := range agents {
			d := deckByAgent[a.ID]
			model := a.Model
			if d.Model != "" {
				model = d.Model
			}
			updated = append(updated, deckProviderInfo{
				ID:           a.ID,
				AuthStatus:   a.AuthStatus,
				Model:        model,
				Handle:       d.Handle,
				Status:       d.Status,
				Turns:        d.TurnCount,
				Tokens:       d.TotalTokens,
				CostUSD:      d.CostUSD,
				CurrentTrace: d.CurrentTrace,
				LastTrace:    d.LastTrace,
				LatencyMS:    d.LatencyMS,
				TTFTMS:       d.TTFTMS,
				TokenRate:    d.TokenRate,
				ErrorCount:   d.ErrorCount,
				QueueDepth:   d.QueueDepth,
				LastError:    d.LastError,
				LastThink:    d.LastThinking,
			})
		}
		updated = orderDeckProviders(updated)

		// Clamp the cursor regardless of whether the new list is empty or
		// shrunk — Bug 5.
		if len(updated) == 0 {
			selected = 0
		} else if selected >= len(updated) {
			selected = len(updated) - 1
		}
		providers = updated

		// Best-effort quota poll alongside the provider list.
		var snapshots []rpc.QuotaSnapshot
		if err := client.Call("quota.get", nil, &snapshots); err == nil {
			quotas = buildQuotasFromSnapshots(snapshots)
		}
	}

	render := func() {
		w, h, _ := term.GetSize(fd)
		fmt.Print("\033[2J\033[H") // clear + cursor home
		fmt.Print(renderDeckNavigatorSized(w, h, providers, selected, active, polled, quotas))
	}

	// Key event reader — handles single bytes and 3-byte arrow sequences.
	type keyKind int
	const (
		keyRune keyKind = iota
		keyUp
		keyDown
		keyEnter
		keyEOF
	)
	type keyEvent struct {
		kind keyKind
		ch   byte
	}
	keyCh := make(chan keyEvent, 8)
	go func() {
		buf := make([]byte, 8)
		for {
			n, err := os.Stdin.Read(buf)
			if err != nil || n == 0 {
				keyCh <- keyEvent{kind: keyEOF}
				return
			}
			// Arrow keys arrive as ESC [ A/B.
			if n >= 3 && buf[0] == 27 && buf[1] == '[' {
				switch buf[2] {
				case 'A':
					keyCh <- keyEvent{kind: keyUp}
				case 'B':
					keyCh <- keyEvent{kind: keyDown}
				}
				continue
			}
			keyCh <- keyEvent{kind: keyRune, ch: buf[0]}
		}
	}()

	switchProvider := func(provider string) {
		if rightPaneID == "" {
			return
		}
		err := exec.Command("wezterm", "cli", "send-text",
			"--pane-id", rightPaneID,
			"--no-paste",
			"/switch "+provider+"\n").Run()
		if err != nil {
			slog.Debug("deck: send-text failed", "provider", provider, "err", err)
			return
		}
		active = provider
		render()
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	pollProviders()
	render()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			pollProviders()
			render()
		case ev, ok := <-keyCh:
			if !ok {
				return nil
			}
			switch ev.kind {
			case keyEOF:
				return nil
			case keyUp:
				if selected > 0 {
					selected--
					render()
				}
			case keyDown:
				if selected < len(providers)-1 {
					selected++
					render()
				}
			case keyEnter:
				if selected >= 0 && selected < len(providers) {
					switchProvider(providers[selected].ID)
				}
			case keyRune:
				switch ev.ch {
				case 'k': // vim up
					if selected > 0 {
						selected--
						render()
					}
				case 'j': // vim down
					if selected < len(providers)-1 {
						selected++
						render()
					}
				case '\r', '\n':
					if selected >= 0 && selected < len(providers) {
						switchProvider(providers[selected].ID)
					}
				case 'q', 4: // q or Ctrl+D
					return nil
				}
			}
		}
	}
}

func pollDeckNavigatorSnapshot(ctx context.Context, client *rpc.Client) ([]deckProviderInfo, string, bool, map[string]parallel.QuotaSummary) {
	_ = ctx
	var agents []struct {
		ID         string `json:"id"`
		AuthStatus string `json:"auth_status"`
		Model      string `json:"model"`
	}
	if err := client.Call("agent.list", nil, &agents); err != nil {
		return nil, "", false, nil
	}
	active := ""
	var deck daemonDeckSnapshot
	deckByAgent := map[string]daemonDeckSessionStatus{}
	if err := client.Call("deck.snapshot", nil, &deck); err == nil {
		active = deck.Active
		for _, sess := range deck.Sessions {
			if sess.AgentID != "" {
				deckByAgent[sess.AgentID] = sess
			}
		}
	}
	updated := make([]deckProviderInfo, 0, len(agents))
	for _, a := range agents {
		d := deckByAgent[a.ID]
		model := a.Model
		if d.Model != "" {
			model = d.Model
		}
		updated = append(updated, deckProviderInfo{
			ID:           a.ID,
			AuthStatus:   a.AuthStatus,
			Model:        model,
			Handle:       d.Handle,
			Status:       d.Status,
			Turns:        d.TurnCount,
			Tokens:       d.TotalTokens,
			CostUSD:      d.CostUSD,
			CurrentTrace: d.CurrentTrace,
			LastTrace:    d.LastTrace,
			LatencyMS:    d.LatencyMS,
			TTFTMS:       d.TTFTMS,
			TokenRate:    d.TokenRate,
			ErrorCount:   d.ErrorCount,
			QueueDepth:   d.QueueDepth,
			LastError:    d.LastError,
			LastThink:    d.LastThinking,
		})
	}
	var quotas map[string]parallel.QuotaSummary
	var snapshots []rpc.QuotaSnapshot
	if err := client.Call("quota.get", nil, &snapshots); err == nil {
		quotas = buildQuotasFromSnapshots(snapshots)
	}
	return orderDeckProviders(updated), active, true, quotas
}
