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
	"context"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/mwigge/milliways/internal/pantry"
)

// snapshotToMemPalaceAsync stores a handoff briefing in MemPalace in a background goroutine.
// When MILLIWAYS_MEMPALACE_MCP_CMD is absent the goroutine exits immediately.
func snapshotToMemPalaceAsync(briefing string) {
	go func() {
		key := "handoff/" + time.Now().UTC().Format(time.RFC3339)
		if cmd := os.Getenv("MILLIWAYS_MEMPALACE_MCP_CMD"); cmd != "" {
			client, err := pantry.NewMemPalaceClient(cmd, strings.Fields(os.Getenv("MILLIWAYS_MEMPALACE_MCP_ARGS"))...)
			if err != nil {
				slog.Debug("mempalace snapshot failed: client unavailable", "key", key, "err", err)
				return
			}
			defer func() { _ = client.Close() }()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := client.AddDrawer(ctx, pantry.AddDrawerRequest{
				Wing:       "milliways",
				Room:       "handoff",
				Content:    briefing,
				AddedBy:    "milliways",
				SourceFile: key,
			}); err != nil {
				slog.Debug("mempalace snapshot failed", "key", key, "err", err)
				return
			}
			slog.Debug("mempalace snapshot stored", "key", key)
			return
		}
		slog.Debug("mempalace snapshot skipped: MILLIWAYS_MEMPALACE_MCP_CMD not set", "key", key)
	}()
}
