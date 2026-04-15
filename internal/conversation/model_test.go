package conversation

import "testing"

func TestConversationSegmentLifecycle(t *testing.T) {
	t.Parallel()

	c := New("conv-1", "b1", "fix auth")
	if len(c.Transcript) != 1 {
		t.Fatalf("expected initial user turn, got %d", len(c.Transcript))
	}

	seg := c.StartSegment("claude")
	if seg.Provider != "claude" {
		t.Fatalf("provider = %q", seg.Provider)
	}
	c.SetNativeSessionID("claude", "sess-1")
	c.EndActiveSegment(SegmentExhausted, "limit")

	if c.ActiveSegment() != nil {
		t.Fatal("expected no active segment")
	}
	if got := c.Segments[0].NativeSessionID; got != "sess-1" {
		t.Fatalf("native session id = %q", got)
	}
	if got := c.Segments[0].Status; got != SegmentExhausted {
		t.Fatalf("status = %q", got)
	}
}

func TestConversationNativeSessionIDs(t *testing.T) {
	t.Parallel()

	c := New("conv-1", "b1", "fix auth")
	c.StartSegment("claude")
	c.SetNativeSessionID("claude", "sess-1")
	c.EndActiveSegment(SegmentDone, "done")
	c.StartSegment("codex")

	got := c.NativeSessionIDs()
	if got["claude"] != "sess-1" {
		t.Fatalf("NativeSessionIDs = %#v", got)
	}
}

func TestConversationSnapshot(t *testing.T) {
	t.Parallel()

	c := New("conv-1", "b1", "fix auth")
	c.Memory.WorkingSummary = "inspected routing"
	c.Memory.NextAction = "continue in codex"
	c.Context.SpecRefs = []string{"spec-1"}
	c.StartSegment("claude")

	ckpt := c.Snapshot("provider exhausted")
	if ckpt.ConversationID != "conv-1" || ckpt.BlockID != "b1" {
		t.Fatalf("checkpoint ids = %#v", ckpt)
	}
	if ckpt.Reason != "provider exhausted" {
		t.Fatalf("Reason = %q", ckpt.Reason)
	}
	if ckpt.SegmentProvider != "claude" {
		t.Fatalf("SegmentProvider = %q", ckpt.SegmentProvider)
	}
	if ckpt.WorkingMemory.NextAction != "continue in codex" {
		t.Fatalf("WorkingMemory = %#v", ckpt.WorkingMemory)
	}
}
