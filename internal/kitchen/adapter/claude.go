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
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/mwigge/milliways/internal/kitchen"
)

// ClaudeAdapter speaks Claude Code's stream-json protocol for full bidirectional
// communication including cost tracking, rate limit detection, and session resume.
type ClaudeAdapter struct {
	kitchen   *kitchen.GenericKitchen
	opts      AdapterOpts
	mu        sync.Mutex
	stdinPipe io.WriteCloser
	sessionID string
	model     string
	processID int
}

// NewClaudeAdapter creates an adapter for the claude kitchen.
func NewClaudeAdapter(k *kitchen.GenericKitchen, opts AdapterOpts) *ClaudeAdapter {
	return &ClaudeAdapter{kitchen: k, opts: opts}
}

// claudeEvent represents a raw JSON event from claude --output-format stream-json.
type claudeEvent struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype,omitempty"`

	// For system/init
	SessionID string   `json:"session_id,omitempty"`
	Model     string   `json:"model,omitempty"`
	Tools     []string `json:"tools,omitempty"`

	// For system/hook_*
	HookName string `json:"hook_name,omitempty"`

	// For assistant messages
	Message *claudeMessage `json:"message,omitempty"`

	// For rate_limit_event
	RateLimitInfo *claudeRateLimit `json:"rate_limit_info,omitempty"`

	// For result
	TotalCostUSD float64      `json:"total_cost_usd,omitempty"`
	DurationMs   int          `json:"duration_ms,omitempty"`
	NumTurns     int          `json:"num_turns,omitempty"`
	Result       string       `json:"result,omitempty"`
	StopReason   string       `json:"stop_reason,omitempty"`
	Usage        *claudeUsage `json:"usage,omitempty"`
	IsError      bool         `json:"is_error,omitempty"`
}

type claudeMessage struct {
	Content []claudeContent `json:"content,omitempty"`
}

type claudeContent struct {
	Type  string          `json:"type"`
	Text  string          `json:"text,omitempty"`
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

type claudeRateLimit struct {
	Status   string `json:"status,omitempty"`
	ResetsAt int64  `json:"resetsAt,omitempty"` // unix timestamp
}

type claudeUsage struct {
	InputTokens              int `json:"input_tokens,omitempty"`
	OutputTokens             int `json:"output_tokens,omitempty"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
}

var (
	claudeLimitRegex      = regexp.MustCompile(`(?i)you've hit your limit`)
	claudeResetClockRegex = regexp.MustCompile(`(?i)resets\s+(\d{1,2})(?::(\d{2}))?\s*(am|pm)\s*\(([^)]+)\)`)
)

// Exec starts claude with stream-json and returns an event channel.
func (a *ClaudeAdapter) Exec(ctx context.Context, task kitchen.Task) (<-chan Event, error) {
	cfg := a.kitchen.Config()

	if !kitchen.IsCmdAllowed(cfg.Cmd) && !kitchen.IsCmdAllowed(cfg.Name) {
		return nil, fmt.Errorf("command %q not in allowed list", cfg.Cmd)
	}

	args := []string{
		"--verbose",
		"--output-format", "stream-json",
		"--input-format", "stream-json",
	}

	if a.opts.ResumeSessionID != "" {
		args = append(args, "--resume", a.opts.ResumeSessionID)
	}

	for _, tool := range a.opts.AllowedTools {
		args = append(args, "--allowedTools", tool)
	}

	cmd := exec.CommandContext(ctx, cfg.Cmd, args...)
	if task.Dir != "" {
		cmd.Dir = task.Dir
	}
	cmd.Env = safeEnv(task.Env)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("creating stdout pipe: %w", err)
	}

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("creating stdin pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("creating stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting claude: %w", err)
	}

	a.mu.Lock()
	a.stdinPipe = stdinPipe
	a.processID = 0
	if cmd.Process != nil {
		a.processID = cmd.Process.Pid
	}
	a.mu.Unlock()

	ch := make(chan Event, 64)
	name := a.kitchen.Name()
	var ioWG sync.WaitGroup

	// Send the initial prompt via stream-json stdin
	go func() {
		promptJSON, err := json.Marshal(map[string]any{
			"type": "user",
			"message": map[string]any{
				"role":    "user",
				"content": task.Prompt,
			},
		})
		if err != nil {
			return // marshal of string map cannot fail in practice
		}
		a.mu.Lock()
		if a.stdinPipe != nil {
			fmt.Fprintf(a.stdinPipe, "%s\n", promptJSON)
		}
		a.mu.Unlock()
	}()

	// Stderr capture goroutine
	ioWG.Add(1)
	go func() {
		defer ioWG.Done()
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			if rl := parseClaudeExhaustionText(name, line, time.Now(), "stderr_text"); rl != nil {
				ch <- *rl
			}
		}
	}()

	// Main event parser goroutine
	go func() {
		defer func() {
			a.mu.Lock()
			if a.stdinPipe != nil {
				a.stdinPipe.Close()
				a.stdinPipe = nil
			}
			a.mu.Unlock()
		}()

		scanner := bufio.NewScanner(stdout)
		// Increase buffer for large JSON lines
		scanner.Buffer(make([]byte, 0, 256*1024), 1024*1024)

		sawDone := false
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				continue
			}

			var evt claudeEvent
			if err := json.Unmarshal([]byte(line), &evt); err != nil {
				if rl := parseClaudeExhaustionText(name, line, time.Now(), "stdout_text"); rl != nil {
					ch <- *rl
				} else {
					ch <- Event{Type: EventText, Kitchen: name, Text: line}
				}
				continue
			}

			events := a.mapEvent(name, &evt)
			for _, e := range events {
				if e.Type == EventDone {
					sawDone = true
				}
				ch <- e
			}

			// After result, close stdin so Claude exits gracefully.
			if sawDone {
				a.mu.Lock()
				if a.stdinPipe != nil {
					a.stdinPipe.Close()
					a.stdinPipe = nil
				}
				a.processID = 0
				a.mu.Unlock()
				break
			}
		}

		if scanErr := scanner.Err(); scanErr != nil {
			ch <- Event{Type: EventError, Kitchen: name, Text: fmt.Sprintf("scanner: %v", scanErr)}
		}

		// Wait for process to exit
		waitErr := cmd.Wait()

		if !sawDone {
			exitCode := 0
			if waitErr != nil {
				var exitErr *exec.ExitError
				if errors.As(waitErr, &exitErr) {
					exitCode = exitErr.ExitCode()
				}
			}
			ch <- Event{
				Type:     EventDone,
				Kitchen:  name,
				ExitCode: exitCode,
			}
		}
		ioWG.Wait()
		close(ch)
	}()

	return ch, nil
}

// mapEvent converts a claude stream-json event to zero or more adapter Events.
func (a *ClaudeAdapter) mapEvent(name string, evt *claudeEvent) []Event {
	switch evt.Type {
	case "system":
		return a.mapSystemEvent(name, evt)
	case "assistant":
		return a.mapAssistantEvent(name, evt)
	case "rate_limit_event":
		return a.mapRateLimitEvent(name, evt)
	case "result":
		return a.mapResultEvent(name, evt)
	default:
		return nil
	}
}

func (a *ClaudeAdapter) mapSystemEvent(name string, evt *claudeEvent) []Event {
	switch evt.Subtype {
	case "init":
		a.mu.Lock()
		a.sessionID = evt.SessionID
		a.model = evt.Model
		a.mu.Unlock()
		return nil // internal bookkeeping, no event emitted
	case "hook_started":
		return []Event{{
			Type:       EventToolUse,
			Kitchen:    name,
			ToolName:   "hook:" + evt.HookName,
			ToolStatus: "started",
		}}
	case "hook_response":
		return []Event{{
			Type:       EventToolUse,
			Kitchen:    name,
			ToolName:   "hook:" + evt.HookName,
			ToolStatus: "done",
		}}
	default:
		return nil
	}
}

func (a *ClaudeAdapter) mapAssistantEvent(name string, evt *claudeEvent) []Event {
	if evt.Message == nil {
		return nil
	}

	var events []Event
	for _, content := range evt.Message.Content {
		switch content.Type {
		case "text":
			if rl := parseClaudeExhaustionText(name, content.Text, time.Now(), "assistant_text"); rl != nil {
				events = append(events, *rl)
			}
			// Parse for code blocks
			parsed := ParseTextToEvents(name, content.Text)
			events = append(events, parsed...)
		case "tool_use":
			events = append(events, Event{
				Type:       EventToolUse,
				Kitchen:    name,
				ToolName:   content.Name,
				ToolStatus: "started",
			})
		}
	}
	return events
}

func (a *ClaudeAdapter) mapRateLimitEvent(name string, evt *claudeEvent) []Event {
	if evt.RateLimitInfo == nil {
		return nil
	}

	info := &RateLimitInfo{
		Status:        evt.RateLimitInfo.Status,
		Kitchen:       name,
		IsExhaustion:  evt.RateLimitInfo.Status == "exhausted",
		DetectionKind: "structured",
	}
	if evt.RateLimitInfo.ResetsAt > 0 {
		info.ResetsAt = time.Unix(evt.RateLimitInfo.ResetsAt, 0)
	}

	return []Event{{
		Type:      EventRateLimit,
		Kitchen:   name,
		RateLimit: info,
	}}
}

func (a *ClaudeAdapter) mapResultEvent(name string, evt *claudeEvent) []Event {
	var events []Event

	if evt.TotalCostUSD > 0 || evt.Usage != nil {
		cost := &CostInfo{
			USD:        evt.TotalCostUSD,
			DurationMs: evt.DurationMs,
		}
		if evt.Usage != nil {
			cost.InputTokens = evt.Usage.InputTokens
			cost.OutputTokens = evt.Usage.OutputTokens
			cost.CacheRead = evt.Usage.CacheReadInputTokens
			cost.CacheWrite = evt.Usage.CacheCreationInputTokens
		}
		events = append(events, Event{
			Type:    EventCost,
			Kitchen: name,
			Cost:    cost,
		})
	}

	exitCode := 0
	if evt.IsError {
		exitCode = 1
	}

	events = append(events, Event{
		Type:     EventDone,
		Kitchen:  name,
		ExitCode: exitCode,
	})

	return events
}

// Send writes a message to claude's stdin via the stream-json protocol.
func (a *ClaudeAdapter) Send(ctx context.Context, msg string) error {
	a.mu.Lock()
	pipe := a.stdinPipe
	a.mu.Unlock()

	if pipe == nil {
		return ErrNotInteractive
	}

	payload, err := json.Marshal(map[string]any{
		"type": "say",
		"content": map[string]string{
			"type": "text",
			"text": msg,
		},
	})
	if err != nil {
		return fmt.Errorf("marshalling message: %w", err)
	}

	done := make(chan error, 1)
	go func() { _, err := fmt.Fprintf(pipe, "%s\n", payload); done <- err }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// SupportsResume returns true — claude supports --resume.
func (a *ClaudeAdapter) SupportsResume() bool { return true }

// SessionID returns the session ID from the init event.
func (a *ClaudeAdapter) SessionID() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.sessionID
}

// ProcessID returns the underlying subprocess pid when available.
func (a *ClaudeAdapter) ProcessID() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.processID
}

// Capabilities returns Claude continuity features.
func (a *ClaudeAdapter) Capabilities() Capabilities {
	return Capabilities{
		NativeResume:        true,
		InteractiveSend:     true,
		StructuredEvents:    true,
		ExhaustionDetection: "structured+stdout+stderr",
	}
}

func parseClaudeExhaustionText(kitchenName, text string, now time.Time, detectionKind string) *Event {
	if !claudeLimitRegex.MatchString(text) {
		return nil
	}

	info := &RateLimitInfo{
		Status:        "exhausted",
		Kitchen:       kitchenName,
		IsExhaustion:  true,
		RawText:       strings.TrimSpace(text),
		DetectionKind: detectionKind,
	}
	if resetsAt, ok := parseClaudeResetTime(text, now); ok {
		info.ResetsAt = resetsAt
	}

	return &Event{
		Type:      EventRateLimit,
		Kitchen:   kitchenName,
		Text:      strings.TrimSpace(text),
		RateLimit: info,
	}
}

func parseClaudeResetTime(text string, now time.Time) (time.Time, bool) {
	matches := claudeResetClockRegex.FindStringSubmatch(text)
	if matches == nil {
		return time.Time{}, false
	}

	loc, err := time.LoadLocation(matches[4])
	if err != nil {
		return time.Time{}, false
	}

	hour := 0
	if _, err := fmt.Sscanf(matches[1], "%d", &hour); err != nil {
		return time.Time{}, false
	}
	minute := 0
	if matches[2] != "" {
		if _, err := fmt.Sscanf(matches[2], "%d", &minute); err != nil {
			return time.Time{}, false
		}
	}

	switch strings.ToLower(matches[3]) {
	case "pm":
		if hour != 12 {
			hour += 12
		}
	case "am":
		if hour == 12 {
			hour = 0
		}
	}

	inLoc := now.In(loc)
	reset := time.Date(inLoc.Year(), inLoc.Month(), inLoc.Day(), hour, minute, 0, 0, loc)
	if !reset.After(inLoc) {
		reset = reset.Add(24 * time.Hour)
	}
	return reset, true
}
