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
	"testing"
	"time"
)

func TestClaudeAdapter_MapSystemInit(t *testing.T) {
	t.Parallel()

	a := &ClaudeAdapter{}
	events := a.mapEvent("claude", &claudeEvent{
		Type:      "system",
		Subtype:   "init",
		SessionID: "sess-123",
		Model:     "claude-opus-4-6",
	})

	if len(events) != 0 {
		t.Errorf("init should produce 0 events, got %d", len(events))
	}
	if a.SessionID() != "sess-123" {
		t.Errorf("SessionID = %q, want %q", a.SessionID(), "sess-123")
	}
}

func TestClaudeAdapter_MapSystemHook(t *testing.T) {
	t.Parallel()

	a := &ClaudeAdapter{}

	started := a.mapEvent("claude", &claudeEvent{
		Type:     "system",
		Subtype:  "hook_started",
		HookName: "SessionStart:startup",
	})
	if len(started) != 1 || started[0].Type != EventToolUse || started[0].ToolStatus != "started" {
		t.Errorf("hook_started: got %+v", started)
	}

	done := a.mapEvent("claude", &claudeEvent{
		Type:     "system",
		Subtype:  "hook_response",
		HookName: "SessionStart:startup",
	})
	if len(done) != 1 || done[0].Type != EventToolUse || done[0].ToolStatus != "done" {
		t.Errorf("hook_response: got %+v", done)
	}
}

func TestClaudeAdapter_MapAssistantText(t *testing.T) {
	t.Parallel()

	a := &ClaudeAdapter{}
	events := a.mapEvent("claude", &claudeEvent{
		Type: "assistant",
		Message: &claudeMessage{
			Content: []claudeContent{
				{Type: "text", Text: "hello world"},
			},
		},
	})

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != EventText {
		t.Errorf("Type = %v, want EventText", events[0].Type)
	}
	if events[0].Text != "hello world" {
		t.Errorf("Text = %q, want %q", events[0].Text, "hello world")
	}
}

func TestClaudeAdapter_MapAssistantCodeBlock(t *testing.T) {
	t.Parallel()

	a := &ClaudeAdapter{}
	events := a.mapEvent("claude", &claudeEvent{
		Type: "assistant",
		Message: &claudeMessage{
			Content: []claudeContent{
				{Type: "text", Text: "here is code:\n```go\nfmt.Println(\"hi\")\n```\ndone"},
			},
		},
	})

	var hasCode, hasText bool
	for _, e := range events {
		if e.Type == EventCodeBlock {
			hasCode = true
			if e.Language != "go" {
				t.Errorf("CodeBlock.Language = %q, want %q", e.Language, "go")
			}
		}
		if e.Type == EventText {
			hasText = true
		}
	}
	if !hasCode {
		t.Error("expected EventCodeBlock, got none")
	}
	if !hasText {
		t.Error("expected EventText around code block, got none")
	}
}

func TestClaudeAdapter_MapAssistantToolUse(t *testing.T) {
	t.Parallel()

	a := &ClaudeAdapter{}
	events := a.mapEvent("claude", &claudeEvent{
		Type: "assistant",
		Message: &claudeMessage{
			Content: []claudeContent{
				{Type: "tool_use", Name: "Edit"},
			},
		},
	})

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != EventToolUse || events[0].ToolName != "Edit" {
		t.Errorf("got %+v, want EventToolUse with ToolName=Edit", events[0])
	}
}

func TestClaudeAdapter_MapRateLimit(t *testing.T) {
	t.Parallel()

	a := &ClaudeAdapter{}
	events := a.mapEvent("claude", &claudeEvent{
		Type: "rate_limit_event",
		RateLimitInfo: &claudeRateLimit{
			Status:   "exhausted",
			ResetsAt: 1776160800,
		},
	})

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != EventRateLimit {
		t.Errorf("Type = %v, want EventRateLimit", events[0].Type)
	}
	if events[0].RateLimit.Status != "exhausted" {
		t.Errorf("Status = %q, want %q", events[0].RateLimit.Status, "exhausted")
	}
	if !events[0].RateLimit.IsExhaustion {
		t.Error("expected IsExhaustion=true")
	}
	if events[0].RateLimit.DetectionKind != "structured" {
		t.Errorf("DetectionKind = %q, want %q", events[0].RateLimit.DetectionKind, "structured")
	}
	if events[0].RateLimit.ResetsAt.IsZero() {
		t.Error("ResetsAt should not be zero")
	}
}

func TestParseClaudeExhaustionText(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 14, 20, 0, 0, 0, time.UTC)
	evt := parseClaudeExhaustionText("claude", "You've hit your limit · resets 10pm (Europe/Stockholm)", now, "stderr_text")
	if evt == nil {
		t.Fatal("expected exhaustion event")
	}
	if evt.Type != EventRateLimit {
		t.Fatalf("Type = %v, want EventRateLimit", evt.Type)
	}
	if !evt.RateLimit.IsExhaustion {
		t.Fatal("expected IsExhaustion=true")
	}
	if evt.RateLimit.DetectionKind != "stderr_text" {
		t.Fatalf("DetectionKind = %q", evt.RateLimit.DetectionKind)
	}
	if evt.RateLimit.ResetsAt.IsZero() {
		t.Fatal("expected parsed reset time")
	}
}

func TestClaudeAdapter_MapResult(t *testing.T) {
	t.Parallel()

	a := &ClaudeAdapter{}
	events := a.mapEvent("claude", &claudeEvent{
		Type:         "result",
		TotalCostUSD: 0.145,
		DurationMs:   3324,
		Usage: &claudeUsage{
			InputTokens:              100,
			OutputTokens:             50,
			CacheReadInputTokens:     10,
			CacheCreationInputTokens: 200,
		},
	})

	if len(events) != 2 {
		t.Fatalf("expected 2 events (Cost + Done), got %d", len(events))
	}

	if events[0].Type != EventCost {
		t.Errorf("events[0].Type = %v, want EventCost", events[0].Type)
	}
	if events[0].Cost.USD != 0.145 {
		t.Errorf("Cost.USD = %f, want 0.145", events[0].Cost.USD)
	}
	if events[0].Cost.InputTokens != 100 {
		t.Errorf("Cost.InputTokens = %d, want 100", events[0].Cost.InputTokens)
	}

	if events[1].Type != EventDone {
		t.Errorf("events[1].Type = %v, want EventDone", events[1].Type)
	}
	if events[1].ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", events[1].ExitCode)
	}
}

func TestClaudeAdapter_Send(t *testing.T) {
	t.Parallel()

	// Without a stdin pipe, Send should return ErrNotInteractive
	a := &ClaudeAdapter{}
	err := a.Send(context.Background(), "hello")
	if err != ErrNotInteractive {
		t.Errorf("Send without pipe = %v, want ErrNotInteractive", err)
	}
}

func TestClaudeAdapter_SupportsResume(t *testing.T) {
	t.Parallel()

	a := &ClaudeAdapter{}
	if !a.SupportsResume() {
		t.Error("SupportsResume() = false, want true")
	}
	caps := a.Capabilities()
	if !caps.NativeResume {
		t.Error("Capabilities.NativeResume = false, want true")
	}
	if !caps.StructuredEvents {
		t.Error("Capabilities.StructuredEvents = false, want true")
	}
	if caps.ExhaustionDetection != "structured+stdout+stderr" {
		t.Errorf("Capabilities.ExhaustionDetection = %q", caps.ExhaustionDetection)
	}
}
