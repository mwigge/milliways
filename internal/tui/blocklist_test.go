package tui

import (
	"testing"
	"time"
)

func TestRenderBlockList_Empty(t *testing.T) {
	t.Parallel()
	result := RenderBlockList(nil, 0, nil, 24, 12)
	if !containsPlain(result, "No dispatches") {
		t.Error("empty block list should show 'No dispatches'")
	}
}

func TestRenderBlockList_SingleBlock(t *testing.T) {
	t.Parallel()
	blocks := []Block{
		{ID: "b1", Prompt: "fix bug", Kitchen: "claude", State: StateStreaming,
			StartedAt: time.Now().Add(-10 * time.Second)},
	}
	result := RenderBlockList(blocks, 0, nil, 30, 12)
	if !containsPlain(result, "fix bug") {
		t.Error("should show block prompt")
	}
	if !containsPlain(result, "Active: 1") {
		t.Error("should show active count")
	}
}

func TestRenderBlockList_MultipleBlocks(t *testing.T) {
	t.Parallel()
	blocks := []Block{
		{ID: "b1", Prompt: "explain auth", Kitchen: "claude", State: StateDone,
			StartedAt: time.Now().Add(-30 * time.Second), Duration: 25 * time.Second},
		{ID: "b2", Prompt: "fix login bug", Kitchen: "opencode", State: StateStreaming,
			StartedAt: time.Now().Add(-10 * time.Second)},
		{ID: "b3", Prompt: "search OWASP", Kitchen: "gemini", State: StateRouting,
			StartedAt: time.Now().Add(-2 * time.Second)},
	}
	result := RenderBlockList(blocks, 1, nil, 24, 12)
	if !containsPlain(result, "Active: 2") {
		t.Error("should show 2 active blocks")
	}
	if !containsPlain(result, "Total: 3") {
		t.Error("should show total 3")
	}
}

func TestRenderBlockList_FocusIndicator(t *testing.T) {
	t.Parallel()
	blocks := []Block{
		{ID: "b1", Prompt: "first task", State: StateDone, Duration: 5 * time.Second},
		{ID: "b2", Prompt: "second task", State: StateStreaming, StartedAt: time.Now()},
	}
	result := RenderBlockList(blocks, 1, nil, 30, 12)
	// The focused block (index 1) should have ">" prefix
	if !containsPlain(result, ">") {
		t.Error("focused block should have > prefix")
	}
}

func TestRenderBlockList_WithQueue(t *testing.T) {
	t.Parallel()
	blocks := []Block{
		{ID: "b1", Prompt: "running task", State: StateStreaming, StartedAt: time.Now()},
	}
	q := &taskQueue{}
	q.Enqueue(QueuedTask{Prompt: "queued task one", QueuedAt: time.Now()})
	q.Enqueue(QueuedTask{Prompt: "queued task two", QueuedAt: time.Now()})

	result := RenderBlockList(blocks, 0, q, 30, 16)
	if !containsPlain(result, "queued") {
		t.Error("should show queued items")
	}
	if !containsPlain(result, "Queued: 2") {
		t.Error("should show queue count")
	}
}

func TestRenderBlockList_QueueOverflow(t *testing.T) {
	t.Parallel()
	blocks := []Block{
		{ID: "b1", Prompt: "running", State: StateStreaming, StartedAt: time.Now()},
	}
	q := &taskQueue{}
	for i := 0; i < 5; i++ {
		q.Enqueue(QueuedTask{Prompt: "task", QueuedAt: time.Now()})
	}

	result := RenderBlockList(blocks, 0, q, 30, 16)
	if !containsPlain(result, "+2 more") {
		t.Error("should show overflow count for queue > 3")
	}
}
