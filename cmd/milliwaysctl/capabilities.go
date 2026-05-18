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

//nolint:errcheck // CLI table output writes are best-effort; RPC/encoding failures are handled explicitly.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/mwigge/milliways/internal/rpc"
)

func runCapabilities(args []string, stdout, stderr io.Writer, socketOverride ...string) int {
	fs := flag.NewFlagSet("capabilities", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print raw capability metadata as JSON")
	socket := fs.String("socket", "", "UDS path (default: ${state}/sock)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if len(socketOverride) > 0 && socketOverride[0] != "" {
		*socket = socketOverride[0]
	}
	if strings.TrimSpace(*socket) == "" {
		*socket = defaultSocket()
	}

	result, rc := callCapabilitiesRPC(stderr, *socket)
	if rc != 0 {
		return rc
	}
	if *asJSON {
		out, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "milliwaysctl capabilities: encode result: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, string(out))
		return 0
	}
	renderCapabilities(stdout, result)
	return 0
}

func callCapabilitiesRPC(stderr io.Writer, sock string) (map[string]any, int) {
	c, err := rpc.Dial(sock)
	if err != nil {
		fmt.Fprintf(stderr, "milliwaysctl capabilities: dial %s: %v\n", sock, err)
		return nil, 1
	}
	defer func() { _ = c.Close() }()

	var result map[string]any
	if err := c.Call("capabilities.get", nil, &result); err != nil {
		fmt.Fprintf(stderr, "milliwaysctl capabilities: %v\n", err)
		return nil, 1
	}
	return result, 0
}

func renderCapabilities(w io.Writer, result map[string]any) {
	clients := mapField(result, "clients")
	if len(clients) == 0 {
		fmt.Fprintln(w, "client capability matrix: no clients reported")
		return
	}

	names := make([]string, 0, len(clients))
	for name := range clients {
		names = append(names, name)
	}
	sort.Strings(names)

	fmt.Fprintln(w, "client   enforcement     read              write             edit              delete            bash              glob              grep              approvals         memory            observability")
	for _, name := range names {
		meta := mapValue(clients[name])
		caps := mapField(meta, "capabilities")
		contract := mapField(caps, "contract")
		fmt.Fprintf(w, "%-8s %-15s %-17s %-17s %-17s %-17s %-17s %-17s %-17s %-17s %-17s %-17s\n",
			name,
			firstString(meta, "level"),
			firstString(contract, "read"),
			firstString(contract, "write"),
			firstString(contract, "edit"),
			firstString(contract, "delete"),
			firstString(contract, "bash"),
			firstString(contract, "glob"),
			firstString(contract, "grep"),
			firstString(contract, "approvals"),
			firstString(caps, "memory"),
			firstString(caps, "observability"),
		)
	}
}

func mapField(values map[string]any, key string) map[string]any {
	if values == nil {
		return nil
	}
	return mapValue(values[key])
}

func mapValue(value any) map[string]any {
	if values, ok := value.(map[string]any); ok {
		return values
	}
	return nil
}

func firstString(values map[string]any, key string) string {
	if values == nil {
		return "unknown"
	}
	if s, ok := values[key].(string); ok && strings.TrimSpace(s) != "" {
		return s
	}
	return "unknown"
}
