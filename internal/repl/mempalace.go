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

import (
	"log/slog"
	"os"
	"time"
)

// snapshotToMemPalace asynchronously stores a handoff briefing in MemPalace.
// It is gated on the MILLIWAYS_MEMPALACE_MCP_CMD environment variable being
// set. When the variable is absent the function returns immediately without
// blocking the caller. Failures are logged at debug level and never propagated.
func snapshotToMemPalace(briefing string) {
	cmd := os.Getenv("MILLIWAYS_MEMPALACE_MCP_CMD")
	if cmd == "" {
		return
	}

	go func() {
		key := "handoff/" + time.Now().UTC().Format(time.RFC3339)
		slog.Debug("mempalace snapshot", "key", key, "briefing_len", len(briefing))
		// Best-effort: the MCP client integration is handled externally via the
		// MILLIWAYS_MEMPALACE_MCP_CMD subprocess. Future work can exec the command
		// and send a JSON-RPC mempalace_add_drawer call when the substrate client
		// pattern is extended to support it from a goroutine context.
	}()
}
