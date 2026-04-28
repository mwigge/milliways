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
// The function itself spawns a goroutine — callers must NOT wrap it in go.
// When MILLIWAYS_MEMPALACE_MCP_CMD is set, a warning is emitted because the
// actual MCP integration is not yet implemented. When the variable is absent
// the goroutine exits immediately at debug level.
func snapshotToMemPalace(briefing string) {
	go func() {
		key := "handoff/" + time.Now().UTC().Format(time.RFC3339)
		if cmd := os.Getenv("MILLIWAYS_MEMPALACE_MCP_CMD"); cmd != "" {
			slog.Warn("mempalace snapshot not yet implemented", "key", key, "cmd", cmd)
			return
		}
		slog.Debug("mempalace snapshot skipped: MILLIWAYS_MEMPALACE_MCP_CMD not set", "key", key)
	}()
}
