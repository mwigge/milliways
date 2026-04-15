package tui

import (
	"testing"
	"time"
)

func TestModel_ConcurrentDispatch_UnderLimit(t *testing.T) {
	t.Parallel()

	m := Model{maxConcurrent: 4}

	// Simulate 3 dispatches — all should be active, none queued.
	for i := 0; i < 3; i++ {
		m.blocks = append(m.blocks, Block{
			ID:    m.nextBlockID(),
			State: StateStreaming,
		})
		m.activeCount++
	}

	if m.activeCount != 3 {
		t.Errorf("activeCount = %d, want 3", m.activeCount)
	}
	if m.queue.Len() != 0 {
		t.Error("queue should be empty when under limit")
	}
}

func TestModel_ConcurrentDispatch_AtLimit(t *testing.T) {
	t.Parallel()

	m := Model{maxConcurrent: 2}

	// Fill to capacity.
	for i := 0; i < 2; i++ {
		m.blocks = append(m.blocks, Block{
			ID:    m.nextBlockID(),
			State: StateStreaming,
		})
		m.activeCount++
	}

	// Next task should overflow to queue.
	if m.activeCount < m.maxConcurrent {
		t.Fatal("should be at limit")
	}

	ok := m.queue.Enqueue(QueuedTask{Prompt: "overflow task", QueuedAt: time.Now()})
	if !ok {
		t.Error("should be able to enqueue overflow task")
	}
	if m.queue.Len() != 1 {
		t.Errorf("queue len = %d, want 1", m.queue.Len())
	}
}

func TestModel_Dequeue_OnCompletion(t *testing.T) {
	t.Parallel()

	m := Model{maxConcurrent: 1}
	m.blocks = append(m.blocks, Block{
		ID:    m.nextBlockID(),
		State: StateStreaming,
	})
	m.activeCount = 1

	m.queue.Enqueue(QueuedTask{Prompt: "queued task", QueuedAt: time.Now()})

	// Simulate block completion.
	m.blocks[0].State = StateDone
	m.activeCount--

	// Check dequeue is possible.
	task, ok := m.queue.Dequeue()
	if !ok {
		t.Fatal("should dequeue when capacity is available")
	}
	if task.Prompt != "queued task" {
		t.Errorf("dequeued wrong task: %q", task.Prompt)
	}
	if m.activeCount >= m.maxConcurrent {
		t.Error("should have capacity after decrement")
	}
}

func TestModel_FindBlock(t *testing.T) {
	t.Parallel()

	m := Model{}
	m.blocks = append(m.blocks,
		Block{ID: "b1", Prompt: "first"},
		Block{ID: "b2", Prompt: "second"},
		Block{ID: "b3", Prompt: "third"},
	)

	b := m.findBlock("b2")
	if b == nil {
		t.Fatal("should find block b2")
	}
	if b.Prompt != "second" {
		t.Errorf("wrong block: %q", b.Prompt)
	}

	b = m.findBlock("nonexistent")
	if b != nil {
		t.Error("should return nil for nonexistent block")
	}
}

func TestModel_BlockIndex(t *testing.T) {
	t.Parallel()

	m := Model{focusedIdx: 0}
	m.blocks = append(m.blocks,
		Block{ID: "b1"},
		Block{ID: "b2"},
		Block{ID: "b3"},
	)

	if idx := m.blockIndex("b2"); idx != 1 {
		t.Errorf("blockIndex(b2) = %d, want 1", idx)
	}
	if idx := m.blockIndex("missing"); idx != 0 {
		t.Errorf("blockIndex(missing) = %d, want focusedIdx (0)", idx)
	}
}

func TestModel_FocusedBlock(t *testing.T) {
	t.Parallel()

	m := Model{focusedIdx: 1}
	m.blocks = append(m.blocks,
		Block{ID: "b1", Prompt: "first"},
		Block{ID: "b2", Prompt: "second"},
	)

	b := m.focusedBlock()
	if b == nil || b.Prompt != "second" {
		t.Error("should return focused block b2")
	}

	m2 := Model{focusedIdx: 5} // out of range
	if m2.focusedBlock() != nil {
		t.Error("out of range focusedIdx should return nil")
	}
}

func TestModel_HasCompletedBlocks(t *testing.T) {
	t.Parallel()

	m := Model{}
	if m.hasCompletedBlocks() {
		t.Error("no blocks should mean no completed blocks")
	}

	m.blocks = append(m.blocks, Block{State: StateStreaming})
	if m.hasCompletedBlocks() {
		t.Error("streaming block is not completed")
	}

	m.blocks = append(m.blocks, Block{State: StateDone})
	if !m.hasCompletedBlocks() {
		t.Error("done block should count as completed")
	}
}

func TestModel_NextBlockID(t *testing.T) {
	t.Parallel()

	m := Model{}
	id1 := m.nextBlockID()
	id2 := m.nextBlockID()
	id3 := m.nextBlockID()

	if id1 == id2 || id2 == id3 {
		t.Error("block IDs should be unique")
	}
	if id1 != "b1" || id2 != "b2" || id3 != "b3" {
		t.Errorf("unexpected IDs: %s, %s, %s", id1, id2, id3)
	}
}

func TestModel_SetMaxConcurrent(t *testing.T) {
	t.Parallel()

	m := Model{maxConcurrent: defaultMaxConcurrent}

	m.SetMaxConcurrent(8)
	if m.maxConcurrent != 8 {
		t.Errorf("maxConcurrent = %d, want 8", m.maxConcurrent)
	}

	m.SetMaxConcurrent(0)
	if m.maxConcurrent != 1 {
		t.Errorf("maxConcurrent = %d, want 1 (minimum)", m.maxConcurrent)
	}

	m.SetMaxConcurrent(-5)
	if m.maxConcurrent != 1 {
		t.Errorf("maxConcurrent = %d, want 1 (minimum)", m.maxConcurrent)
	}
}
