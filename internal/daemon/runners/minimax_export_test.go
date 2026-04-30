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

package runners

// Test-only helpers for the minimax runner. Lives in a *_test.go file so
// the testing import doesn't enter the production binary (Code-quality B1).

import (
	"testing"

	"github.com/mwigge/milliways/internal/tools"
)

// withMinimaxToolRegistry installs `r` as the registry seen by RunMiniMax for
// the duration of the test. Restored automatically on test cleanup.
func withMinimaxToolRegistry(t *testing.T, r *tools.Registry) {
	t.Helper()
	minimaxToolRegistryMu.Lock()
	prev := minimaxToolRegistryOverride
	minimaxToolRegistryOverride = r
	minimaxToolRegistryMu.Unlock()
	t.Cleanup(func() {
		minimaxToolRegistryMu.Lock()
		minimaxToolRegistryOverride = prev
		minimaxToolRegistryMu.Unlock()
	})
}
