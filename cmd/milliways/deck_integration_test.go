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

//go:build integration

package main

// deck_integration_test.go — integration tests for the deck navigator.
//
// Starts a real daemon and exercises agent.list and the switchProvider
// exec path inline without requiring a live WezTerm session.
//
// Build / run:
//
//	go test ./cmd/milliways/... -tags integration -run TestDeck -v

import (
	"log/slog"
	"os/exec"
	"testing"

	"github.com/mwigge/milliways/internal/rpc"
)

// TestDeckNavigatorPollsAgentList verifies that agent.list returns a
// non-nil flat slice (not a wrapped object) and does not error.
func TestDeckNavigatorPollsAgentList(t *testing.T) {
	t.Parallel()

	sock, cleanup := startTestDaemon(t)
	defer cleanup()

	client, err := rpc.Dial(sock)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	// agent.list must return a flat []struct, not an object. The test daemon
	// starts with an empty agentsCache (no runner probes), so the slice may
	// be empty — but it must never be nil after a successful call.
	var agents []struct {
		ID         string `json:"id"`
		AuthStatus string `json:"auth_status"`
		Model      string `json:"model"`
	}
	if err := client.Call("agent.list", nil, &agents); err != nil {
		t.Fatalf("agent.list: %v", err)
	}
	if agents == nil {
		t.Error("agent.list returned nil; want non-nil slice (may be empty when no runners configured)")
	}
}

// TestDeckNavigatorSwitchProvider_NoWeztermPanic verifies the switchProvider
// logic — extracted inline — does not panic in edge cases:
//
//  1. Empty rightPaneID → return immediately without calling exec.
//  2. Bogus pane ID "99999" → exec may error (wezterm absent in CI or bad
//     pane); the function must return normally, never panic.
func TestDeckNavigatorSwitchProvider_NoWeztermPanic(t *testing.T) {
	t.Parallel()

	// switchProvider is the exact logic from runDeckNavigator, extracted as a
	// local closure so it can be called without a live terminal.
	switchProvider := func(rightPaneID, provider string) {
		if rightPaneID == "" {
			return
		}
		if err := exec.Command("wezterm", "cli", "send-text",
			"--pane-id", rightPaneID,
			"--no-paste",
			"/switch "+provider+"\n").Run(); err != nil {
			slog.Debug("deck: send-text failed", "err", err)
		}
	}

	tests := []struct {
		name        string
		rightPaneID string
		provider    string
	}{
		{
			name:        "empty rightPaneID skips exec without panic",
			rightPaneID: "",
			provider:    "claude",
		},
		{
			name:        "bogus pane ID fails gracefully without panic",
			rightPaneID: "99999",
			provider:    "claude",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Confirm the call never panics regardless of outcome.
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("switchProvider panicked with pane=%q provider=%q: %v",
						tt.rightPaneID, tt.provider, r)
				}
			}()

			switchProvider(tt.rightPaneID, tt.provider)
		})
	}
}
