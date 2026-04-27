package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/mwigge/milliways/internal/daemon/charts"
	"github.com/mwigge/milliways/internal/rpc"
)

// tokenHistoryCap is the number of recent tokens-in samples kept per
// agent for the context-render sparkline. 30 ≈ 30 turns; the ring is
// in-process and lost when the renderer subprocess restarts.
const tokenHistoryCap = 30

// tokenHistory is a per-agent rolling ring of tokens-in samples used
// to drive the trend sparkline in context-render. It lives in-process
// in the renderer subprocess; daemon-side persistence lands in a
// follow-up.
//
// Concurrency: contextRender runs single-goroutine, so no mutex is
// needed. Tests that exercise concurrent push/snapshot would need to
// add one — the type is small enough to extend.
type tokenHistory struct {
	cap  int
	data map[string][]float64
}

// newTokenHistory returns a ring with the given per-agent capacity.
func newTokenHistory(cap int) *tokenHistory {
	if cap < 1 {
		cap = 1
	}
	return &tokenHistory{cap: cap, data: make(map[string][]float64)}
}

// push appends v to agentID's history, evicting the oldest sample
// when the ring is full.
func (h *tokenHistory) push(agentID string, v float64) {
	cur := h.data[agentID]
	if len(cur) == h.cap {
		cur = cur[1:]
	}
	h.data[agentID] = append(cur, v)
}

// snapshot returns a copy of agentID's history (so callers can pass
// it to charts.Sparkline without aliasing the ring).
func (h *tokenHistory) snapshot(agentID string) []float64 {
	src := h.data[agentID]
	if len(src) == 0 {
		return nil
	}
	out := make([]float64, len(src))
	copy(out, src)
	return out
}

// contextRender subscribes to context.subscribe for the requested agent
// (or aggregate for "_all") and writes a text frame to stdout for every
// snapshot received. Frames are prefixed with `\x1b[2J\x1b[H` (clear+home)
// so the daemon-bridged pane redraws in place.
//
// Lives in milliwaysctl rather than the Rust pane because the daemon owns
// snapshot composition; the renderer is a thin presentation layer that can
// be unit-tested with table-driven fixtures (see render_test.go).
func contextRender(socket, agentID string) {
	c, err := rpc.Dial(socket)
	if err != nil {
		die("dial: %v", err)
	}
	defer c.Close()

	all := agentID == "_all"
	params := map[string]any{}
	if all {
		params["agent_id"] = "_all"
	} else {
		params["agent_id"] = agentID
	}
	events, cancel, err := c.Subscribe("context.subscribe", params)
	if err != nil {
		die("context.subscribe: %v", err)
	}
	defer cancel()

	hist := newTokenHistory(tokenHistoryCap)

	for ev := range events {
		var msg struct {
			T        string          `json:"t"`
			Snapshot json.RawMessage `json:"snapshot"`
		}
		if err := json.Unmarshal(ev, &msg); err != nil {
			continue
		}
		if msg.T != "data" {
			continue
		}
		if all {
			var agg aggregateView
			if err := json.Unmarshal(msg.Snapshot, &agg); err != nil {
				continue
			}
			// Push each agent's tokens-in into its own ring so the
			// per-agent micro-cards (when we add charts there) get
			// a stable history.
			for _, a := range agg.Agents {
				hist.push(a.AgentID, float64(a.Tokens.Input))
			}
			writeFrame(os.Stdout, renderAggregate(agg, time.Now()))
		} else {
			var snap snapshotView
			if err := json.Unmarshal(msg.Snapshot, &snap); err != nil {
				continue
			}
			hist.push(snap.AgentID, float64(snap.Tokens.Input))
			writeFrame(os.Stdout, renderSnapshotWithTrend(snap, time.Now(), hist.snapshot(snap.AgentID)))
		}
	}
}

func writeFrame(w io.Writer, frame string) {
	// \x1b[2J clears the screen, \x1b[H homes the cursor. Together they let
	// the pane redraw in place without flicker on conformant terminals.
	_, _ = fmt.Fprint(w, "\x1b[2J\x1b[H")
	_, _ = io.WriteString(w, frame)
}

// snapshotView mirrors daemon.ContextSnapshot for unmarshalling. Kept local
// to milliwaysctl so the daemon types stay private.
type snapshotView struct {
	AgentID        string     `json:"agent_id"`
	Model          string     `json:"model"`
	SessionID      string     `json:"session_id"`
	UptimeS        float64    `json:"uptime_s"`
	Tokens         tokensView `json:"tokens"`
	Tools          []toolView `json:"tools"`
	MCPServers     []mcpView  `json:"mcp_servers"`
	FilesInContext []fileView `json:"files_in_context"`
	CostUSD        float64    `json:"cost_usd"`
	Errors5m       int        `json:"errors_5m"`
}

type tokensView struct {
	Input  int `json:"input"`
	Output int `json:"output"`
	Cached int `json:"cached"`
}

type toolView struct {
	Name string `json:"name"`
}

type mcpView struct {
	Name      string `json:"name"`
	Connected bool   `json:"connected"`
	ToolCount int    `json:"tool_count"`
}

type fileView struct {
	Path  string `json:"path"`
	Bytes int    `json:"bytes"`
}

type aggregateView struct {
	Agents []snapshotView `json:"agents"`
	Totals totalsView     `json:"totals"`
}

type totalsView struct {
	TokensIn     int     `json:"tokens_in"`
	TokensOut    int     `json:"tokens_out"`
	Cached       int     `json:"cached"`
	CostUSD      float64 `json:"cost_usd"`
	ActiveAgents int     `json:"active_agents"`
	Errors5m     int     `json:"errors_5m"`
}

// renderSnapshot composes the per-agent text frame. Dash placeholders fill
// in for empty fields so the layout is stable across token states.
//
// This is a thin wrapper around renderSnapshotWithTrend with a nil
// trend slice; callers wanting a sparkline of recent tokens-in
// samples should call renderSnapshotWithTrend directly.
func renderSnapshot(s snapshotView, now time.Time) string {
	return renderSnapshotWithTrend(s, now, nil)
}

// renderSnapshotWithTrend composes the per-agent text frame and
// optionally embeds a kitty-graphics sparkline of recent tokens-in
// samples after the cached line. If trend is empty the frame falls
// back to the text-only layout (no "trend:" row at all) so the pane
// stays compact during cold-start.
func renderSnapshotWithTrend(s snapshotView, now time.Time, trend []float64) string {
	var b strings.Builder
	model := dash(s.Model)
	sess := dash(shortSession(s.SessionID))
	ts := now.Format("15:04:05")
	fmt.Fprintf(&b, "╭── /context — %s ── %s ──\n", s.AgentID, ts)
	fmt.Fprintf(&b, "│ model:       %s\n", model)
	fmt.Fprintf(&b, "│ session:     %s\n", sess)
	fmt.Fprintf(&b, "│ uptime:      %ds\n", int(s.UptimeS))
	fmt.Fprintf(&b, "│\n")
	fmt.Fprintf(&b, "│ tokens:\n")
	fmt.Fprintf(&b, "│   in:       %d↑\n", s.Tokens.Input)
	fmt.Fprintf(&b, "│   out:      %d↓\n", s.Tokens.Output)
	fmt.Fprintf(&b, "│   cached:   %d\n", s.Tokens.Cached)
	if len(trend) > 0 {
		// One row label + escape on its own line so the kitty image
		// renders flush-left (most terminals position it where the
		// cursor is when they parse the escape).
		png := charts.Sparkline(trend, charts.DefaultTheme())
		fmt.Fprintf(&b, "│   trend:    %s\n", charts.KittyEscape(png, 0))
	}
	fmt.Fprintf(&b, "│\n")
	fmt.Fprintf(&b, "│ tools:       %d (—)\n", len(s.Tools))
	fmt.Fprintf(&b, "│ mcp:         %d (—)\n", len(s.MCPServers))
	fmt.Fprintf(&b, "│ files:       %d (—)\n", len(s.FilesInContext))
	fmt.Fprintf(&b, "│\n")
	fmt.Fprintf(&b, "│ cost:        $%.4f\n", s.CostUSD)
	fmt.Fprintf(&b, "│ errors_5m:   %d\n", s.Errors5m)
	fmt.Fprintf(&b, "╰──\n")
	return b.String()
}

// renderAggregate composes the multi-agent text frame: a totals header
// followed by one mini-card per agent.
func renderAggregate(a aggregateView, now time.Time) string {
	var b strings.Builder
	ts := now.Format("15:04:05")
	fmt.Fprintf(&b, "╭── /context — totals ── %s ──\n", ts)
	fmt.Fprintf(&b, "│ active:      %d agent(s)\n", a.Totals.ActiveAgents)
	fmt.Fprintf(&b, "│ tokens in:   %d↑\n", a.Totals.TokensIn)
	fmt.Fprintf(&b, "│ tokens out:  %d↓\n", a.Totals.TokensOut)
	fmt.Fprintf(&b, "│ cached:      %d\n", a.Totals.Cached)
	fmt.Fprintf(&b, "│ cost:        $%.4f\n", a.Totals.CostUSD)
	fmt.Fprintf(&b, "│ errors_5m:   %d\n", a.Totals.Errors5m)
	fmt.Fprintf(&b, "╰──\n\n")
	for _, agent := range a.Agents {
		fmt.Fprintf(&b, "  · %-8s  model=%s  sess=%s  in=%d  out=%d  cost=$%.4f  err=%d\n",
			agent.AgentID,
			dash(agent.Model),
			dash(shortSession(agent.SessionID)),
			agent.Tokens.Input,
			agent.Tokens.Output,
			agent.CostUSD,
			agent.Errors5m,
		)
	}
	return b.String()
}

// dash returns "—" if v is empty, else v. Keeps the layout stable when a
// data point is unavailable.
func dash(v string) string {
	if v == "" {
		return "—"
	}
	return v
}

// shortSession truncates a session id to 8 chars for compact display.
func shortSession(v string) string {
	if len(v) > 8 {
		return v[:8]
	}
	return v
}
