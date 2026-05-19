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
	"os"
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
	case "templates":
		return runWorkflowTemplates(rest, stdout, stderr, socketOverride...)
	case "create":
		return runWorkflowCreate(rest, stdout, stderr, socketOverride...)
	case "show":
		return runWorkflowShow(rest, stdout, stderr, socketOverride...)
	case "export":
		return runWorkflowExport(rest, stdout, stderr, socketOverride...)
	case "import":
		return runWorkflowImport(rest, stdout, stderr, socketOverride...)
	case "ready":
		return runWorkflowReady(rest, stdout, stderr, socketOverride...)
	case "start":
		return runWorkflowStart(rest, stdout, stderr, socketOverride...)
	case "delegate":
		return runWorkflowDelegate(rest, stdout, stderr, socketOverride...)
	case "retry":
		return runWorkflowRetry(rest, stdout, stderr, socketOverride...)
	case "complete":
		return runWorkflowComplete(rest, stdout, stderr, socketOverride...)
	case "fail":
		return runWorkflowFail(rest, stdout, stderr, socketOverride...)
	case "cancel":
		return runWorkflowCancel(rest, stdout, stderr, socketOverride...)
	case "wait-approval":
		return runWorkflowWaitApproval(rest, stdout, stderr, socketOverride...)
	case "resume":
		return runWorkflowResume(rest, stdout, stderr, socketOverride...)
	case "deny":
		return runWorkflowDeny(rest, stdout, stderr, socketOverride...)
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

func runWorkflowTemplates(args []string, stdout, stderr io.Writer, socketOverride ...string) int {
	fs := flag.NewFlagSet("workflow templates", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print raw template list as JSON")
	socket := fs.String("socket", "", "UDS path (default: ${state}/sock)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	applyWorkflowSocket(socket, socketOverride)

	result, rc := callWorkflowRPC("workflow templates", "workflow.templates", nil, stderr, *socket)
	if rc != 0 {
		return rc
	}
	if *asJSON {
		return printWorkflowJSON(stdout, stderr, "workflow templates", result)
	}
	renderWorkflowTemplates(stdout, result)
	return 0
}

func runWorkflowCreate(args []string, stdout, stderr io.Writer, socketOverride ...string) int {
	fs := flag.NewFlagSet("workflow create", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print raw created workflow result as JSON")
	socket := fs.String("socket", "", "UDS path (default: ${state}/sock)")
	args, jsonAfterArgs := pluckWorkflowJSONFlag(args)
	var id, goal string
	args, id = pluckWorkflowFlagValue(args, "--id")
	args, goal = pluckWorkflowFlagValue(args, "--goal")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if jsonAfterArgs {
		*asJSON = true
	}
	if fs.NArg() != 1 || strings.TrimSpace(fs.Arg(0)) == "" {
		fmt.Fprintln(stderr, "workflow create requires <template>")
		return 2
	}
	if strings.TrimSpace(id) == "" {
		fmt.Fprintln(stderr, "workflow create requires --id")
		return 2
	}
	applyWorkflowSocket(socket, socketOverride)

	params := map[string]any{
		"template": strings.TrimSpace(fs.Arg(0)),
		"id":       strings.TrimSpace(id),
	}
	if strings.TrimSpace(goal) != "" {
		params["goal"] = strings.TrimSpace(goal)
	}
	result, rc := callWorkflowRPC("workflow create", "workflow.create", params, stderr, *socket)
	if rc != 0 {
		return rc
	}
	if *asJSON {
		return printWorkflowJSON(stdout, stderr, "workflow create", result)
	}
	renderWorkflowCreate(stdout, result)
	return 0
}

func runWorkflowReady(args []string, stdout, stderr io.Writer, socketOverride ...string) int {
	fs := flag.NewFlagSet("workflow ready", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print raw ready-node result as JSON")
	socket := fs.String("socket", "", "UDS path (default: ${state}/sock)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 || strings.TrimSpace(fs.Arg(0)) == "" {
		fmt.Fprintln(stderr, "workflow ready requires <id>")
		return 2
	}
	applyWorkflowSocket(socket, socketOverride)

	result, rc := callWorkflowRPC("workflow ready", "workflow.ready", map[string]any{"id": strings.TrimSpace(fs.Arg(0))}, stderr, *socket)
	if rc != 0 {
		return rc
	}
	if *asJSON {
		return printWorkflowJSON(stdout, stderr, "workflow ready", result)
	}
	renderWorkflowReady(stdout, result)
	return 0
}

func runWorkflowStart(args []string, stdout, stderr io.Writer, socketOverride ...string) int {
	fs := flag.NewFlagSet("workflow start", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print raw started-node result as JSON")
	socket := fs.String("socket", "", "UDS path (default: ${state}/sock)")
	args, jsonAfterArgs := pluckWorkflowJSONFlag(args)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if jsonAfterArgs {
		*asJSON = true
	}
	if fs.NArg() != 2 || strings.TrimSpace(fs.Arg(0)) == "" || strings.TrimSpace(fs.Arg(1)) == "" {
		fmt.Fprintln(stderr, "workflow start requires <workflow-id> <node-id>")
		return 2
	}
	applyWorkflowSocket(socket, socketOverride)

	result, rc := callWorkflowRPC("workflow start", "workflow.node.start", map[string]any{
		"id":      strings.TrimSpace(fs.Arg(0)),
		"node_id": strings.TrimSpace(fs.Arg(1)),
	}, stderr, *socket)
	if rc != 0 {
		return rc
	}
	if *asJSON {
		return printWorkflowJSON(stdout, stderr, "workflow start", result)
	}
	renderWorkflowStart(stdout, result)
	return 0
}

func runWorkflowDelegate(args []string, stdout, stderr io.Writer, socketOverride ...string) int {
	fs := flag.NewFlagSet("workflow delegate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print raw delegated-node result as JSON")
	socket := fs.String("socket", "", "UDS path (default: ${state}/sock)")
	args, jsonAfterArgs := pluckWorkflowJSONFlag(args)
	var agent, dir, prompt string
	args, agent = pluckWorkflowFlagValue(args, "--agent")
	args, dir = pluckWorkflowFlagValue(args, "--dir")
	args, prompt = pluckWorkflowFlagValue(args, "--prompt")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if jsonAfterArgs {
		*asJSON = true
	}
	if fs.NArg() != 2 || strings.TrimSpace(fs.Arg(0)) == "" || strings.TrimSpace(fs.Arg(1)) == "" {
		fmt.Fprintln(stderr, "workflow delegate requires <workflow-id> <node-id>")
		return 2
	}
	applyWorkflowSocket(socket, socketOverride)

	params := map[string]any{
		"id":      strings.TrimSpace(fs.Arg(0)),
		"node_id": strings.TrimSpace(fs.Arg(1)),
	}
	if strings.TrimSpace(agent) != "" {
		params["agent"] = strings.TrimSpace(agent)
	}
	if strings.TrimSpace(dir) != "" {
		params["dir"] = strings.TrimSpace(dir)
	}
	if strings.TrimSpace(prompt) != "" {
		params["prompt"] = strings.TrimSpace(prompt)
	}
	result, rc := callWorkflowRPC("workflow delegate", "workflow.node.delegate", params, stderr, *socket)
	if rc != 0 {
		return rc
	}
	if *asJSON {
		return printWorkflowJSON(stdout, stderr, "workflow delegate", result)
	}
	renderWorkflowDelegate(stdout, result)
	return 0
}

func runWorkflowRetry(args []string, stdout, stderr io.Writer, socketOverride ...string) int {
	fs := flag.NewFlagSet("workflow retry", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print raw retried-node result as JSON")
	socket := fs.String("socket", "", "UDS path (default: ${state}/sock)")
	args, jsonAfterArgs := pluckWorkflowJSONFlag(args)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if jsonAfterArgs {
		*asJSON = true
	}
	if fs.NArg() != 2 || strings.TrimSpace(fs.Arg(0)) == "" || strings.TrimSpace(fs.Arg(1)) == "" {
		fmt.Fprintln(stderr, "workflow retry requires <workflow-id> <node-id>")
		return 2
	}
	applyWorkflowSocket(socket, socketOverride)

	result, rc := callWorkflowRPC("workflow retry", "workflow.node.retry", map[string]any{
		"id":      strings.TrimSpace(fs.Arg(0)),
		"node_id": strings.TrimSpace(fs.Arg(1)),
	}, stderr, *socket)
	if rc != 0 {
		return rc
	}
	if *asJSON {
		return printWorkflowJSON(stdout, stderr, "workflow retry", result)
	}
	renderWorkflowRetry(stdout, result)
	return 0
}

func runWorkflowComplete(args []string, stdout, stderr io.Writer, socketOverride ...string) int {
	fs := flag.NewFlagSet("workflow complete", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print raw completed-node result as JSON")
	socket := fs.String("socket", "", "UDS path (default: ${state}/sock)")
	args, jsonAfterArgs := pluckWorkflowJSONFlag(args)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if jsonAfterArgs {
		*asJSON = true
	}
	if fs.NArg() != 2 || strings.TrimSpace(fs.Arg(0)) == "" || strings.TrimSpace(fs.Arg(1)) == "" {
		fmt.Fprintln(stderr, "workflow complete requires <workflow-id> <node-id>")
		return 2
	}
	applyWorkflowSocket(socket, socketOverride)

	result, rc := callWorkflowRPC("workflow complete", "workflow.node.complete", map[string]any{
		"id":      strings.TrimSpace(fs.Arg(0)),
		"node_id": strings.TrimSpace(fs.Arg(1)),
	}, stderr, *socket)
	if rc != 0 {
		return rc
	}
	if *asJSON {
		return printWorkflowJSON(stdout, stderr, "workflow complete", result)
	}
	renderWorkflowComplete(stdout, result)
	return 0
}

func runWorkflowFail(args []string, stdout, stderr io.Writer, socketOverride ...string) int {
	fs := flag.NewFlagSet("workflow fail", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print raw failed-node result as JSON")
	socket := fs.String("socket", "", "UDS path (default: ${state}/sock)")
	args, jsonAfterArgs := pluckWorkflowJSONFlag(args)
	var message string
	args, message = pluckWorkflowFlagValue(args, "--error")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if jsonAfterArgs {
		*asJSON = true
	}
	if fs.NArg() != 2 || strings.TrimSpace(fs.Arg(0)) == "" || strings.TrimSpace(fs.Arg(1)) == "" {
		fmt.Fprintln(stderr, "workflow fail requires <workflow-id> <node-id>")
		return 2
	}
	if strings.TrimSpace(message) == "" {
		fmt.Fprintln(stderr, "workflow fail requires --error")
		return 2
	}
	applyWorkflowSocket(socket, socketOverride)

	result, rc := callWorkflowRPC("workflow fail", "workflow.node.fail", map[string]any{
		"id":      strings.TrimSpace(fs.Arg(0)),
		"node_id": strings.TrimSpace(fs.Arg(1)),
		"error":   strings.TrimSpace(message),
	}, stderr, *socket)
	if rc != 0 {
		return rc
	}
	if *asJSON {
		return printWorkflowJSON(stdout, stderr, "workflow fail", result)
	}
	renderWorkflowFail(stdout, result)
	return 0
}

func runWorkflowCancel(args []string, stdout, stderr io.Writer, socketOverride ...string) int {
	fs := flag.NewFlagSet("workflow cancel", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print raw canceled-workflow result as JSON")
	socket := fs.String("socket", "", "UDS path (default: ${state}/sock)")
	args, jsonAfterArgs := pluckWorkflowJSONFlag(args)
	var reason string
	args, reason = pluckWorkflowFlagValue(args, "--reason")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if jsonAfterArgs {
		*asJSON = true
	}
	if fs.NArg() != 1 || strings.TrimSpace(fs.Arg(0)) == "" {
		fmt.Fprintln(stderr, "workflow cancel requires <workflow-id>")
		return 2
	}
	applyWorkflowSocket(socket, socketOverride)

	params := map[string]any{"id": strings.TrimSpace(fs.Arg(0))}
	if strings.TrimSpace(reason) != "" {
		params["reason"] = strings.TrimSpace(reason)
	}
	result, rc := callWorkflowRPC("workflow cancel", "workflow.cancel", params, stderr, *socket)
	if rc != 0 {
		return rc
	}
	if *asJSON {
		return printWorkflowJSON(stdout, stderr, "workflow cancel", result)
	}
	renderWorkflowCancel(stdout, result)
	return 0
}

func runWorkflowWaitApproval(args []string, stdout, stderr io.Writer, socketOverride ...string) int {
	fs := flag.NewFlagSet("workflow wait-approval", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print raw result as JSON")
	socket := fs.String("socket", "", "UDS path (default: ${state}/sock)")
	args, jsonAfterArgs := pluckWorkflowJSONFlag(args)
	var reason string
	args, reason = pluckWorkflowFlagValue(args, "--reason")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if jsonAfterArgs {
		*asJSON = true
	}
	if fs.NArg() != 2 || strings.TrimSpace(fs.Arg(0)) == "" || strings.TrimSpace(fs.Arg(1)) == "" {
		fmt.Fprintln(stderr, "workflow wait-approval requires <workflow-id> <node-id>")
		return 2
	}
	applyWorkflowSocket(socket, socketOverride)

	params := map[string]any{
		"id":      strings.TrimSpace(fs.Arg(0)),
		"node_id": strings.TrimSpace(fs.Arg(1)),
	}
	if strings.TrimSpace(reason) != "" {
		params["reason"] = strings.TrimSpace(reason)
	}
	result, rc := callWorkflowRPC("workflow wait-approval", "workflow.node.wait_approval", params, stderr, *socket)
	if rc != 0 {
		return rc
	}
	if *asJSON {
		return printWorkflowJSON(stdout, stderr, "workflow wait-approval", result)
	}
	renderWorkflowWaitApproval(stdout, result)
	return 0
}

func runWorkflowResume(args []string, stdout, stderr io.Writer, socketOverride ...string) int {
	fs := flag.NewFlagSet("workflow resume", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print raw result as JSON")
	socket := fs.String("socket", "", "UDS path (default: ${state}/sock)")
	args, jsonAfterArgs := pluckWorkflowJSONFlag(args)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if jsonAfterArgs {
		*asJSON = true
	}
	if fs.NArg() != 2 || strings.TrimSpace(fs.Arg(0)) == "" || strings.TrimSpace(fs.Arg(1)) == "" {
		fmt.Fprintln(stderr, "workflow resume requires <workflow-id> <node-id>")
		return 2
	}
	applyWorkflowSocket(socket, socketOverride)

	result, rc := callWorkflowRPC("workflow resume", "workflow.node.resume", map[string]any{
		"id":      strings.TrimSpace(fs.Arg(0)),
		"node_id": strings.TrimSpace(fs.Arg(1)),
	}, stderr, *socket)
	if rc != 0 {
		return rc
	}
	if *asJSON {
		return printWorkflowJSON(stdout, stderr, "workflow resume", result)
	}
	renderWorkflowResume(stdout, result)
	return 0
}

func runWorkflowDeny(args []string, stdout, stderr io.Writer, socketOverride ...string) int {
	fs := flag.NewFlagSet("workflow deny", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print raw result as JSON")
	socket := fs.String("socket", "", "UDS path (default: ${state}/sock)")
	args, jsonAfterArgs := pluckWorkflowJSONFlag(args)
	var reason string
	args, reason = pluckWorkflowFlagValue(args, "--reason")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if jsonAfterArgs {
		*asJSON = true
	}
	if fs.NArg() != 2 || strings.TrimSpace(fs.Arg(0)) == "" || strings.TrimSpace(fs.Arg(1)) == "" {
		fmt.Fprintln(stderr, "workflow deny requires <workflow-id> <node-id>")
		return 2
	}
	if strings.TrimSpace(reason) == "" {
		fmt.Fprintln(stderr, "workflow deny requires --reason")
		return 2
	}
	applyWorkflowSocket(socket, socketOverride)

	result, rc := callWorkflowRPC("workflow deny", "workflow.node.deny", map[string]any{
		"id":      strings.TrimSpace(fs.Arg(0)),
		"node_id": strings.TrimSpace(fs.Arg(1)),
		"reason":  strings.TrimSpace(reason),
	}, stderr, *socket)
	if rc != 0 {
		return rc
	}
	if *asJSON {
		return printWorkflowJSON(stdout, stderr, "workflow deny", result)
	}
	renderWorkflowDeny(stdout, result)
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

func runWorkflowExport(args []string, stdout, stderr io.Writer, socketOverride ...string) int {
	fs := flag.NewFlagSet("workflow export", flag.ContinueOnError)
	fs.SetOutput(stderr)
	output := fs.String("output", "", "write workflow JSON to path instead of stdout")
	socket := fs.String("socket", "", "UDS path (default: ${state}/sock)")
	var outputPath string
	args, outputPath = pluckWorkflowFlagValue(args, "--output")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(outputPath) != "" {
		*output = strings.TrimSpace(outputPath)
	}
	if fs.NArg() != 1 || strings.TrimSpace(fs.Arg(0)) == "" {
		fmt.Fprintln(stderr, "workflow export requires <id>")
		return 2
	}
	applyWorkflowSocket(socket, socketOverride)

	result, rc := callWorkflowRPC("workflow export", "workflow.export", map[string]any{"id": strings.TrimSpace(fs.Arg(0))}, stderr, *socket)
	if rc != 0 {
		return rc
	}
	out, err := json.MarshalIndent(mapField(result, "workflow"), "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "milliwaysctl workflow export: encode workflow: %v\n", err)
		return 1
	}
	if strings.TrimSpace(*output) != "" {
		if err := os.WriteFile(*output, append(out, '\n'), 0o600); err != nil {
			fmt.Fprintf(stderr, "milliwaysctl workflow export: write %s: %v\n", *output, err)
			return 1
		}
		fmt.Fprintf(stdout, "exported %s to %s\n", firstString(mapField(result, "workflow"), "id"), *output)
		return 0
	}
	fmt.Fprintln(stdout, string(out))
	return 0
}

func runWorkflowImport(args []string, stdout, stderr io.Writer, socketOverride ...string) int {
	fs := flag.NewFlagSet("workflow import", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print raw imported workflow result as JSON")
	socket := fs.String("socket", "", "UDS path (default: ${state}/sock)")
	args, jsonAfterArgs := pluckWorkflowJSONFlag(args)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if jsonAfterArgs {
		*asJSON = true
	}
	if fs.NArg() != 1 || strings.TrimSpace(fs.Arg(0)) == "" {
		fmt.Fprintln(stderr, "workflow import requires <path>")
		return 2
	}
	raw, err := os.ReadFile(strings.TrimSpace(fs.Arg(0)))
	if err != nil {
		fmt.Fprintf(stderr, "milliwaysctl workflow import: read %s: %v\n", strings.TrimSpace(fs.Arg(0)), err)
		return 1
	}
	var workflow map[string]any
	if err := json.Unmarshal(raw, &workflow); err != nil {
		fmt.Fprintf(stderr, "milliwaysctl workflow import: decode %s: %v\n", strings.TrimSpace(fs.Arg(0)), err)
		return 1
	}
	applyWorkflowSocket(socket, socketOverride)

	result, rc := callWorkflowRPC("workflow import", "workflow.import", map[string]any{"workflow": workflow}, stderr, *socket)
	if rc != 0 {
		return rc
	}
	if *asJSON {
		return printWorkflowJSON(stdout, stderr, "workflow import", result)
	}
	wf := mapField(result, "workflow")
	nodes, _ := wf["nodes"].([]any)
	fmt.Fprintf(stdout, "imported %s status=%s nodes=%d\n", firstString(wf, "id"), firstString(wf, "status"), len(nodes))
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

func renderWorkflowTemplates(w io.Writer, result map[string]any) {
	items, _ := result["templates"].([]any)
	if len(items) == 0 {
		fmt.Fprintln(w, "no workflow templates")
		return
	}
	fmt.Fprintln(w, "TEMPLATE                    NODES  DESCRIPTION")
	for _, item := range items {
		row := mapValue(item)
		fmt.Fprintf(w, "%-27s %-6s %s\n",
			firstString(row, "name"),
			numberString(row["nodes"]),
			firstString(row, "description"),
		)
	}
}

func renderWorkflowCreate(w io.Writer, result map[string]any) {
	wf := mapField(result, "workflow")
	nodes, _ := wf["nodes"].([]any)
	fmt.Fprintf(w, "created %s status=%s nodes=%d\n",
		firstString(wf, "id"),
		firstString(wf, "status"),
		len(nodes),
	)
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
		renderWorkflowNodeDetails(w, node, "    ")
	}
}

func renderWorkflowReady(w io.Writer, result map[string]any) {
	nodes, _ := result["nodes"].([]any)
	if len(nodes) == 0 {
		fmt.Fprintln(w, "no ready nodes")
		return
	}
	fmt.Fprintln(w, "READY NODE       TYPE             STATUS           CLIENT")
	for _, item := range nodes {
		node := mapValue(item)
		fmt.Fprintf(w, "%-16s %-16s %-16s %s\n",
			firstString(node, "id"),
			firstString(node, "type"),
			firstString(node, "status"),
			firstString(node, "client"),
		)
		renderWorkflowNodeDetails(w, node, "  ")
	}
}

func renderWorkflowStart(w io.Writer, result map[string]any) {
	node := mapField(result, "node")
	fmt.Fprintf(w, "started %s status=%s type=%s client=%s\n",
		firstString(node, "id"),
		firstString(node, "status"),
		firstString(node, "type"),
		firstString(node, "client"),
	)
}

func renderWorkflowDelegate(w io.Writer, result map[string]any) {
	node := mapField(result, "node")
	fmt.Fprintf(w, "delegated %s status=%s type=%s client=%s\n",
		firstString(node, "id"),
		firstString(node, "status"),
		firstString(node, "type"),
		firstString(node, "client"),
	)
}

func renderWorkflowRetry(w io.Writer, result map[string]any) {
	node := mapField(result, "node")
	fmt.Fprintf(w, "retried %s status=%s retry=%s\n",
		firstString(node, "id"),
		firstString(node, "status"),
		workflowRetryString(node),
	)
}

func renderWorkflowNodeDetails(w io.Writer, node map[string]any, indent string) {
	if node == nil {
		return
	}
	if retry := workflowRetryString(node); retry != "0" {
		fmt.Fprintf(w, "%sretry: %s\n", indent, retry)
	}
	if priority := numberString(node["priority"]); priority != "0" {
		fmt.Fprintf(w, "%spriority: %s\n", indent, priority)
	}
	if err := firstString(node, "error"); err != "unknown" {
		fmt.Fprintf(w, "%serror: %s\n", indent, err)
	}
	if security := mapField(node, "security"); len(security) > 0 {
		fmt.Fprintf(w, "%ssecurity: operation=%s approval=%s risk=%s reason=%s paths=%s\n",
			indent,
			firstString(security, "operation"),
			firstString(security, "approval"),
			firstString(security, "risk"),
			firstString(security, "reason"),
			workflowStringList(security["paths"]),
		)
	}
	if memory := mapField(node, "memory"); len(memory) > 0 {
		fmt.Fprintf(w, "%smemory: reads=%s writes=%s\n",
			indent,
			workflowStringList(memory["reads"]),
			workflowStringList(memory["writes"]),
		)
	}
	if artifacts, _ := node["artifacts"].([]any); len(artifacts) > 0 {
		fmt.Fprintf(w, "%sartifacts:", indent)
		for _, item := range artifacts {
			artifact := mapValue(item)
			fmt.Fprintf(w, " %s", firstString(artifact, "kind"))
			if path := firstString(artifact, "path"); path != "unknown" {
				fmt.Fprintf(w, ":%s", path)
			} else if ref := firstString(artifact, "ref"); ref != "unknown" {
				fmt.Fprintf(w, ":%s", ref)
			}
		}
		fmt.Fprintln(w)
	}
}

func renderWorkflowComplete(w io.Writer, result map[string]any) {
	node := mapField(result, "node")
	ready, _ := result["ready_nodes"].([]any)
	next := "none"
	if len(ready) > 0 {
		next = firstString(mapValue(ready[0]), "id")
	}
	fmt.Fprintf(w, "completed %s status=%s type=%s client=%s next=%s\n",
		firstString(node, "id"),
		firstString(node, "status"),
		firstString(node, "type"),
		firstString(node, "client"),
		next,
	)
}

func renderWorkflowFail(w io.Writer, result map[string]any) {
	node := mapField(result, "node")
	fmt.Fprintf(w, "failed %s status=%s type=%s client=%s error=%s\n",
		firstString(node, "id"),
		firstString(node, "status"),
		firstString(node, "type"),
		firstString(node, "client"),
		firstString(node, "error"),
	)
}

func renderWorkflowCancel(w io.Writer, result map[string]any) {
	wf := mapField(result, "workflow")
	nodes, _ := wf["nodes"].([]any)
	fmt.Fprintf(w, "canceled %s status=%s nodes=%d\n",
		firstString(wf, "id"),
		firstString(wf, "status"),
		len(nodes),
	)
}

func renderWorkflowWaitApproval(w io.Writer, result map[string]any) {
	node := mapField(result, "node")
	fmt.Fprintf(w, "waiting_approval %s status=%s reason=%s\n",
		firstString(node, "id"),
		firstString(node, "status"),
		firstString(node, "error"),
	)
}

func renderWorkflowResume(w io.Writer, result map[string]any) {
	node := mapField(result, "node")
	fmt.Fprintf(w, "resumed %s status=%s\n",
		firstString(node, "id"),
		firstString(node, "status"),
	)
}

func renderWorkflowDeny(w io.Writer, result map[string]any) {
	node := mapField(result, "node")
	fmt.Fprintf(w, "denied %s status=%s reason=%s\n",
		firstString(node, "id"),
		firstString(node, "status"),
		firstString(node, "error"),
	)
}

func pluckWorkflowJSONFlag(args []string) ([]string, bool) {
	var filtered []string
	found := false
	for _, arg := range args {
		if arg == "--json" {
			found = true
			continue
		}
		filtered = append(filtered, arg)
	}
	return filtered, found
}

func pluckWorkflowFlagValue(args []string, name string) ([]string, string) {
	filtered := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == name && i+1 < len(args) {
			i++
			return append(filtered, args[i+1:]...), args[i]
		}
		filtered = append(filtered, arg)
	}
	return filtered, ""
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

func workflowRetryString(node map[string]any) string {
	if node == nil {
		return "0"
	}
	if _, ok := node["retry_count"]; ok {
		return numberString(node["retry_count"])
	}
	return numberString(node["retry"])
}

func workflowStringList(value any) string {
	items, ok := value.([]any)
	if !ok || len(items) == 0 {
		return "none"
	}
	parts := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
			parts = append(parts, s)
		}
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, ",")
}

func printWorkflowUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: milliwaysctl workflow <list|templates|create|show|export|import|ready|start|delegate|retry|complete|fail|cancel|wait-approval|resume|deny> [--json]")
	fmt.Fprintln(w, "  list [--json]                           list stored workflow graphs")
	fmt.Fprintln(w, "  templates [--json]                      list built-in workflow templates")
	fmt.Fprintln(w, "  create <template> --id <id> [--goal <text>] [--json] create workflow from template")
	fmt.Fprintln(w, "  show <id> [--json]                      show one stored workflow graph")
	fmt.Fprintln(w, "  export <id> [--output <path>]           export one workflow graph as JSON")
	fmt.Fprintln(w, "  import <path> [--json]                  import one workflow graph from JSON")
	fmt.Fprintln(w, "  ready <id> [--json]                     show queued nodes with completed dependencies")
	fmt.Fprintln(w, "  start <workflow-id> <node-id> [--json]  start a ready node")
	fmt.Fprintln(w, "  delegate <workflow-id> <node-id> [--agent <id>] [--dir <path>] [--prompt <text>] [--json] start delegate node")
	fmt.Fprintln(w, "  retry <workflow-id> <node-id> [--json]  retry a failed node")
	fmt.Fprintln(w, "  complete <workflow-id> <node-id> [--json] complete a running node")
	fmt.Fprintln(w, "  fail <workflow-id> <node-id> --error <message> [--json] fail a running node")
	fmt.Fprintln(w, "  cancel <workflow-id> [--reason <message>] [--json] cancel a workflow")
	fmt.Fprintln(w, "  wait-approval <workflow-id> <node-id> [--reason <reason>] [--json] pause for approval")
	fmt.Fprintln(w, "  resume <workflow-id> <node-id> [--json] resume a waiting node")
	fmt.Fprintln(w, "  deny <workflow-id> <node-id> --reason <reason> [--json] deny approval")
}
