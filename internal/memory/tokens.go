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
