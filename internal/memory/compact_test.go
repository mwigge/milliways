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

package memory

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mwigge/milliways/internal/observability"
	"github.com/mwigge/milliways/internal/session"
)

type stubProvider struct {
	summary string
	err     error
}

func (s stubProvider) Summarize(_ context.Context, _ []session.Message) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	return s.summary, nil
}

func TestShouldCompact(t *testing.T) {
	t.Parallel()

	sess := &session.Session{Tokens: session.TokenCount{InputTotal: 500, OutputTotal: 300}}
	if !ShouldCompact(sess, 1000) {
		t.Fatal("expected compaction to trigger")
	}
	if ShouldCompact(sess, 2000) {
		t.Fatal("did not expect compaction to trigger")
	}
}

func TestCompactReplacesTranscriptAndAppendsSummary(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 20, 10, 0, 0, 0, time.UTC)
	sess := &session.Session{
		ID:        "session-1",
		CreatedAt: now,
		UpdatedAt: now,
		Messages: []session.Message{
			{Role: session.RoleUser, Content: "first"},
			{Role: session.RoleAssistant, Content: "answer"},
			{Role: session.RoleUser, Content: "last user message"},
		},
	}

	compacted, err := Compact(sess, stubProvider{summary: "short summary"})
	if err != nil {
		t.Fatalf("Compact() error = %v", err)
	}
	if len(compacted.Messages) != 2 {
		t.Fatalf("len(Messages) = %d, want 2", len(compacted.Messages))
	}
	if compacted.Messages[0].Content != "last user message" {
		t.Fatalf("first compacted message = %q", compacted.Messages[0].Content)
	}
	if compacted.Messages[1].Content != "short summary" {
		t.Fatalf("summary message = %q", compacted.Messages[1].Content)
	}
	if got := compacted.Memory[len(compacted.Memory)-1].Key; got != "compact_summary" {
		t.Fatalf("memory key = %q, want compact_summary", got)
	}
	if got := compacted.Events[len(compacted.Events)-1].Kind; got != observability.EventKindSessionCompact {
		t.Fatalf("event kind = %q, want %q", got, observability.EventKindSessionCompact)
	}
}

func TestCompactPropagatesProviderErrors(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("boom")
	_, err := Compact(&session.Session{}, stubProvider{err: wantErr})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Compact() error = %v, want %v", err, wantErr)
	}
}
