package adapter

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/mwigge/milliways/internal/kitchen"
)

// quotaResetRegex parses Gemini's TerminalQuotaError reset duration.
var quotaResetRegex = regexp.MustCompile(`reset after (\d+)h(\d+)m(\d+)s`)

// GeminiAdapter speaks Gemini CLI's stream-json protocol.
// Gemini in --prompt mode is one-shot — no bidirectional dialogue.
type GeminiAdapter struct {
	kitchen   *kitchen.GenericKitchen
	opts      AdapterOpts
	mu        sync.Mutex
	processID int
}

// NewGeminiAdapter creates an adapter for the gemini kitchen.
func NewGeminiAdapter(k *kitchen.GenericKitchen, opts AdapterOpts) *GeminiAdapter {
	return &GeminiAdapter{kitchen: k, opts: opts}
}

// geminiEvent represents a raw JSON event from gemini --output-format stream-json.
type geminiEvent struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	Model     string `json:"model,omitempty"`
	Role      string `json:"role,omitempty"`
	Content   string `json:"content,omitempty"`
}

// Exec starts gemini with stream-json output and returns an event channel.
func (a *GeminiAdapter) Exec(ctx context.Context, task kitchen.Task) (<-chan Event, error) {
	cfg := a.kitchen.Config()
	args := []string{
		"--prompt", task.Prompt,
		"--output-format", "stream-json",
	}

	if !kitchen.IsCmdAllowed(cfg.Cmd) && !kitchen.IsCmdAllowed(cfg.Name) {
		return nil, fmt.Errorf("command %q not in allowed list", cfg.Cmd)
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

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("creating stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting gemini: %w", err)
	}

	a.mu.Lock()
	a.processID = 0
	if cmd.Process != nil {
		a.processID = cmd.Process.Pid
	}
	a.mu.Unlock()

	ch := make(chan Event, 64)
	name := a.kitchen.Name()

	var wg sync.WaitGroup
	wg.Add(1)

	// Stderr capture — parse quota errors.
	// Uses a non-blocking send so the stderr goroutine never blocks on a full
	// ch while the stdout goroutine is blocked in cmd.Wait(), which would deadlock.
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			if rl := parseGeminiQuotaError(name, line); rl != nil {
				select {
				case ch <- *rl:
				default:
				}
			}
		}
	}()

	// Stdout JSON event parser
	go func() {
		defer close(ch)
		defer func() {
			a.mu.Lock()
			a.processID = 0
			a.mu.Unlock()
		}()

		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 256*1024), 1024*1024)

		sawDone := false
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				continue
			}

			var evt geminiEvent
			if err := json.Unmarshal([]byte(line), &evt); err != nil {
				// Non-JSON line — emit as text
				ch <- Event{Type: EventText, Kitchen: name, Text: line}
				continue
			}

			switch evt.Type {
			case "init":
				// Internal bookkeeping
			case "message":
				if evt.Role == "model" || evt.Role == "assistant" {
					parsed := ParseTextToEvents(name, evt.Content)
					for _, e := range parsed {
						ch <- e
					}
				}
			case "result":
				sawDone = true
				ch <- Event{Type: EventDone, Kitchen: name, ExitCode: 0}
			}
		}

		if scanErr := scanner.Err(); scanErr != nil {
			ch <- Event{Type: EventError, Kitchen: name, Text: fmt.Sprintf("scanner: %v", scanErr)}
		}

		waitErr := cmd.Wait()
		wg.Wait() // wait for stderr goroutine before closing ch

		if !sawDone {
			exitCode := 0
			if waitErr != nil {
				var exitErr *exec.ExitError
				if errors.As(waitErr, &exitErr) {
					exitCode = exitErr.ExitCode()
				}
			}
			ch <- Event{Type: EventDone, Kitchen: name, ExitCode: exitCode}
		}
	}()

	return ch, nil
}

// parseGeminiQuotaError checks a stderr line for TerminalQuotaError and returns
// an EventRateLimit event if found.
func parseGeminiQuotaError(kitchenName, line string) *Event {
	matches := quotaResetRegex.FindStringSubmatch(line)
	if matches == nil {
		return nil
	}

	hours, _ := strconv.Atoi(matches[1])
	minutes, _ := strconv.Atoi(matches[2])
	seconds, _ := strconv.Atoi(matches[3])

	resetDuration := time.Duration(hours)*time.Hour +
		time.Duration(minutes)*time.Minute +
		time.Duration(seconds)*time.Second

	return &Event{
		Type:    EventRateLimit,
		Kitchen: kitchenName,
		RateLimit: &RateLimitInfo{
			Status:   "exhausted",
			ResetsAt: time.Now().Add(resetDuration),
			Kitchen:  kitchenName,
		},
	}
}

// Send returns ErrNotInteractive — gemini --prompt is one-shot.
func (a *GeminiAdapter) Send(_ context.Context, _ string) error {
	return ErrNotInteractive
}

// SupportsResume returns false for gemini in --prompt mode.
func (a *GeminiAdapter) SupportsResume() bool { return false }

// SessionID returns "" for gemini.
func (a *GeminiAdapter) SessionID() string { return "" }

// ProcessID returns the running subprocess pid when available.
func (a *GeminiAdapter) ProcessID() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.processID
}

// Capabilities returns Gemini continuity features.
func (a *GeminiAdapter) Capabilities() Capabilities {
	return Capabilities{
		NativeResume:        false,
		InteractiveSend:     false,
		StructuredEvents:    true,
		ExhaustionDetection: "stderr",
	}
}
