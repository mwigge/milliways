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
	"context"
	"errors"
	"testing"

	"github.com/mwigge/milliways/internal/parallel"
)

// stubMP is a minimal parallel.MPClient for testing.
type stubMP struct {
	addedSubject string
	addedObject  string
	addedProps   map[string]string
	err          error
}

func (s *stubMP) KGQuery(_ context.Context, _, _ string, _ map[string]string) ([]parallel.KGTriple, error) {
	return nil, nil
}

func (s *stubMP) KGAdd(_ context.Context, subject, _ string, object string, props map[string]string) error {
	s.addedSubject = subject
	s.addedObject = object
	s.addedProps = props
	return s.err
}

// TestMempalaceWriteHandoff_NilMP verifies that mempalaceWriteHandoff
// returns ok=false with a reason when no MemPalace client is configured.
func TestMempalaceWriteHandoff_NilMP(t *testing.T) {
	t.Parallel()

	_, send, readResp, cleanup := parallelTestHarness(t)
	defer cleanup()

	// MEMPALACE_MCP_CMD is not set in the test environment so
	// mempalaceClient() returns nil — the handler must respond ok=false.
	send("mempalace.write_handoff", map[string]any{
		"target_provider": "codex",
		"from_provider":   "claude",
		"briefing":        "test briefing text",
	}, 1)

	resp, err := readResp()
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if errObj, ok := resp["error"].(map[string]any); ok {
		t.Fatalf("expected result not error; got error: %v", errObj)
	}

	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result map, got: %T %v", resp["result"], resp["result"])
	}

	okVal, _ := result["ok"].(bool)
	if okVal {
		t.Error("expected ok=false when MemPalace is not configured")
	}
	reason, _ := result["reason"].(string)
	if reason == "" {
		t.Error("expected non-empty reason when ok=false")
	}
}

// TestMempalaceWriteHandoff_WritesToMP verifies that mempalaceWriteHandoff
// calls KGAdd with the correct subject (handoff:<target>) and stores the
// briefing text when a MemPalace client is available.
func TestMempalaceWriteHandoff_WritesToMP(t *testing.T) {
	t.Parallel()

	stub := &stubMP{}
	// Call the handler directly to inject our stub MP client.
	testWriteHandoff(t, stub, "gemini", "claude", "briefing content")

	wantSubject := "handoff:gemini"
	if stub.addedSubject != wantSubject {
		t.Errorf("KGAdd subject = %q, want %q", stub.addedSubject, wantSubject)
	}
	if stub.addedObject != "briefing content" {
		t.Errorf("KGAdd object = %q, want %q", stub.addedObject, "briefing content")
	}
	if stub.addedProps["from"] != "claude" {
		t.Errorf("KGAdd props[from] = %q, want %q", stub.addedProps["from"], "claude")
	}
	if stub.addedProps["ts"] == "" {
		t.Error("KGAdd props[ts] should be non-empty RFC3339 timestamp")
	}
}

// TestMempalaceWriteHandoff_WritesToMP_Error verifies that a KGAdd error
// is propagated back as ok=false in the RPC result.
func TestMempalaceWriteHandoff_WritesToMP_Error(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("kgadd transient error")
	stub := &stubMP{err: sentinel}

	ok, reason := writeHandoffWithStub(stub, "codex", "claude", "briefing")
	if ok {
		t.Error("expected ok=false when KGAdd returns error")
	}
	if !errors.Is(errors.New(reason), errors.New(sentinel.Error())) {
		// Just check the reason is non-empty; exact message varies.
		if reason == "" {
			t.Error("expected non-empty reason on KGAdd error")
		}
	}
}
