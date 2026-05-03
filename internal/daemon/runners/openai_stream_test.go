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
	"strings"
	"testing"
)

func TestStreamOpenAITurnFiltersReasoning(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"choices":[{"delta":{"content":"Visible "}}]}`,
		``,
		`data: {"choices":[{"delta":{"content":"<thi"}}]}`,
		``,
		`data: {"choices":[{"delta":{"content":"nk>secret"}}]}`,
		``,
		`data: {"choices":[{"delta":{"content":"</thi"}}]}`,
		``,
		`data: {"choices":[{"delta":{"content":"nk> answer"}}]}`,
		``,
		`data: {"choices":[{"delta":{"reasoning_content":"structured secret"}}]}`,
		``,
		`data: {"choices":[{"finish_reason":"stop","delta":{}}]}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	pusher := &fakePusher{}
	result, err := streamOpenAITurn(context.Background(), strings.NewReader(stream), pusher)
	if err != nil {
		t.Fatalf("streamOpenAITurn() error = %v", err)
	}
	if result.Content != "Visible  answer" {
		t.Fatalf("content = %q, want visible answer only", result.Content)
	}
	if !strings.Contains(result.Reasoning, "secret") || !strings.Contains(result.Reasoning, "structured secret") {
		t.Fatalf("reasoning = %q, want hidden and structured reasoning", result.Reasoning)
	}

	var pushed strings.Builder
	var sawThinking bool
	for _, event := range pusher.snapshot() {
		switch event["t"] {
		case "thinking":
			sawThinking = true
			continue
		case "data":
		default:
			continue
		}
		b64, _ := event["b64"].(string)
		raw, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			t.Fatalf("decode stream event: %v", err)
		}
		pushed.Write(raw)
	}
	if got := pushed.String(); got != "Visible  answer" {
		t.Fatalf("pushed content = %q, want visible answer only", got)
	}
	if !sawThinking {
		t.Fatalf("expected thinking event for filtered reasoning, events=%v", pusher.snapshot())
	}
}

func TestThinkTagSplitterFlushesPartialVisibleTag(t *testing.T) {
	var splitter thinkTagSplitter
	visible, reasoning := splitter.Push("hello <thi")
	if visible != "hello " || reasoning != "" {
		t.Fatalf("Push() = (%q, %q), want held partial tag", visible, reasoning)
	}
	visible, reasoning = splitter.Flush()
	if visible != "<thi" || reasoning != "" {
		t.Fatalf("Flush() = (%q, %q), want partial tag restored as visible", visible, reasoning)
	}
}
