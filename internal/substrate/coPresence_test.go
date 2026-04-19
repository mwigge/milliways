package substrate

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/mwigge/milliways/internal/conversation"
)

type blockingCaller struct {
	mu         sync.Mutex
	started    chan struct{}
	release    chan struct{}
	callCount  int
	resultFunc func() ConversationRecord
}

func newBlockingCaller(resultFunc func() ConversationRecord) *blockingCaller {
	return &blockingCaller{
		started:    make(chan struct{}, 2),
		release:    make(chan struct{}),
		resultFunc: resultFunc,
	}
}

func (b *blockingCaller) CallTool(_ context.Context, toolName string, args map[string]any) (json.RawMessage, error) {
	b.mu.Lock()
	b.callCount++
	b.mu.Unlock()

	if toolName != "mempalace_conversation_get" {
		return mustJSON(struct{}{}), nil
	}

	select {
	case b.started <- struct{}{}:
	default:
	}

	<-b.release

	conversationID, _ := args["conversation_id"].(string)
	rec := b.resultFunc()
	rec.ConversationID = conversationID
	return mustJSON(rec), nil
}

func (b *blockingCaller) calls() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.callCount
}

func waitForSignal(t *testing.T, ch <-chan struct{}, label string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", label)
	}
}

func TestReadOpsCanHappenConcurrentlyForSameConversation(t *testing.T) {
	t.Parallel()

	caller := newBlockingCaller(func() ConversationRecord {
		return ConversationRecord{Prompt: "shared prompt", Status: "active"}
	})
	reader := NewCachedReader(NewWithCaller(caller))

	ctx := context.Background()
	results := make(chan ConversationRecord, 2)
	errCh := make(chan error, 2)

	for range 2 {
		go func() {
			rec, err := reader.GetConversation(ctx, "conv-co-presence")
			if err != nil {
				errCh <- err
				return
			}
			results <- rec
		}()
	}

	waitForSignal(t, caller.started, "first concurrent read")
	waitForSignal(t, caller.started, "second concurrent read")
	close(caller.release)

	for i := 0; i < 2; i++ {
		select {
		case err := <-errCh:
			t.Fatalf("GetConversation: %v", err)
		case rec := <-results:
			if rec.ConversationID != "conv-co-presence" {
				t.Fatalf("ConversationID = %q, want conv-co-presence", rec.ConversationID)
			}
		}
	}

	if got := caller.calls(); got != 2 {
		t.Fatalf("conversation_get calls = %d, want 2 concurrent calls", got)
	}
	if _, err := reader.GetConversation(ctx, "conv-co-presence"); err != nil {
		t.Fatalf("cached GetConversation: %v", err)
	}
	if got := caller.calls(); got != 2 {
		t.Fatalf("conversation_get calls after cache hit = %d, want 2", got)
	}
}

func TestSecondReaderSeesSameStateAsWriter(t *testing.T) {
	t.Parallel()

	var (
		mu    sync.Mutex
		state = ConversationRecord{
			ConversationID: "conv-shared",
			Prompt:         "initial prompt",
			Status:         "active",
			Transcript: []conversation.Turn{{
				Role:     conversation.RoleUser,
				Provider: "user",
				Text:     "initial prompt",
			}},
		}
	)

	caller := &fakeCaller{}
	caller.result = mustJSON(state)
	client := NewWithCaller(caller)
	readerA := NewCachedReader(client)
	readerB := NewCachedReader(client)

	ctx := context.Background()
	if _, err := readerA.GetConversation(ctx, state.ConversationID); err != nil {
		t.Fatalf("readerA initial GetConversation: %v", err)
	}

	mu.Lock()
	state.Memory.WorkingSummary = "writer updated summary"
	state.Memory.NextAction = "continue in codex"
	state.Memory.StickyKitchen = "claude"
	state.Transcript = append(state.Transcript, conversation.Turn{
		Role:     conversation.RoleAssistant,
		Provider: "claude",
		Text:     "updated answer",
	})
	caller.result = mustJSON(state)
	mu.Unlock()

	readerB.InvalidateConversation(state.ConversationID)
	rec, err := readerB.GetConversation(ctx, state.ConversationID)
	if err != nil {
		t.Fatalf("readerB GetConversation: %v", err)
	}
	if rec.Memory.WorkingSummary != "writer updated summary" {
		t.Fatalf("WorkingSummary = %q, want writer updated summary", rec.Memory.WorkingSummary)
	}
	if rec.Memory.StickyKitchen != "claude" {
		t.Fatalf("StickyKitchen = %q, want claude", rec.Memory.StickyKitchen)
	}
	if len(rec.Transcript) != 2 || rec.Transcript[1].Text != "updated answer" {
		t.Fatalf("Transcript = %#v, want updated shared transcript", rec.Transcript)
	}
}
