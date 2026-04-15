package tui

import (
	"testing"
	"time"
)

func TestQueue_FIFO(t *testing.T) {
	var q taskQueue

	q.Enqueue(QueuedTask{Prompt: "first", QueuedAt: time.Now()})
	q.Enqueue(QueuedTask{Prompt: "second", QueuedAt: time.Now()})
	q.Enqueue(QueuedTask{Prompt: "third", QueuedAt: time.Now()})

	if q.Len() != 3 {
		t.Fatalf("Len() = %d, want 3", q.Len())
	}

	task, ok := q.Dequeue()
	if !ok || task.Prompt != "first" {
		t.Errorf("Dequeue() = %q, want %q", task.Prompt, "first")
	}

	task, ok = q.Dequeue()
	if !ok || task.Prompt != "second" {
		t.Errorf("Dequeue() = %q, want %q", task.Prompt, "second")
	}

	task, ok = q.Dequeue()
	if !ok || task.Prompt != "third" {
		t.Errorf("Dequeue() = %q, want %q", task.Prompt, "third")
	}
}

func TestQueue_EmptyDequeue(t *testing.T) {
	var q taskQueue

	_, ok := q.Dequeue()
	if ok {
		t.Error("Dequeue() on empty queue should return false")
	}
}

func TestQueue_Peek(t *testing.T) {
	var q taskQueue

	_, ok := q.Peek()
	if ok {
		t.Error("Peek() on empty queue should return false")
	}

	q.Enqueue(QueuedTask{Prompt: "first"})
	task, ok := q.Peek()
	if !ok || task.Prompt != "first" {
		t.Errorf("Peek() = %q, want %q", task.Prompt, "first")
	}
	if q.Len() != 1 {
		t.Error("Peek() should not remove element")
	}
}

func TestQueue_MaxSize(t *testing.T) {
	var q taskQueue

	for i := 0; i < maxQueueSize; i++ {
		if !q.Enqueue(QueuedTask{Prompt: "task"}) {
			t.Fatalf("Enqueue failed at %d, should accept up to %d", i, maxQueueSize)
		}
	}

	if q.Enqueue(QueuedTask{Prompt: "overflow"}) {
		t.Error("Enqueue should return false when queue is full")
	}

	if q.Len() != maxQueueSize {
		t.Errorf("Len() = %d, want %d", q.Len(), maxQueueSize)
	}
}

func TestQueue_WithKitchenForce(t *testing.T) {
	var q taskQueue
	q.Enqueue(QueuedTask{Prompt: "task", KitchenForce: "gemini"})

	task, ok := q.Dequeue()
	if !ok {
		t.Fatal("Dequeue() failed")
	}
	if task.KitchenForce != "gemini" {
		t.Errorf("KitchenForce = %q, want %q", task.KitchenForce, "gemini")
	}
}
