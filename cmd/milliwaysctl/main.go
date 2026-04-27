// Package main is the milliwaysctl thin client — used by the wezterm Lua
// status bar and by humans. JSON-RPC 2.0 over UDS to milliwaysd. See
// openspec/changes/milliways-emulator-fork/specs/term-daemon-rpc/spec.md.
package main

import (
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/mwigge/milliways/internal/rpc"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	sub := os.Args[1]
	rest := os.Args[2:]

	fs := flag.NewFlagSet(sub, flag.ExitOnError)
	socket := fs.String("socket", "", "UDS path (default: ${state}/sock)")
	agentID := fs.String("agent", "", "agent_id (for `bridge` and `open`)")
	handleFlag := fs.Int64("handle", 0, "agent handle (for `bridge`)")
	metricName := fs.String("metric", "", "metric name (for `metrics`)")
	metricTier := fs.String("tier", "raw", "tier: raw|hourly|daily|weekly|monthly (for `metrics`)")
	metricRange := fs.String("range", "", "relative range (e.g. -24h, -7d, -12mo) for `metrics`")
	metricAgent := fs.String("agent-id", "", "filter by agent_id (for `metrics`)")
	fs.Parse(rest)
	if *socket == "" {
		*socket = defaultSocket()
	}

	switch sub {
	case "ping":
		callJSON(*socket, "ping", nil)
	case "status":
		callJSON(*socket, "status.get", nil)
	case "agents":
		callJSON(*socket, "agent.list", nil)
	case "quota":
		callJSON(*socket, "quota.get", nil)
	case "routing":
		callJSON(*socket, "routing.peek", nil)
	case "subscribe-status":
		subscribeStatus(*socket)
	case "spans":
		callJSON(*socket, "observability.spans", nil)
	case "open":
		if *agentID == "" {
			die("open requires --agent <agent_id>")
		}
		callJSON(*socket, "agent.open", map[string]any{"agent_id": *agentID})
	case "bridge":
		if *handleFlag == 0 {
			die("bridge requires --handle <id>; obtain via `milliwaysctl open --agent <id>`")
		}
		bridge(*socket, *handleFlag)
	case "metrics":
		if *metricName == "" {
			die("metrics requires --metric <name>")
		}
		params := map[string]any{
			"metric": *metricName,
			"tier":   *metricTier,
		}
		if *metricRange != "" {
			params["range"] = map[string]any{"from": *metricRange}
		}
		if *metricAgent != "" {
			params["agent_id"] = *metricAgent
		}
		callJSON(*socket, "metrics.rollup.get", params)
	case "observe-render":
		observeRender(*socket)
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "milliwaysctl: unknown subcommand %q\n", sub)
		usage()
		os.Exit(2)
	}
}

// callJSON invokes a JSON-RPC method and prints the result indented to
// stdout. Used by every read-only subcommand below.
func callJSON(socket, method string, params any) {
	c, err := rpc.Dial(socket)
	if err != nil {
		die("dial %s: %v", socket, err)
	}
	defer c.Close()
	var result any
	if err := c.Call(method, params, &result); err != nil {
		die("%s: %v", method, err)
	}
	out, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(out))
}

// subscribeStatus opens a status.subscribe stream and prints each NDJSON
// event line to stdout until the daemon ends it or the user kills the
// process. Useful for human smoke-testing.
func subscribeStatus(socket string) {
	c, err := rpc.Dial(socket)
	if err != nil {
		die("dial %s: %v", socket, err)
	}
	defer c.Close()
	events, cancel, err := c.Subscribe("status.subscribe", nil)
	if err != nil {
		die("subscribe: %v", err)
	}
	defer cancel()
	for ev := range events {
		fmt.Println(string(ev))
	}
}

// bridge is the AgentDomain pane shim. Spawned as a subprocess by
// milliways-term's AgentDomain, it bridges between the parent's
// stdin/stdout (a slave PTY) and the agent.send / agent.stream surface
// of milliwaysd.
//
// Architecture:
//   stdin  → bytes → agent.send({handle, b64})
//   sidecar `{"t":"data","b64":...}` events → bytes → stdout
//
// On end-of-stream, exits 0. On stdin EOF, leaves the stream open until
// the daemon ends it (in case the agent has more to say).
func bridge(socket string, handle int64) {
	c, err := rpc.Dial(socket)
	if err != nil {
		die("dial: %v", err)
	}
	defer c.Close()

	events, cancel, err := c.Subscribe("agent.stream", map[string]any{"handle": handle})
	if err != nil {
		die("agent.stream: %v", err)
	}
	defer cancel()

	// stdin → agent.send (separate Client to avoid the half-duplex
	// limitation of single-client call/subscribe).
	go func() {
		sendClient, err := rpc.Dial(socket)
		if err != nil {
			fmt.Fprintf(os.Stderr, "bridge: send-dial: %v\n", err)
			return
		}
		defer sendClient.Close()
		buf := make([]byte, 4096)
		for {
			n, err := os.Stdin.Read(buf)
			if n > 0 {
				b64 := base64.StdEncoding.EncodeToString(buf[:n])
				var ack map[string]any
				if err := sendClient.Call("agent.send", map[string]any{
					"handle": handle, "b64": b64,
				}, &ack); err != nil {
					fmt.Fprintf(os.Stderr, "bridge: agent.send: %v\n", err)
					return
				}
			}
			if err == io.EOF {
				return
			}
			if err != nil {
				fmt.Fprintf(os.Stderr, "bridge: stdin: %v\n", err)
				return
			}
		}
	}()

	// sidecar → stdout
	for ev := range events {
		var msg struct {
			T   string `json:"t"`
			B64 string `json:"b64"`
		}
		if err := json.Unmarshal(ev, &msg); err != nil {
			continue
		}
		switch msg.T {
		case "data":
			bytes, err := base64.StdEncoding.DecodeString(msg.B64)
			if err == nil {
				os.Stdout.Write(bytes)
			}
		case "end":
			return
		}
	}
}

func defaultSocket() string {
	if x := os.Getenv("XDG_RUNTIME_DIR"); x != "" {
		return filepath.Join(x, "milliways", "sock")
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".local", "state", "milliways", "sock")
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: milliwaysctl <subcommand> [--socket PATH]")
	fmt.Fprintln(os.Stderr, "  ping     — verify the daemon is reachable")
	fmt.Fprintln(os.Stderr, "  status   — fetch live cockpit state (status.get)")
	fmt.Fprintln(os.Stderr, "  agents   — list registered agents (agent.list)")
	fmt.Fprintln(os.Stderr, "  quota    — show pantry quota snapshots (quota.get)")
	fmt.Fprintln(os.Stderr, "  routing  — peek recent sommelier decisions (routing.peek)")
	fmt.Fprintln(os.Stderr, "  subscribe-status  — stream live cockpit state (status.subscribe)")
	fmt.Fprintln(os.Stderr, "  spans    — recent OTel-flavoured spans (observability.spans)")
	fmt.Fprintln(os.Stderr, "  open --agent <id>     — open an agent session (agent.open)")
	fmt.Fprintln(os.Stderr, "  bridge --handle <id>  — pane shim: stdin↔agent.send, sidecar↔stdout")
	fmt.Fprintln(os.Stderr, "  metrics --metric <name> [--tier raw|hourly|daily|weekly|monthly]")
	fmt.Fprintln(os.Stderr, "          [--range -24h] [--agent-id <id>]  — query metrics.rollup.get")
	fmt.Fprintln(os.Stderr, "  observe-render  — observability cockpit renderer (text frame at 1 Hz)")
}

func die(f string, a ...any) {
	fmt.Fprintf(os.Stderr, "milliwaysctl: "+f+"\n", a...)
	os.Exit(1)
}
