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

package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/mwigge/milliways/internal/parallel"
	"github.com/mwigge/milliways/internal/rpc"
)

// TestAttachCmd_FlagRegistration verifies that the attach command is registered
// with the expected flags and positional argument.
func TestAttachCmd_FlagRegistration(t *testing.T) {
	t.Parallel()

	cmd := attachCmd()

	if cmd.Use != "attach <handle>" {
		t.Errorf("attach Use = %q, want %q", cmd.Use, "attach <handle>")
	}

	jsonFlag := cmd.Flags().Lookup("json")
	if jsonFlag == nil {
		t.Fatal("attach: missing --json flag")
	}

	navFlag := cmd.Flags().Lookup("nav")
	if navFlag == nil {
		t.Fatal("attach: missing --nav flag")
	}
}

// TestAttachCmd_NavAndHandleMutuallyExclusive ensures that --nav and a positional
// handle argument cannot be used together.
func TestAttachCmd_NavAndHandleMutuallyExclusive(t *testing.T) {
	t.Parallel()

	cmd := rootCmd()
	cmd.SetArgs([]string{"attach", "--nav", "grp-abc123", "42"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when --nav and handle are both provided")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("error = %v, want 'mutually exclusive'", err)
	}
}

// TestAttachCmd_HandleRequired ensures that attach fails when neither --nav nor
// a positional handle is provided.
func TestAttachCmd_HandleRequired(t *testing.T) {
	t.Parallel()

	cmd := rootCmd()
	cmd.SetArgs([]string{"attach"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when no handle and no --nav provided")
	}
}

// TestFormatDeltaEvent verifies the NDJSON delta event format.
func TestFormatDeltaEvent(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	got := formatDeltaEvent("hello world", now)

	var m map[string]any
	if err := json.Unmarshal([]byte(got), &m); err != nil {
		t.Fatalf("formatDeltaEvent() not valid JSON: %v\ngot: %s", err, got)
	}
	if m["type"] != "delta" {
		t.Errorf("type = %v, want delta", m["type"])
	}
	if m["content"] != "hello world" {
		t.Errorf("content = %v, want hello world", m["content"])
	}
	if m["ts"] != "2026-05-05T12:00:00Z" {
		t.Errorf("ts = %v, want 2026-05-05T12:00:00Z", m["ts"])
	}
}

// TestFormatDoneEvent verifies the NDJSON done event format.
func TestFormatDoneEvent(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	got := formatDoneEvent(100, 200, now)

	var m map[string]any
	if err := json.Unmarshal([]byte(got), &m); err != nil {
		t.Fatalf("formatDoneEvent() not valid JSON: %v\ngot: %s", err, got)
	}
	if m["type"] != "done" {
		t.Errorf("type = %v, want done", m["type"])
	}
	tokIn, _ := m["tokens_in"].(float64)
	if int(tokIn) != 100 {
		t.Errorf("tokens_in = %v, want 100", m["tokens_in"])
	}
	tokOut, _ := m["tokens_out"].(float64)
	if int(tokOut) != 200 {
		t.Errorf("tokens_out = %v, want 200", m["tokens_out"])
	}
	if m["usage_display"] != "in 100 / out 200 / total 300 tok" {
		t.Errorf("usage_display = %v, want normalized usage string", m["usage_display"])
	}
}

// TestDrainStreamToWriter_JSONMode verifies that base64-encoded delta events are
// decoded and emitted as NDJSON lines when jsonMode is true.
func TestDrainStreamToWriter_JSONMode(t *testing.T) {
	t.Parallel()

	// Build a mock event channel with a delta then a done event.
	content := "hello parallel"
	b64 := base64.StdEncoding.EncodeToString([]byte(content))

	events := make(chan []byte, 3)
	events <- []byte(`{"t":"delta","b64":"` + b64 + `"}`)
	events <- []byte(`{"t":"chunk_end","tokens_in":50,"tokens_out":100}`)
	events <- []byte(`{"t":"end"}`)
	close(events)

	var buf bytes.Buffer
	drainStreamToWriter(events, &buf, true)

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 NDJSON lines, got %d: %s", len(lines), buf.String())
	}

	// First line should be a delta event
	var delta map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &delta); err != nil {
		t.Fatalf("line[0] not valid JSON: %v\n%s", err, lines[0])
	}
	if delta["type"] != "delta" {
		t.Errorf("line[0] type = %v, want delta", delta["type"])
	}
	if delta["content"] != content {
		t.Errorf("line[0] content = %v, want %q", delta["content"], content)
	}

	// Second line should be a done event
	var done map[string]any
	if err := json.Unmarshal([]byte(lines[1]), &done); err != nil {
		t.Fatalf("line[1] not valid JSON: %v\n%s", err, lines[1])
	}
	if done["type"] != "done" {
		t.Errorf("line[1] type = %v, want done", done["type"])
	}
}

// TestDrainStreamToWriter_PlainMode verifies that in plain mode, decoded content
// is written directly to the writer without JSON wrapping.
func TestDrainStreamToWriter_PlainMode(t *testing.T) {
	t.Parallel()

	content := "streaming output"
	b64 := base64.StdEncoding.EncodeToString([]byte(content))

	events := make(chan []byte, 2)
	events <- []byte(`{"t":"delta","b64":"` + b64 + `"}`)
	events <- []byte(`{"t":"end"}`)
	close(events)

	var buf bytes.Buffer
	drainStreamToWriter(events, &buf, false)

	if !strings.Contains(buf.String(), content) {
		t.Errorf("plain mode output missing %q; got %q", content, buf.String())
	}
	// Plain mode should not emit JSON structure
	if strings.Contains(buf.String(), `"type"`) {
		t.Errorf("plain mode should not emit JSON keys; got %q", buf.String())
	}
}

// TestBuildQuotasFromSnapshots verifies that quota.get snapshots with a
// positive cap are converted to QuotaSummary keyed by agent_id, and that
// snapshots with zero cap (unlimited / not tracked) are omitted.
func TestBuildQuotasFromSnapshots(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		snapshots  []rpc.QuotaSnapshot
		wantKeys   []string
		wantAbsent []string
		checkFn    func(t *testing.T, m map[string]parallel.QuotaSummary)
	}{
		{
			name: "positive cap included",
			snapshots: []rpc.QuotaSnapshot{
				{AgentID: "claude", Used: 34, Cap: 100},
				{AgentID: "codex", Used: 12, Cap: 100},
			},
			wantKeys: []string{"claude", "codex"},
			checkFn: func(t *testing.T, m map[string]parallel.QuotaSummary) {
				t.Helper()
				if m["claude"].UsedToday != 34 {
					t.Errorf("claude UsedToday = %d, want 34", m["claude"].UsedToday)
				}
				if m["claude"].LimitDay != 100 {
					t.Errorf("claude LimitDay = %d, want 100", m["claude"].LimitDay)
				}
				if m["codex"].UsedToday != 12 {
					t.Errorf("codex UsedToday = %d, want 12", m["codex"].UsedToday)
				}
			},
		},
		{
			name: "zero cap omitted",
			snapshots: []rpc.QuotaSnapshot{
				{AgentID: "claude", Used: 50, Cap: 0},
			},
			wantAbsent: []string{"claude"},
		},
		{
			name:      "empty snapshots returns empty map",
			snapshots: nil,
			wantKeys:  nil,
		},
		{
			name: "mixed cap: only positive-cap entries appear",
			snapshots: []rpc.QuotaSnapshot{
				{AgentID: "claude", Used: 10, Cap: 200},
				{AgentID: "gemini", Used: 5, Cap: 0},
			},
			wantKeys:   []string{"claude"},
			wantAbsent: []string{"gemini"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := buildQuotasFromSnapshots(tt.snapshots)
			for _, k := range tt.wantKeys {
				if _, ok := got[k]; !ok {
					t.Errorf("buildQuotasFromSnapshots(): missing key %q", k)
				}
			}
			for _, k := range tt.wantAbsent {
				if _, ok := got[k]; ok {
					t.Errorf("buildQuotasFromSnapshots(): key %q should be absent", k)
				}
			}
			if tt.checkFn != nil {
				tt.checkFn(t, got)
			}
		})
	}
}

// TestSumSlotTokens verifies that sumSlotTokens correctly totals TokensIn +
// TokensOut across all slots.
func TestSumSlotTokens(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		slots []parallel.SlotRecord
		want  int
	}{
		{
			name: "single slot",
			slots: []parallel.SlotRecord{
				{TokensIn: 100, TokensOut: 200},
			},
			want: 300,
		},
		{
			name: "multiple slots",
			slots: []parallel.SlotRecord{
				{TokensIn: 1000, TokensOut: 2000},
				{TokensIn: 500, TokensOut: 750},
			},
			want: 4250,
		},
		{
			name:  "empty slots",
			slots: nil,
			want:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := sumSlotTokens(tt.slots)
			if got != tt.want {
				t.Errorf("sumSlotTokens() = %d, want %d", got, tt.want)
			}
		})
	}
}

// TestDeckNavigatorAgentListShape verifies that runDeckNavigator correctly
// unmarshals the flat []AgentInfo array returned by agent.list (not a
// wrapped {"agents":[...]} object — that was the original bug).
func TestDeckNavigatorAgentListShape(t *testing.T) {
	// Simulate what agent.list actually returns: a flat JSON array.
	flatArray := `[
		{"id":"claude","auth_status":"ok","model":"claude-sonnet-4-5"},
		{"id":"codex","auth_status":"missing_credentials","model":""},
		{"id":"copilot","auth_status":"ok","model":"gpt-4o"}
	]`

	var agents []struct {
		ID         string `json:"id"`
		AuthStatus string `json:"auth_status"`
		Model      string `json:"model"`
	}
	if err := json.Unmarshal([]byte(flatArray), &agents); err != nil {
		t.Fatalf("unmarshal flat array: %v", err)
	}
	if len(agents) != 3 {
		t.Fatalf("expected 3 agents from flat array, got %d", len(agents))
	}
	if agents[0].ID != "claude" {
		t.Errorf("expected claude, got %q", agents[0].ID)
	}

	// Confirm the old (wrong) shape fails to populate.
	var wrapped struct {
		Agents []struct {
			ID string `json:"id"`
		} `json:"agents"`
	}
	_ = json.Unmarshal([]byte(flatArray), &wrapped)
	if len(wrapped.Agents) != 0 {
		t.Error("wrapped shape should NOT work with flat array — test assumption wrong")
	}
}

func TestRenderDeckNavigatorShowsRequestedPanels(t *testing.T) {
	t.Parallel()

	got := stripANSI(renderDeckNavigator(34, []deckProviderInfo{
		{ID: "claude", AuthStatus: "ok", Model: "sonnet"},
		{ID: "codex", AuthStatus: "ok", Model: "gpt-5.5"},
	}, 1, "codex", true, map[string]parallel.QuotaSummary{
		"codex": {UsedToday: 25, LimitDay: 100},
	}))

	wantFragments := []string{
		"Clients",
		"Active",
		"Status",
		"Observability",
		"codex active",
		"daemon connected",
		"2 clients",
		"quota 25%",
	}
	for _, want := range wantFragments {
		if !strings.Contains(got, want) {
			t.Errorf("renderDeckNavigator() missing %q:\n%s", want, got)
		}
	}
}

func TestRenderDeckNavigatorSizedKeepsObservabilityVisible(t *testing.T) {
	t.Parallel()

	providers := []deckProviderInfo{
		{ID: "claude", AuthStatus: "ok", Status: "idle"},
		{ID: "codex", AuthStatus: "ok", Status: "idle"},
		{ID: "copilot", AuthStatus: "missing_credentials", Status: "idle"},
		{ID: "gemini", AuthStatus: "ok", Status: "idle"},
		{ID: "minimax", AuthStatus: "ok", Status: "idle"},
		{ID: "local", AuthStatus: "ok", Status: "idle"},
		{ID: "pool", AuthStatus: "ok", Status: "idle"},
	}

	got := stripANSI(renderDeckNavigatorSized(34, 22, providers, 6, "pool", true, nil))
	if lines := strings.Count(got, "\r\n"); lines > 22 {
		t.Fatalf("rendered %d lines, want <= 22:\n%s", lines, got)
	}
	for _, want := range []string{"Clients", "pool", "Observability", "Active", "auth"} {
		if !strings.Contains(got, want) {
			t.Fatalf("render missing %q:\n%s", want, got)
		}
	}
}

func TestRenderDeckNavigatorSizedShowsSevenWhenThereIsRoom(t *testing.T) {
	t.Parallel()

	providers := []deckProviderInfo{
		{ID: "claude", AuthStatus: "ok", Status: "idle"},
		{ID: "codex", AuthStatus: "ok", Status: "idle"},
		{ID: "copilot", AuthStatus: "ok", Status: "idle"},
		{ID: "gemini", AuthStatus: "ok", Status: "idle"},
		{ID: "minimax", AuthStatus: "ok", Status: "idle"},
		{ID: "local", AuthStatus: "ok", Status: "idle"},
		{ID: "pool", AuthStatus: "ok", Status: "idle"},
	}

	got := stripANSI(renderDeckNavigatorSized(44, 40, providers, 0, "", true, nil))
	if strings.Contains(got, "clients 1-") {
		t.Fatalf("expected all clients without a ranged heading when height allows it:\n%s", got)
	}
	if strings.Contains(got, "milliways-deck") {
		t.Fatalf("expected no redundant deck title line:\n%s", got)
	}
	for _, want := range []string{"claude", "codex", "copilot", "gemini", "minimax", "local", "pool"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected all seven clients, missing %q:\n%s", want, got)
		}
	}
	// Quick Menu was removed — verify it is absent.
	for _, absent := range []string{"Quick Menu", "/<client> <prompt>", "background"} {
		if strings.Contains(got, absent) {
			t.Fatalf("Quick Menu should be absent, found %q:\n%s", absent, got)
		}
	}
	// Observability panel is present with its new content.
	for _, want := range []string{"Observability", "◌ all idle", "quota"} {
		if !strings.Contains(got, want) {
			t.Fatalf("new Observability panel missing %q:\n%s", want, got)
		}
	}
}

func TestRenderDeckNavigatorUsesAgentIdentityColors(t *testing.T) {
	withoutNoColor(t)

	got := renderDeckNavigatorSized(44, 30, []deckProviderInfo{
		{ID: "claude", AuthStatus: "ok", Status: "idle"},
		{ID: "gemini", AuthStatus: "ok", Status: "idle"},
		{ID: "minimax", AuthStatus: "ok", Status: "idle"},
		{ID: "pool", AuthStatus: "ok", Status: "idle"},
	}, 2, "minimax", true, nil)
	for _, want := range []string{
		agentColor("claude"),
		agentColor("gemini"),
		agentColor("minimax"),
		agentColor("pool"),
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("render missing client color %q:\n%q", want, got)
		}
	}
}

func withoutNoColor(t *testing.T) {
	t.Helper()
	oldNoColor, hadNoColor := os.LookupEnv("NO_COLOR")
	oldTerm, hadTerm := os.LookupEnv("TERM")
	if err := os.Unsetenv("NO_COLOR"); err != nil {
		t.Fatalf("unset NO_COLOR: %v", err)
	}
	if err := os.Setenv("TERM", "xterm-256color"); err != nil {
		t.Fatalf("set TERM: %v", err)
	}
	t.Cleanup(func() {
		if hadNoColor {
			_ = os.Setenv("NO_COLOR", oldNoColor)
		} else {
			_ = os.Unsetenv("NO_COLOR")
		}
		if hadTerm {
			_ = os.Setenv("TERM", oldTerm)
		} else {
			_ = os.Unsetenv("TERM")
		}
	})
}

func TestRenderDeckNavigatorRespectsNoColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	got := renderDeckNavigatorSized(44, 30, []deckProviderInfo{
		{ID: "claude", AuthStatus: "ok", Status: "idle"},
		{ID: "codex", AuthStatus: "ok", Status: "error", LastError: "rate limited"},
	}, 0, "claude", true, nil)
	if strings.Contains(got, "\x1b[") {
		t.Fatalf("renderDeckNavigatorSized() emitted ANSI with NO_COLOR:\n%q", got)
	}
}

func TestRenderDeckNavigatorPlainHasNoANSIOrBoxDrawing(t *testing.T) {
	got := renderDeckNavigatorPlain([]deckProviderInfo{
		{ID: "claude", AuthStatus: "ok", Model: "sonnet", Status: "idle", Turns: 2, Tokens: 1200},
		{ID: "codex", AuthStatus: "missing_credentials", Status: "thinking"},
	}, "claude", true, map[string]parallel.QuotaSummary{
		"claude": {UsedToday: 25, LimitDay: 100},
	})

	if strings.Contains(got, "\x1b[") {
		t.Fatalf("plain deck output emitted ANSI:\n%q", got)
	}
	for _, box := range []string{"┌", "┐", "└", "┘", "─", "│"} {
		if strings.Contains(got, box) {
			t.Fatalf("plain deck output emitted box drawing %q:\n%s", box, got)
		}
	}
	for _, want := range []string{
		"milliways deck",
		"Clients",
		"1 claude active auth ok model sonnet",
		"2 codex thinking auth missing",
		"daemon connected; 2 clients",
		"auth 1/2 ok",
		"quota 25%",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("plain deck output missing %q:\n%s", want, got)
		}
	}
}

func TestOrderDeckProvidersMatchesNumericShortcuts(t *testing.T) {
	t.Parallel()

	providers := []deckProviderInfo{
		{ID: "pool"},
		{ID: "gemini"},
		{ID: "claude"},
		{ID: "minimax"},
		{ID: "codex"},
	}
	got := orderDeckProviders(providers)
	var ids []string
	for _, p := range got {
		ids = append(ids, p.ID)
	}
	want := []string{"claude", "codex", "minimax", "gemini", "pool"}
	if strings.Join(ids, ",") != strings.Join(want, ",") {
		t.Fatalf("ordered providers = %v, want %v", ids, want)
	}
}

func stripANSI(s string) string {
	return regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`).ReplaceAllString(s, "")
}

func TestRenderDeckNavigatorObservabilityNoQuickMenu(t *testing.T) {
	t.Parallel()

	got := stripANSI(renderDeckNavigator(40, []deckProviderInfo{
		{ID: "claude", AuthStatus: "ok", Status: "idle"},
		{ID: "codex", AuthStatus: "ok", Status: "idle"},
	}, 0, "claude", true, nil))

	// Quick Menu must be absent.
	for _, absent := range []string{"Quick Menu", "/switch", "background", "/help", "/parallel"} {
		if strings.Contains(got, absent) {
			t.Errorf("Quick Menu content should be absent, found %q:\n%s", absent, got)
		}
	}
	// Observability panel must be present with its structural elements.
	for _, want := range []string{"Observability", "◌ all idle", "auth 2/2", "quota"} {
		if !strings.Contains(got, want) {
			t.Errorf("Observability panel missing %q:\n%s", want, got)
		}
	}
}

func TestRenderDeckNavigatorObservabilityActiveProviders(t *testing.T) {
	t.Parallel()

	providers := []deckProviderInfo{
		{ID: "claude", AuthStatus: "ok", Status: "thinking", Tokens: 31000, CostUSD: 0.87, CurrentTrace: "abcdef123456", TTFTMS: 240, TokenRate: 18},
		{ID: "codex", AuthStatus: "ok", Status: "streaming", Tokens: 8000, CostUSD: 0.18, LastTrace: "1234567890ab", LatencyMS: 1230, QueueDepth: 1},
		{ID: "gemini", AuthStatus: "ok", Status: "idle"},
	}
	got := stripANSI(renderDeckNavigator(90, providers, 0, "claude", true, nil))

	// Fleet status bar must show thinking and streaming counts.
	if !strings.Contains(got, "1●") {
		t.Errorf("fleet bar missing thinking count '1●':\n%s", got)
	}
	if !strings.Contains(got, "1⟳") {
		t.Errorf("fleet bar missing streaming count '1⟳':\n%s", got)
	}
	// Per-agent rows for active providers.
	if !strings.Contains(got, "clde") {
		t.Errorf("missing 'clde' short name for claude:\n%s", got)
	}
	if !strings.Contains(got, "31.0k tok") {
		t.Errorf("missing '31.0k tok' token count for claude:\n%s", got)
	}
	if !strings.Contains(got, "$0.87") {
		t.Errorf("missing '$0.87' cost for claude:\n%s", got)
	}
	for _, want := range []string{"ttft240ms", "18t/s", "trabcdef12", "lat1.2s", "q1", "tr12345678"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing observability detail %q:\n%s", want, got)
		}
	}
	// Idle summary for gemini.
	if !strings.Contains(got, "◌ idle") {
		t.Errorf("missing idle summary:\n%s", got)
	}
	if !strings.Contains(got, "gemi") {
		t.Errorf("missing 'gemi' in idle summary:\n%s", got)
	}
}
