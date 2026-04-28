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

package repl

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
)

type codexModelEntry struct {
	ID   string
	Note string
}

// CodexModelCatalog lists the OpenAI models supported by the codex CLI via /model.
var CodexModelCatalog = []codexModelEntry{
	{"o4-mini", "fast reasoning"},
	{"o3", "strong reasoning"},
	{"o3-mini", "fast reasoning"},
	{"o1", "strong reasoning"},
	{"gpt-4.1", ""},
	{"gpt-4.1-mini", ""},
	{"gpt-4.1-nano", ""},
	{"gpt-4o", ""},
}

type CodexRunner struct {
	binary        string
	model         string
	profile       string
	sandbox       string
	approval      string
	reasoningMode CodexReasoningMode
	search        bool
	images        []string
}

var ErrCodexProxyBlocked = errors.New("codex connection blocked by proxy")

type CodexReasoningMode string

const (
	CodexReasoningOff     CodexReasoningMode = "off"
	CodexReasoningSummary CodexReasoningMode = "summary"
	CodexReasoningVerbose CodexReasoningMode = "verbose"
)

func NewCodexRunner() *CodexRunner {
	return &CodexRunner{
		binary:        "codex",
		reasoningMode: CodexReasoningVerbose,
	}
}

func (r *CodexRunner) Name() string { return "codex" }

func (r *CodexRunner) Execute(ctx context.Context, req DispatchRequest, out io.Writer) error {
	args := r.execArgs(buildTextPrompt(req))
	// Insert --image flags before the final "--" + prompt argument.
	// execArgs always ends with ["--", prompt]; find the "--" sentinel.
	if len(req.Attachments) > 0 {
		insertAt := len(args) - 2 // position of "--" separator
		if insertAt < 0 {
			insertAt = 0
		}
		extra := make([]string, 0, len(req.Attachments)*2)
		for _, a := range req.Attachments {
			extra = append(extra, "--image", a.FilePath)
		}
		args = append(args[:insertAt], append(extra, args[insertAt:]...)...)
	}
	cmd := exec.CommandContext(ctx, r.binary, args...)
	return runCodexJSON(ctx, cmd, out, r.reasoningMode)
}

func (r *CodexRunner) execArgs(prompt string) []string {
	args := []string{"exec", "--json", "--color", "never", "--skip-git-repo-check"}
	if r.model != "" {
		args = append(args, "--model", r.model)
	}
	if r.profile != "" {
		args = append(args, "--profile", r.profile)
	}
	if r.sandbox != "" {
		args = append(args, "--sandbox", r.sandbox)
	}
	if r.approval != "" {
		args = append(args, "--ask-for-approval", r.approval)
	}
	if r.search {
		args = append(args, "--search")
	}
	for _, image := range r.images {
		args = append(args, "--image", image)
	}
	return append(args, "--", prompt)
}

func (r *CodexRunner) SetModel(model string) {
	r.model = strings.TrimSpace(model)
}

func (r *CodexRunner) SetProfile(profile string) {
	r.profile = strings.TrimSpace(profile)
}

func (r *CodexRunner) SetSandbox(sandbox string) {
	r.sandbox = strings.TrimSpace(sandbox)
}

func (r *CodexRunner) SetApproval(approval string) {
	r.approval = strings.TrimSpace(approval)
}

func (r *CodexRunner) SetSearch(enabled bool) {
	r.search = enabled
}

func (r *CodexRunner) SetReasoningMode(mode CodexReasoningMode) {
	switch mode {
	case CodexReasoningOff, CodexReasoningSummary, CodexReasoningVerbose:
		r.reasoningMode = mode
	default:
		r.reasoningMode = CodexReasoningVerbose
	}
}

func (r *CodexRunner) AddImage(path string) {
	path = strings.TrimSpace(path)
	if path == "" {
		return
	}
	r.images = append(r.images, path)
}

func (r *CodexRunner) ClearImages() {
	r.images = nil
}

func (r *CodexRunner) Settings() CodexSettings {
	return CodexSettings{
		Model:     r.model,
		Profile:   r.profile,
		Sandbox:   r.sandbox,
		Approval:  r.approval,
		Reasoning: r.reasoningMode,
		Search:    r.search,
		Images:    append([]string(nil), r.images...),
	}
}

type CodexSettings struct {
	Model     string
	Profile   string
	Sandbox   string
	Approval  string
	Reasoning CodexReasoningMode
	Search    bool
	Images    []string
}

func (r *CodexRunner) AuthStatus() (bool, error) {
	return true, nil
}

func (r *CodexRunner) Login() error {
	cmd := exec.Command("codex", "login")
	_, err := runPTY(cmd)
	return err
}

func (r *CodexRunner) Logout() error {
	cmd := exec.Command("codex", "logout")
	_, err := runPTY(cmd)
	return err
}

func (r *CodexRunner) Quota() (*QuotaInfo, error) {
	return nil, nil
}

type codexJSONEvent struct {
	Type    string          `json:"type"`
	Content string          `json:"content,omitempty"`
	Message string          `json:"message,omitempty"`
	Text    string          `json:"text,omitempty"`
	Delta   string          `json:"delta,omitempty"`
	Item    json.RawMessage `json:"item,omitempty"`
	Error   json.RawMessage `json:"error,omitempty"`
}

type codexJSONItem struct {
	ItemType string `json:"item_type,omitempty"`
	Type     string `json:"type,omitempty"`
	Text     string `json:"text,omitempty"`
	Content  string `json:"content,omitempty"`
	Message  string `json:"message,omitempty"`
}

func runCodexJSON(ctx context.Context, cmd *exec.Cmd, out io.Writer, reasoningMode CodexReasoningMode) error {
	// stdout: use io.Pipe so exec.Cmd's internal goroutine writes to pw;
	// cmd.Wait() waits for that goroutine, then we close pw → scanner gets EOF.
	// This eliminates the "file already closed" race from StdoutPipe.
	pr, pw := io.Pipe()
	cmd.Stdout = pw

	// stderr: collect via a pipe writer for proxy-block detection.
	stderrPR, stderrPW := io.Pipe()
	cmd.Stderr = stderrPW

	if err := cmd.Start(); err != nil {
		_ = pw.CloseWithError(err)
		_ = pr.Close()
		_ = stderrPW.CloseWithError(err)
		_ = stderrPR.Close()
		return err
	}

	var wroteAssistant bool
	var wroteProgress bool
	var sawProxyBlock bool
	var stderrLines []string

	writeText := func(text string) {
		text = strings.TrimRight(text, "\r\n")
		if text == "" {
			return
		}
		_, _ = out.Write([]byte(text))
		if !strings.HasSuffix(text, "\n") {
			_, _ = out.Write([]byte("\n"))
		}
		wroteAssistant = true
	}

	writeProgress := func(text string) {
		text = strings.TrimSpace(text)
		if text == "" {
			return
		}
		_, _ = out.Write([]byte(text))
		if !strings.HasSuffix(text, "\n") {
			_, _ = out.Write([]byte("\n"))
		}
		wroteProgress = true
	}

	// stderr goroutine: drain stderrPR until EOF.
	var stderrWg sync.WaitGroup
	stderrWg.Add(1)
	go func() {
		defer stderrWg.Done()
		scanner := bufio.NewScanner(stderrPR)
		scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			if codexLineLooksProxyBlocked(line) {
				sawProxyBlock = true
			} else {
				stderrLines = append(stderrLines, line)
			}
		}
	}()

	// cmd.Wait() in background: waits for process exit AND exec's internal
	// stdout copy goroutine, then closes pw → pr scanner gets EOF below.
	waitDone := make(chan error, 1)
	go func() {
		err := cmd.Wait()
		_ = pw.CloseWithError(err)
		_ = stderrPW.CloseWithError(err)
		waitDone <- err
	}()

	// Scan stdout JSON lines in this goroutine (blocks until pw is closed).
	scanner := bufio.NewScanner(pr)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if text, ok := codexAssistantText(line); ok {
			writeText(text)
			continue
		}
		if progress, ok := codexProgressText(line, reasoningMode); ok {
			writeProgress(progress)
			continue
		}
		if codexLineLooksProxyBlocked(line) {
			sawProxyBlock = true
		}
	}
	_ = pr.Close()

	stderrWg.Wait()
	waitErr := <-waitDone

	if !wroteAssistant && sawProxyBlock {
		_, _ = out.Write([]byte("[codex blocked by Zscaler/proxy; open ChatGPT in a browser, approve the security prompt, then retry]\n"))
		return fmt.Errorf("%w: browser approval required", ErrCodexProxyBlocked)
	}
	if !wroteAssistant && !wroteProgress && len(stderrLines) > 0 {
		_, _ = out.Write([]byte(strings.Join(stderrLines, "\n") + "\n"))
	}
	return waitErr
}

func codexAssistantText(line string) (string, bool) {
	var evt codexJSONEvent
	if err := json.Unmarshal([]byte(line), &evt); err != nil {
		return "", false
	}

	switch evt.Type {
	case "message", "assistant", "text", "response.output_text.done", "agent_message":
		return firstNonEmpty(evt.Content, evt.Message, evt.Text), firstNonEmpty(evt.Content, evt.Message, evt.Text) != ""
	case "response.output_text.delta":
		return evt.Delta, evt.Delta != ""
	case "item.completed":
		var item codexJSONItem
		if len(evt.Item) > 0 && json.Unmarshal(evt.Item, &item) == nil {
			if item.ItemType == "assistant_message" || item.Type == "assistant_message" || item.Type == "message" {
				text := firstNonEmpty(item.Text, item.Content, item.Message)
				return text, text != ""
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

func codexProgressText(line string, reasoningMode CodexReasoningMode) (string, bool) {
	if reasoningMode == CodexReasoningOff {
		return "", false
	}

	var evt map[string]any
	if err := json.Unmarshal([]byte(line), &evt); err != nil {
		return "", false
	}

	eventType := stringField(evt, "type")
	if eventType == "" {
		return "", false
	}
	// agent_message carries the full assistant text — handled by codexAssistantText.
	if eventType == "agent_message" {
		return "", false
	}
	item := mapField(evt, "item")

	if strings.Contains(eventType, "turn.started") {
		return "● started", true
	}
	if strings.Contains(eventType, "turn.completed") || strings.Contains(eventType, "done") {
		return "● done", true
	}

	home, _ := os.UserHomeDir()
	abbrevPath := func(p string) string {
		if home != "" && strings.HasPrefix(p, home) {
			return "~" + p[len(home):]
		}
		return p
	}

	kind := firstNonEmpty(stringField(item, "item_type"), stringField(item, "type"), eventType)
	lowerKind := strings.ToLower(kind)
	if lowerKind == "assistant_message" || lowerKind == "message" {
		return "", false
	}

	if strings.Contains(strings.ToLower(eventType), "reasoning") || strings.Contains(lowerKind, "reasoning") {
		detail := firstNonEmpty(stringField(evt, "summary"), stringField(evt, "text"), stringField(item, "summary"), stringField(item, "text"))
		if detail == "" {
			return "● Thinking", true
		}
		return fmt.Sprintf("● Thinking  %s", oneLine(detail)), true
	}

	toolName := firstNonEmpty(
		stringField(evt, "tool"),
		stringField(evt, "tool_name"),
		stringField(evt, "name"),
		stringField(item, "tool"),
		stringField(item, "tool_name"),
		stringField(item, "name"),
	)
	command := firstNonEmpty(
		stringField(evt, "command"),
		stringField(evt, "cmd"),
		stringField(item, "command"),
		stringField(item, "cmd"),
		stringField(mapField(item, "input"), "command"),
	)
	path := firstNonEmpty(
		stringField(evt, "path"),
		stringField(evt, "file"),
		stringField(evt, "file_path"),
		stringField(item, "path"),
		stringField(item, "file"),
		stringField(item, "file_path"),
	)

	if command != "" || strings.Contains(lowerKind, "exec") || strings.Contains(lowerKind, "shell") || strings.Contains(lowerKind, "command") {
		if command == "" {
			command = toolName
		}
		if command == "" {
			command = kind
		}
		cmd := oneLine(command)
		if len(cmd) > 72 {
			cmd = cmd[:72] + "…"
		}
		return fmt.Sprintf("● Shell  %s", cmd), true
	}

	if path != "" || strings.Contains(lowerKind, "patch") || strings.Contains(lowerKind, "file") {
		if path == "" {
			path = kind
		}
		return fmt.Sprintf("● Edit  %s", abbrevPath(oneLine(path))), true
	}

	if toolName != "" || strings.Contains(lowerKind, "tool") || strings.Contains(lowerKind, "function") {
		if toolName == "" {
			toolName = kind
		}
		return fmt.Sprintf("● %s", oneLine(toolName)), true
	}

	if reasoningMode == CodexReasoningVerbose {
		detail := firstNonEmpty(
			stringField(evt, "message"),
			stringField(evt, "text"),
			stringField(evt, "description"),
			stringField(item, "message"),
			stringField(item, "text"),
			stringField(item, "description"),
		)
		if detail != "" {
			return fmt.Sprintf("● %s  %s", oneLine(kind), oneLine(detail)), true
		}
	}

	if reasoningMode == CodexReasoningVerbose && strings.Contains(strings.ToLower(eventType), "started") {
		return fmt.Sprintf("● %s", oneLine(kind)), true
	}
	return "", false
}

func codexProgressStatus(eventType, explicit string) string {
	status := strings.ToLower(strings.TrimSpace(explicit))
	switch {
	case strings.Contains(status, "fail") || strings.Contains(status, "error"):
		return "!"
	case strings.Contains(status, "done") || strings.Contains(status, "complete") || strings.Contains(eventType, "completed"):
		return "ok"
	case strings.Contains(status, "running") || strings.Contains(status, "start") || strings.Contains(eventType, "started"):
		return "*"
	default:
		return "*"
	}
}

func mapField(m map[string]any, key string) map[string]any {
	if m == nil {
		return nil
	}
	v, ok := m[key]
	if !ok {
		return nil
	}
	if typed, ok := v.(map[string]any); ok {
		return typed
	}
	return nil
}

func stringField(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	switch typed := v.(type) {
	case string:
		return typed
	case []any:
		var parts []string
		for _, item := range typed {
			if s, ok := item.(string); ok {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, " ")
	default:
		return fmt.Sprint(typed)
	}
}

func oneLine(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	const maxLen = 160
	if len(value) > maxLen {
		return value[:maxLen-3] + "..."
	}
	return value
}

func codexLineLooksProxyBlocked(line string) bool {
	lower := strings.ToLower(line)
	return strings.Contains(lower, "zscaler") ||
		strings.Contains(lower, "internet security by zscaler") ||
		strings.Contains(lower, "unexpected status 403 forbidden") ||
		strings.Contains(lower, "307 temporary redirect") ||
		(strings.Contains(lower, "chatgpt.com/backend-api/codex") && strings.Contains(lower, "failed to connect"))
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
