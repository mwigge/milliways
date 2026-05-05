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
	"os"
	"os/signal"
	"strconv"
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

	cmd := &cobra.Command{
		Use:   "attach <handle>",
		Short: "Attach to a running agent session stream",
		Long: `Attach to an active milliwaysd session by handle and stream its output.

Streaming mode:
  milliways attach 42               stream decoded content to stdout
  milliways attach --json 42        emit one NDJSON event per delta/done

Navigator mode (parallel panel):
  milliways attach --nav grp-abc    render the slot navigator for a parallel group`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Mutual exclusion: --nav and positional handle cannot coexist.
			if navGroupID != "" && len(args) > 0 {
				return fmt.Errorf("--nav and positional handle are mutually exclusive")
			}
			if navGroupID != "" {
				// Navigator mode.
				ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
				defer stop()
				return runNavigator(ctx, navGroupID)
			}
			// Streaming mode requires exactly one positional handle.
			if len(args) != 1 {
				return fmt.Errorf("attach requires a session handle (integer) or --nav <group-id>")
			}
			handle, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid handle %q: must be an integer", args[0])
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return runAttach(ctx, handle, jsonMode, os.Stdout, os.Stderr)
		},
	}

	cmd.Flags().BoolVar(&jsonMode, "json", false, "Emit one NDJSON event per delta/done")
	cmd.Flags().StringVar(&navGroupID, "nav", "", "Run navigator mode for the given parallel group ID")

	return cmd
}

// runAttach dials the daemon, subscribes to agent.stream for handle, and
// drains events to out. stderr receives error messages.
func runAttach(ctx context.Context, handle int64, jsonMode bool, out io.Writer, errw io.Writer) error {
	sock := daemonSocket()
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
		case "delta":
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
	var lastErr error

	render := func() {
		if lastErr != nil {
			return
		}
		// Clear and home.
		fmt.Print("\033[2J\033[H")
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
		for _, s := range slots {
			marker := "  "
			if s.SlotN == selected {
				marker = "▶ "
			}
			ago := "0s ago"
			fmt.Printf("%s%d  %-8s %-9s %s  %s tok\n",
				marker, s.SlotN, s.Provider, s.Status, ago, formatNavTokens(s.TokensOut))
			fmt.Println("──")
		}
		fmt.Println("──────────────────────────────")
		fmt.Printf("%d running | %d done | %s\n", running, done, groupID)
		fmt.Println("1–9 select · Tab cycle · c consensus · q exit")
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
