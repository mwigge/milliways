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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mwigge/milliways/internal/security"
	"github.com/mwigge/milliways/internal/security/clientprofiles"
)

func TestExternalCLIPreflightBlocksStrictCriticalProfileBeforeHandoff(t *testing.T) {
	oldCheck := externalCLIPreflightCheck
	externalCLIPreflightCheck = func(context.Context, string, string) externalCLIPreflightResult {
		return externalCLIPreflightResult{
			Mode: security.ModeStrict,
			Warnings: []clientprofiles.ProfileWarning{{
				Client:   AgentIDCodex,
				ID:       "codex-danger-full-access",
				Severity: clientprofiles.SeverityCritical,
				Summary:  "Codex sandbox is configured for unrestricted filesystem access.",
				Path:     "/work/.codex/config.toml",
				Key:      "sandbox_mode",
			}},
		}
	}
	t.Cleanup(func() { externalCLIPreflightCheck = oldCheck })

	pusher := &fakePusher{}
	obs := &mockObserver{}
	ok := runExternalCLIPreflight(context.Background(), AgentIDCodex, "/work", pusher, obs)
	if ok {
		t.Fatalf("preflight allowed strict critical profile")
	}
	events := pusher.snapshot()
	event := requireExternalPreflightEvent(t, events, "err")
	if event["agent"] != AgentIDCodex {
		t.Fatalf("agent = %v, want %q", event["agent"], AgentIDCodex)
	}
	if event["code"] != -32031 {
		t.Fatalf("code = %v, want -32031", event["code"])
	}
	msg, _ := event["msg"].(string)
	if !strings.Contains(msg, "security profile blocked handoff") || !strings.Contains(msg, "codex-danger-full-access") {
		t.Fatalf("unexpected msg %q", msg)
	}
	if got := obs.counterTotal(MetricErrorCount, AgentIDCodex); got != 1 {
		t.Fatalf("error_count = %v, want 1", got)
	}
}

func TestExternalCLIPreflightWarnsAndAllowsWarnProfileBeforeHandoff(t *testing.T) {
	oldCheck := externalCLIPreflightCheck
	externalCLIPreflightCheck = func(context.Context, string, string) externalCLIPreflightResult {
		return externalCLIPreflightResult{
			Mode: security.ModeWarn,
			Warnings: []clientprofiles.ProfileWarning{{
				Client:   AgentIDClaude,
				ID:       "claude-hooks-enabled",
				Severity: clientprofiles.SeverityWarning,
				Summary:  "Client hooks are configured and should be reviewed.",
				Path:     "/work/.claude/settings.json",
				Key:      "hooks",
			}},
		}
	}
	t.Cleanup(func() { externalCLIPreflightCheck = oldCheck })

	pusher := &fakePusher{}
	obs := &mockObserver{}
	ok := runExternalCLIPreflight(context.Background(), AgentIDClaude, "/work", pusher, obs)
	if !ok {
		t.Fatalf("preflight blocked warn-mode profile")
	}
	events := pusher.snapshot()
	event := requireExternalPreflightEvent(t, events, "warn")
	if event["agent"] != AgentIDClaude {
		t.Fatalf("agent = %v, want %q", event["agent"], AgentIDClaude)
	}
	if event["code"] != -32030 {
		t.Fatalf("code = %v, want -32030", event["code"])
	}
	msg, _ := event["msg"].(string)
	if !strings.Contains(msg, "security profile warning before handoff") || !strings.Contains(msg, "claude-hooks-enabled") {
		t.Fatalf("unexpected msg %q", msg)
	}
	if got := obs.counterTotal(MetricErrorCount, AgentIDClaude); got != 0 {
		t.Fatalf("error_count = %v, want 0", got)
	}
}

func TestRunCodex_PreflightBlocksStrictCriticalProfileBeforeHandoff(t *testing.T) {
	argsFile := filepath.Join(t.TempDir(), "args.tsv")
	withCodexTestBinary(t, codexRecorderScript(argsFile, `printf '%s\n' '{"type":"message","content":"should not run"}'`))

	oldCheck := externalCLIPreflightCheck
	externalCLIPreflightCheck = func(context.Context, string, string) externalCLIPreflightResult {
		return externalCLIPreflightResult{
			Mode: security.ModeStrict,
			Warnings: []clientprofiles.ProfileWarning{{
				Client:   AgentIDCodex,
				ID:       "codex-danger-full-access",
				Severity: clientprofiles.SeverityCritical,
				Summary:  "Codex sandbox is configured for unrestricted filesystem access.",
			}},
		}
	}
	t.Cleanup(func() { externalCLIPreflightCheck = oldCheck })

	pusher, obs := runCodexPrompts(t, context.Background(), "hi")
	events := pusher.snapshot()
	errEvent, ok := findCodexEvent(events, "err")
	if !ok {
		t.Fatalf("expected err event, got %v", events)
	}
	if got := codexEventCode(errEvent); got != externalCLIPreflightBlockCode {
		t.Fatalf("err code = %d, want %d; event=%v", got, externalCLIPreflightBlockCode, errEvent)
	}
	if _, ok := findCodexEvent(events, "chunk_end"); !ok {
		t.Fatalf("expected chunk_end after block, got %v", events)
	}
	if data, err := os.ReadFile(argsFile); err == nil && len(data) > 0 {
		t.Fatalf("codex binary was invoked despite block; calls=%q", string(data))
	}
	if got := obs.counterTotal(MetricErrorCount, AgentIDCodex); got != 1 {
		t.Fatalf("error_count = %v, want 1", got)
	}
}

func TestRunCodex_PreflightWarnsThenHandsOffInWarnMode(t *testing.T) {
	argsFile := filepath.Join(t.TempDir(), "args.tsv")
	withCodexTestBinary(t, codexRecorderScript(argsFile, `printf '%s\n' '{"type":"message","content":"ok"}'`))

	oldCheck := externalCLIPreflightCheck
	externalCLIPreflightCheck = func(context.Context, string, string) externalCLIPreflightResult {
		return externalCLIPreflightResult{
			Mode: security.ModeWarn,
			Warnings: []clientprofiles.ProfileWarning{{
				Client:   AgentIDCodex,
				ID:       "codex-no-approval",
				Severity: clientprofiles.SeverityHigh,
				Summary:  "Codex approval policy disables human approval prompts.",
			}},
		}
	}
	t.Cleanup(func() { externalCLIPreflightCheck = oldCheck })

	pusher, obs := runCodexPrompts(t, context.Background(), "hi")
	events := pusher.snapshot()
	warnAt := externalPreflightEventIndex(events, "warn")
	dataAt := externalPreflightEventIndex(events, "data")
	if warnAt < 0 {
		t.Fatalf("expected warn event, got %v", events)
	}
	if dataAt < 0 {
		t.Fatalf("expected data event after handoff, got %v", events)
	}
	if warnAt > dataAt {
		t.Fatalf("warning occurred after handoff data; warn=%d data=%d events=%v", warnAt, dataAt, events)
	}
	if got := decodeCodexData(events); got != "ok" {
		t.Fatalf("decoded data = %q, want ok; events=%v", got, events)
	}
	calls := readCodexArgCalls(t, argsFile)
	if len(calls) != 1 {
		t.Fatalf("codex calls = %v, want one", calls)
	}
	if got := obs.counterTotal(MetricErrorCount, AgentIDCodex); got != 0 {
		t.Fatalf("error_count = %v, want 0", got)
	}
}

func requireExternalPreflightEvent(t *testing.T, events []map[string]any, typ string) map[string]any {
	t.Helper()
	for _, event := range events {
		if event["t"] == typ {
			return event
		}
	}
	t.Fatalf("expected %s event, got %v", typ, events)
	return nil
}

func externalPreflightEventIndex(events []map[string]any, typ string) int {
	for i, event := range events {
		if event["t"] == typ {
			return i
		}
	}
	return -1
}
