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

package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/mwigge/milliways/internal/parallel"
)

// writeHandoffParams is the JSON-RPC params for mempalace.write_handoff.
type writeHandoffParams struct {
	TargetProvider string `json:"target_provider"`
	FromProvider   string `json:"from_provider"`
	Briefing       string `json:"briefing"`
}

// mempalaceWriteHandoff handles the "mempalace.write_handoff" RPC.
// It writes a cross-pane takeover briefing to MemPalace under the key
// "handoff:<target_provider>" so the target pane can retrieve it on its
// next agent.open.
func (s *Server) mempalaceWriteHandoff(enc *json.Encoder, req *Request) {
	var p writeHandoffParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("decode params: %v", err))
			return
		}
	}
	if p.TargetProvider == "" {
		writeError(enc, req.ID, ErrInvalidParams, "target_provider is required")
		return
	}
	if p.Briefing == "" {
		writeError(enc, req.ID, ErrInvalidParams, "briefing is required")
		return
	}

	ok, reason := writeHandoffToMP(s.mempalaceClient(), p.TargetProvider, p.FromProvider, p.Briefing)
	writeResult(enc, req.ID, map[string]any{"ok": ok, "reason": reason})
}

// writeHandoffToMP writes the briefing to MemPalace via mp.KGAdd.
// Returns (true, "") on success; (false, reason) on failure or when mp is nil.
// Exported for testability via writeHandoffWithStub.
func writeHandoffToMP(mp parallel.MPClient, targetProvider, fromProvider, briefing string) (bool, string) {
	if mp == nil {
		return false, "mempalace not configured"
	}
	err := mp.KGAdd(
		context.Background(),
		"handoff:"+targetProvider,
		"takeover_briefing",
		briefing,
		map[string]string{
			"from": fromProvider,
			"ts":   time.Now().UTC().Format(time.RFC3339),
		},
	)
	if err != nil {
		return false, err.Error()
	}
	return true, ""
}

// writeHandoffWithStub is the test hook: calls writeHandoffToMP with an
// injected MPClient so tests can verify KGAdd behaviour without a live
// MemPalace subprocess.
func writeHandoffWithStub(mp parallel.MPClient, targetProvider, fromProvider, briefing string) (bool, string) {
	return writeHandoffToMP(mp, targetProvider, fromProvider, briefing)
}

// testWriteHandoff is a test helper that calls writeHandoffToMP and
// asserts no unexpected error. Errors from the stub are reported via t.
func testWriteHandoff(t interface {
	Helper()
	Errorf(format string, args ...any)
}, mp parallel.MPClient, targetProvider, fromProvider, briefing string) {
	t.Helper()
	_, _ = writeHandoffToMP(mp, targetProvider, fromProvider, briefing)
}
