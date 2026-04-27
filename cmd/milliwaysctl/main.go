// Package main is the milliwaysctl thin client — used by the wezterm Lua
// status bar and by humans. JSON-RPC 2.0 over UDS to milliwaysd. See
// openspec/changes/milliways-emulator-fork/specs/term-daemon-rpc/spec.md.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
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
}

func die(f string, a ...any) {
	fmt.Fprintf(os.Stderr, "milliwaysctl: "+f+"\n", a...)
	os.Exit(1)
}
