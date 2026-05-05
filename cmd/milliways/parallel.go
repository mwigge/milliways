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

import (
	"fmt"
	"strings"

	"github.com/mwigge/milliways/internal/rpc"
)

// parseParallelArgs extracts an optional --providers <list> flag and the
// remaining prompt text from the rest of a /parallel command line.
//
// Format: [--providers p1,p2,...] <prompt text>
//
// Returns (providers, prompt). providers is nil if the flag is absent.
func parseParallelArgs(rest string) (providers []string, prompt string) {
	rest = strings.TrimSpace(rest)
	if strings.HasPrefix(rest, "--providers ") || strings.HasPrefix(rest, "--providers\t") {
		// Consume the flag name.
		rest = strings.TrimPrefix(rest, "--providers")
		rest = strings.TrimLeft(rest, " \t")

		// Find the end of the providers value (first whitespace after the
		// comma-separated list marks the start of the prompt).
		idx := strings.IndexAny(rest, " \t")
		if idx < 0 {
			// providers flag with no following prompt.
			rawList := rest
			for _, p := range strings.Split(rawList, ",") {
				if p = strings.TrimSpace(p); p != "" {
					providers = append(providers, p)
				}
			}
			return providers, ""
		}
		rawList := rest[:idx]
		for _, p := range strings.Split(rawList, ",") {
			if p = strings.TrimSpace(p); p != "" {
				providers = append(providers, p)
			}
		}
		prompt = strings.TrimSpace(rest[idx+1:])
		return providers, prompt
	}
	return nil, rest
}

// handleParallel is the /parallel slash-command handler.
//
// Usage: /parallel [--providers p1,p2,...] <prompt>
//
// Steps:
//  1. Parse --providers flag (optional).
//  2. If providers list is empty, use agent.list to discover available providers.
//  3. Validate prompt is non-empty.
//  4. Call parallel.dispatch RPC.
//  5. Print result or stub note if layout is not yet available.
func (l *chatLoop) handleParallel(rest string) {
	providers, prompt := parseParallelArgs(rest)

	if prompt == "" {
		fmt.Fprintln(l.errw, "usage: /parallel [--providers p1,p2,...] <prompt>")
		return
	}

	// If no providers specified, discover from agent.list.
	if len(providers) == 0 {
		if l.client != nil {
			var agents []map[string]any
			if err := l.client.Call("agent.list", nil, &agents); err == nil {
				for _, a := range agents {
					id, _ := a["id"].(string)
					avail, _ := a["available"].(bool)
					if id != "" && avail {
						providers = append(providers, id)
					}
				}
			}
		}
		if len(providers) == 0 {
			fmt.Fprintln(l.errw, "[parallel] no providers available; specify with --providers")
			return
		}
	}

	// Call parallel.dispatch.
	if l.client == nil {
		fmt.Fprintln(l.errw, "[parallel] not connected to daemon")
		return
	}

	var result rpc.ParallelDispatchResult
	err := l.client.Call("parallel.dispatch", rpc.ParallelDispatchParams{
		Prompt:    prompt,
		Providers: providers,
	}, &result)
	if err != nil {
		fmt.Fprintln(l.errw, "[parallel] dispatch error: "+err.Error())
		return
	}

	// Print summary. Layout (Agent C) is not yet available; print handles inline.
	fmt.Fprintf(l.out, "[parallel] group %s started — %d slot(s)\n", result.GroupID, len(result.Slots))
	for _, slot := range result.Slots {
		fmt.Fprintf(l.out, "  slot provider=%s handle=%d\n", slot.Provider, slot.Handle)
	}
	if len(result.Skipped) > 0 {
		fmt.Fprintf(l.out, "  [parallel] skipped %d provider(s):\n", len(result.Skipped))
		for _, sk := range result.Skipped {
			fmt.Fprintf(l.out, "    %s: %s\n", sk.Provider, sk.Reason)
		}
	}
	// Note for layout integration (Agent C's worktree).
	fmt.Fprintln(l.out, "  [parallel] layout panel not yet available — use `milliwaysctl parallel status "+result.GroupID+"` to track progress")
}
