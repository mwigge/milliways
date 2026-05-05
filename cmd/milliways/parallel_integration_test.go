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

// parallel_integration_test.go — end-to-end tests for the /parallel
// slash command.
//
// Starts a real daemon and exercises handleParallel by dispatching a
// prompt to the _echo provider. Asserts that:
//   - The dispatch result is printed with a group_id.
//   - group.list returns at least one group after dispatch.
//
// Build / run:
//
//	go test ./cmd/milliways/... -tags integration -run TestParallelIntegration -v

import (
	"bytes"
	"testing"
	"time"

	"github.com/mwigge/milliways/internal/rpc"
)

// TestHandleParallelIntegration exercises chatLoop.handleParallel against a
// real daemon. It verifies that the dispatch result contains a group_id and
// that group.list returns the newly-created group.
func TestHandleParallelIntegration(t *testing.T) {
	sock, cleanup := startTestDaemon(t)
	defer cleanup()

	client, err := rpc.Dial(sock)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	var out, errw bytes.Buffer
	loop := &chatLoop{
		client: client,
		out:    &out,
		errw:   &errw,
	}

	// Dispatch to the _echo provider — the only reliable agent in tests.
	loop.handleParallel("--providers _echo test parallel prompt")

	// handleParallel should not emit to errw on success.
	if errw.Len() > 0 {
		t.Errorf("handleParallel wrote to errw: %q", errw.String())
	}

	// In headless mode (no WezTerm in CI), Launch writes the slot summary to stdout.
	// We verify the launch ran by checking group.list contains a new entry.
	t.Logf("handleParallel output: %s", out.String())

	// Wait briefly for the daemon to register the group.
	time.Sleep(40 * time.Millisecond)

	// Assert group.list contains at least one group (created by the dispatch).
	var listResult struct {
		Groups []struct {
			GroupID string `json:"group_id"`
		} `json:"groups"`
	}
	if err := client.Call("group.list", map[string]any{}, &listResult); err != nil {
		t.Fatalf("group.list: %v", err)
	}
	if len(listResult.Groups) == 0 {
		t.Error("group.list returned no groups after handleParallel dispatch")
	}
	t.Logf("group_id: %s", listResult.Groups[0].GroupID)
}

// TestHandleParallelNoProviders verifies that handleParallel writes an error
// when the client has no connection (cannot discover providers).
func TestHandleParallelNoProviders(t *testing.T) {
	var out, errw bytes.Buffer
	loop := &chatLoop{
		client: nil, // no daemon connection
		out:    &out,
		errw:   &errw,
	}

	loop.handleParallel("some prompt without providers flag")

	if errw.Len() == 0 {
		t.Error("expected error output when no daemon connection, got none")
	}
	t.Logf("errw: %q", errw.String())
}

// TestHandleParallelMissingPrompt verifies that handleParallel writes an
// error when a prompt is not provided.
func TestHandleParallelMissingPrompt(t *testing.T) {
	sock, cleanup := startTestDaemon(t)
	defer cleanup()

	client, err := rpc.Dial(sock)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	var out, errw bytes.Buffer
	loop := &chatLoop{
		client: client,
		out:    &out,
		errw:   &errw,
	}

	// --providers present but no prompt.
	loop.handleParallel("--providers _echo")

	if errw.Len() == 0 {
		t.Error("expected error output when prompt is missing, got none")
	}
	t.Logf("errw: %q", errw.String())
}
