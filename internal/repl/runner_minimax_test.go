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
	"io"
	"net/http"
	"strings"
	"testing"
)

type minimaxRoundTripFunc func(*http.Request) (*http.Response, error)

func (f minimaxRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestMinimaxThinkFilter_NoThinkTags(t *testing.T) {
	t.Parallel()
	var f minimaxThinkFilter
	var got, thinking strings.Builder
	f.write("Hello world", func(s string) { got.WriteString(s) }, func(s string) { thinking.WriteString(s) })
	if got.String() != "Hello world" || thinking.Len() != 0 {
		t.Fatalf("got=%q thinking=%q", got.String(), thinking.String())
	}
}

func TestMinimaxThinkFilter_SingleChunk(t *testing.T) {
	t.Parallel()
	var f minimaxThinkFilter
	var got, thinking strings.Builder
	f.write("<think>reasoning here</think>answer", func(s string) { got.WriteString(s) }, func(s string) { thinking.WriteString(s) })
	if got.String() != "answer" {
		t.Fatalf("content = %q, want %q", got.String(), "answer")
	}
	if !strings.Contains(thinking.String(), "reasoning here") {
		t.Fatalf("thinking = %q, want 'reasoning here'", thinking.String())
	}
}

func TestMinimaxThinkFilter_SpansChunks(t *testing.T) {
	t.Parallel()
	var f minimaxThinkFilter
	var got, thinking strings.Builder
	write := func(s string) { got.WriteString(s) }
	think := func(s string) { thinking.WriteString(s) }

	f.write("<think>\nthought part one\n", write, think)
	f.write("thought part two\n</think>\n\nactual answer", write, think)

	if !strings.Contains(got.String(), "actual answer") {
		t.Fatalf("content missing answer: %q", got.String())
	}
	if strings.Contains(got.String(), "<think>") || strings.Contains(got.String(), "thought") {
		t.Fatalf("content leaked think block: %q", got.String())
	}
	if !strings.Contains(thinking.String(), "thought part one") {
		t.Fatalf("thinking missing content: %q", thinking.String())
	}
}

func TestMinimaxStreamIntegrityDetectsUnclosedHeredoc(t *testing.T) {
	t.Parallel()

	var integrity minimaxStreamIntegrity
	integrity.observe("cat > internal/kitchen/adapter/http_test.go << 'EOF'")
	integrity.observe("package adapter")

	if !integrity.incomplete() {
		t.Fatal("expected incomplete heredoc to be detected")
	}
	if got := integrity.reason(); got != "unclosed heredoc" {
		t.Fatalf("reason = %q, want unclosed heredoc", got)
	}
}

func TestMinimaxStreamIntegrityClosesHeredoc(t *testing.T) {
	t.Parallel()

	var integrity minimaxStreamIntegrity
	integrity.observe("cat > file.go << 'EOF'")
	integrity.observe("package main")
	integrity.observe("EOF")

	if integrity.incomplete() {
		t.Fatal("heredoc marker should close the integrity warning")
	}
}

func TestRunMinimaxSSEIncompleteGeneratedEditReturnsError(t *testing.T) {
	t.Parallel()

	body := strings.Join([]string{
		"data: {\"choices\":[{\"delta\":{\"content\":\"```bash\\ncat > internal/kitchen/adapter/http_test.go << 'EOF'\\npackage adapter\\n\"}}]}",
		"",
	}, "\n")
	client := &http.Client{Transport: minimaxRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	})}
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "http://minimax.test", nil)

	var out bytes.Buffer
	_, err := runMinimaxSSE(context.Background(), client, req, "MiniMax-M2.7", &out, MinimaxReasoningVerbose)
	if err == nil || !strings.Contains(err.Error(), "incomplete SSE stream") {
		t.Fatalf("err = %v, want incomplete SSE stream", err)
	}
	if !strings.Contains(out.String(), "! minimax: incomplete stream") {
		t.Fatalf("output missing incomplete stream warning: %q", out.String())
	}
	if strings.Contains(out.String(), "ok minimax: done") {
		t.Fatalf("output should not report successful completion: %q", out.String())
	}
}

func TestRunMinimaxSSECompletedOpenAIStreamAllowsEOF(t *testing.T) {
	t.Parallel()

	body := strings.Join([]string{
		`data: {"choices":[{"delta":{"content":"done"},"finish_reason":"stop"}]}`,
		"",
	}, "\n")
	client := &http.Client{Transport: minimaxRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	})}
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "http://minimax.test", nil)

	var out bytes.Buffer
	_, err := runMinimaxSSE(context.Background(), client, req, "MiniMax-M2.7", &out, MinimaxReasoningVerbose)
	if err != nil {
		t.Fatalf("runMinimaxSSE() error = %v", err)
	}
	if !strings.Contains(out.String(), "ok minimax: done") {
		t.Fatalf("output missing done marker: %q", out.String())
	}
}

func TestRunMinimaxSSECompletedGeneratedEditWarnsNotExecuted(t *testing.T) {
	t.Parallel()

	body := strings.Join([]string{
		"data: {\"choices\":[{\"delta\":{\"content\":\"cat > file.go << 'EOF'\\npackage main\\nEOF\\n\"},\"finish_reason\":\"stop\"}]}",
		"",
	}, "\n")
	client := &http.Client{Transport: minimaxRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	})}
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "http://minimax.test", nil)

	var out bytes.Buffer
	_, err := runMinimaxSSE(context.Background(), client, req, "MiniMax-M2.7", &out, MinimaxReasoningVerbose)
	if err != nil {
		t.Fatalf("runMinimaxSSE() error = %v", err)
	}
	if !strings.Contains(out.String(), "generated file-write command was not executed") {
		t.Fatalf("output missing not-executed warning: %q", out.String())
	}
	if !strings.Contains(out.String(), "ok minimax: done") {
		t.Fatalf("output missing done marker: %q", out.String())
	}
}
