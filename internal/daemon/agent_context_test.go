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

package daemon

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

func TestMergeContextIntoPromptKeepsContextAndUserPromptInOneTurn(t *testing.T) {
	got := string(mergeContextIntoPrompt([]string{"memory: previous decision", "codegraph: auth.go"}, "what is the status?"))
	for _, want := range []string{
		"[milliways context]",
		"memory: previous decision",
		"codegraph: auth.go",
		"[user prompt]",
		"what is the status?",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("merged prompt missing %q:\n%s", want, got)
		}
	}
	if strings.Count(got, "[user prompt]") != 1 {
		t.Fatalf("merged prompt should contain one user prompt marker:\n%s", got)
	}
}

func TestAgentSessionContextQueueDrainsOnce(t *testing.T) {
	sess := &AgentSession{}
	sess.queueContextBlock("  security finding  ")
	sess.queueContextBlock("")
	sess.queueContextBlock("handoff summary")

	got := sess.drainContextBlocks()
	if len(got) != 2 {
		t.Fatalf("expected two queued context blocks, got %d: %#v", len(got), got)
	}
	if got[0] != "security finding" || got[1] != "handoff summary" {
		t.Fatalf("unexpected queued context blocks: %#v", got)
	}
	if again := sess.drainContextBlocks(); len(again) != 0 {
		t.Fatalf("context queue should drain once, got %#v", again)
	}
}

func TestAgentSessionContextCanBeRequeuedAfterSendFailure(t *testing.T) {
	sess := &AgentSession{}
	sess.queueContextBlock("handoff summary")
	drained := sess.drainContextBlocks()
	sess.requeueContextBlocks(drained)

	got := sess.drainContextBlocks()
	if len(got) != 1 || got[0] != "handoff summary" {
		t.Fatalf("requeued context = %#v, want handoff summary", got)
	}
}

func TestRecordingPusherFlushesTurnMemoryOnChunkEnd(t *testing.T) {
	completed := make(chan string, 2)
	sess := &AgentSession{onTurnComplete: func(text string) { completed <- text }}
	pusher := &recordingPusher{sess: sess}

	pusher.Push(map[string]any{"t": "data", "b64": base64.StdEncoding.EncodeToString([]byte("first finding"))})
	pusher.Push(map[string]any{"t": "chunk_end"})
	pusher.Push(map[string]any{"t": "data", "b64": base64.StdEncoding.EncodeToString([]byte("second finding"))})
	pusher.Push(map[string]any{"t": "chunk_end"})

	got := []string{
		waitForCompletedTurn(t, completed),
		waitForCompletedTurn(t, completed),
	}
	seen := map[string]bool{got[0]: true, got[1]: true}
	if !seen["first finding"] || !seen["second finding"] {
		t.Fatalf("completed turns = %#v", got)
	}
}

func TestAgentSessionFirstSendContextRequeuesOnRunnerError(t *testing.T) {
	sess := &AgentSession{}
	sess.markFirstSendPending([]string{"handoff summary"})
	sess.firstSendDone.Store(1)
	pusher := &recordingPusher{sess: sess}

	pusher.Push(map[string]any{"t": "err", "msg": "missing api key"})
	pusher.Push(map[string]any{"t": "chunk_end"})

	if got := sess.firstSendDone.Load(); got != 0 {
		t.Fatalf("firstSendDone = %d, want reset after pre-delivery error", got)
	}
	requeued := sess.drainContextBlocks()
	if len(requeued) != 1 || requeued[0] != "handoff summary" {
		t.Fatalf("requeued context = %#v", requeued)
	}
}

func TestAgentSessionFirstSendDeliveryHookAfterChunkEnd(t *testing.T) {
	delivered := make(chan struct{}, 1)
	sess := &AgentSession{onFirstSendDelivered: func() { delivered <- struct{}{} }}
	sess.markFirstSendPending([]string{"handoff summary"})
	pusher := &recordingPusher{sess: sess}

	pusher.Push(map[string]any{"t": "chunk_end"})

	select {
	case <-delivered:
	default:
		t.Fatal("first-send delivery hook did not run on chunk_end")
	}
	if requeued := sess.drainContextBlocks(); len(requeued) != 0 {
		t.Fatalf("context should not be requeued after delivery: %#v", requeued)
	}
}

func TestRecordingPusherKeepsLargeTurnPrefixForExtraction(t *testing.T) {
	completed := make(chan string, 1)
	sess := &AgentSession{onTurnComplete: func(text string) { completed <- text }}
	pusher := &recordingPusher{sess: sess}

	prefix := "IMPORTANT finding near beginning\n"
	padding := strings.Repeat("x", responseBufCap+1024)
	pusher.Push(map[string]any{"t": "data", "b64": base64.StdEncoding.EncodeToString([]byte(prefix + padding))})
	pusher.Push(map[string]any{"t": "chunk_end"})

	got := waitForCompletedTurn(t, completed)
	if !strings.Contains(got, prefix) {
		t.Fatalf("turn extraction buffer lost early finding")
	}
}

func waitForCompletedTurn(t *testing.T, completed <-chan string) string {
	t.Helper()
	select {
	case got := <-completed:
		return got
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for completed turn")
		return ""
	}
}
