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
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/mwigge/milliways/internal/daemon/textproc"
	"github.com/mwigge/milliways/internal/parallel"
	"github.com/mwigge/milliways/internal/security"
)

// JSON-RPC method handlers for the agent.* surface. Each handler reads
// params off the Request, calls the AgentRegistry / AgentSession, and
// writes either a result or a typed error.

type agentOpenParams struct {
	AgentID   string `json:"agent_id"`
	SessionID string `json:"session_id,omitempty"`
	// SecurityContext controls whether a security context priming block is
	// injected before the first user turn. Defaults to true when nil.
	SecurityContext *bool `json:"security_context,omitempty"`
}

type agentOpenResult struct {
	Handle int64 `json:"handle"`
	// PtySize is reserved for future PTY allocation negotiation.
	PtySize ptySize `json:"pty_size"`
}

type ptySize struct {
	Cols int `json:"cols"`
	Rows int `json:"rows"`
}

func (s *Server) agentOpen(enc *json.Encoder, req *Request) {
	var p agentOpenParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			writeError(enc, req.ID, ErrInvalidParams, "invalid agent.open params: "+err.Error())
			return
		}
	}
	if p.AgentID == "" {
		writeError(enc, req.ID, ErrInvalidParams, "agent.open requires agent_id")
		return
	}
	sess, err := s.agents.Open(p.AgentID)
	if err != nil {
		// Reserved/known agent_ids that aren't implemented yet hit here.
		// Real runner lift (TASK-1.4) plugs claude/codex/minimax/copilot in.
		if strings.HasPrefix(err.Error(), "agent_not_implemented") {
			writeError(enc, req.ID, ErrAgentNotImplemented,
				"agent_id "+p.AgentID+" not yet wired (runner lift pending; TASK-1.4)")
			return
		}
		writeError(enc, req.ID, ErrInvalidParams, err.Error())
		return
	}
	// Track current agent for the status bar.
	s.statusMu.Lock()
	s.currentAgent = p.AgentID
	s.statusMu.Unlock()

	// Inject security context priming block BEFORE writeResult so the session
	// receives it before the client starts sending messages.
	injectSec := p.SecurityContext == nil || *p.SecurityContext
	if injectSec && s.pantryDB != nil {
		findings, err := s.pantryDB.Security().ListActive([]string{"CRITICAL", "HIGH"})
		if err != nil {
			slog.Debug("security context: list active findings", "err", err)
		} else if len(findings) > 0 {
			block := security.BuildContextBlock(findings, security.DefaultTokenCap)
			if block != "" {
				if sendErr := sess.Send([]byte(block)); sendErr != nil {
					slog.Debug("security context: send priming block", "err", sendErr)
				}
			}
		}
	}

	// Wire turn-complete hook: extract findings from each response and write to MemPalace.
	if s.pantryDB != nil {
		mp := s.mempalaceClient()
		agentID := p.AgentID
		sess.onTurnComplete = func(finalText string) {
			if mp == nil {
				return
			}
			findings := parallel.ExtractFindings(finalText)
			if len(findings) == 0 {
				return
			}
			if err := parallel.WriteFindings(context.Background(), findings, mp, agentID, "deck"); err != nil {
				slog.Debug("agentOpen: WriteFindings failed", "agent", agentID, "err", err)
			}
		}
	}

	// Inject pending cross-pane handoff briefing for deck /takeover.
	if s.pantryDB != nil {
		if mp := s.mempalaceClient(); mp != nil {
			triples, err := mp.KGQuery(context.Background(), "handoff:"+p.AgentID, "takeover_briefing", nil)
			if err != nil {
				slog.Debug("handoff: KGQuery failed", "agent", p.AgentID, "err", err)
			} else if len(triples) > 0 {
				t := triples[len(triples)-1]
				if t.Object != "" {
					from := t.Properties["from"]
					_ = sess.Send([]byte("[handoff from " + from + "]\n" + t.Object))
				}
			}
		}
	}

	writeResult(enc, req.ID, agentOpenResult{
		Handle:  int64(sess.Handle),
		PtySize: ptySize{Cols: 80, Rows: 24},
	})
}

type agentSendParams struct {
	Handle        int64  `json:"handle"`
	B64           string `json:"b64,omitempty"`   // base64 bytes
	Bytes         string `json:"bytes,omitempty"` // raw string (alt to b64)
	ExpandContext *bool  `json:"expand_context,omitempty"`
}

func (s *Server) agentSend(enc *json.Encoder, req *Request) {
	var p agentSendParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		writeError(enc, req.ID, ErrInvalidParams, "invalid agent.send params: "+err.Error())
		return
	}
	sess, ok := s.agents.Get(AgentHandle(p.Handle))
	if !ok {
		writeError(enc, req.ID, ErrInvalidParams, "unknown handle")
		return
	}
	var bytes []byte
	if p.B64 != "" {
		var err error
		bytes, err = base64.StdEncoding.DecodeString(p.B64)
		if err != nil {
			writeError(enc, req.ID, ErrInvalidParams, "bad base64: "+err.Error())
			return
		}
	} else {
		bytes = []byte(p.Bytes)
	}
	promptBytes := append([]byte(nil), bytes...)
	// expand_context defaults to true; explicit false opts out.
	if p.ExpandContext == nil || *p.ExpandContext {
		bytes = textproc.ExpandContext(context.Background(), bytes)
	}
	sess.recordPrompt(string(promptBytes))

	// On the first send in a session, inject MemPalace baseline context before
	// the user's bytes. CompareAndSwap(0→1) ensures exactly one send injects.
	if sess.firstSendDone.CompareAndSwap(0, 1) && s.pantryDB != nil {
		mp := s.mempalaceClient()
		ctx := context.Background()
		baseline := parallel.InjectBaseline(ctx, string(bytes), mp)
		if baseline != "" {
			if err := sess.Send([]byte(baseline)); err != nil {
				slog.Debug("agentSend: baseline inject failed", "err", err)
			}
		}
		// TODO: wire cg when CodeGraph MCP client is available on Server.
		_ = parallel.InjectCodeGraph(ctx, string(bytes), nil)
	}

	if err := sess.Send(bytes); err != nil {
		writeError(enc, req.ID, ErrInvalidParams, err.Error())
		return
	}
	writeResult(enc, req.ID, map[string]any{"sent": len(bytes)})
}

type agentStreamParams struct {
	Handle int64 `json:"handle"`
}

type agentStreamResult struct {
	StreamID     int64 `json:"stream_id"`
	OutputOffset int64 `json:"output_offset"`
}

func (s *Server) agentStream(enc *json.Encoder, req *Request) {
	var p agentStreamParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		writeError(enc, req.ID, ErrInvalidParams, "invalid agent.stream params: "+err.Error())
		return
	}
	sess, ok := s.agents.Get(AgentHandle(p.Handle))
	if !ok {
		writeError(enc, req.ID, ErrInvalidParams, "unknown handle")
		return
	}
	stream := sess.AttachStream(s)
	writeResult(enc, req.ID, agentStreamResult{
		StreamID:     stream.ID,
		OutputOffset: 0,
	})
}

type agentCloseParams struct {
	Handle int64 `json:"handle"`
}

func (s *Server) agentClose(enc *json.Encoder, req *Request) {
	var p agentCloseParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		writeError(enc, req.ID, ErrInvalidParams, err.Error())
		return
	}
	s.agents.Close(AgentHandle(p.Handle))
	writeResult(enc, req.ID, map[string]any{"closed": true})
}

type agentSetActiveParams struct {
	AgentID string `json:"agent_id"`
}

func (s *Server) agentSetActive(enc *json.Encoder, req *Request) {
	var p agentSetActiveParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		writeError(enc, req.ID, ErrInvalidParams, err.Error())
		return
	}
	if p.AgentID == "" {
		writeError(enc, req.ID, ErrInvalidParams, "agent.set_active requires agent_id")
		return
	}
	if !historyAgents[p.AgentID] {
		writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("unknown agent_id: %q", p.AgentID))
		return
	}
	s.statusMu.Lock()
	s.currentAgent = p.AgentID
	s.statusMu.Unlock()
	writeResult(enc, req.ID, map[string]any{"ok": true, "active_agent": p.AgentID})
}

func (s *Server) deckSnapshot(enc *json.Encoder, req *Request) {
	s.statusMu.Lock()
	active := s.currentAgent
	s.statusMu.Unlock()
	writeResult(enc, req.ID, s.agents.DeckSnapshot(active))
}
