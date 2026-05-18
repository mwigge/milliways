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

//nolint:errcheck // CLI table/detail output writes are best-effort; RPC/encoding failures are handled explicitly.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/mwigge/milliways/internal/rpc"
)

func runWorkflow(args []string, stdout, stderr io.Writer, socketOverride ...string) int {
	if len(args) == 0 {
		printWorkflowUsage(stderr)
		return 2
	}
	verb := args[0]
	rest := args[1:]
	switch verb {
	case "-h", "--help", "help":
		printWorkflowUsage(stdout)
		return 0
	case "list":
		return runWorkflowList(rest, stdout, stderr, socketOverride...)
	case "show":
		return runWorkflowShow(rest, stdout, stderr, socketOverride...)
	default:
		fmt.Fprintf(stderr, "milliwaysctl workflow: unknown subcommand %q\n", verb)
		printWorkflowUsage(stderr)
		return 2
	}
}

func runWorkflowList(args []string, stdout, stderr io.Writer, socketOverride ...string) int {
	fs := flag.NewFlagSet("workflow list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print raw workflow list as JSON")
	socket := fs.String("socket", "", "UDS path (default: ${state}/sock)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	applyWorkflowSocket(socket, socketOverride)

	result, rc := callWorkflowRPC("workflow list", "workflow.list", nil, stderr, *socket)
	if rc != 0 {
		return rc
	}
	if *asJSON {
		return printWorkflowJSON(stdout, stderr, "workflow list", result)
	}
	renderWorkflowList(stdout, result)
	return 0
}

func runWorkflowShow(args []string, stdout, stderr io.Writer, socketOverride ...string) int {
	fs := flag.NewFlagSet("workflow show", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print raw workflow graph as JSON")
	socket := fs.String("socket", "", "UDS path (default: ${state}/sock)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 || strings.TrimSpace(fs.Arg(0)) == "" {
		fmt.Fprintln(stderr, "workflow show requires <id>")
		return 2
	}
	applyWorkflowSocket(socket, socketOverride)

	result, rc := callWorkflowRPC("workflow show", "workflow.get", map[string]any{"id": strings.TrimSpace(fs.Arg(0))}, stderr, *socket)
	if rc != 0 {
		return rc
	}
	if *asJSON {
		return printWorkflowJSON(stdout, stderr, "workflow show", result)
	}
	renderWorkflowShow(stdout, result)
	return 0
}

func callWorkflowRPC(label, method string, params map[string]any, stderr io.Writer, sock string) (map[string]any, int) {
	c, err := rpc.Dial(sock)
	if err != nil {
		fmt.Fprintf(stderr, "milliwaysctl %s: dial %s: %v\n", label, sock, err)
		return nil, 1
	}
	defer func() { _ = c.Close() }()

	var result map[string]any
	if err := c.Call(method, params, &result); err != nil {
		fmt.Fprintf(stderr, "milliwaysctl %s: %v\n", label, err)
		return nil, 1
	}
	return result, 0
}

func printWorkflowJSON(stdout, stderr io.Writer, label string, result map[string]any) int {
	out, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "milliwaysctl %s: encode result: %v\n", label, err)
		return 1
	}
	fmt.Fprintln(stdout, string(out))
	return 0
}

func renderWorkflowList(w io.Writer, result map[string]any) {
	items, _ := result["workflows"].([]any)
	if len(items) == 0 {
		fmt.Fprintln(w, "no workflows")
		return
	}
	fmt.Fprintln(w, "ID        STATUS             NODES  GOAL")
	for _, item := range items {
		row := mapValue(item)
		fmt.Fprintf(w, "%-9s %-18s %-6s %s\n",
			firstString(row, "id"),
			firstString(row, "status"),
			numberString(row["nodes"]),
			firstString(row, "goal"),
		)
	}
}

func renderWorkflowShow(w io.Writer, result map[string]any) {
	wf := mapField(result, "workflow")
	nodes, _ := wf["nodes"].([]any)
	fmt.Fprintf(w, "workflow %s\n", firstString(wf, "id"))
	fmt.Fprintf(w, "status: %s\n", firstString(wf, "status"))
	if goal := firstString(wf, "goal"); goal != "unknown" {
		fmt.Fprintf(w, "goal: %s\n", goal)
	}
	fmt.Fprintf(w, "nodes: %d\n", len(nodes))
	for _, item := range nodes {
		node := mapValue(item)
		fmt.Fprintf(w, "  %-16s %-16s %s\n", firstString(node, "id"), firstString(node, "type"), firstString(node, "status"))
	}
}

func applyWorkflowSocket(socket *string, socketOverride []string) {
	if len(socketOverride) > 0 && socketOverride[0] != "" {
		*socket = socketOverride[0]
	}
	if strings.TrimSpace(*socket) == "" {
		*socket = defaultSocket()
	}
}

func numberString(value any) string {
	switch n := value.(type) {
	case float64:
		return fmt.Sprintf("%.0f", n)
	case int:
		return fmt.Sprintf("%d", n)
	case int64:
		return fmt.Sprintf("%d", n)
	default:
		return "0"
	}
}

func printWorkflowUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: milliwaysctl workflow <list|show> [--json]")
	fmt.Fprintln(w, "  list [--json]       list stored workflow graphs")
	fmt.Fprintln(w, "  show <id> [--json]  show one stored workflow graph")
}
