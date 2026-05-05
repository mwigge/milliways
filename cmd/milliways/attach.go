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
				return runDeckNavigator(ctx, rightPaneID)
			}
			if navGroupID != "" {
				if len(args) > 0 {
					return fmt.Errorf("--nav and positional handle are mutually exclusive")
				}
				return runNavigator(ctx, navGroupID)
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
		fmt.Fprintf(errw, "unknown handle: %d\n", handle)
		return fmt.Errorf("dial milliwaysd: %w", err)
	}
	defer func() { _ = client.Close() }()

	events, cancel, err := client.Subscribe("agent.stream", map[string]any{"handle": handle})
	if err != nil {
		// Handle not found — check if the error message suggests an unknown handle.
		fmt.Fprintf(errw, "unknown handle: %d\n", handle)
		return err
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
	var tokensIn, tokensOut int
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
			tokensIn += ev.TokensIn
			tokensOut += ev.TokensOut
			if jsonMode {
				fmt.Fprintln(w, formatDoneEvent(tokensIn, tokensOut, time.Now().UTC()))
			}
		case "end":
			return
		}
	}
}

// streamEvent is the minimal subset of daemon stream event fields we need.
type streamEvent struct {
	T         string `json:"t"`
	B64       string `json:"b64,omitempty"`
	Status    string `json:"status,omitempty"`
	TokensIn  int    `json:"tokens_in,omitempty"`
	TokensOut int    `json:"tokens_out,omitempty"`
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
	b, _ := json.Marshal(map[string]any{
		"type":       "done",
		"tokens_in":  tokensIn,
		"tokens_out": tokensOut,
		"ts":         ts.UTC().Format(time.RFC3339),
	})
	return string(b)
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
func runNavigator(ctx context.Context, groupID string) error {
	sock := daemonSocket()
	client, err := rpc.Dial(sock)
	if err != nil {
		return fmt.Errorf("dial milliwaysd for navigator: %w", err)
	}
	defer func() { _ = client.Close() }()

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
		var resp struct {
			Slots []parallel.SlotRecord `json:"slots"`
		}
		if err := client.Call("group.status", map[string]any{"group_id": groupID}, &resp); err != nil {
			lastErr = err
			return
		}
		slots = resp.Slots
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
	ID         string
	AuthStatus string
	Model      string
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
		fmt.Print("\033[?25h\033[0m\n")
	}()
	fmt.Print("\033[?25l") // hide cursor while navigating

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
		// Always update providers from any successful response, even an empty
		// one. An empty list means no runners are configured, not that we are
		// still connecting — Bug 3.
		polled = true
		updated := make([]deckProviderInfo, 0, len(agents))
		for _, a := range agents {
			updated = append(updated, deckProviderInfo{
				ID:         a.ID,
				AuthStatus: a.AuthStatus,
				Model:      a.Model,
			})
		}
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

	// ln prints a line in raw-mode-safe way: \r\n instead of \n so the
	// cursor returns to column 0 on each new line.
	ln := func(format string, args ...any) {
		fmt.Printf(format+"\r\n", args...)
	}

	render := func() {
		w, _, _ := term.GetSize(fd)
		if w <= 0 {
			w = 36
		}
		const reset = "\033[0m"
		const dim = "\033[2m"
		const bold = "\033[1m"

		fmt.Print("\033[2J\033[H") // clear + cursor home

		// Title bar — guard against negative repeat count when the pane is
		// narrower than the title string (13 chars).
		title := " milliways "
		padN := (w - len(title)) / 2
		if padN < 0 {
			padN = 0
		}
		pad := strings.Repeat("─", padN)
		ln("%s%s%s", dim, pad+title+pad, reset)
		ln("")

		if len(providers) == 0 {
			if polled {
				ln("  %sno providers%s", dim, reset)
			} else {
				ln("  %sconnecting...%s", dim, reset)
			}
		}

		for i, p := range providers {
			// Auth indicator
			authMark := "?"
			authColor := "\033[33m"
			switch p.AuthStatus {
			case "ok":
				authMark = "✓"
				authColor = "\033[32m"
			case "missing_credentials":
				authMark = "✗"
				authColor = "\033[2m"
			}

			provColor := parallel.ProviderColor(p.ID)
			model := p.Model
			if model == "" {
				model = "—"
			}
			// Trim model name to fit: pane width minus fixed columns
			maxModel := w - 16
			if maxModel < 4 {
				maxModel = 4
			}
			if len(model) > maxModel {
				model = model[:maxModel-1] + "…"
			}

			if i == selected {
				ln("%s▶ %s%-9s%s %s%s%s  %s%s\033[K",
					bold,
					provColor, p.ID, reset+bold,
					authColor, authMark, reset+bold,
					model, reset)
			} else {
				ln("  %s%-9s%s %s%s%s  %s%s\033[K",
					provColor, p.ID, reset,
					authColor, authMark, reset,
					dim+model, reset)
			}
		}

		// ── Status bar ──────────────────────────────────────
		ln("%s%s%s", dim, strings.Repeat("─", w), reset)
		if active != "" {
			provColor := parallel.ProviderColor(active)
			// Show quota if we have it for the active provider.
			quotaLine := ""
			if q, ok := quotas[active]; ok && q.LimitDay > 0 {
				pct := int(q.UsedPct())
				quotaLine = fmt.Sprintf("  %d%% quota", pct)
			}
			ln("%s● %s%s%s active%s%s", dim, provColor, active, dim, quotaLine, reset)
		} else {
			ln("%sno active provider%s", dim, reset)
		}
		ln("%s↑↓ move  ↩ switch  q quit%s", dim, reset)
	}

	// Key event reader — handles single bytes and 3-byte arrow sequences.
	type keyKind int
	const (
		keyRune  keyKind = iota
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
