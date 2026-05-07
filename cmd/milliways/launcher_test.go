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

	if deckNavigatorPanePercent > 18 {
		t.Fatalf("deckNavigatorPanePercent = %d, want <= 18", deckNavigatorPanePercent)
	}
}
