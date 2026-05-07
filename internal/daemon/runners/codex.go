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
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// codexJSONEvent mirrors the subset of the `codex exec --json` event
// stream we care about for the daemon-side bridge. Cribbed (without
// lifting wholesale) from the legacy REPL runner.
type codexJSONEvent struct {
	Type           string          `json:"type"`
	Content        string          `json:"content,omitempty"`
	Message        string          `json:"message,omitempty"`
	Text           string          `json:"text,omitempty"`
	Summary        string          `json:"summary,omitempty"`
	Delta          string          `json:"delta,omitempty"`
	ThreadID       string          `json:"thread_id,omitempty"`
	SessionID      string          `json:"session_id,omitempty"`
	ConversationID string          `json:"conversation_id,omitempty"`
	Item           json.RawMessage `json:"item,omitempty"`
	Error          json.RawMessage `json:"error,omitempty"`
}

type codexJSONItem struct {
	ItemType string `json:"item_type,omitempty"`
	Type     string `json:"type,omitempty"`
	Text     string `json:"text,omitempty"`
	Content  string `json:"content,omitempty"`
	Message  string `json:"message,omitempty"`
}

// codexBinary is the executable name; var (not const) so tests can swap it.
var codexBinary = "codex"

type codexSessionState struct {
	sessionID string
	model     string
}

// RunCodex is the daemon-side codex session loop. It reads prompts from
// `input`, spawns one `codex exec --json --color never --skip-git-repo-check`
// subprocess per prompt, decodes the assistant text from each NDJSON line,
// and pushes {"t":"data","b64":...} events to the stream. After the
// subprocess exits a final {"t":"chunk_end","cost_usd":0} marks
// end-of-response.
//
// Codex's JSON event stream does not currently expose token usage, so
// only error_count is observed via `metrics` (non-nil); tokens_in /
// tokens_out / cost_usd are left for a future codex CLI release that
// surfaces them.
//
// Lifecycle:
//   - One subprocess per prompt; if Codex emits a thread/session id, later
//     prompts use `codex exec resume <id>` to preserve native continuity.
//   - When `input` is closed, RunCodex pushes {"t":"end"} and returns.
//   - The caller (AgentRegistry) is responsible for Close()ing the stream.
func RunCodex(ctx context.Context, input <-chan []byte, stream Pusher, metrics MetricsObserver) {
	state := &codexSessionState{}
	for prompt := range input {
		if stream == nil {
			continue
		}
		runCodexOnce(ctx, prompt, stream, metrics, state)
	}
	if stream != nil {
		stream.Push(map[string]any{"t": "end"})
	}
}

func runCodexOnce(parent context.Context, prompt []byte, stream Pusher, metrics MetricsObserver, state *codexSessionState) {
	text := strings.TrimRight(string(prompt), "\r\n")
	if text == "" {
		stream.Push(map[string]any{"t": "chunk_end", "cost_usd": 0.0, "input_tokens": 0, "output_tokens": 0, "total_tokens": 0})
		return
	}
	if state == nil {
		state = &codexSessionState{}
	}

	spanCtx, span := startDispatchSpan(parent, AgentIDCodex, "")
	ctx, cancel := contextWithOptionalTimeout(spanCtx, codexRequestTimeout())
	defer cancel()

	spanErr := ""
	defer func() {
		endDispatchSpan(span, 0, 0, 0, spanErr)
		stream.Push(map[string]any{"t": "chunk_end", "cost_usd": 0.0, "input_tokens": 0, "output_tokens": 0, "total_tokens": 0})
	}()

	model := codexModelFromEnv()
	if state.sessionID != "" && model != state.model {
		state.sessionID = ""
	}
	state.model = model
	pushModel(stream, AgentIDCodex)

	cwd, _ := os.Getwd()
	cmd := exec.CommandContext(ctx, codexBinary, buildCodexCmdArgsWithSession(text, cwd, codexModelExtraArgs(model), state.sessionID)...)
	cmd.Env = safeRunnerEnv()
	cmd.WaitDelay = 5 * time.Second
	if cwd != "" {
		cmd.Dir = cwd
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		observeError(metrics, AgentIDCodex)
		spanErr = err.Error()
		stream.Push(classifyCodexStartError(err))
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		observeError(metrics, AgentIDCodex)
		spanErr = err.Error()
		stream.Push(classifyCodexStartError(err))
		return
	}
	if err := cmd.Start(); err != nil {
		observeError(metrics, AgentIDCodex)
		spanErr = err.Error()
		if ctxErr := ctx.Err(); ctxErr != nil {
			stream.Push(classifyDispatchError(AgentIDCodex, ctxErr))
		} else {
			stream.Push(classifyCodexStartError(err))
		}
		return
	}

	// Capture stderr; check for proxy-block and session-limit signals at the end.
	// sawProxyBlock is sync/atomic.Bool — single field, two goroutines, no
	// need for a separate mutex (Code-quality B5 / Reviewer #17).
	var (
		stderrLines   []string
		stderrMu      sync.Mutex
		stderrWg      sync.WaitGroup
		sawProxyBlock atomic.Bool
		sawSessionErr atomic.Bool
	)
	stderrWg.Add(1)
	go func() {
		defer stderrWg.Done()
		s := bufio.NewScanner(stderr)
		s.Buffer(make([]byte, 0, 64*1024), 256*1024)
		for s.Scan() {
			line := strings.TrimSpace(s.Text())
			if line == "" {
				continue
			}
			if codexLineLooksProxyBlocked(line) {
				sawProxyBlock.Store(true)
				continue
			}
			if codexLineSignalsSessionFailure(line) {
				sawSessionErr.Store(true)
			}
			if codexLineLooksBackendModelNoise(line) {
				continue
			}
			stderrMu.Lock()
			stderrLines = append(stderrLines, line)
			stderrMu.Unlock()
			stream.Push(encodeThinking(line))
			slog.Debug("codex stderr", "line", line, "agent", AgentIDCodex)
		}
	}()

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 256*1024), 1024*1024)

	var sawSessionLimit bool
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if sessionID := extractCodexSessionID(line); sessionID != "" {
			state.sessionID = sessionID
		}
		if model := extractModelFromJSONLine(line); model != "" {
			pushObservedModel(stream, model)
		}
		if codexLineLooksProxyBlocked(line) {
			sawProxyBlock.Store(true)
			continue
		}
		if codexLineSignalsSessionLimit(line) {
			sawSessionLimit = true
		}
		if codexLineSignalsSessionFailure(line) {
			sawSessionErr.Store(true)
		}
		if text, ok := extractCodexThinkingText(line); ok {
			stream.Push(encodeThinking(text))
			continue
		}
		if text, ok := extractCodexAssistantText(line); ok {
			stream.Push(encodeData(text))
		}
	}
	scanErr := scanner.Err()

	waitErr := cmd.Wait()
	stderrWg.Wait()
	stderrMu.Lock()
	lines := append([]string(nil), stderrLines...)
	stderrMu.Unlock()

	if sawProxyBlock.Load() {
		observeError(metrics, AgentIDCodex)
		spanErr = "proxy_block"
		stream.Push(map[string]any{
			"t":     "err",
			"agent": AgentIDCodex,
			"code":  -32014,
			"msg":   "codex: blocked by Zscaler/proxy — open ChatGPT in a browser, approve the security prompt, then retry",
		})
	} else if sawSessionLimit || sawSessionErr.Load() {
		observeError(metrics, AgentIDCodex)
		spanErr = "session_limit"
		stream.Push(map[string]any{
			"t":     "err",
			"agent": AgentIDCodex,
			"code":  -32015,
			"msg":   "codex: session failure or limit reached",
		})
	} else if ctxErr := ctx.Err(); ctxErr != nil {
		observeError(metrics, AgentIDCodex)
		spanErr = ctxErr.Error()
		stream.Push(classifyDispatchError(AgentIDCodex, ctxErr))
	} else if scanErr != nil {
		observeError(metrics, AgentIDCodex)
		spanErr = scanErr.Error()
		stream.Push(map[string]any{
			"t":     "err",
			"agent": AgentIDCodex,
			"code":  -32012,
			"msg":   "codex: stream read failed — " + scrubBearer(scanErr.Error()),
		})
	} else if waitErr != nil {
		observeError(metrics, AgentIDCodex)
		spanErr = waitErr.Error()
		stream.Push(map[string]any{"t": "err", "agent": AgentIDCodex, "code": -32010, "msg": exitMsg("codex", waitErr, lines)})
	}
}

func codexRequestTimeout() time.Duration {
	return runnerRequestTimeout("CODEX_TIMEOUT")
}

func codexModelFromEnv() string {
	model := strings.TrimSpace(os.Getenv("CODEX_MODEL"))
	if model == "" {
		model = strings.TrimSpace(os.Getenv("OPENAI_MODEL"))
	}
	return model
}

func codexModelExtraArgsFromEnv() []string {
	return codexModelExtraArgs(codexModelFromEnv())
}

func codexModelExtraArgs(model string) []string {
	if model == "" {
		return nil
	}
	return []string{"--model", model}
}

// buildCodexCmdArgs assembles the codex CLI argv for a fresh exec turn.
func buildCodexCmdArgs(prompt, cwd string, extra []string) []string {
	return buildCodexCmdArgsWithSession(prompt, cwd, extra, "")
}

// buildCodexCmdArgsWithSession assembles the codex CLI argv. Root-level
// defaults (`--sandbox workspace-write --ask-for-approval never`) are placed
// before `exec`, because recent Codex CLIs reject --ask-for-approval when it
// appears after `codex exec`.
//
// -C sets the working root so codex sees the project directory regardless
// of what directory the daemon was launched from.
//
// Without --sandbox/--ask-for-approval, recent codex CLI versions run in
// read-only / on-request mode and silently refuse tool execution.
func buildCodexCmdArgsWithSession(prompt, cwd string, extra []string, sessionID string) []string {
	rootArgs, execExtra := codexSplitRootArgs(extra)
	if !codexHasAnyFlag(extra, "--sandbox", "-s", "--full-auto", "--dangerously-bypass-approvals-and-sandbox") {
		rootArgs = append(rootArgs, "--sandbox", "workspace-write")
	}
	if !codexHasAnyFlag(extra, "--ask-for-approval", "-a", "--full-auto", "--dangerously-bypass-approvals-and-sandbox") {
		rootArgs = append(rootArgs, "--ask-for-approval", "never")
	}

	args := append([]string(nil), rootArgs...)
	if sessionID != "" {
		args = append(args, "exec", "resume", "--json", "--skip-git-repo-check")
		args = append(args, execExtra...)
		args = append(args, sessionID, "--", prompt)
		return args
	}

	args = append(args, "exec", "--json", "--color", "never", "--skip-git-repo-check")
	if cwd != "" && !codexHasAnyFlag(extra, "-C", "--cd") {
		args = append(args, "-C", cwd)
	}
	args = append(args, execExtra...)
	args = append(args, "--", prompt)
	return args
}

func codexSplitRootArgs(args []string) ([]string, []string) {
	var rootArgs, execArgs []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if codexIsRootFlag(arg) {
			rootArgs = append(rootArgs, arg)
			if codexFlagTakesValue(arg) && i+1 < len(args) {
				i++
				rootArgs = append(rootArgs, args[i])
			}
			continue
		}
		execArgs = append(execArgs, arg)
	}
	return rootArgs, execArgs
}

func codexIsRootFlag(arg string) bool {
	return arg == "--sandbox" || strings.HasPrefix(arg, "--sandbox=") ||
		arg == "-s" ||
		arg == "--ask-for-approval" || strings.HasPrefix(arg, "--ask-for-approval=") ||
		arg == "-a"
}

func codexFlagTakesValue(arg string) bool {
	return arg == "--sandbox" || arg == "-s" || arg == "--ask-for-approval" || arg == "-a"
}

func codexHasAnyFlag(args []string, flags ...string) bool {
	for _, flag := range flags {
		if codexHasFlag(args, flag) {
			return true
		}
	}
	return false
}

func codexHasFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag || strings.HasPrefix(a, flag+"=") {
			return true
		}
	}
	return false
}

func classifyCodexStartError(err error) map[string]any {
	msg := "codex: failed to start — try again"
	if errors.Is(err, exec.ErrNotFound) {
		msg = "codex: could not start — " + installHint("codex")
	} else if err != nil {
		msg = "codex: failed to start — " + scrubBearer(err.Error())
	}
	return map[string]any{
		"t":     "err",
		"agent": AgentIDCodex,
		"code":  -32016,
		"msg":   msg,
	}
}

// codexLineLooksProxyBlocked returns true when a stdout/stderr line carries
// the signature of a Zscaler / corporate-proxy interception of the codex
// CLI's connection to chatgpt.com. Mirrors REPL's check.
func codexLineLooksProxyBlocked(line string) bool {
	lower := strings.ToLower(line)
	return strings.Contains(lower, "zscaler") ||
		strings.Contains(lower, "internet security by zscaler") ||
		strings.Contains(lower, "unexpected status 403 forbidden") ||
		strings.Contains(lower, "307 temporary redirect") ||
		(strings.Contains(lower, "chatgpt.com/backend-api/codex") && strings.Contains(lower, "failed to connect"))
}

func codexLineLooksBackendModelNoise(line string) bool {
	lower := strings.ToLower(line)
	return strings.Contains(lower, "chatgpt.com/backend-api/codex/models") ||
		strings.Contains(lower, "backend-api/codex/models?client_version=") ||
		(strings.Contains(lower, "url: https://chatgpt.com/backend-api/codex") && strings.Contains(lower, "color:")) ||
		(strings.Contains(lower, "white-space:") && strings.Contains(lower, "max-width:"))
}

// codexLineSignalsSessionLimit returns true when a stdout JSON event line
// indicates that the codex session has hit its turn or context limit.
// Mirrors REPL's runner_codex.go check.
func codexLineSignalsSessionLimit(line string) bool {
	var evt codexJSONEvent
	if err := json.Unmarshal([]byte(line), &evt); err != nil {
		return false
	}
	switch evt.Type {
	case "max_turns", "context_length_exceeded":
		return true
	case "error":
		lower := strings.ToLower(codexFirstNonEmpty(evt.Message, evt.Content, evt.Text))
		return strings.Contains(lower, "context") || strings.Contains(lower, "limit")
	}
	return false
}

func codexLineSignalsSessionFailure(line string) bool {
	if codexLineSignalsSessionLimit(line) {
		return true
	}
	lower := strings.ToLower(line)
	return strings.Contains(lower, "thread/start failed") ||
		strings.Contains(lower, "thread/resume failed") ||
		strings.Contains(lower, "cannot access session files") ||
		strings.Contains(lower, "session not found") ||
		strings.Contains(lower, "cannot resume") ||
		strings.Contains(lower, "context_window_exceeded") ||
		strings.Contains(lower, "usage_limit_exceeded")
}

func extractCodexSessionID(line string) string {
	var evt codexJSONEvent
	if err := json.Unmarshal([]byte(line), &evt); err != nil {
		return ""
	}
	return codexFirstNonEmpty(evt.ThreadID, evt.SessionID, evt.ConversationID)
}

// extractCodexAssistantText returns the assistant text from a codex
// JSON event line, or false if the line is not an assistant-bearing
// event with non-empty text.
func extractCodexAssistantText(line string) (string, bool) {
	var evt codexJSONEvent
	if err := json.Unmarshal([]byte(line), &evt); err != nil {
		return "", false
	}

	switch evt.Type {
	case "message", "assistant", "text", "response.output_text.done", "agent_message":
		out := codexFirstNonEmpty(evt.Content, evt.Message, evt.Text)
		return out, out != ""
	case "response.output_text.delta":
		return evt.Delta, evt.Delta != ""
	case "item.completed":
		var item codexJSONItem
		if len(evt.Item) > 0 && json.Unmarshal(evt.Item, &item) == nil {
			if item.ItemType == "assistant_message" || item.Type == "assistant_message" || item.Type == "message" {
				out := codexFirstNonEmpty(item.Text, item.Content, item.Message)
				return out, out != ""
			}
		}
	}

	if len(evt.Error) > 0 {
		var payload map[string]any
		if json.Unmarshal(evt.Error, &payload) == nil {
			if msg, ok := payload["message"].(string); ok && msg != "" {
				return msg, true
			}
		}
	}
	return "", false
}

func extractCodexThinkingText(line string) (string, bool) {
	var evt codexJSONEvent
	if err := json.Unmarshal([]byte(line), &evt); err != nil {
		return "", false
	}

	switch evt.Type {
	case "reasoning", "reasoning.delta", "reasoning.summary", "response.reasoning_summary.delta", "response.reasoning_summary.done":
		out := codexFirstNonEmpty(evt.Delta, evt.Summary, evt.Text, evt.Message, evt.Content)
		return out, out != ""
	case "item.completed":
		var item codexJSONItem
		if len(evt.Item) > 0 && json.Unmarshal(evt.Item, &item) == nil {
			if strings.Contains(item.ItemType, "reasoning") || strings.Contains(item.Type, "reasoning") {
				out := codexFirstNonEmpty(item.Text, item.Content, item.Message)
				return out, out != ""
			}
		}
	}

	return "", false
}

func codexFirstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
