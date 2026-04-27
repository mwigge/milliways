package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/mwigge/milliways/internal/rpc"
)

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
			writeFrame(os.Stdout, renderAggregate(agg, time.Now()))
		} else {
			var snap snapshotView
			if err := json.Unmarshal(msg.Snapshot, &snap); err != nil {
				continue
			}
			writeFrame(os.Stdout, renderSnapshot(snap, time.Now()))
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
func renderSnapshot(s snapshotView, now time.Time) string {
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
