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

// Package main is the milliwaysctl thin client — used by the wezterm Lua
// status bar and by humans. JSON-RPC 2.0 over UDS to milliwaysd. See
// openspec/changes/milliways-emulator-fork/specs/term-daemon-rpc/spec.md.
package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mwigge/milliways/internal/rpc"
)

var version = "dev"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	sub := os.Args[1]
	rest := os.Args[2:]

	// Subcommands that need their own flag parsing must dispatch before the
	// global FlagSet runs (which uses flag.ExitOnError and would reject any
	// flag it does not know about).
	if sub == "local" {
		os.Exit(runLocal(rest, os.Stdout, os.Stderr))
	}
	if sub == "opsx" {
		os.Exit(runOpsx(rest, os.Stdout, os.Stderr))
	}
	if sub == "install" {
		os.Exit(runInstall(rest, os.Stdout, os.Stderr))
	}
	if sub == "upgrade" {
		os.Exit(runUpgrade(rest, os.Stdout, os.Stderr))
	}
	if sub == "codegraph" {
		os.Exit(runCodegraph(rest, os.Stdout, os.Stderr))
	}
	if sub == "check" {
		os.Exit(runCheck(rest, os.Stdout, os.Stderr))
	}
	if sub == "parallel" {
		os.Exit(runParallel(rest, os.Stdout, os.Stderr))
	}
	if sub == "security" {
		os.Exit(runSecurity(rest, os.Stdout, os.Stderr))
	}
	if sub == "daemon" {
		os.Exit(runDaemon(rest, os.Stdout, os.Stderr))
	}

	fs := flag.NewFlagSet(sub, flag.ExitOnError)
	socket := fs.String("socket", "", "UDS path (default: ${state}/sock)")
	stateDir := fs.String("state", "", "state directory (default: XDG_RUNTIME_DIR or ~/.local/state/milliways)")
	watchMs := fs.Int("debounce-ms", 250, "debounce milliseconds for status --watch (min interval between writes)")
	watchFlag := fs.Bool("watch", false, "watch mode for status: subscribe and write atomic status.cur")
	agentID := fs.String("agent", "", "agent_id (for `bridge`, `open`, `context`, `context-render`)")
	handleFlag := fs.Int64("handle", 0, "agent handle (for `bridge` / `apply`)")
	metricName := fs.String("metric", "", "metric name (for `metrics`)")
	metricTier := fs.String("tier", "raw", "tier: raw|hourly|daily|weekly|monthly (for `metrics`)")
	metricRange := fs.String("range", "", "relative range (e.g. -24h, -7d, -12mo) for `metrics`")
	metricAgent := fs.String("agent-id", "", "filter by agent_id (for `metrics`)")
	applyIndex := fs.Int("index", -1, "code-block index (for `apply`); default = print all")
	applyOut := fs.String("out", "", "write block to this path (for `apply`); blank = stdout")
	allFlag := fs.Bool("all", false, "aggregate across all agents (for `context`)")
	chartKind := fs.String("kind", "", "chart kind: sparkline|bars (for `chart`)")
	chartData := fs.String("data", "", "chart input as JSON (for `chart`)")
	fs.Parse(rest)
	if *socket == "" {
		*socket = defaultSocket()
	}
	if *stateDir == "" {
		// derive from socket default location
		d := defaultSocket()
		*stateDir = filepath.Dir(d)
	}

	switch sub {
	case "ping":
		callJSON(*socket, "ping", nil)
	case "status":
		if *watchFlag {
			watchStatus(*socket, *stateDir, *watchMs)
		} else {
			callJSON(*socket, "status.get", nil)
		}
	case "observe":
		runObserve(*socket, *stateDir, *watchMs)
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
	case "apply":
		if *handleFlag == 0 {
			die("apply requires --handle <id>; obtain via `milliwaysctl open --agent <id>`")
		}
		apply(*socket, *handleFlag, *applyIndex, *applyOut)
	case "context":
		if *allFlag {
			callJSON(*socket, "context.get_all", nil)
		} else {
			if *agentID == "" {
				die("context requires --agent <agent_id> or --all")
			}
			callJSON(*socket, "context.get", map[string]any{"agent_id": *agentID})
		}
	case "context-render":
		if *agentID == "" {
			die("context-render requires --agent <agent_id> (use _all for aggregate)")
		}
		contextRender(*socket, *agentID)
	case "metrics":
		if *metricName == "" {
			// No specific metric requested → show full dashboard.
			runMetricsDashboard(*socket, *watchFlag)
		} else {
			callMetricsRollup(*socket, *metricName, *metricTier, *metricRange, *metricAgent)
		}
	case "observe-render":
		observeRender(*socket)
	case "history-append":
		// usage: milliwaysctl history-append --agent <id> --data '{"x":1}'
		if *agentID == "" {
			die("history-append requires --agent <id>")
		}
		var payload any = nil
		if *chartData != "" {
			if err := json.Unmarshal([]byte(*chartData), &payload); err != nil {
				die("invalid --data JSON: %v", err)
			}
		}
		c, err := rpc.Dial(*socket)
		if err != nil {
			die("dial %s: %v", *socket, err)
		}
		defer c.Close()
		var appendRes any
		if err := c.Call("history.append", map[string]any{"agent_id": *agentID, "payload": payload, "max_lines": 1000}, &appendRes); err != nil {
			die("history.append: %v", err)
		}
		out, _ := json.MarshalIndent(appendRes, "", "  ")
		fmt.Println(string(out))
	case "history-get":
		if *agentID == "" {
			die("history-get requires --agent <id>")
		}
		limit := -1
		if *applyIndex >= 0 {
			limit = *applyIndex
		}
		cGet, err := rpc.Dial(*socket)
		if err != nil {
			die("dial %s: %v", *socket, err)
		}
		defer cGet.Close()
		var res any
		if err := cGet.Call("history.get", map[string]any{"agent_id": *agentID, "limit": limit}, &res); err != nil {
			die("history.get: %v", err)
		}
		out, _ := json.MarshalIndent(res, "", "  ")
		fmt.Println(string(out))
	case "history-summary":
		if *agentID == "" {
			die("history-summary requires --agent <id>")
		}
		limit := 20
		if *applyIndex >= 0 {
			limit = *applyIndex
		}
		historySummary(*socket, *agentID, limit)
	case "chart":
		if *chartKind == "" || *chartData == "" {
			die("chart requires --kind <sparkline|bars> --data <json>")
		}
		if err := renderChart(os.Stdout, *chartKind, *chartData); err != nil {
			die("chart: %v", err)
		}
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

// watchStatus subscribes to status.subscribe and writes debounced latest
// NDJSON line into stateDir/status.cur using a tmp+fsync+rename atomic update.
// Debounce is enforced to avoid writing faster than 4Hz (250ms min).
func watchStatus(socket, stateDir string, debounceMs int) {
	if debounceMs < 250 {
		debounceMs = 250
	}
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

	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		die("state dir: %v", err)
	}
	tmpPath := filepath.Join(stateDir, "status.cur.tmp")
	finalPath := filepath.Join(stateDir, "status.cur")
	interval := time.Duration(debounceMs) * time.Millisecond

	updates := make(chan []byte, 128)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// parent death watcher: exit if ppid==1
	go func() {
		pp := os.Getppid()
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(1 * time.Second):
				if os.Getppid() == 1 && pp != 1 {
					os.Exit(0)
				}
			}
		}
	}()

	// writer goroutine with debounce
	go func() {
		var timer *time.Timer
		var mu sync.Mutex
		var pending []byte
		var lastWrite time.Time
		writeNow := func(b []byte) {
			f, err := os.Create(tmpPath)
			if err != nil {
				slog.Warn("watchStatus: create tmp", "err", err)
				return
			}
			if _, err := f.Write(append(b, '\n')); err != nil {
				f.Close()
				slog.Warn("watchStatus: write tmp", "err", err)
				return
			}
			if err := f.Sync(); err != nil {
				// best effort
			}
			if err := f.Close(); err != nil {
				slog.Warn("watchStatus: close tmp", "err", err)
				return
			}
			if err := os.Rename(tmpPath, finalPath); err != nil {
				slog.Warn("watchStatus: rename", "err", err)
				return
			}
			lastWrite = time.Now()
		}

		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-updates:
				if !ok {
					return
				}
				mu.Lock()
				pending = ev
				// if enough time elapsed since last write, write immediately
				if time.Since(lastWrite) >= interval {
					writeNow(pending)
					pending = nil
					if timer != nil {
						timer.Stop()
						timer = nil
					}
				} else {
					// schedule timer if not already scheduled
					if timer == nil {
						delay := interval - time.Since(lastWrite)
						timer = time.AfterFunc(delay, func() {
							mu.Lock()
							if pending != nil {
								writeNow(pending)
								pending = nil
							}
							mu.Unlock()
						})
					}
				}
				mu.Unlock()
			}
		}
	}()

	// pump events into updates
	for ev := range events {
		select {
		case updates <- ev:
		default:
			// drop if overwhelmed; keep system responsive
		}
		// short-circuit exit if context done
		select {
		case <-ctx.Done():
			return
		default:
		}
	}
}

// observeConfig holds the static list of available agents shown in the status bar.
var observeAgents = []string{"claude", "codex", "copilot", "minimax", "gemini", "local", "pool"}

func normalizeObserveSecurityStatus(result map[string]any) map[string]any {
	if len(result) == 0 {
		return nil
	}
	mode := stringMapField(result, "mode")
	posture := strings.ToLower(strings.TrimSpace(firstStringField(result, "state", "posture", "level")))
	warnings := intMapField(result, "warnings", "warn_count", "warning_count")
	blocks := intMapField(result, "blocks", "block_count", "blocked_count")
	if blocks > 0 {
		posture = "block"
	} else if warnings > 0 && posture == "" {
		posture = "warn"
	} else if posture == "" {
		posture = "ok"
	}
	installed, _ := result["installed"].(bool)
	enabled, _ := result["enabled"].(bool)
	return map[string]any{
		"posture":   posture,
		"warnings":  warnings,
		"blocks":    blocks,
		"mode":      mode,
		"installed": installed,
		"enabled":   enabled,
	}
}

func fetchObserveSecurityStatus(c *rpc.Client) map[string]any {
	var result map[string]any
	if err := c.Call("security.status", map[string]any{}, &result); err != nil {
		return nil
	}
	return normalizeObserveSecurityStatus(result)
}

// runObserve writes a compact JSON status to ${stateDir}/observe.cur every debounceMs.
// Format: {"v":"<version>","p":"<cwd>","c":"<current_agent>","a":["claude","codex","copilot","minimax","gemini","local","pool"],"sec":{"posture":"ok|warn|block"}}
//
// This file is read by the wezterm Lua sidecar to render the full status bar:
//
//	[≈≈ MW v0.x] [path] [●claude] [1:C 2:X 3:Cp 4:M 5:G 6:L 7:P]
//
// Also writes a heartbeat file every 30s. On startup, if the heartbeat is
// stale by more than 60s, the system was asleep — a "woke_ago" field is
// included in observe.cur for the following 5 minutes so wezterm can show
// a wake badge.
func runObserve(socket, stateDir string, debounceMs int) {
	if debounceMs < 250 {
		debounceMs = 250
	}
	c, err := rpc.Dial(socket)
	if err != nil {
		die("dial: %v", err)
	}
	defer c.Close()

	events, cancel, err := c.Subscribe("status.subscribe", nil)
	if err != nil {
		die("status.subscribe: %v", err)
	}
	defer cancel()

	// Also grab the agent list once.
	var agentList []map[string]any
	c.Call("agent.list", nil, &agentList)

	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		die("state dir: %v", err)
	}
	tmpPath := filepath.Join(stateDir, "observe.cur.tmp")
	finalPath := filepath.Join(stateDir, "observe.cur")
	hbPath := filepath.Join(stateDir, "heartbeat")

	// Detect wake: if heartbeat is >60s stale, the process was suspended.
	var wokeAt time.Time
	if hbData, err := os.ReadFile(hbPath); err == nil {
		if ts, err := time.Parse(time.RFC3339, strings.TrimSpace(string(hbData))); err == nil {
			if time.Since(ts) > 60*time.Second {
				wokeAt = time.Now()
			}
		}
	}

	// Get current working directory for path display.
	cwd, _ := os.Getwd()

	interval := time.Duration(debounceMs) * time.Millisecond
	updates := make(chan []byte, 128)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	var securityStatus map[string]any
	var lastSecurityPoll time.Time

	// ppid watcher — exit if orphaned.
	go func() {
		pp := os.Getppid()
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(1 * time.Second):
				if os.Getppid() == 1 && pp != 1 {
					os.Exit(0)
				}
			}
		}
	}()

	// heartbeat ticker — write current timestamp every 30s.
	// When the system sleeps, this process is suspended, so the file
	// becomes stale. On next startup, the staleness reveals the sleep.
	go func() {
		writeHB := func() {
			_ = os.WriteFile(hbPath, []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0o644)
		}
		writeHB()
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				writeHB()
			}
		}
	}()

	// writer goroutine with debounce.
	go func() {
		var mu sync.Mutex
		var pending []byte
		var lastWrite time.Time
		writeNow := func(b []byte) {
			f, err := os.Create(tmpPath)
			if err != nil {
				slog.Warn("observe: create tmp", "err", err)
				return
			}
			if _, err := f.Write(append(b, '\n')); err != nil {
				f.Close()
				slog.Warn("observe: write tmp", "err", err)
				return
			}
			if err := f.Sync(); err != nil {
				slog.Warn("observe: fsync", "err", err)
			}
			if err := f.Close(); err != nil {
				slog.Warn("observe: close tmp", "err", err)
				return
			}
			if err := os.Rename(tmpPath, finalPath); err != nil {
				slog.Warn("observe: rename", "err", err)
				return
			}
			lastWrite = time.Now()
		}

		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-updates:
				if !ok {
					return
				}
				mu.Lock()
				pending = ev
				if time.Since(lastWrite) >= interval {
					writeNow(pending)
					pending = nil
				}
				mu.Unlock()
			}
		}
	}()

	for ev := range events {
		var frame struct {
			T        string `json:"t"`
			Snapshot struct {
				Proto       any     `json:"proto"`
				ActiveAgent *string `json:"active_agent"`
				TokensIn    int     `json:"tokens_in"`
				TokensOut   int     `json:"tokens_out"`
				CostUSD     float64 `json:"cost_usd"`
				QuotaPct    float64 `json:"quota_pct"`
				Errors5m    int     `json:"errors_5m"`
			} `json:"snapshot"`
		}
		if err := json.Unmarshal(ev, &frame); err != nil {
			continue
		}
		if frame.T != "data" {
			continue
		}

		cur := ""
		if frame.Snapshot.ActiveAgent != nil {
			cur = *frame.Snapshot.ActiveAgent
		}

		status := map[string]any{
			"v":      version,
			"p":      cwd,
			"c":      cur,
			"a":      observeAgents,
			"tin":    frame.Snapshot.TokensIn,
			"tout":   frame.Snapshot.TokensOut,
			"cost":   frame.Snapshot.CostUSD,
			"quota":  frame.Snapshot.QuotaPct,
			"errors": frame.Snapshot.Errors5m,
		}
		if time.Since(lastSecurityPoll) >= 5*time.Second {
			securityStatus = fetchObserveSecurityStatus(c)
			lastSecurityPoll = time.Now()
		}
		if len(securityStatus) > 0 {
			status["sec"] = securityStatus
		}
		// Include woke_ago (seconds) for 5 minutes after a detected wake.
		if !wokeAt.IsZero() {
			elapsed := time.Since(wokeAt)
			if elapsed < 5*time.Minute {
				status["woke_ago"] = int(elapsed.Seconds())
			} else {
				wokeAt = time.Time{} // clear after 5 min
			}
		}
		b, _ := json.Marshal(status)
		select {
		case updates <- b:
		default:
		}
	}
}

// bridge is the AgentDomain pane shim. Spawned as a subprocess by
// milliways-term's AgentDomain, it bridges between the parent's
// stdin/stdout (a slave PTY) and the agent.send / agent.stream surface
// of milliwaysd.
//
// Architecture:
//
//	stdin  → bytes → agent.send({handle, b64})
//	sidecar `{"t":"data","b64":...}` events → bytes → stdout
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
					"handle":         handle,
					"b64":            b64,
					"expand_context": true,
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

// apply queries apply.extract for the given handle and writes the
// chosen block (or all blocks, if index<0) to stdout or to a file.
//
// When --out is set and the chosen block carries a Filename, the
// resolved output path is `--out` (treated as a directory path if it
// ends with `/`) joined with the block's filename — otherwise the
// `--out` value is used as-is.
func apply(socket string, handle int64, index int, outPath string) {
	c, err := rpc.Dial(socket)
	if err != nil {
		die("dial %s: %v", socket, err)
	}
	defer c.Close()

	var result struct {
		Blocks []struct {
			Language string `json:"language,omitempty"`
			Filename string `json:"filename,omitempty"`
			Content  string `json:"content"`
		} `json:"blocks"`
	}
	if err := c.Call("apply.extract", map[string]any{"handle": handle}, &result); err != nil {
		die("apply.extract: %v", err)
	}
	if len(result.Blocks) == 0 {
		die("no code blocks found in the most recent response")
	}

	if index < 0 {
		// Print every block to stdout, separated by a fence-style header.
		for i, b := range result.Blocks {
			fmt.Printf("--- block %d (lang=%q file=%q) ---\n", i, b.Language, b.Filename)
			fmt.Println(b.Content)
		}
		return
	}
	if index >= len(result.Blocks) {
		die("index %d out of range (have %d blocks)", index, len(result.Blocks))
	}
	chosen := result.Blocks[index]

	if outPath == "" {
		fmt.Print(chosen.Content)
		if !strings.HasSuffix(chosen.Content, "\n") {
			fmt.Println()
		}
		return
	}

	target := outPath
	if chosen.Filename != "" {
		// If --out names a directory (ends in '/' or exists as a dir),
		// join with the block's filename.
		if strings.HasSuffix(outPath, string(filepath.Separator)) {
			target = filepath.Join(outPath, chosen.Filename)
		} else if fi, err := os.Stat(outPath); err == nil && fi.IsDir() {
			target = filepath.Join(outPath, chosen.Filename)
		}
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		die("mkdir %s: %v", filepath.Dir(target), err)
	}
	if err := os.WriteFile(target, []byte(chosen.Content), 0o644); err != nil {
		die("write %s: %v", target, err)
	}
	fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", target, len(chosen.Content))
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
	fmt.Fprintln(os.Stderr, "  apply  --handle <id> [--index N] [--out PATH]  — extract code blocks (apply.extract)")
	fmt.Fprintln(os.Stderr, "  metrics --metric <name> [--tier raw|hourly|daily|weekly|monthly]")
	fmt.Fprintln(os.Stderr, "          [--range -24h] [--agent-id <id>]  — query metrics.rollup.get")
	fmt.Fprintln(os.Stderr, "  context --agent <id> | --all   — fetch /context snapshot (context.get / .get_all)")
	fmt.Fprintln(os.Stderr, "  context-render --agent <id|_all>  — pane: subscribe context.subscribe, print frames")
	fmt.Fprintln(os.Stderr, "  observe [--watch]  — write MW status bar JSON to ${state}/observe.cur")
	fmt.Fprintln(os.Stderr, "  observe-render  — observability cockpit renderer (text frame at 1 Hz)")
	fmt.Fprintln(os.Stderr, "  chart --kind <sparkline|bars> --data <json>  — render a kitty-graphics chart on stdout")
	fmt.Fprintln(os.Stderr, "  history-append --agent <id> --data <json>  — append to per-agent history (history.append)")
	fmt.Fprintln(os.Stderr, "  history-get --agent <id> [--index N]        — fetch per-agent history (history.get)")
	fmt.Fprintln(os.Stderr, "  history-summary --agent <id> [--index N]    — compact cost+token summary for wezterm status")
	fmt.Fprintln(os.Stderr, "  local <verb> [args...]                     — local-model bootstrap (try `milliwaysctl local --help`)")
	fmt.Fprintln(os.Stderr, "  opsx <verb> [args...]                      — openspec wrapper (try `milliwaysctl opsx --help`)")
	fmt.Fprintln(os.Stderr, "  install <client>                           — install upstream CLI (claude|codex|copilot|gemini|local)")
	fmt.Fprintln(os.Stderr, "  upgrade [--check] [--yes] [--version <tag>] — upgrade milliways to the latest release")
	fmt.Fprintln(os.Stderr, "  codegraph <verb> [args...]                 — CodeGraph index management (try `milliwaysctl codegraph --help`)")
	fmt.Fprintln(os.Stderr, "  check                                      — health check — verify all features are installed")
	fmt.Fprintln(os.Stderr, "  parallel list                              — list recent parallel dispatch groups")
	fmt.Fprintln(os.Stderr, "  parallel status <group-id>                 — show per-slot status for a group")
	fmt.Fprintln(os.Stderr, "  parallel consensus <group-id>              — print the consensus aggregate summary")
	fmt.Fprintln(os.Stderr, "  security list [--include-accepted]         — list active security findings (CVE/OSV)")
	fmt.Fprintln(os.Stderr, "  security show <cve-id>                     — show full CVE detail")
	fmt.Fprintln(os.Stderr, "  security accept <cve-id> --package <name> --reason <text> --expires <YYYY-MM-DD>")
	fmt.Fprintln(os.Stderr, "  daemon stop                                — stop the running milliwaysd")
}

func die(f string, a ...any) {
	fmt.Fprintf(os.Stderr, "milliwaysctl: "+f+"\n", a...)
	os.Exit(1)
}
