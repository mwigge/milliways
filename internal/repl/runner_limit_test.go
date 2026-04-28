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
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// ----- Claude -----

func TestClaudeStderrSignalsLimit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		lines []string
		want  bool
	}{
		{
			name:  "context window phrase",
			lines: []string{"Error: context window exceeded"},
			want:  true,
		},
		{
			name:  "session limit phrase",
			lines: []string{"your session limit has been reached"},
			want:  true,
		},
		{
			name:  "context_length field",
			lines: []string{"context_length=128000 exceeded"},
			want:  true,
		},
		{
			name:  "too long phrase",
			lines: []string{"prompt is too long for this model"},
			want:  true,
		},
		{
			name:  "case insensitive",
			lines: []string{"CONTEXT WINDOW FULL"},
			want:  true,
		},
		{
			name:  "unrelated error",
			lines: []string{"network timeout", "unexpected EOF"},
			want:  false,
		},
		{
			name:  "empty lines",
			lines: nil,
			want:  false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := claudeStderrSignalsLimit(tt.lines)
			if got != tt.want {
				t.Errorf("claudeStderrSignalsLimit(%v) = %v, want %v", tt.lines, got, tt.want)
			}
		})
	}
}

func TestRunClaudeJSONEmitsSentinelOnLimitStderr(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Simulate claude writing a context-limit message to stderr and exiting non-zero.
	cmd := exec.CommandContext(ctx, "sh", "-c",
		`echo 'context window exceeded' >&2; exit 1`)

	var buf bytes.Buffer
	_, err := runClaudeJSON(ctx, cmd, DispatchRequest{Prompt: "test"}, &buf, ClaudeReasoningOff)

	if !errors.Is(err, ErrSessionLimit) {
		t.Errorf("expected error wrapping ErrSessionLimit; got: %v", err)
	}
	if strings.Contains(buf.String(), SessionLimitSentinel) {
		t.Errorf("output must not contain SessionLimitSentinel; got: %q", buf.String())
	}
}

func TestRunClaudeJSONNoSentinelOnNormalError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", `echo 'network timeout' >&2; exit 1`)

	var buf bytes.Buffer
	_, err := runClaudeJSON(ctx, cmd, DispatchRequest{Prompt: "test"}, &buf, ClaudeReasoningOff)

	if errors.Is(err, ErrSessionLimit) {
		t.Errorf("error must not wrap ErrSessionLimit for non-limit error; got: %v", err)
	}
	if strings.Contains(buf.String(), SessionLimitSentinel) {
		t.Errorf("output must not contain SessionLimitSentinel for non-limit error; got: %q", buf.String())
	}
}

// ----- Codex -----

func TestCodexLineSignalsSessionLimit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		line string
		want bool
	}{
		{
			name: "max_turns event",
			line: `{"type":"max_turns"}`,
			want: true,
		},
		{
			name: "context_length_exceeded event",
			line: `{"type":"context_length_exceeded"}`,
			want: true,
		},
		{
			name: "error event with context in message",
			line: `{"type":"error","message":"context length exceeded the model limit"}`,
			want: true,
		},
		{
			name: "error event with limit in message",
			line: `{"type":"error","message":"turn limit reached"}`,
			want: true,
		},
		{
			name: "error event with unrelated message",
			line: `{"type":"error","message":"network connection refused"}`,
			want: false,
		},
		{
			name: "assistant message event",
			line: `{"type":"message","content":"hello"}`,
			want: false,
		},
		{
			name: "non-JSON line",
			line: `not json at all`,
			want: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := codexLineSignalsSessionLimit(tt.line)
			if got != tt.want {
				t.Errorf("codexLineSignalsSessionLimit(%q) = %v, want %v", tt.line, got, tt.want)
			}
		})
	}
}

func TestRunCodexJSONEmitsSentinelOnMaxTurns(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c",
		`printf '%s\n' '{"type":"max_turns"}'`)

	var buf bytes.Buffer
	err := runCodexJSON(ctx, cmd, &buf, CodexReasoningOff)

	if !errors.Is(err, ErrSessionLimit) {
		t.Errorf("expected error wrapping ErrSessionLimit; got: %v", err)
	}
	if strings.Contains(buf.String(), SessionLimitSentinel) {
		t.Errorf("output must not contain SessionLimitSentinel; got: %q", buf.String())
	}
}

func TestRunCodexJSONEmitsSentinelOnContextLengthExceeded(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c",
		`printf '%s\n' '{"type":"context_length_exceeded"}'`)

	var buf bytes.Buffer
	err := runCodexJSON(ctx, cmd, &buf, CodexReasoningOff)

	if !errors.Is(err, ErrSessionLimit) {
		t.Errorf("expected error wrapping ErrSessionLimit; got: %v", err)
	}
	if strings.Contains(buf.String(), SessionLimitSentinel) {
		t.Errorf("output must not contain SessionLimitSentinel; got: %q", buf.String())
	}
}

func TestRunCodexJSONNoSentinelOnNormalOutput(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c",
		`printf '%s\n' '{"type":"message","content":"all done"}'`)

	var buf bytes.Buffer
	err := runCodexJSON(ctx, cmd, &buf, CodexReasoningOff)

	if errors.Is(err, ErrSessionLimit) {
		t.Errorf("error must not wrap ErrSessionLimit for normal output; got: %v", err)
	}
	if strings.Contains(buf.String(), SessionLimitSentinel) {
		t.Errorf("output must not contain SessionLimitSentinel; got: %q", buf.String())
	}
}

// ----- MiniMax -----

func TestMinimaxBodySignalsLimit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want bool
	}{
		{
			name: "quota_exceeded body",
			body: `{"error":{"code":"quota_exceeded","message":"Daily quota exceeded"}}`,
			want: true,
		},
		{
			name: "rate_limit body",
			body: `{"error":{"code":"rate_limit","message":"Too many requests"}}`,
			want: true,
		},
		{
			name: "case insensitive quota_exceeded",
			body: `{"error":"QUOTA_EXCEEDED"}`,
			want: true,
		},
		{
			name: "generic server error",
			body: `{"error":{"code":"internal_error","message":"something went wrong"}}`,
			want: false,
		},
		{
			name: "empty body",
			body: "",
			want: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := minimaxBodySignalsLimit([]byte(tt.body))
			if got != tt.want {
				t.Errorf("minimaxBodySignalsLimit(%q) = %v, want %v", tt.body, got, tt.want)
			}
		})
	}
}

func TestRunMinimaxSSEEmitsSentinelOn429QuotaExceeded(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := &http.Client{Transport: minimaxRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"error":{"code":"quota_exceeded","message":"Daily quota exceeded"}}` + "\n")),
		}, nil
	})}
	req, _ := http.NewRequestWithContext(ctx, "POST", "http://minimax.test", nil)

	var buf bytes.Buffer
	_, err := runMinimaxSSE(ctx, client, req, "MiniMax-M2.7", &buf, MinimaxReasoningOff)

	if !errors.Is(err, ErrSessionLimit) {
		t.Errorf("expected error wrapping ErrSessionLimit; got: %v", err)
	}
	if strings.Contains(buf.String(), SessionLimitSentinel) {
		t.Errorf("output must not contain SessionLimitSentinel; got: %q", buf.String())
	}
}

func TestRunMinimaxSSENoSentinelOn429NonQuota(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := &http.Client{Transport: minimaxRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"error":{"code":"too_many_requests","message":"slow down"}}` + "\n")),
		}, nil
	})}
	req, _ := http.NewRequestWithContext(ctx, "POST", "http://minimax.test", nil)

	var buf bytes.Buffer
	_, err := runMinimaxSSE(ctx, client, req, "MiniMax-M2.7", &buf, MinimaxReasoningOff)

	if errors.Is(err, ErrSessionLimit) {
		t.Errorf("error must not wrap ErrSessionLimit for non-quota 429; got: %v", err)
	}
	if strings.Contains(buf.String(), SessionLimitSentinel) {
		t.Errorf("output must not contain SessionLimitSentinel; got: %q", buf.String())
	}
}

// ----- Copilot -----

func TestCopilotStderrSignalsLimit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		lines []string
		want  bool
	}{
		{
			name:  "rate limit phrase",
			lines: []string{"Error: rate limit exceeded"},
			want:  true,
		},
		{
			name:  "context phrase",
			lines: []string{"context window is full"},
			want:  true,
		},
		{
			name:  "case insensitive rate limit",
			lines: []string{"RATE LIMIT HIT"},
			want:  true,
		},
		{
			name:  "unrelated error",
			lines: []string{"authentication failed"},
			want:  false,
		},
		{
			name:  "empty",
			lines: nil,
			want:  false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := copilotStderrSignalsLimit(tt.lines)
			if got != tt.want {
				t.Errorf("copilotStderrSignalsLimit(%v) = %v, want %v", tt.lines, got, tt.want)
			}
		})
	}
}

func TestRunCopilotCmdEmitsSentinelOnRateLimitStderr(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c",
		`echo 'hello from copilot'; echo 'rate limit exceeded' >&2; exit 1`)

	var buf bytes.Buffer
	err := runCopilotCmd(ctx, cmd, &buf)

	if !errors.Is(err, ErrSessionLimit) {
		t.Errorf("expected error wrapping ErrSessionLimit; got: %v", err)
	}
	if strings.Contains(buf.String(), SessionLimitSentinel) {
		t.Errorf("output must not contain SessionLimitSentinel; got: %q", buf.String())
	}
	// stdout output should still be present
	if !strings.Contains(buf.String(), "hello from copilot") {
		t.Errorf("stdout output missing; got: %q", buf.String())
	}
}

func TestRunCopilotCmdNoSentinelOnNormalStderr(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c",
		`echo 'hello from copilot'; echo 'authentication failed' >&2; exit 1`)

	var buf bytes.Buffer
	err := runCopilotCmd(ctx, cmd, &buf)

	if errors.Is(err, ErrSessionLimit) {
		t.Errorf("error must not wrap ErrSessionLimit for non-limit error; got: %v", err)
	}
	if strings.Contains(buf.String(), SessionLimitSentinel) {
		t.Errorf("output must not contain SessionLimitSentinel; got: %q", buf.String())
	}
}
