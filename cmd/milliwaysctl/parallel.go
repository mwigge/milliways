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

package main

// `milliwaysctl parallel <verb>` — inspect parallel dispatch groups.
//
// Verbs:
//
//	list                  — list up to 20 recent groups (table)
//	status <group-id>     — show per-slot status for a group
//	consensus <group-id>  — print the consensus aggregate summary

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/mwigge/milliways/internal/rpc"
)

// runParallel is the top-level dispatcher for `milliwaysctl parallel`.
// Returns an exit code: 0 = ok, 1 = error, 2 = usage.
func runParallel(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printParallelUsage(stderr)
		return 2
	}
	switch args[0] {
	case "-h", "--help", "help":
		printParallelUsage(stdout)
		return 0
	case "list":
		return runParallelList(args[1:], stdout, stderr)
	case "status":
		return runParallelStatus(args[1:], stdout, stderr)
	case "consensus":
		return runParallelConsensus(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "milliwaysctl parallel: unknown verb %q\n", args[0])
		printParallelUsage(stderr)
		return 2
	}
}

func printParallelUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: milliwaysctl parallel <verb> [args...]")
	fmt.Fprintln(w, "verbs:")
	fmt.Fprintln(w, "  list                    list recent parallel dispatch groups")
	fmt.Fprintln(w, "  status <group-id>       show per-slot status for a group")
	fmt.Fprintln(w, "  consensus <group-id>    print the consensus aggregate summary")
}

// runParallelList calls group.list and renders a table.
// stdout and stderr may be nil when not needed (tests pass nil).
func runParallelList(args []string, stdout, stderr io.Writer, socketOverride ...string) int {
	_ = args // no additional flags for list
	sock := defaultSocket()
	if len(socketOverride) > 0 && socketOverride[0] != "" {
		sock = socketOverride[0]
	}
	c, err := rpc.Dial(sock)
	if err != nil {
		if stderr != nil {
			fmt.Fprintf(stderr, "milliwaysctl parallel list: dial %s: %v\n", sock, err)
		}
		return 1
	}
	defer func() { _ = c.Close() }()

	var result map[string]any
	if err := c.Call("group.list", map[string]any{}, &result); err != nil {
		if stderr != nil {
			fmt.Fprintf(stderr, "milliwaysctl parallel list: group.list: %v\n", err)
		}
		return 1
	}

	if stdout != nil {
		renderParallelList(result, stdout)
	}
	return 0
}

// renderParallelList renders the group list table to w.
// Exported (unexported in practice but accessible within the package) for testing.
func renderParallelList(result map[string]any, w io.Writer) {
	groups, _ := result["groups"].([]any)
	if len(groups) == 0 {
		fmt.Fprintln(w, "no groups found")
		return
	}

	const colGroupID = 16
	const colPrompt = 42
	const colStatus = 10
	const colCreated = 12

	header := fmt.Sprintf("%-*s  %-*s  %-*s  %-*s",
		colGroupID, "GROUP ID",
		colPrompt, "PROMPT",
		colStatus, "STATUS",
		colCreated, "CREATED")
	fmt.Fprintln(w, header)
	fmt.Fprintln(w, strings.Repeat("-", len(header)))

	for _, item := range groups {
		g, ok := item.(map[string]any)
		if !ok {
			continue
		}
		groupID, _ := g["group_id"].(string)
		prompt, _ := g["prompt"].(string)
		status, _ := g["status"].(string)
		createdAt, _ := g["created_at"].(string)

		shortID := groupID
		if len(shortID) > colGroupID {
			shortID = shortID[:colGroupID]
		}
		shortPrompt := prompt
		if len(shortPrompt) > colPrompt {
			shortPrompt = shortPrompt[:colPrompt-3] + "..."
		}
		created := humanizeTime(createdAt)

		fmt.Fprintf(w, "%-*s  %-*s  %-*s  %-*s\n",
			colGroupID, shortID,
			colPrompt, shortPrompt,
			colStatus, status,
			colCreated, created)
	}
}

// runParallelStatus calls group.status for the given group ID and renders the
// slot table. stdout and/or stderr may be nil.
func runParallelStatus(args []string, stdout, stderr io.Writer, socketOverride ...string) int {
	if len(args) == 0 {
		if stderr != nil {
			fmt.Fprintln(stderr, "usage: milliwaysctl parallel status <group-id>")
		}
		return 2
	}
	groupID := args[0]

	sock := defaultSocket()
	if len(socketOverride) > 0 && socketOverride[0] != "" {
		sock = socketOverride[0]
	}
	c, err := rpc.Dial(sock)
	if err != nil {
		if stderr != nil {
			fmt.Fprintf(stderr, "milliwaysctl parallel status: dial %s: %v\n", sock, err)
		}
		return 1
	}
	defer func() { _ = c.Close() }()

	var result map[string]any
	if err := c.Call("group.status", map[string]any{"group_id": groupID}, &result); err != nil {
		if stderr != nil {
			fmt.Fprintf(stderr, "milliwaysctl parallel status: group.status: %v\n", err)
		}
		return 1
	}

	if stdout != nil {
		renderParallelStatus(result, stdout)
	}
	return 0
}

// renderParallelStatus renders the slot status table to w.
func renderParallelStatus(result map[string]any, w io.Writer) {
	groupID, _ := result["group_id"].(string)
	prompt, _ := result["prompt"].(string)
	status, _ := result["status"].(string)
	createdAt, _ := result["created_at"].(string)

	fmt.Fprintf(w, "Group:   %s\n", groupID)
	fmt.Fprintf(w, "Prompt:  %s\n", prompt)
	fmt.Fprintf(w, "Status:  %s\n", status)
	fmt.Fprintf(w, "Created: %s\n", createdAt)
	fmt.Fprintln(w)

	slots, _ := result["slots"].([]any)
	if len(slots) == 0 {
		fmt.Fprintln(w, "no slots")
		return
	}

	header := fmt.Sprintf("%-4s  %-12s  %-8s  %-10s  %-10s  %9s  %10s",
		"SLOT", "PROVIDER", "STATUS", "STARTED", "COMPLETED", "TOKENS IN", "TOKENS OUT")
	fmt.Fprintln(w, header)
	fmt.Fprintln(w, strings.Repeat("-", len(header)))

	for i, item := range slots {
		sl, ok := item.(map[string]any)
		if !ok {
			continue
		}
		provider, _ := sl["provider"].(string)
		slStatus, _ := sl["status"].(string)
		startedAt, _ := sl["started_at"].(string)
		completedAt, _ := sl["completed_at"].(string)
		tokensIn, _ := sl["tokens_in"].(float64)
		tokensOut, _ := sl["tokens_out"].(float64)

		fmt.Fprintf(w, "%-4d  %-12s  %-8s  %-10s  %-10s  %9.0f  %10.0f\n",
			i+1, provider, slStatus,
			shortTime(startedAt), shortTime(completedAt),
			tokensIn, tokensOut)
	}
}

// runParallelConsensus calls consensus.aggregate and prints the summary.
func runParallelConsensus(args []string, stdout, stderr io.Writer, socketOverride ...string) int {
	if len(args) == 0 {
		if stderr != nil {
			fmt.Fprintln(stderr, "usage: milliwaysctl parallel consensus <group-id>")
		}
		return 2
	}
	groupID := args[0]

	sock := defaultSocket()
	if len(socketOverride) > 0 && socketOverride[0] != "" {
		sock = socketOverride[0]
	}
	c, err := rpc.Dial(sock)
	if err != nil {
		if stderr != nil {
			fmt.Fprintf(stderr, "milliwaysctl parallel consensus: dial %s: %v\n", sock, err)
		}
		return 1
	}
	defer func() { _ = c.Close() }()

	var result map[string]any
	if err := c.Call("consensus.aggregate", map[string]any{"group_id": groupID}, &result); err != nil {
		if stderr != nil {
			fmt.Fprintf(stderr, "milliwaysctl parallel consensus: %v\n", err)
		}
		return 1
	}

	if stdout != nil {
		summary, _ := result["summary"].(string)
		fmt.Fprintln(stdout, summary)
	}
	return 0
}

// humanizeTime returns a short human-readable duration since an RFC3339 timestamp.
// Returns the raw string if parsing fails.
func humanizeTime(ts string) string {
	if ts == "" {
		return "-"
	}
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return ts
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
}

// shortTime returns HH:MM:SS from an RFC3339 timestamp, or "-" for empty.
func shortTime(ts string) string {
	if ts == "" {
		return "-"
	}
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return ts
	}
	return t.UTC().Format("15:04:05")
}
