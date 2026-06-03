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
// Starts a real daemon and exercises agent.list plus the native deck switch
// control path without requiring a live WezTerm session.
//
// Build / run:
//
//	go test ./cmd/milliways/... -tags integration -run TestDeck -v

import (
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

func TestDeckNavigatorSwitchProviderWritesNativeControl(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	if err := writeDeckSwitchControl("claude"); err != nil {
		t.Fatalf("writeDeckSwitchControl: %v", err)
	}
	line, ok := newDeckSwitchControlPoller()()
	if ok || line != "" {
		t.Fatalf("new poller consumed stale switch = (%q,%t), want empty", line, ok)
	}

	poll := newDeckSwitchControlPoller()
	if err := writeDeckSwitchControl("codex"); err != nil {
		t.Fatalf("writeDeckSwitchControl fresh: %v", err)
	}
	line, ok = poll()
	if !ok || line != "/switch codex" {
		t.Fatalf("native switch poll = (%q,%t), want /switch codex", line, ok)
	}
}
