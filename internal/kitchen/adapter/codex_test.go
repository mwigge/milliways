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

package adapter

import (
	"context"
	"slices"
	"testing"

	"github.com/mwigge/milliways/internal/kitchen"
)

func TestCodexAdapter_Send_WithoutPipe(t *testing.T) {
	t.Parallel()

	a := NewCodexAdapter(newTestKitchen("echo"), AdapterOpts{})
	if err := a.Send(context.Background(), "msg"); err != ErrNotInteractive {
		t.Errorf("Send without pipe = %v, want ErrNotInteractive", err)
	}
}

func TestCodexAdapter_Resume(t *testing.T) {
	t.Parallel()

	a := NewCodexAdapter(newTestKitchen("echo"), AdapterOpts{})
	if a.SupportsResume() {
		t.Error("SupportsResume() = true, want false")
	}
	if a.SessionID() != "" {
		t.Errorf("SessionID() = %q, want empty", a.SessionID())
	}
	caps := a.Capabilities()
	if caps.NativeResume {
		t.Error("Capabilities.NativeResume = true, want false")
	}
	if !caps.StructuredEvents {
		t.Error("Capabilities.StructuredEvents = false, want true")
	}
}

func TestBuildCodexArgs_DefaultsInjectSandboxAndApproval(t *testing.T) {
	t.Parallel()

	cfg := kitchen.GenericConfig{Cmd: "codex"}
	args := buildCodexArgs(cfg, kitchen.Task{Prompt: "do the thing"})

	wantContains := [][]string{
		{"--sandbox", "workspace-write"},
		{"--ask-for-approval", "never"},
	}
	for _, pair := range wantContains {
		if !containsPair(args, pair[0], pair[1]) {
			t.Errorf("buildCodexArgs missing %q %q in %v", pair[0], pair[1], args)
		}
	}
	if got, want := args[0], "exec"; got != want {
		t.Errorf("args[0] = %q, want %q", got, want)
	}
	if got, want := args[len(args)-1], "do the thing"; got != want {
		t.Errorf("args[last] = %q, want %q (prompt)", got, want)
	}
}

func TestBuildCodexArgs_RespectsUserSandbox(t *testing.T) {
	t.Parallel()

	cfg := kitchen.GenericConfig{
		Cmd:  "codex",
		Args: []string{"--sandbox", "read-only"},
	}
	args := buildCodexArgs(cfg, kitchen.Task{Prompt: "p"})

	if containsPair(args, "--sandbox", "workspace-write") {
		t.Errorf("default --sandbox workspace-write injected despite user override: %v", args)
	}
	if !containsPair(args, "--sandbox", "read-only") {
		t.Errorf("user --sandbox read-only not present: %v", args)
	}
	if !containsPair(args, "--ask-for-approval", "never") {
		t.Errorf("default --ask-for-approval never should still be injected: %v", args)
	}
}

func TestBuildCodexArgs_RespectsUserApproval(t *testing.T) {
	t.Parallel()

	cfg := kitchen.GenericConfig{
		Cmd:  "codex",
		Args: []string{"--ask-for-approval", "on-request"},
	}
	args := buildCodexArgs(cfg, kitchen.Task{Prompt: "p"})

	if containsPair(args, "--ask-for-approval", "never") {
		t.Errorf("default --ask-for-approval never injected despite user override: %v", args)
	}
	if !containsPair(args, "--ask-for-approval", "on-request") {
		t.Errorf("user --ask-for-approval on-request not present: %v", args)
	}
	if !containsPair(args, "--sandbox", "workspace-write") {
		t.Errorf("default --sandbox workspace-write should still be injected: %v", args)
	}
}

func TestBuildCodexArgs_PromptIsLastArg(t *testing.T) {
	t.Parallel()

	cfg := kitchen.GenericConfig{Cmd: "codex", Args: []string{"--model", "o4-mini"}}
	args := buildCodexArgs(cfg, kitchen.Task{Prompt: "the prompt"})

	if got := args[len(args)-1]; got != "the prompt" {
		t.Errorf("last arg = %q, want %q", got, "the prompt")
	}
	// --model and the user value must precede the prompt.
	idxModel := slices.Index(args, "--model")
	if idxModel < 0 || idxModel >= len(args)-2 {
		t.Errorf("--model not before prompt in %v", args)
	}
}

func containsPair(args []string, flag, value string) bool {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == flag && args[i+1] == value {
			return true
		}
	}
	return false
}

func TestParseGenericExhaustionText_Codex(t *testing.T) {
	t.Parallel()

	evt := parseGenericExhaustionText("codex", "rate limit exceeded for current plan", "stdout_text")
	if evt == nil {
		t.Fatal("expected exhaustion event")
	}
	if evt.RateLimit == nil || !evt.RateLimit.IsExhaustion {
		t.Fatalf("rate limit = %#v", evt.RateLimit)
	}
}
