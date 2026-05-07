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
	"time"

	"github.com/mwigge/milliways/internal/parallel"
	"github.com/mwigge/milliways/internal/rpc"
)

type parallelCommandOptions struct {
	Providers []string
	Prompt    string
	Watch     bool
}

// parseParallelArgs extracts an optional --providers <list> flag and the
// remaining prompt text from the rest of a /parallel command line.
//
// Format: [--providers p1,p2,...] <prompt text>
//
// Returns (providers, prompt). providers is nil if the flag is absent.
func parseParallelArgs(rest string) (providers []string, prompt string) {
	opts := parseParallelCommand(rest)
	return opts.Providers, opts.Prompt
}

func parseParallelCommand(rest string) parallelCommandOptions {
	var opts parallelCommandOptions
	rest = strings.TrimSpace(rest)
	for rest != "" {
		field, tail := splitLeadingField(rest)
		switch field {
		case "--watch", "-w":
			opts.Watch = true
			rest = strings.TrimSpace(tail)
		case "--providers":
			rawList, next := splitLeadingField(tail)
			opts.Providers = parseProviderList(rawList)
			rest = strings.TrimSpace(next)
		default:
			opts.Prompt = rest
			return opts
		}
	}
	return opts
}

func splitLeadingField(s string) (field, rest string) {
	s = strings.TrimLeft(s, " \t")
	idx := strings.IndexAny(s, " \t")
	if idx < 0 {
		return s, ""
	}
	return s[:idx], s[idx+1:]
}

func parseProviderList(rawList string) []string {
	var providers []string
	for _, p := range strings.Split(rawList, ",") {
		if p = strings.TrimSpace(p); p != "" {
			providers = append(providers, p)
		}
	}
	return providers
}

// handleParallel is the /parallel slash-command handler.
//
// Usage: /parallel [--watch] [--providers p1,p2,...] <prompt>
//
// Steps:
//  1. Parse --watch and --providers flags.
//  2. If providers list is empty, use agent.list to discover available providers.
//  3. Validate prompt is non-empty.
//  4. Call parallel.dispatch RPC.
//  5. Print result or stub note if layout is not yet available.
//  6. With --watch, render the live grouped comparison in the chat REPL.
func (l *chatLoop) handleParallel(rest string) {
	opts := parseParallelCommand(rest)
	providers, prompt := opts.Providers, opts.Prompt

	if prompt == "" {
		fmt.Fprintln(l.errw, "usage: /parallel [--watch] [--providers p1,p2,...] <prompt>")
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
		fmt.Fprintln(l.errw, friendlyError("[parallel] dispatch: ", "", err))
		return
	}

	if len(result.Skipped) > 0 {
		fmt.Fprintf(l.out, "[parallel] skipped %d provider(s):\n", len(result.Skipped))
		for _, sk := range result.Skipped {
			fmt.Fprintf(l.out, "  %s: %s\n", sk.Provider, sk.Reason)
		}
	}

	// Build parallel.DispatchResult for the layout launcher.
	dispatchResult := parallel.DispatchResult{GroupID: result.GroupID}
	for i, s := range result.Slots {
		dispatchResult.Slots = append(dispatchResult.Slots, parallel.SlotRecord{
			SlotN:    i + 1,
			Handle:   s.Handle,
			Provider: s.Provider,
			Status:   parallel.SlotRunning,
		})
	}
	if l.deck != nil {
		l.deck.MarkParallelDispatch(providers, prompt)
		summaries := make([]parallelSlotSummary, 0, len(result.Slots))
		for _, s := range result.Slots {
			summaries = append(summaries, parallelSlotSummary{Provider: s.Provider, Handle: s.Handle})
		}
		l.deck.MarkParallelSlots(summaries)
	}

	// Launch split-pane layout. In non-WezTerm environments this prints a
	// headless fallback with per-slot attach hints.
	if err := parallel.Launch(dispatchResult, result.GroupID); err != nil {
		fmt.Fprintln(l.errw, friendlyError("[parallel] layout: ", "", err))
		for _, s := range result.Slots {
			fmt.Fprintf(l.out, "  milliways attach %d  (%s)\n", s.Handle, s.Provider)
		}
	}
	fmt.Fprintf(l.out, "[parallel] group %s — /parallel-view --watch %s for live comparison\n", result.GroupID, result.GroupID)
	if opts.Watch {
		fmt.Fprintf(l.out, "[parallel] watching %s — Ctrl+C to stop\n", result.GroupID)
		l.handleParallelView("--watch " + result.GroupID)
	}
}

func parseParallelViewArgs(rest string) (watch bool, groupID string) {
	fields := strings.Fields(rest)
	for _, field := range fields {
		switch field {
		case "--watch", "-w":
			watch = true
		default:
			if groupID == "" {
				groupID = field
			}
		}
	}
	return watch, groupID
}

func (l *chatLoop) handleParallelView(rest string) {
	watch, groupID := parseParallelViewArgs(rest)
	if groupID == "" {
		fmt.Fprintln(l.errw, "usage: /parallel-view [--watch] <group-id>")
		return
	}
	if l.client == nil {
		fmt.Fprintln(l.errw, "[parallel] not connected to daemon")
		return
	}
	if !watch {
		l.printParallelView(groupID, false)
		return
	}

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	deadline := time.After(2 * time.Minute)
	for {
		done := l.printParallelView(groupID, true)
		if done {
			return
		}
		select {
		case <-deadline:
			fmt.Fprintln(l.errw, "[parallel] watch timed out; run /parallel-view --watch "+groupID+" to continue")
			return
		case <-ticker.C:
		}
	}
}

func (l *chatLoop) printParallelView(groupID string, clear bool) bool {
	status, consensus, err := l.fetchParallelView(groupID)
	if err != nil {
		fmt.Fprintln(l.errw, friendlyError("[parallel] view: ", "", err))
		return true
	}
	if clear {
		fmt.Fprint(l.out, "\033[2J\033[H")
	}
	writeParallelComparison(l.out, status, consensus, 110)
	return parallelGroupDone(status)
}

func (l *chatLoop) fetchParallelView(groupID string) (rpc.GroupStatusResult, string, error) {
	var status rpc.GroupStatusResult
	if err := l.client.Call("group.status", rpc.GroupStatusParams{GroupID: groupID}, &status); err != nil {
		return rpc.GroupStatusResult{}, "", err
	}
	var consensus rpc.ConsensusAggregateResult
	_ = l.client.Call("consensus.aggregate", rpc.ConsensusAggregateParams{GroupID: groupID}, &consensus)
	return status, consensus.Summary, nil
}

func parallelGroupDone(status rpc.GroupStatusResult) bool {
	if len(status.Slots) == 0 {
		return true
	}
	for _, slot := range status.Slots {
		switch slot.Status {
		case "running", "thinking", "streaming":
			return false
		}
	}
	return true
}
