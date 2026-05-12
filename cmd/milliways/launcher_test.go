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
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// paneListJSON builds a minimal wezterm cli list JSON payload.
func paneListJSON(panes ...map[string]any) []byte {
	var entries []string
	for _, p := range panes {
		active := "false"
		if a, ok := p["is_active"].(bool); ok && a {
			active = "true"
		}
		entries = append(entries, fmt.Sprintf(
			`{"pane_id":%d,"is_active":%s,"tty_name":%q}`,
			p["pane_id"], active, p["tty_name"],
		))
	}
	return []byte("[" + strings.Join(entries, ",") + "]")
}

func TestDetectWeztermCurrentPaneID_ExactTTYMatch(t *testing.T) {
	j := paneListJSON(
		map[string]any{"pane_id": 0, "is_active": false, "tty_name": "/dev/ttys001"},
		map[string]any{"pane_id": 3, "is_active": true, "tty_name": "/dev/ttys005"},
		map[string]any{"pane_id": 5, "is_active": false, "tty_name": "/dev/ttys009"},
	)
	id, reason := detectWeztermCurrentPaneIDWith(
		func() (string, error) { return "/dev/ttys005", nil },
		func() ([]byte, error) { return j, nil },
	)
	if id != "3" {
		t.Errorf("expected pane 3 (exact TTY match), got %q reason=%q", id, reason)
	}
}

func TestDetectWeztermCurrentPaneID_FallbackToActive(t *testing.T) {
	j := paneListJSON(
		map[string]any{"pane_id": 7, "is_active": false, "tty_name": "/dev/ttys010"},
		map[string]any{"pane_id": 9, "is_active": true, "tty_name": "/dev/ttys011"},
	)
	id, reason := detectWeztermCurrentPaneIDWith(
		func() (string, error) { return "/dev/ttys099", nil },
		func() ([]byte, error) { return j, nil },
	)
	if id != "9" {
		t.Errorf("expected fallback to active pane 9, got %q reason=%q", id, reason)
	}
}

func TestDetectWeztermCurrentPaneID_TtyCommandFails(t *testing.T) {
	id, reason := detectWeztermCurrentPaneIDWith(
		func() (string, error) { return "", fmt.Errorf("not a tty") },
		func() ([]byte, error) { return nil, nil },
	)
	if id != "" {
		t.Errorf("expected empty id, got %q", id)
	}
	if !strings.Contains(reason, "tty") {
		t.Errorf("reason should mention tty failure, got %q", reason)
	}
}

func TestDetectWeztermCurrentPaneID_WeztermListFails(t *testing.T) {
	id, reason := detectWeztermCurrentPaneIDWith(
		func() (string, error) { return "/dev/ttys005", nil },
		func() ([]byte, error) { return nil, fmt.Errorf("connection refused") },
	)
	if id != "" {
		t.Errorf("expected empty id, got %q", id)
	}
	if !strings.Contains(reason, "wezterm list") {
		t.Errorf("reason should mention wezterm list failure, got %q", reason)
	}
}

func TestDetectWeztermCurrentPaneID_MalformedJSON(t *testing.T) {
	id, reason := detectWeztermCurrentPaneIDWith(
		func() (string, error) { return "/dev/ttys005", nil },
		func() ([]byte, error) { return []byte("not json"), nil },
	)
	if id != "" {
		t.Errorf("expected empty id, got %q", id)
	}
	if !strings.Contains(reason, "json") {
		t.Errorf("reason should mention json parse, got %q", reason)
	}
}

func TestDetectWeztermCurrentPaneID_EmptyPaneList(t *testing.T) {
	id, reason := detectWeztermCurrentPaneIDWith(
		func() (string, error) { return "/dev/ttys005", nil },
		func() ([]byte, error) { return []byte("[]"), nil },
	)
	if id != "" {
		t.Errorf("expected empty id for empty pane list, got %q", id)
	}
	if reason == "" {
		t.Error("expected non-empty reason when no pane found")
	}
}

func TestDetectWeztermCurrentPaneID_PaneIDZero(t *testing.T) {
	j := paneListJSON(
		map[string]any{"pane_id": 0, "is_active": true, "tty_name": "/dev/ttys000"},
	)
	id, reason := detectWeztermCurrentPaneIDWith(
		func() (string, error) { return "/dev/ttys000", nil },
		func() ([]byte, error) { return j, nil },
	)
	if id != "0" {
		t.Errorf("expected pane 0, got %q reason=%q", id, reason)
	}
}

func TestEnsureCockpitHintFileWritesDurableHint(t *testing.T) {
	state := t.TempDir()

	if err := ensureCockpitHintFile(state); err != nil {
		t.Fatalf("ensureCockpitHintFile: %v", err)
	}
	got, err := os.ReadFile(cockpitHintPath(state))
	if err != nil {
		t.Fatalf("read cockpit hint: %v", err)
	}
	for _, want := range []string{"Milliways terminal setup", "wezterm.lua", "cockpit-hint.txt"} {
		if !strings.Contains(string(got), want) {
			t.Fatalf("durable hint missing %q:\n%s", want, got)
		}
	}
}

func TestEnsureCockpitHintFileDoesNotOverwrite(t *testing.T) {
	state := t.TempDir()
	path := cockpitHintPath(state)
	if err := os.WriteFile(path, []byte("custom note\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := ensureCockpitHintFile(state); err != nil {
		t.Fatalf("ensureCockpitHintFile: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "custom note\n" {
		t.Fatalf("hint file overwritten: %q", got)
	}
}

func TestCockpitHintPathUsesStateDir(t *testing.T) {
	state := t.TempDir()
	if got, want := cockpitHintPath(state), filepath.Join(state, "cockpit-hint.txt"); got != want {
		t.Fatalf("cockpitHintPath = %q, want %q", got, want)
	}
}

func TestDeckNavigatorPanePercentIsThin(t *testing.T) {
	t.Parallel()

	if deckNavigatorPanePercent != 25 {
		t.Fatalf("deckNavigatorPanePercent = %d, want 25", deckNavigatorPanePercent)
	}
}

func TestDeckObservePanePercentFitsUnderNavigator(t *testing.T) {
	t.Parallel()

	if deckObservePanePercent != 25 {
		t.Fatalf("deckObservePanePercent = %d, want 25", deckObservePanePercent)
	}
}

func TestParseWeztermSplitPaneID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		out  string
		want string
	}{
		{name: "plain", out: "12\n", want: "12"},
		{name: "labelled", out: "pane_id: 34\n", want: "34"},
		{name: "last numeric line", out: "warning 2026\n56\n", want: "56"},
		{name: "empty", out: "", want: ""},
		{name: "none", out: "created pane\n", want: ""},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := parseWeztermSplitPaneID(tt.out); got != tt.want {
				t.Fatalf("parseWeztermSplitPaneID(%q) = %q, want %q", tt.out, got, tt.want)
			}
		})
	}
}

func TestDeckSplitArgsTargetExpectedPanes(t *testing.T) {
	t.Parallel()

	nav := strings.Join(deckNavSplitArgs("7", "/bin/milliways"), " ")
	for _, want := range []string{
		"split-pane --pane-id 7 --left --percent 25",
		"/bin/milliways attach --deck --right-pane 7",
	} {
		if !strings.Contains(nav, want) {
			t.Fatalf("nav split args missing %q: %s", want, nav)
		}
	}

	observe := strings.Join(deckObserveSplitArgs("8", "/bin/milliwaysctl"), " ")
	for _, want := range []string{
		"split-pane --pane-id 8 --bottom --percent 25",
		"/bin/milliwaysctl observe-render",
	} {
		if !strings.Contains(observe, want) {
			t.Fatalf("observe split args missing %q: %s", want, observe)
		}
	}
}

func TestResolveMilliwaysCtlBinFromSibling(t *testing.T) {
	dir := t.TempDir()
	milliwaysBin := filepath.Join(dir, "milliways")
	milliwaysCtlBin := filepath.Join(dir, "milliwaysctl")
	if err := os.WriteFile(milliwaysBin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(milliwaysCtlBin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", "")

	if got := resolveMilliwaysCtlBin(milliwaysBin); got != milliwaysCtlBin {
		t.Fatalf("resolveMilliwaysCtlBin = %q, want %q", got, milliwaysCtlBin)
	}
}
