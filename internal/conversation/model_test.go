package conversation

import (
	"slices"
	"testing"
)

func TestConversationSegmentLifecycle(t *testing.T) {
	t.Parallel()

	c := New("conv-1", "b1", "fix auth")
	if len(c.Transcript) != 1 {
		t.Fatalf("expected initial user turn, got %d", len(c.Transcript))
	}

	repoContext := &RepoContext{
		RepoRoot:         "/tmp/repo",
		RepoName:         "repo",
		Branch:           "main",
		Commit:           "abc123",
		CodeGraphSymbols: 42,
		PalaceDrawers:    7,
	}
	seg := c.StartSegment("claude", repoContext)
	if seg.Provider != "claude" {
		t.Fatalf("provider = %q", seg.Provider)
	}
	if seg.RepoContext == nil || seg.RepoContext.RepoName != "repo" {
		t.Fatalf("repo context = %#v", seg.RepoContext)
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
	if got := c.Segments[0].RepoContext; got == nil || got.Commit != "abc123" {
		t.Fatalf("stored repo context = %#v", got)
	}
}

func TestConversationNativeSessionIDs(t *testing.T) {
	t.Parallel()

	c := New("conv-1", "b1", "fix auth")
	c.StartSegment("claude", nil)
	c.SetNativeSessionID("claude", "sess-1")
	c.EndActiveSegment(SegmentDone, "done")
	c.StartSegment("codex", nil)

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
	c.StartSegment("claude", nil)

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

func TestAppendTurnWithContext_StoresReposAccessed(t *testing.T) {
	t.Parallel()

	c := New("conv-1", "b1", "hello")
	c.Transcript = nil
	repos := []string{"/Users/dev/src/myrepo", "/Users/dev/src/other"}

	c.AppendTurnWithContext(RoleUser, "claude", "hello", repos, nil)

	if len(c.Transcript) != 1 {
		t.Fatalf("len = %d, want 1", len(c.Transcript))
	}
	if !slices.Equal(c.Transcript[0].ReposAccessed, repos) {
		t.Fatalf("ReposAccessed = %v, want %v", c.Transcript[0].ReposAccessed, repos)
	}
}
