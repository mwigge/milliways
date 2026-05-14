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

package runners

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mwigge/milliways/internal/pantry"
	"github.com/mwigge/milliways/internal/provider"
	"github.com/mwigge/milliways/internal/security"
	"github.com/mwigge/milliways/internal/security/firewall"
	"github.com/mwigge/milliways/internal/security/outputgate"
	"github.com/mwigge/milliways/internal/tools"
)

// stubClient is a Client that returns a queued sequence of TurnResults,
// recording every Send call so tests can inspect the message stream.
type stubClient struct {
	turns []TurnResult
	calls int
	seen  []int // length of messages slice at each call
}

func (c *stubClient) Send(_ context.Context, messages []Message, _ []provider.ToolDef) (TurnResult, error) {
	c.seen = append(c.seen, len(messages))
	if c.calls >= len(c.turns) {
		return TurnResult{}, errors.New("stubClient: out of queued turns")
	}
	t := c.turns[c.calls]
	c.calls++
	return t, nil
}

func newRegistryWithEcho() *tools.Registry {
	r := tools.NewRegistry()
	r.Register("echo", func(_ context.Context, args map[string]any) (string, error) {
		if v, ok := args["text"].(string); ok {
			return v, nil
		}
		return "", nil
	}, provider.ToolDef{Name: "echo"})
	r.Register("boom", func(_ context.Context, _ map[string]any) (string, error) {
		return "", errors.New("kaboom")
	}, provider.ToolDef{Name: "boom"})
	return r
}

func TestRunAgenticLoop_CleanStopExecutesNoTools(t *testing.T) {
	t.Parallel()

	client := &stubClient{turns: []TurnResult{
		{Content: "all done", FinishReason: FinishStop},
	}}
	registry := newRegistryWithEcho()
	messages := []Message{{Role: RoleUser, Content: "hi"}}

	result, err := RunAgenticLoop(context.Background(), client, registry, &messages, LoopOptions{})
	if err != nil {
		t.Fatalf("RunAgenticLoop err = %v", err)
	}
	if result.Turns != 1 {
		t.Errorf("turns = %d, want 1", result.Turns)
	}
	if result.StoppedAt != StopReasonStop {
		t.Errorf("stopped = %q, want %q", result.StoppedAt, StopReasonStop)
	}
	if result.FinalContent != "all done" {
		t.Errorf("final content = %q, want %q", result.FinalContent, "all done")
	}
	// Only the original user message — no tool messages appended.
	if len(messages) != 2 {
		t.Errorf("messages len = %d, want 2 (user + final assistant)", len(messages))
	}
}

func TestRunAgenticLoop_MultipleToolCallsExecutedInOrder(t *testing.T) {
	t.Parallel()

	client := &stubClient{turns: []TurnResult{
		{
			ToolCalls: []ToolCall{
				{ID: "c1", Name: "echo", Args: `{"text":"first"}`},
				{ID: "c2", Name: "echo", Args: `{"text":"second"}`},
			},
			FinishReason: FinishToolCalls,
		},
		{Content: "done", FinishReason: FinishStop},
	}}
	registry := newRegistryWithEcho()
	messages := []Message{{Role: RoleUser, Content: "go"}}

	result, err := RunAgenticLoop(context.Background(), client, registry, &messages, LoopOptions{})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if result.Turns != 2 {
		t.Errorf("turns = %d, want 2", result.Turns)
	}
	// Expect: user, assistant(toolCalls), tool(c1), tool(c2), assistant(final)
	if len(messages) != 5 {
		t.Fatalf("messages len = %d, want 5; got %+v", len(messages), messages)
	}
	// Tool results are wrapped in <tool_result> markers for prompt-injection
	// hardening; assert the substring rather than exact equality.
	if messages[2].Role != RoleTool || messages[2].ToolCallID != "c1" || !strings.Contains(messages[2].Content, "first") {
		t.Errorf("messages[2] = %+v, want tool/c1 containing 'first'", messages[2])
	}
	if messages[3].Role != RoleTool || messages[3].ToolCallID != "c2" || !strings.Contains(messages[3].Content, "second") {
		t.Errorf("messages[3] = %+v, want tool/c2 containing 'second'", messages[3])
	}
}

func TestRunAgenticLoop_CommandFirewallWarnAllowsBashExecution(t *testing.T) {
	t.Parallel()

	client := &stubClient{turns: []TurnResult{
		{
			ToolCalls:    []ToolCall{{ID: "c1", Name: "Bash", Args: `{"command":"npm install left-pad"}`}},
			FinishReason: FinishToolCalls,
		},
		{Content: "done", FinishReason: FinishStop},
	}}
	registry := tools.NewRegistry()
	executed := false
	registry.Register("Bash", func(_ context.Context, args map[string]any) (string, error) {
		executed = true
		if got, _ := args["command"].(string); got != "npm install left-pad" {
			t.Fatalf("command = %q", got)
		}
		return "installed", nil
	}, provider.ToolDef{Name: "Bash"})
	messages := []Message{{Role: RoleUser, Content: "go"}}

	_, err := RunAgenticLoop(context.Background(), client, registry, &messages, LoopOptions{
		CommandFirewall: StaticCommandFirewall{
			Policy: firewall.Policy{Mode: security.ModeWarn},
		},
	})
	if err != nil {
		t.Fatalf("RunAgenticLoop err = %v", err)
	}
	if !executed {
		t.Fatalf("Bash tool was not executed in warn mode")
	}
	if len(messages) < 3 || !strings.Contains(messages[2].Content, "installed") {
		t.Fatalf("tool result missing executed output; messages = %+v", messages)
	}
}

func TestRunAgenticLoop_CommandFirewallStrictBlocksBashExecution(t *testing.T) {
	t.Parallel()

	client := &stubClient{turns: []TurnResult{
		{
			ToolCalls:    []ToolCall{{ID: "c1", Name: "Bash", Args: `{"command":"npm install left-pad"}`}},
			FinishReason: FinishToolCalls,
		},
		{Content: "done", FinishReason: FinishStop},
	}}
	registry := tools.NewRegistry()
	executed := false
	registry.Register("Bash", func(context.Context, map[string]any) (string, error) {
		executed = true
		return "should not run", nil
	}, provider.ToolDef{Name: "Bash"})
	messages := []Message{{Role: RoleUser, Content: "go"}}

	_, err := RunAgenticLoop(context.Background(), client, registry, &messages, LoopOptions{
		CommandFirewall: StaticCommandFirewall{
			Policy: firewall.Policy{Mode: security.ModeStrict},
		},
	})
	if err != nil {
		t.Fatalf("RunAgenticLoop err = %v", err)
	}
	if executed {
		t.Fatalf("Bash tool executed despite strict firewall block")
	}
	if len(messages) < 3 {
		t.Fatalf("messages len = %d, want tool error message", len(messages))
	}
	content := messages[2].Content
	if !strings.Contains(content, "error: command blocked by security firewall") {
		t.Fatalf("tool content = %q, want firewall block error", content)
	}
	if !strings.Contains(content, "package install") {
		t.Fatalf("tool content = %q, want firewall reason", content)
	}
}

func TestRunAgenticLoop_CommandFirewallIgnoresNonBashTools(t *testing.T) {
	t.Parallel()

	client := &stubClient{turns: []TurnResult{
		{
			ToolCalls:    []ToolCall{{ID: "c1", Name: "echo", Args: `{"text":"npm install left-pad"}`}},
			FinishReason: FinishToolCalls,
		},
		{Content: "done", FinishReason: FinishStop},
	}}
	registry := newRegistryWithEcho()
	messages := []Message{{Role: RoleUser, Content: "go"}}

	_, err := RunAgenticLoop(context.Background(), client, registry, &messages, LoopOptions{
		CommandFirewall: StaticCommandFirewall{
			Policy: firewall.Policy{Mode: security.ModeStrict},
		},
	})
	if err != nil {
		t.Fatalf("RunAgenticLoop err = %v", err)
	}
	if len(messages) < 3 || !strings.Contains(messages[2].Content, "npm install left-pad") {
		t.Fatalf("non-Bash tool result was unexpectedly blocked; messages = %+v", messages)
	}
}

func TestRunAgenticLoop_OutputGateScansGeneratedSecretAndSASTFiles(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	client := &stubClient{turns: []TurnResult{
		{
			ToolCalls:    []ToolCall{{ID: "c1", Name: "WriteApp", Args: `{}`}},
			FinishReason: FinishToolCalls,
		},
		{Content: "done", FinishReason: FinishStop},
	}}
	registry := tools.NewRegistry()
	registry.Register("WriteApp", func(context.Context, map[string]any) (string, error) {
		if err := os.WriteFile(workspace+"/app.go", []byte("package main\n"), 0o644); err != nil {
			return "", err
		}
		return "wrote app", nil
	}, provider.ToolDef{Name: "WriteApp"})
	secret := &outputGateFakeAdapter{
		name:      "secret-tool",
		installed: true,
		result: security.ScanResult{Findings: []security.Finding{{
			ID:       "secret-1",
			Severity: "HIGH",
			FilePath: "app.go",
			Summary:  "generated token",
		}}},
	}
	sast := &outputGateFakeAdapter{
		name:      "sast-tool",
		installed: true,
		result: security.ScanResult{Findings: []security.Finding{{
			ID:       "sast-1",
			Severity: "MEDIUM",
			FilePath: "app.go",
			Summary:  "generated issue",
		}}},
	}
	messages := []Message{{Role: RoleUser, Content: "go"}}

	_, err := RunAgenticLoop(context.Background(), client, registry, &messages, LoopOptions{
		OutputGate: OutputGateOptions{
			Workspace: workspace,
			Mode:      security.ModeWarn,
			Scanners: []outputgate.Scanner{
				{Kind: security.ScanSecret, Adapter: secret},
				{Kind: security.ScanSAST, Adapter: sast},
			},
		},
	})
	if err != nil {
		t.Fatalf("RunAgenticLoop err = %v", err)
	}
	assertOutputGateCall(t, secret, workspace, []string{"app.go"})
	assertOutputGateCall(t, sast, workspace, []string{"app.go"})
	content := messages[2].Content
	if !strings.Contains(content, "wrote app") || !strings.Contains(content, "security output gate:") {
		t.Fatalf("tool content missing output gate report: %q", content)
	}
	if !strings.Contains(content, "secret-1") || !strings.Contains(content, "sast-1") {
		t.Fatalf("tool content missing scanner findings: %q", content)
	}
}

func TestRunAgenticLoop_OutputGateWarnsWhenScannerMissing(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	client := &stubClient{turns: []TurnResult{
		{
			ToolCalls:    []ToolCall{{ID: "c1", Name: "WriteApp", Args: `{}`}},
			FinishReason: FinishToolCalls,
		},
		{Content: "done", FinishReason: FinishStop},
	}}
	registry := tools.NewRegistry()
	registry.Register("WriteApp", func(context.Context, map[string]any) (string, error) {
		return "wrote app", os.WriteFile(workspace+"/app.go", []byte("package main\n"), 0o644)
	}, provider.ToolDef{Name: "WriteApp"})
	messages := []Message{{Role: RoleUser, Content: "go"}}

	_, err := RunAgenticLoop(context.Background(), client, registry, &messages, LoopOptions{
		OutputGate: OutputGateOptions{
			Workspace: workspace,
			Mode:      security.ModeWarn,
			Scanners:  []outputgate.Scanner{},
		},
	})
	if err != nil {
		t.Fatalf("RunAgenticLoop err = %v", err)
	}
	content := messages[2].Content
	if !strings.Contains(content, "secret scan skipped: no secret scanner adapter configured") {
		t.Fatalf("tool content missing secret scanner warning: %q", content)
	}
	if !strings.Contains(content, "sast scan skipped: no sast scanner adapter configured") {
		t.Fatalf("tool content missing sast scanner warning: %q", content)
	}
}

func TestRunAgenticLoop_OutputGatePersistsFindingsAndWarnings(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	db, err := pantry.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store := db.Security()
	client := &stubClient{turns: []TurnResult{
		{
			ToolCalls:    []ToolCall{{ID: "c1", Name: "WriteApp", Args: `{}`}},
			FinishReason: FinishToolCalls,
		},
		{Content: "done", FinishReason: FinishStop},
	}}
	registry := tools.NewRegistry()
	registry.Register("WriteApp", func(context.Context, map[string]any) (string, error) {
		return "wrote app", os.WriteFile(workspace+"/app.go", []byte("package main\n"), 0o644)
	}, provider.ToolDef{Name: "WriteApp"})
	secret := &outputGateFakeAdapter{
		name:      "gitleaks",
		installed: true,
		result: security.ScanResult{Findings: []security.Finding{{
			ID:       "secret-1",
			Category: security.FindingSecret,
			Severity: "HIGH",
			FilePath: "app.go",
			Line:     7,
			Summary:  "generated token",
			Status:   security.FindingBlocked,
		}}},
	}
	messages := []Message{{Role: RoleUser, Content: "go"}}

	_, err = RunAgenticLoop(context.Background(), client, registry, &messages, LoopOptions{
		OutputGate: OutputGateOptions{
			Workspace: workspace,
			Mode:      security.ModeWarn,
			Store:     store,
			Scanners: []outputgate.Scanner{
				{Kind: security.ScanSecret, Adapter: secret},
			},
		},
	})
	if err != nil {
		t.Fatalf("RunAgenticLoop err = %v", err)
	}

	status, err := store.SecurityStatus(workspace)
	if err != nil {
		t.Fatalf("SecurityStatus: %v", err)
	}
	if status.CountsByCategory["secret"] != 1 {
		t.Fatalf("secret count = %d, want 1", status.CountsByCategory["secret"])
	}
	if status.CountsBySeverity["HIGH"] != 1 {
		t.Fatalf("HIGH count = %d, want 1", status.CountsBySeverity["HIGH"])
	}
	findings, err := store.ListAll()
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(findings) != 1 || findings[0].Status != "blocked" {
		t.Fatalf("findings = %#v, want one blocked finding", findings)
	}
	if len(status.Warnings) != 1 || !strings.Contains(status.Warnings[0].Message, "no sast scanner adapter configured") {
		t.Fatalf("warnings = %#v, want persisted missing sast warning", status.Warnings)
	}
}

func TestRunAgenticLoop_OutputGateNoopWhenNoModifiedFiles(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	client := &stubClient{turns: []TurnResult{
		{
			ToolCalls:    []ToolCall{{ID: "c1", Name: "Noop", Args: `{}`}},
			FinishReason: FinishToolCalls,
		},
		{Content: "done", FinishReason: FinishStop},
	}}
	registry := tools.NewRegistry()
	registry.Register("Noop", func(context.Context, map[string]any) (string, error) {
		return "nothing changed", nil
	}, provider.ToolDef{Name: "Noop"})
	secret := &outputGateFakeAdapter{name: "secret-tool", installed: true}
	messages := []Message{{Role: RoleUser, Content: "go"}}

	_, err := RunAgenticLoop(context.Background(), client, registry, &messages, LoopOptions{
		OutputGate: OutputGateOptions{
			Workspace: workspace,
			Mode:      security.ModeWarn,
			Scanners:  []outputgate.Scanner{{Kind: security.ScanSecret, Adapter: secret}},
		},
	})
	if err != nil {
		t.Fatalf("RunAgenticLoop err = %v", err)
	}
	if len(secret.calls) != 0 {
		t.Fatalf("scanner calls = %#v, want none", secret.calls)
	}
	content := messages[2].Content
	if strings.Contains(content, "security output gate:") {
		t.Fatalf("tool content included output gate report despite no changes: %q", content)
	}
}

func TestRunAgenticLoop_UserInputRequestStopsBeforeToolExecution(t *testing.T) {
	t.Parallel()

	client := &stubClient{turns: []TurnResult{
		{
			Content:      "This will edit files. Should I proceed?",
			ToolCalls:    []ToolCall{{ID: "c1", Name: "echo", Args: `{"text":"first"}`}},
			FinishReason: FinishToolCalls,
		},
	}}
	registry := newRegistryWithEcho()
	messages := []Message{{Role: RoleUser, Content: "go"}}

	result, err := RunAgenticLoop(context.Background(), client, registry, &messages, LoopOptions{
		StopOnUserInputRequest: true,
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if result.StoppedAt != StopReasonNeedsInput {
		t.Fatalf("stopped = %q, want %q", result.StoppedAt, StopReasonNeedsInput)
	}
	if result.FinalContent != "This will edit files. Should I proceed?" {
		t.Fatalf("final content = %q", result.FinalContent)
	}
	if client.calls != 1 {
		t.Fatalf("client calls = %d, want 1", client.calls)
	}
	if len(messages) != 2 {
		t.Fatalf("messages len = %d, want 2; got %+v", len(messages), messages)
	}
	if messages[1].Role != RoleAssistant || len(messages[1].ToolCalls) != 0 {
		t.Fatalf("assistant confirmation should be stored without executable tool calls; got %+v", messages[1])
	}
}

func TestRunAgenticLoop_MaxTurnsCap(t *testing.T) {
	t.Parallel()

	// Five queued turns that all request a tool call; cap at 3.
	turns := make([]TurnResult, 5)
	for i := range turns {
		turns[i] = TurnResult{
			ToolCalls:    []ToolCall{{ID: "c", Name: "echo", Args: `{"text":"x"}`}},
			FinishReason: FinishToolCalls,
		}
	}
	client := &stubClient{turns: turns}
	registry := newRegistryWithEcho()
	messages := []Message{{Role: RoleUser, Content: "loop"}}

	result, err := RunAgenticLoop(context.Background(), client, registry, &messages, LoopOptions{MaxTurns: 3})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if result.StoppedAt != StopReasonMaxTurns {
		t.Errorf("stopped = %q, want %q", result.StoppedAt, StopReasonMaxTurns)
	}
	if result.Turns != 3 {
		t.Errorf("turns = %d, want 3", result.Turns)
	}
	if client.calls != 3 {
		t.Errorf("client calls = %d, want 3", client.calls)
	}
}

func TestRunAgenticLoop_ToolFailureFoldedAsErrorAndLoopContinues(t *testing.T) {
	t.Parallel()

	client := &stubClient{turns: []TurnResult{
		{
			ToolCalls:    []ToolCall{{ID: "c1", Name: "boom", Args: `{}`}},
			FinishReason: FinishToolCalls,
		},
		{Content: "recovered", FinishReason: FinishStop},
	}}
	registry := newRegistryWithEcho()
	messages := []Message{{Role: RoleUser, Content: "try"}}

	result, err := RunAgenticLoop(context.Background(), client, registry, &messages, LoopOptions{})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if result.StoppedAt != StopReasonStop {
		t.Errorf("stopped = %q, want %q", result.StoppedAt, StopReasonStop)
	}
	// Find the tool message and verify it carries the error prefix.
	var foundToolMsg *Message
	for i := range messages {
		if messages[i].Role == RoleTool {
			foundToolMsg = &messages[i]
			break
		}
	}
	if foundToolMsg == nil {
		t.Fatalf("no tool message appended; messages = %+v", messages)
	}
	// Tool results are wrapped in <tool_result> markers; assert that the
	// error appears anywhere in the wrapped payload.
	if !strings.Contains(foundToolMsg.Content, "error: ") {
		t.Errorf("tool content = %q, want it to contain \"error: \"", foundToolMsg.Content)
	}
	if !strings.Contains(foundToolMsg.Content, "kaboom") {
		t.Errorf("tool content = %q, want it to contain underlying error", foundToolMsg.Content)
	}
}

func TestRunAgenticLoop_UnknownToolFoldedAsError(t *testing.T) {
	t.Parallel()

	client := &stubClient{turns: []TurnResult{
		{
			ToolCalls:    []ToolCall{{ID: "c1", Name: "nonesuch", Args: `{}`}},
			FinishReason: FinishToolCalls,
		},
		{Content: "ok", FinishReason: FinishStop},
	}}
	registry := newRegistryWithEcho()
	messages := []Message{{Role: RoleUser, Content: "go"}}

	_, err := RunAgenticLoop(context.Background(), client, registry, &messages, LoopOptions{})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	var toolMsg *Message
	for i := range messages {
		if messages[i].Role == RoleTool {
			toolMsg = &messages[i]
			break
		}
	}
	if toolMsg == nil {
		t.Fatalf("no tool message; messages = %+v", messages)
	}
	if !strings.Contains(toolMsg.Content, "error: ") || !strings.Contains(toolMsg.Content, "not found") {
		t.Errorf("tool content = %q, want error mentioning not found", toolMsg.Content)
	}
}

func TestRunAgenticLoop_MalformedArgsJSONFoldedAsError(t *testing.T) {
	t.Parallel()

	client := &stubClient{turns: []TurnResult{
		{
			ToolCalls:    []ToolCall{{ID: "c1", Name: "echo", Args: `{not valid json`}},
			FinishReason: FinishToolCalls,
		},
		{Content: "ok", FinishReason: FinishStop},
	}}
	registry := newRegistryWithEcho()
	messages := []Message{{Role: RoleUser, Content: "go"}}

	_, err := RunAgenticLoop(context.Background(), client, registry, &messages, LoopOptions{})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	var toolMsg *Message
	for i := range messages {
		if messages[i].Role == RoleTool {
			toolMsg = &messages[i]
			break
		}
	}
	if toolMsg == nil {
		t.Fatalf("no tool message; messages = %+v", messages)
	}
	if !strings.Contains(toolMsg.Content, "error: ") {
		t.Errorf("tool content = %q, want it to contain error", toolMsg.Content)
	}
}

func TestRunAgenticLoop_DefaultMaxTurnsIsDefaultMaxTurns(t *testing.T) {
	t.Setenv("MILLIWAYS_MAX_TURNS", "") // isolate from host env; can't Parallel with Setenv

	over := DefaultMaxTurns + 5
	turns := make([]TurnResult, over)
	for i := range turns {
		turns[i] = TurnResult{
			ToolCalls:    []ToolCall{{ID: "c", Name: "echo", Args: `{"text":"x"}`}},
			FinishReason: FinishToolCalls,
		}
	}
	client := &stubClient{turns: turns}
	registry := newRegistryWithEcho()
	messages := []Message{{Role: RoleUser, Content: "loop"}}

	result, err := RunAgenticLoop(context.Background(), client, registry, &messages, LoopOptions{}) // MaxTurns left zero
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if result.StoppedAt != StopReasonMaxTurns {
		t.Errorf("stopped = %q, want %q", result.StoppedAt, StopReasonMaxTurns)
	}
	if result.Turns != DefaultMaxTurns {
		t.Errorf("turns = %d, want %d (DefaultMaxTurns)", result.Turns, DefaultMaxTurns)
	}
}

func TestRunAgenticLoop_MaxTurnsEnvVar(t *testing.T) {
	t.Setenv("MILLIWAYS_MAX_TURNS", "3")

	turns := make([]TurnResult, 10)
	for i := range turns {
		turns[i] = TurnResult{
			ToolCalls:    []ToolCall{{ID: "c", Name: "echo", Args: `{"text":"x"}`}},
			FinishReason: FinishToolCalls,
		}
	}
	client := &stubClient{turns: turns}
	registry := newRegistryWithEcho()
	messages := []Message{{Role: RoleUser, Content: "loop"}}

	result, err := RunAgenticLoop(context.Background(), client, registry, &messages, LoopOptions{})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if result.Turns != 3 {
		t.Errorf("turns = %d, want 3 (from MILLIWAYS_MAX_TURNS=3)", result.Turns)
	}
}

type outputGateFakeAdapter struct {
	name      string
	installed bool
	result    security.ScanResult
	err       error
	calls     []outputGateScanCall
}

type outputGateScanCall struct {
	workspace string
	targets   []string
}

func (a *outputGateFakeAdapter) Name() string {
	return a.name
}

func (a *outputGateFakeAdapter) Installed() bool {
	return a.installed
}

func (a *outputGateFakeAdapter) Version(context.Context) (string, error) {
	return "", nil
}

func (a *outputGateFakeAdapter) Scan(_ context.Context, workspace string, targets []string) (security.ScanResult, error) {
	a.calls = append(a.calls, outputGateScanCall{workspace: workspace, targets: append([]string(nil), targets...)})
	if a.err != nil {
		return security.ScanResult{}, a.err
	}
	result := a.result
	if result.ScannedAt.IsZero() {
		result.ScannedAt = time.Unix(1, 0).UTC()
	}
	return result, nil
}

func (a *outputGateFakeAdapter) RenderFinding(security.Finding) string {
	return ""
}

func assertOutputGateCall(t *testing.T, adapter *outputGateFakeAdapter, workspace string, targets []string) {
	t.Helper()
	if len(adapter.calls) != 1 {
		t.Fatalf("%s calls = %#v, want one call", adapter.name, adapter.calls)
	}
	if adapter.calls[0].workspace != workspace {
		t.Fatalf("%s workspace = %q, want %q", adapter.name, adapter.calls[0].workspace, workspace)
	}
	if strings.Join(adapter.calls[0].targets, ",") != strings.Join(targets, ",") {
		t.Fatalf("%s targets = %#v, want %#v", adapter.name, adapter.calls[0].targets, targets)
	}
}
