package memory

import "sync"

// TokenTracker keeps running token totals for a session.
type TokenTracker struct {
	mu          sync.Mutex
	inputTotal  int
	outputTotal int
}

// Add increments the running totals.
func (t *TokenTracker) Add(input, output int) {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.inputTotal += input
	t.outputTotal += output
	t.mu.Unlock()
}

// Totals returns the current input and output totals.
func (t *TokenTracker) Totals() (input, output int) {
	if t == nil {
		return 0, 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.inputTotal, t.outputTotal
}
