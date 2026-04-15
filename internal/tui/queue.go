package tui

import "time"

const maxQueueSize = 20

// QueuedTask is a task waiting to be dispatched.
type QueuedTask struct {
	Prompt       string
	KitchenForce string // from @kitchen prefix, or ""
	QueuedAt     time.Time
}

// taskQueue is a simple FIFO queue for pending tasks.
type taskQueue struct {
	items []QueuedTask
}

// Enqueue adds a task to the back of the queue.
// Returns false if the queue is full.
func (q *taskQueue) Enqueue(task QueuedTask) bool {
	if len(q.items) >= maxQueueSize {
		return false
	}
	q.items = append(q.items, task)
	return true
}

// Dequeue removes and returns the front task.
// Returns false if the queue is empty.
func (q *taskQueue) Dequeue() (QueuedTask, bool) {
	if len(q.items) == 0 {
		return QueuedTask{}, false
	}
	task := q.items[0]
	q.items = q.items[1:]
	return task, true
}

// Len returns the number of queued tasks.
func (q *taskQueue) Len() int {
	return len(q.items)
}

// Peek returns the front task without removing it.
func (q *taskQueue) Peek() (QueuedTask, bool) {
	if len(q.items) == 0 {
		return QueuedTask{}, false
	}
	return q.items[0], true
}
