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

package parallel

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// Launch opens the WezTerm parallel panel layout for result, or falls back to
// a headless summary when WezTerm is not available. It is a thin wrapper
// around launchWithWriter for testability.
func Launch(result DispatchResult, groupID string) error {
	return launchWithWriter(os.Stdout, result, groupID)
}

// launchWithWriter is the testable core of Launch. It writes output to w
// instead of os.Stdout so unit tests can capture the headless summary without
// forking a process.
func launchWithWriter(w io.Writer, result DispatchResult, groupID string) error {
	if isWezTerm() {
		return launchWezTerm(result, groupID)
	}
	return printHeadlessFallback(w, result)
}

// isWezTerm returns true when the process is running inside WezTerm AND the
// wezterm CLI is on PATH — both conditions must hold to attempt pane splits.
func isWezTerm() bool {
	if os.Getenv("TERM_PROGRAM") != "WezTerm" {
		return false
	}
	_, err := exec.LookPath("wezterm")
	return err == nil
}

// launchWezTerm opens the navigator pane and one pane per slot using
// `wezterm cli split-pane`.
func launchWezTerm(result DispatchResult, groupID string) error {
	// Navigator pane: 30% width, runs `milliways attach --nav <groupID>`
	navArgs := []string{"cli", "split-pane", "--percent", "30", "--",
		"milliways", "attach", "--nav", groupID}
	if out, err := exec.Command("wezterm", navArgs...).CombinedOutput(); err != nil {
		return fmt.Errorf("wezterm split-pane (nav): %w\n%s", err, out)
	}

	// One pane per slot.
	for _, slot := range result.Slots {
		slotArgs := []string{"cli", "split-pane", "--",
			"milliways", "attach", fmt.Sprintf("%d", slot.Handle)}
		if out, err := exec.Command("wezterm", slotArgs...).CombinedOutput(); err != nil {
			return fmt.Errorf("wezterm split-pane (slot %d): %w\n%s", slot.SlotN, err, out)
		}
	}
	return nil
}

// printHeadlessFallback writes a human-readable slot summary to w.
func printHeadlessFallback(w io.Writer, result DispatchResult) error {
	fmt.Fprintln(w, "[parallel] WezTerm not detected — running headless.")
	for _, slot := range result.Slots {
		fmt.Fprintf(w, "  slot %d: %s  (milliways attach %d)\n",
			slot.SlotN, slot.Provider, slot.Handle)
	}
	return nil
}

// RenderHeader produces a single-line status bar string for the parallel panel.
// It is designed to be embedded in a terminal title or status overlay.
//
// Format (wide):
//
//	claude 12.4k tok  34% quota ●  |  codex 8.1k tok  12% quota ●  |  total: 23.7k tok
//
// Format (narrow — termWidth < 60 or termWidth/len(slots) < 20):
//
//	parallel group · N running · total Xk tok
func RenderHeader(slots []SlotRecord, quotas map[string]QuotaSummary, totalTokens int, termWidth int) string {
	if len(slots) == 0 {
		return ""
	}

	// Narrow collapse decision.
	narrow := termWidth < 60
	if !narrow && len(slots) > 0 {
		narrow = termWidth/len(slots) < 20
	}

	if narrow {
		running := 0
		for _, s := range slots {
			if s.Status == "running" {
				running++
			}
		}
		return fmt.Sprintf("parallel group · %d running · total %s tok", running, formatTokens(totalTokens))
	}

	// Wide format: one column per slot joined by " | ".
	const reset = "\033[0m"
	const green = "\033[32m"
	const yellow = "\033[33m"
	const red = "\033[31m"

	cols := make([]string, 0, len(slots)+1)
	for _, slot := range slots {
		// Status bullet
		bullet := yellow + "●" + reset // default: done/idle
		switch slot.Status {
		case "running":
			bullet = green + "●" + reset
		case "error":
			bullet = red + "●" + reset
		}

		// Token count
		tok := formatTokens(slot.TokensOut) + " tok"

		// Quota
		quotaStr := "— quota"
		if quotas != nil {
			if q, ok := quotas[slot.Provider]; ok {
				pct := q.UsedPct()
				var pctColor string
				switch {
				case pct > 95:
					pctColor = red
				case pct > 80:
					pctColor = yellow
				default:
					pctColor = ""
				}
				if pctColor != "" {
					quotaStr = fmt.Sprintf("%s%.0f%%%s quota", pctColor, pct, reset)
				} else {
					quotaStr = fmt.Sprintf("%.0f%% quota", pct)
				}
			}
		}

		cols = append(cols, fmt.Sprintf("%s %s  %s %s", slot.Provider, tok, quotaStr, bullet))
	}

	// Append total.
	cols = append(cols, fmt.Sprintf("total: %s tok", formatTokens(totalTokens)))

	return strings.Join(cols, "  |  ")
}

// formatTokens formats a token count as a compact string:
// - under 1000: rendered as the plain integer (e.g. "999")
// - 1000+: rendered as "X.Yk" rounded to one decimal place (e.g. "1.2k", "12.4k")
func formatTokens(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	k := float64(n) / 1000.0
	return fmt.Sprintf("%.1fk", k)
}
