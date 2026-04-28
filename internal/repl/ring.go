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

package repl

import "errors"

// ErrRingExhausted is returned by nextRingRunner when every runner in the ring
// has no remaining quota.
var ErrRingExhausted = errors.New("all ring runners exhausted")

// nextRingRunner advances the ring position and returns the next runner name.
// It skips runners whose daily quota is zero (pantry-reported).
// Returns (name, newPos, nil) on success, or ("", -1, ErrRingExhausted) when
// every runner in the ring is unavailable.
//
// available is a callback returning true if the named runner has quota > 0.
func nextRingRunner(ring *RingConfig, available func(name string) bool) (string, int, error) {
	if ring == nil {
		return "", -1, ErrRingExhausted
	}
	n := len(ring.Runners)
	if n == 0 {
		return "", -1, ErrRingExhausted
	}

	// Try up to n positions starting immediately after the current one.
	for i := 1; i <= n; i++ {
		pos := (ring.Pos + i) % n
		name := ring.Runners[pos]
		if available(name) {
			return name, pos, nil
		}
	}
	return "", -1, ErrRingExhausted
}

// runnerAvailable reports whether the named runner has available daily quota.
// When quota information is absent or unknown, the runner is assumed available.
func (r *REPL) runnerAvailable(name string) bool {
	if r.getQuota == nil {
		return true
	}
	q, err := r.getQuota(name)
	if err != nil || q == nil {
		return true // assume available if quota unknown
	}
	if q.Day != nil && q.Day.Limit > 0 && q.Day.Ratio >= 1.0 {
		return false
	}
	return true
}
