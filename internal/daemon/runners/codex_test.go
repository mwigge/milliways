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
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func withCodexTestBinary(t *testing.T, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "codex-test")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake codex: %v", err)
	}
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatalf("chmod fake codex: %v", err)
	}
	prev := codexBinary
	codexBinary = path
	t.Cleanup(func() { codexBinary = prev })
	return path
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func codexRecorderScript(argsFile, body string) string {
	return "#!/bin/sh\n" +
		"printf 'CALL' >> " + shellQuote(argsFile) + "\n" +
		"for arg in \"$@\"; do printf '\\t%s' \"$arg\" >> " + shellQuote(argsFile) + "; done\n" +
		"printf '\\n' >> " + shellQuote(argsFile) + "\n" +
		body + "\n"
}

func runCodexPrompts(t *testing.T, ctx context.Context, prompts ...string) (*fakePusher, *mockObserver) {
	t.Helper()
	pusher := &fakePusher{}
	obs := &mockObserver{}
	in := make(chan []byte, len(prompts))
	for _, prompt := range prompts {
		in <- []byte(prompt)
	}
	close(in)

	done := make(chan struct{})
	go func() {
		RunCodex(ctx, in, pusher, obs)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("RunCodex did not return")
	}
	return pusher, obs
}

func codexEventCode(event map[string]any) int {
	switch v := event["code"].(type) {
	case int:
		return v
	case float64:
		return int(v)
	default:
		return 0
	}
}

func findCodexEvent(events []map[string]any, typ string) (map[string]any, bool) {
	for _, event := range events {
		if event["t"] == typ {
			return event, true
		}
	}
	return nil, false
}

func decodeCodexData(events []map[string]any) string {
	var out strings.Builder
	for _, event := range events {
		if event["t"] != "data" {
			continue
		}
		b64, _ := event["b64"].(string)
		raw, err := base64.StdEncoding.DecodeString(b64)
		if err == nil {
			out.Write(raw)
		}
	}
	return out.String()
}

func decodeCodexThinking(events []map[string]any) string {
	var out strings.Builder
	for _, event := range events {
		if event["t"] != "thinking" {
			continue
		}
		b64, _ := event["b64"].(string)
		raw, err := base64.StdEncoding.DecodeString(b64)
		if err == nil {
			out.Write(raw)
			out.WriteByte('\n')
		}
	}
	return out.String()
}

func TestExtractCodexAssistantText_Message(t *testing.T) {
	t.Parallel()
	line := `{"type":"message","content":"hello world"}`
	got, ok := extractCodexAssistantText(line)
	if !ok {
		t.Fatalf("extractCodexAssistantText: expected ok=true")
	}
	if got != "hello world" {
		t.Errorf("text = %q, want %q", got, "hello world")
	}
}

func TestExtractCodexAssistantText_Delta(t *testing.T) {
	t.Parallel()
	line := `{"type":"response.output_text.delta","delta":"chunk"}`
	got, ok := extractCodexAssistantText(line)
	if !ok {
		t.Fatalf("expected ok=true for delta event")
	}
	if got != "chunk" {
		t.Errorf("text = %q, want %q", got, "chunk")
	}
}

func TestExtractCodexAssistantText_AgentMessage(t *testing.T) {
	t.Parallel()
	line := `{"type":"agent_message","message":"final answer"}`
	got, ok := extractCodexAssistantText(line)
	if !ok {
		t.Fatalf("expected ok=true for agent_message")
	}
	if got != "final answer" {
		t.Errorf("text = %q, want %q", got, "final answer")
	}
}

func TestExtractCodexAssistantText_ItemCompleted(t *testing.T) {
	t.Parallel()
	line := `{"type":"item.completed","item":{"item_type":"assistant_message","text":"done"}}`
	got, ok := extractCodexAssistantText(line)
	if !ok {
		t.Fatalf("expected ok=true for item.completed assistant_message")
	}
	if got != "done" {
		t.Errorf("text = %q, want %q", got, "done")
	}
}

func TestExtractCodexAssistantText_NotJSON(t *testing.T) {
	t.Parallel()
	if _, ok := extractCodexAssistantText("not json"); ok {
		t.Errorf("expected ok=false for non-JSON")
	}
}

func TestExtractCodexAssistantText_ReasoningSkipped(t *testing.T) {
	t.Parallel()
	// Non-message events (reasoning/tool/etc.) are not assistant text.
	line := `{"type":"reasoning","summary":"thinking..."}`
	if _, ok := extractCodexAssistantText(line); ok {
		t.Errorf("expected ok=false for reasoning event")
	}
}

func TestExtractCodexThinkingText_Reasoning(t *testing.T) {
	t.Parallel()
	line := `{"type":"reasoning","summary":"checking files"}`
	got, ok := extractCodexThinkingText(line)
	if !ok {
		t.Fatalf("expected ok=true for reasoning event")
	}
	if got != "checking files" {
		t.Errorf("thinking text = %q, want checking files", got)
	}
}

func TestExtractCodexAssistantText_ErrorEnvelope(t *testing.T) {
	t.Parallel()
	line := `{"type":"error","error":{"message":"something broke"}}`
	got, ok := extractCodexAssistantText(line)
	if !ok {
		t.Fatalf("expected ok=true for error envelope")
	}
	if got != "something broke" {
		t.Errorf("text = %q, want %q", got, "something broke")
	}
}

func TestExtractCodexSessionID(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		line string
		want string
	}{
		{"thread id", `{"type":"session_configured","thread_id":"thread-123"}`, "thread-123"},
		{"session id", `{"type":"session_configured","session_id":"sess-123"}`, "sess-123"},
		{"conversation id", `{"type":"thread.started","conversation_id":"conv-123"}`, "conv-123"},
		{"not json", `not json`, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := extractCodexSessionID(c.line); got != c.want {
				t.Errorf("extractCodexSessionID = %q, want %q", got, c.want)
			}
		})
	}
}

func TestCodexRequestTimeout(t *testing.T) {
	cases := []struct {
		raw  string
		want time.Duration
	}{
		{"", 0},
		{"off", 0},
		{"none", 0},
		{"0", 0},
		{"250ms", 250 * time.Millisecond},
		{"2", 2 * time.Second},
		{"bad", 0},
	}
	for _, c := range cases {
		t.Run(c.raw, func(t *testing.T) {
			t.Setenv("CODEX_TIMEOUT", c.raw)
			if got := codexRequestTimeout(); got != c.want {
				t.Errorf("codexRequestTimeout() = %v, want %v", got, c.want)
			}
		})
	}
}

func TestBuildCodexCmdArgs_DefaultsAreRootFlags(t *testing.T) {
	t.Parallel()

	args := buildCodexCmdArgs("do it", "/repo", nil)
	wantPrefix := []string{"--sandbox", "workspace-write", "--ask-for-approval", "never", "exec", "--json", "--color", "never", "--skip-git-repo-check", "-C", "/repo"}
	if len(args) < len(wantPrefix) {
		t.Fatalf("args too short: %v", args)
	}
	for i, want := range wantPrefix {
		if args[i] != want {
			t.Fatalf("args[%d] = %q, want %q; args=%v", i, args[i], want, args)
		}
	}
	if got := args[len(args)-2]; got != "--" {
		t.Errorf("penultimate arg = %q, want --; args=%v", got, args)
	}
	if got := args[len(args)-1]; got != "do it" {
		t.Errorf("last arg = %q, want prompt", got)
	}
}

func TestBuildCodexCmdArgs_RespectsRootOverrides(t *testing.T) {
	t.Parallel()

	extra := []string{"--ask-for-approval", "on-request", "--sandbox=read-only", "--model", "gpt-5"}
	args := buildCodexCmdArgs("p", "/repo", extra)
	joined := strings.Join(args, "\x00")
	if strings.Contains(joined, "--ask-for-approval\x00never") {
		t.Fatalf("default approval injected despite override: %v", args)
	}
	if strings.Contains(joined, "--sandbox\x00workspace-write") {
		t.Fatalf("default sandbox injected despite override: %v", args)
	}
	execIdx := indexCodexArg(args, "exec")
	askIdx := indexCodexArg(args, "--ask-for-approval")
	sandboxIdx := indexCodexArg(args, "--sandbox=read-only")
	if askIdx < 0 || sandboxIdx < 0 || execIdx < 0 {
		t.Fatalf("expected root override args before exec, got %v", args)
	}
	if askIdx > execIdx || sandboxIdx > execIdx {
		t.Fatalf("root overrides must be before exec, got %v", args)
	}
	if !codexArgsContainPair(args, "--model", "gpt-5") {
		t.Fatalf("exec extra --model missing: %v", args)
	}
}

func TestBuildCodexCmdArgs_ResumeUsesSessionID(t *testing.T) {
	t.Parallel()

	args := buildCodexCmdArgsWithSession("next prompt", "/repo", []string{"--model", "gpt-5"}, "thread-abc")
	if !containsCodexSubsequence(args, []string{"exec", "resume", "--json", "--skip-git-repo-check"}) {
		t.Fatalf("resume command shape missing: %v", args)
	}
	if !codexArgsContainPair(args, "--model", "gpt-5") {
		t.Fatalf("resume model arg missing: %v", args)
	}
	if !containsCodexSubsequence(args, []string{"thread-abc", "--", "next prompt"}) {
		t.Fatalf("resume session/prompt tail missing: %v", args)
	}
	if indexCodexArg(args, "-C") >= 0 {
		t.Fatalf("resume args should not include exec-only -C flag: %v", args)
	}
}

func TestCodexModelExtraArgsFromEnv(t *testing.T) {
	t.Setenv("CODEX_MODEL", "gpt-5.3-codex")
	t.Setenv("OPENAI_MODEL", "gpt-5.5")
	args := codexModelExtraArgsFromEnv()
	if !codexArgsContainPair(args, "--model", "gpt-5.3-codex") {
		t.Fatalf("CODEX_MODEL should win, got %v", args)
	}
}

func TestRunCodex_PassesConfiguredModel(t *testing.T) {
	t.Setenv("CODEX_MODEL", "gpt-5.3-codex")
	argsFile := filepath.Join(t.TempDir(), "args.tsv")
	withCodexTestBinary(t, codexRecorderScript(argsFile, `printf '%s\n' '{"type":"message","content":"ok"}'`))

	runCodexPrompts(t, context.Background(), "hello")

	data, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	args := strings.Split(strings.TrimSpace(strings.TrimPrefix(string(data), "CALL\t")), "\t")
	if !codexArgsContainPair(args, "--model", "gpt-5.3-codex") {
		t.Fatalf("codex exec did not receive configured model; args=%v", args)
	}
}

func TestRunCodex_ModelChangeStartsFreshSession(t *testing.T) {
	t.Setenv("CODEX_MODEL", "")
	t.Setenv("OPENAI_MODEL", "")
	argsFile := filepath.Join(t.TempDir(), "args.tsv")
	withCodexTestBinary(t, codexRecorderScript(argsFile, `printf '%s\n' '{"type":"session_configured","session_id":"sess-1"}'`))

	state := &codexSessionState{}
	runCodexOnce(context.Background(), []byte("first"), &fakePusher{}, &mockObserver{}, state, "")
	if state.sessionID == "" {
		t.Fatal("first run did not capture a session id")
	}

	t.Setenv("CODEX_MODEL", "gpt-5.5")
	runCodexOnce(context.Background(), []byte("second"), &fakePusher{}, &mockObserver{}, state, "")

	data, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("recorded calls = %d, want 2; data=%q", len(lines), string(data))
	}
	firstArgs := strings.Split(strings.TrimPrefix(lines[0], "CALL\t"), "\t")
	secondArgs := strings.Split(strings.TrimPrefix(lines[1], "CALL\t"), "\t")
	if containsCodexSubsequence(firstArgs, []string{"exec", "resume"}) {
		t.Fatalf("first call should be fresh, got %v", firstArgs)
	}
	if containsCodexSubsequence(secondArgs, []string{"exec", "resume"}) {
		t.Fatalf("model change should not resume old Codex session; args=%v", secondArgs)
	}
	if !codexArgsContainPair(secondArgs, "--model", "gpt-5.5") {
		t.Fatalf("second call missing new model arg; args=%v", secondArgs)
	}
}

func TestRunCodex_SuppressesBackendModelFetchNoise(t *testing.T) {
	withCodexTestBinary(t, codexRecorderScript(filepath.Join(t.TempDir(), "args.tsv"), `
printf '%s\n' 'body { color:#009dd0; } url: https://chatgpt.com/backend-api/codex/models?client_version=0.125.0' >&2
printf '%s\n' '{"type":"message","content":"ok"}'
`))

	pusher, _ := runCodexPrompts(t, context.Background(), "hello")
	events := pusher.snapshot()
	thinking := decodeCodexThinking(events)
	if strings.Contains(thinking, "backend-api/codex/models") || strings.Contains(thinking, "color:#009dd0") {
		t.Fatalf("backend model fetch noise leaked as thinking: %q", thinking)
	}
	if got := decodeCodexData(events); got != "ok" {
		t.Fatalf("assistant data = %q, want ok", got)
	}
}

func TestRunCodex_SuppressesProxyRetryNoiseAfterActionableError(t *testing.T) {
	withCodexTestBinary(t, codexRecorderScript(filepath.Join(t.TempDir(), "args.tsv"), `
printf '%s\n' '2026-05-05T20:30:35Z ERROR codex_api::endpoint::responses_websocket: failed to connect to websocket: HTTP error: 307 Temporary Redirect, url: wss://chatgpt.com/backend-api/codex/responses' >&2
exit 1
`))

	pusher, _ := runCodexPrompts(t, context.Background(), "hello")
	events := pusher.snapshot()
	thinking := decodeCodexThinking(events)
	if strings.Contains(thinking, "307 Temporary Redirect") || strings.Contains(thinking, "backend-api/codex/responses") {
		t.Fatalf("proxy retry noise leaked as thinking: %q", thinking)
	}
	errEvent, ok := findCodexEvent(events, "err")
	if !ok {
		t.Fatalf("expected actionable err event, got %+v", events)
	}
	msg, _ := errEvent["msg"].(string)
	if !strings.Contains(msg, "blocked by Zscaler/proxy") {
		t.Fatalf("error message = %q, want proxy guidance", msg)
	}
}

// TestRunCodex_InputClose_PushesEnd verifies that closing the input
// channel triggers a final {"t":"end"} push.
func TestRunCodex_InputClose_PushesEnd(t *testing.T) {
	t.Parallel()
	pusher := &fakePusher{}
	in := make(chan []byte)
	done := make(chan struct{})
	go func() {
		RunCodex(context.Background(), in, pusher, nil)
		close(done)
	}()
	close(in)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("RunCodex did not return after input close")
	}
	events := pusher.snapshot()
	if len(events) == 0 {
		t.Fatalf("expected at least one event, got 0")
	}
	last := events[len(events)-1]
	if last["t"] != "end" {
		t.Errorf("last event t = %v, want end", last["t"])
	}
}

// TestRunCodex_NilStream verifies nil-stream guard does not panic.
func TestRunCodex_NilStream(t *testing.T) {
	t.Parallel()
	in := make(chan []byte, 1)
	in <- []byte("hi")
	close(in)
	done := make(chan struct{})
	go func() {
		RunCodex(context.Background(), in, nil, nil)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("RunCodex(nil stream) did not return")
	}
}

func TestRunCodex_EmptyPromptPushesChunkEnd(t *testing.T) {
	pusher, _ := runCodexPrompts(t, context.Background(), "\n")
	events := pusher.snapshot()
	if _, ok := findCodexEvent(events, "chunk_end"); !ok {
		t.Fatalf("expected chunk_end for empty prompt, got %v", events)
	}
	if last := events[len(events)-1]; last["t"] != "end" {
		t.Fatalf("last event = %v, want end", last)
	}
}

// TestRunCodex_NoBinary asserts that a missing codex binary surfaces
// an error_count tick (and no token observations).
func TestRunCodex_NoBinary(t *testing.T) {
	prev := codexBinary
	codexBinary = "/no/such/binary/that/should/not/exist"
	defer func() { codexBinary = prev }()

	pusher := &fakePusher{}
	obs := &mockObserver{}
	in := make(chan []byte, 1)
	in <- []byte("hi")
	close(in)

	done := make(chan struct{})
	go func() {
		RunCodex(context.Background(), in, pusher, obs)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("RunCodex did not return")
	}

	if got := obs.counterTotal(MetricErrorCount, AgentIDCodex); got < 1 {
		t.Errorf("error_count total = %v, want >= 1", got)
	}
	events := pusher.snapshot()
	errEvent, ok := findCodexEvent(events, "err")
	if !ok {
		t.Fatalf("expected err event, got %v", events)
	}
	if got := codexEventCode(errEvent); got != -32016 {
		t.Fatalf("err code = %d, want -32016; event=%v", got, errEvent)
	}
	if _, ok := findCodexEvent(events, "chunk_end"); !ok {
		t.Fatalf("expected chunk_end after start failure, got %v", events)
	}
	if n := obs.counterCount(MetricTokensIn, AgentIDCodex); n != 0 {
		t.Errorf("expected no tokens_in observations for codex, got %d", n)
	}
}

func TestRunCodex_StreamsJSONAndRecordsArgs(t *testing.T) {
	argsFile := filepath.Join(t.TempDir(), "args.tsv")
	withCodexTestBinary(t, codexRecorderScript(argsFile, strings.Join([]string{
		`printf '%s\n' '{"type":"session_configured","thread_id":"thread-xyz"}'`,
		`printf '%s\n' '{"type":"response.output_text.delta","delta":"hello"}'`,
		`printf '%s\n' '{"type":"item.completed","item":{"item_type":"assistant_message","text":" world"}}'`,
	}, "\n")))

	pusher, obs := runCodexPrompts(t, context.Background(), "say hi")
	events := pusher.snapshot()
	if got := decodeCodexData(events); got != "hello world" {
		t.Fatalf("decoded data = %q, want hello world; events=%v", got, events)
	}
	if _, ok := findCodexEvent(events, "chunk_end"); !ok {
		t.Fatalf("expected chunk_end, got %v", events)
	}
	if got := obs.counterTotal(MetricErrorCount, AgentIDCodex); got != 0 {
		t.Fatalf("error_count = %v, want 0", got)
	}

	calls := readCodexArgCalls(t, argsFile)
	if len(calls) != 1 {
		t.Fatalf("arg calls = %v, want one call", calls)
	}
	if !containsCodexSubsequence(calls[0], []string{"--ask-for-approval", "never", "exec", "--json", "--color", "never", "--skip-git-repo-check"}) {
		t.Fatalf("first call missing expected argv shape: %v", calls[0])
	}
	if calls[0][len(calls[0])-1] != "say hi" {
		t.Fatalf("prompt arg = %q, want say hi; args=%v", calls[0][len(calls[0])-1], calls[0])
	}
}

func TestRunCodex_ResumesAfterSessionID(t *testing.T) {
	t.Setenv("CODEX_MODEL", "gpt-5.5")
	t.Setenv("OPENAI_MODEL", "")
	argsFile := filepath.Join(t.TempDir(), "args.tsv")
	withCodexTestBinary(t, codexRecorderScript(argsFile, strings.Join([]string{
		`printf '%s\n' '{"type":"session_configured","thread_id":"thread-abc"}'`,
		`printf '%s\n' '{"type":"response.output_text.delta","delta":"ok"}'`,
	}, "\n")))

	runCodexPrompts(t, context.Background(), "first", "second")
	calls := readCodexArgCalls(t, argsFile)
	if len(calls) != 2 {
		t.Fatalf("arg calls = %v, want two calls", calls)
	}
	if containsCodexSubsequence(calls[0], []string{"exec", "resume"}) {
		t.Fatalf("first call should be fresh exec, got %v", calls[0])
	}
	if !containsCodexSubsequence(calls[1], []string{"exec", "resume", "--json", "--skip-git-repo-check"}) ||
		!containsCodexSubsequence(calls[1], []string{"thread-abc", "--", "second"}) {
		t.Fatalf("second call should resume thread-abc, got %v", calls[1])
	}
}

func TestRunCodex_TimeoutUsesConfiguredEnv(t *testing.T) {
	withCodexTestBinary(t, "#!/bin/sh\nexec sleep 5\n")
	t.Setenv("CODEX_TIMEOUT", "20ms")

	pusher, obs := runCodexPrompts(t, context.Background(), "slow")
	events := pusher.snapshot()
	errEvent, ok := findCodexEvent(events, "err")
	if !ok {
		t.Fatalf("expected timeout err event, got %v", events)
	}
	if got := codexEventCode(errEvent); got != -32009 {
		t.Fatalf("err code = %d, want -32009; event=%v", got, errEvent)
	}
	if _, ok := findCodexEvent(events, "chunk_end"); !ok {
		t.Fatalf("expected chunk_end after timeout, got %v", events)
	}
	if got := obs.counterTotal(MetricErrorCount, AgentIDCodex); got < 1 {
		t.Fatalf("error_count = %v, want >= 1", got)
	}
}

func TestRunCodex_ParentCancellationIsClassified(t *testing.T) {
	withCodexTestBinary(t, "#!/bin/sh\nexec sleep 5\n")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	pusher, _ := runCodexPrompts(t, ctx, "cancelled")
	events := pusher.snapshot()
	errEvent, ok := findCodexEvent(events, "err")
	if !ok {
		t.Fatalf("expected cancellation err event, got %v", events)
	}
	if got := codexEventCode(errEvent); got != -32008 {
		t.Fatalf("err code = %d, want -32008; event=%v", got, errEvent)
	}
	if _, ok := findCodexEvent(events, "chunk_end"); !ok {
		t.Fatalf("expected chunk_end after cancellation, got %v", events)
	}
}

func TestRunCodex_ProxyAndSessionFailuresAreClassified(t *testing.T) {
	cases := []struct {
		name     string
		script   string
		wantCode int
		wantMsg  string
	}{
		{
			name: "proxy",
			script: strings.Join([]string{
				"#!/bin/sh",
				"echo 'unexpected status 403 forbidden from Zscaler' >&2",
				"exit 1",
			}, "\n"),
			wantCode: -32014,
			wantMsg:  "Zscaler",
		},
		{
			name: "session",
			script: strings.Join([]string{
				"#!/bin/sh",
				"echo 'thread/start failed: cannot access session files' >&2",
				"exit 1",
			}, "\n"),
			wantCode: -32015,
			wantMsg:  "session",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			withCodexTestBinary(t, c.script)
			pusher, obs := runCodexPrompts(t, context.Background(), "hi")
			events := pusher.snapshot()
			errEvent, ok := findCodexEvent(events, "err")
			if !ok {
				t.Fatalf("expected err event, got %v", events)
			}
			if got := codexEventCode(errEvent); got != c.wantCode {
				t.Fatalf("err code = %d, want %d; event=%v", got, c.wantCode, errEvent)
			}
			if msg, _ := errEvent["msg"].(string); !strings.Contains(msg, c.wantMsg) {
				t.Fatalf("err msg = %q, want contains %q", msg, c.wantMsg)
			}
			if _, ok := findCodexEvent(events, "chunk_end"); !ok {
				t.Fatalf("expected chunk_end after err, got %v", events)
			}
			if got := obs.counterTotal(MetricErrorCount, AgentIDCodex); got < 1 {
				t.Fatalf("error_count = %v, want >= 1", got)
			}
		})
	}
}

// Sanity: encodeData is shared with claude — confirm it produces the
// {"t":"data","b64":...} shape codex events use.
func TestEncodeData_Codex(t *testing.T) {
	t.Parallel()
	got := encodeData("codex hi")
	if got["t"] != "data" {
		t.Errorf("t = %v, want data", got["t"])
	}
	want := base64.StdEncoding.EncodeToString([]byte("codex hi"))
	if got["b64"] != want {
		t.Errorf("b64 = %v, want %v", got["b64"], want)
	}
}

func readCodexArgCalls(t *testing.T, path string) [][]string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	calls := make([][]string, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if parts[0] != "CALL" {
			t.Fatalf("bad args line %q", line)
		}
		calls = append(calls, parts[1:])
	}
	return calls
}

func indexCodexArg(args []string, want string) int {
	for i, arg := range args {
		if arg == want {
			return i
		}
	}
	return -1
}

func codexArgsContainPair(args []string, flag, value string) bool {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == flag && args[i+1] == value {
			return true
		}
	}
	return false
}

func containsCodexSubsequence(args, want []string) bool {
	if len(want) == 0 {
		return true
	}
	for start := 0; start+len(want) <= len(args); start++ {
		ok := true
		for i := range want {
			if args[start+i] != want[i] {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}
