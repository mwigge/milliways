package integration

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/mwigge/milliways/internal/conversation"
	"github.com/mwigge/milliways/internal/kitchen"
	"github.com/mwigge/milliways/internal/kitchen/adapter"
	"github.com/mwigge/milliways/internal/orchestrator"
	"github.com/mwigge/milliways/internal/sommelier"
	"github.com/mwigge/milliways/internal/tui"
)

func testdataPath(name string) string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "kitchen", "adapter", "testdata", name)
}

// WS-22.1: Full dispatch cycle through ClaudeAdapter → Event stream → Block model
func TestIntegration_ClaudeAdapter_FullCycle(t *testing.T) {
	t.Parallel()

	scriptPath := testdataPath("mock_claude.sh")

	k := kitchen.NewGeneric(kitchen.GenericConfig{
		Name:    "claude",
		Cmd:     scriptPath,
		Enabled: true,
	})

	adapt := adapter.NewClaudeAdapter(k, adapter.AdapterOpts{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	task := kitchen.Task{Prompt: "hello world"}
	eventCh, err := adapt.Exec(ctx, task)
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}

	// Collect events into a Block
	block := &tui.Block{
		ID:        "b1",
		Prompt:    "hello world",
		Kitchen:   "claude",
		Decision:  sommelier.Decision{Kitchen: "claude", Tier: "keyword", Reason: "test dispatch"},
		StartedAt: time.Now(),
	}

	var gotText, gotCode, gotCost, gotDone bool
	var costUSD float64
	exitCode := 0

	for evt := range eventCh {
		block.AppendEvent(evt)

		switch evt.Type {
		case adapter.EventText:
			gotText = true
		case adapter.EventCodeBlock:
			gotCode = true
			if evt.Language != "go" {
				t.Errorf("CodeBlock language = %q, want %q", evt.Language, "go")
			}
		case adapter.EventCost:
			gotCost = true
			costUSD = evt.Cost.USD
		case adapter.EventDone:
			gotDone = true
			exitCode = evt.ExitCode
		}
	}

	sessionID := adapt.SessionID()
	block.Complete(exitCode, &adapter.CostInfo{USD: costUSD})

	if !gotText {
		t.Error("expected EventText events")
	}
	if !gotCode {
		t.Error("expected EventCodeBlock event")
	}
	if !gotCost {
		t.Error("expected EventCost event")
	}
	if !gotDone {
		t.Error("expected EventDone event")
	}

	if costUSD < 0.04 || costUSD > 0.06 {
		t.Errorf("cost = %.4f, want ~0.05", costUSD)
	}

	if sessionID != "test-session-123" {
		t.Errorf("sessionID = %q, want %q", sessionID, "test-session-123")
	}

	// Verify block model
	if len(block.Lines) == 0 {
		t.Error("block should have output lines")
	}

	// Verify rendered body contains expected content
	rendered := block.RenderBody(80, tui.RenderRaw)
	if rendered == "" {
		t.Error("rendered body is empty")
	}

	if !adapt.SupportsResume() {
		t.Error("ClaudeAdapter should support resume")
	}
}

// WS-22.2: Quota exhaustion → failover routing → different kitchen selected
func TestIntegration_QuotaExhaustion_Failover(t *testing.T) {
	t.Parallel()

	reg := kitchen.NewRegistry()
	reg.Register(kitchen.NewGeneric(kitchen.GenericConfig{
		Name:    "claude",
		Cmd:     "echo",
		Args:    []string{"hi"},
		Enabled: true,
	}))
	reg.Register(kitchen.NewGeneric(kitchen.GenericConfig{
		Name:    "opencode",
		Cmd:     "echo",
		Args:    []string{"hi"},
		Enabled: true,
	}))

	som := sommelier.New(
		map[string]string{"explain": "claude"},
		"claude", "opencode", nil, reg,
	)

	checker := &mockQuotaChecker{exhausted: map[string]bool{"claude": true}}
	som.SetQuotaChecker(checker, map[string]int{"claude": 50})

	decision := som.Route("explain the auth flow")
	if decision.Kitchen == "claude" {
		t.Error("should not route to exhausted claude")
	}
	if decision.Kitchen == "" {
		t.Fatal("should find a fallback kitchen")
	}
	if decision.Kitchen != "opencode" {
		t.Errorf("expected fallback to opencode, got %q", decision.Kitchen)
	}

	checker.exhausted["claude"] = false
	decision = som.Route("explain the auth flow")
	if decision.Kitchen != "claude" {
		t.Errorf("after un-exhaust, expected claude, got %q", decision.Kitchen)
	}
}

type mockQuotaChecker struct {
	exhausted map[string]bool
}

func (m *mockQuotaChecker) IsExhausted(k string, _ int) (bool, error) {
	return m.exhausted[k], nil
}

// WS-22.3: Multiple blocks to different kitchens → correct state tracking
func TestIntegration_MultiKitchen_BlockSummary(t *testing.T) {
	t.Parallel()

	blocks := []tui.Block{
		{
			ID:        "b1",
			Prompt:    "explain auth",
			Kitchen:   "claude",
			Decision:  sommelier.Decision{Kitchen: "claude", Tier: "keyword"},
			StartedAt: time.Now().Add(-5 * time.Second),
		},
		{
			ID:        "b2",
			Prompt:    "refactor handler",
			Kitchen:   "opencode",
			Decision:  sommelier.Decision{Kitchen: "opencode", Tier: "keyword"},
			StartedAt: time.Now().Add(-10 * time.Second),
		},
		{
			ID:        "b3",
			Prompt:    "search DORA regs",
			Kitchen:   "gemini",
			Decision:  sommelier.Decision{Kitchen: "gemini", Tier: "keyword"},
			StartedAt: time.Now().Add(-3 * time.Second),
		},
	}

	// Dispatch 1: claude (success)
	blocks[0].AppendEvent(adapter.Event{Type: adapter.EventText, Kitchen: "claude", Text: "Auth uses JWT tokens."})
	blocks[0].AppendEvent(adapter.Event{Type: adapter.EventCodeBlock, Kitchen: "claude", Code: "token := jwt.Sign(claims)", Language: "go"})
	blocks[0].Complete(0, &adapter.CostInfo{USD: 0.14, InputTokens: 200, OutputTokens: 100, DurationMs: 3100})

	// Dispatch 2: opencode (success)
	blocks[1].AppendEvent(adapter.Event{Type: adapter.EventText, Kitchen: "opencode", Text: "Refactoring auth/handler.go..."})
	blocks[1].AppendEvent(adapter.Event{Type: adapter.EventText, Kitchen: "opencode", Text: "Done. 3 files modified."})
	blocks[1].Complete(0, &adapter.CostInfo{DurationMs: 8400})

	// Dispatch 3: gemini (failed with rate limit)
	blocks[2].AppendEvent(adapter.Event{Type: adapter.EventError, Kitchen: "gemini", Text: "quota exceeded"})
	blocks[2].AppendEvent(adapter.Event{Type: adapter.EventRateLimit, Kitchen: "gemini", RateLimit: &adapter.RateLimitInfo{
		Status:   "exhausted",
		ResetsAt: time.Now().Add(2 * time.Hour),
		Kitchen:  "gemini",
	}})
	blocks[2].Complete(1, nil)

	// Verify block states
	successCount := 0
	failCount := 0
	kitchenCounts := make(map[string]int)
	for _, b := range blocks {
		kitchenCounts[b.Kitchen]++
		if b.ExitCode == 0 {
			successCount++
		} else {
			failCount++
		}
	}

	if len(blocks) != 3 {
		t.Errorf("total blocks = %d, want 3", len(blocks))
	}
	if successCount != 2 {
		t.Errorf("successCount = %d, want 2", successCount)
	}
	if failCount != 1 {
		t.Errorf("failCount = %d, want 1", failCount)
	}
	if kitchenCounts["claude"] != 1 || kitchenCounts["opencode"] != 1 || kitchenCounts["gemini"] != 1 {
		t.Errorf("unexpected kitchen counts: %v", kitchenCounts)
	}

	// Verify rendered body contains expected content
	for _, b := range blocks {
		rendered := b.RenderBody(100, tui.RenderRaw)
		if rendered == "" {
			t.Errorf("block %s rendered body is empty", b.ID)
		}
	}

	// Verify claude block body has expected text
	claudeBody := blocks[0].RenderBody(100, tui.RenderRaw)
	for _, want := range []string{"JWT tokens"} {
		if !strings.Contains(claudeBody, want) {
			t.Errorf("claude body missing %q", want)
		}
	}

	// Verify code block in claude block
	hasCode := false
	for _, line := range blocks[0].Lines {
		if line.Type == tui.LineCode {
			hasCode = true
			break
		}
	}
	if !hasCode {
		t.Error("claude block should have a code block")
	}

	// Verify rate limit system line in gemini block
	hasSystem := false
	for _, line := range blocks[2].Lines {
		if line.Type == tui.LineSystem {
			hasSystem = true
			break
		}
	}
	if !hasSystem {
		t.Error("gemini block should have a system line for rate limit")
	}
}

func TestIntegration_ProviderContinuity_SameBlockChain(t *testing.T) {
	t.Parallel()

	claude := &stubAdapter{events: []adapter.Event{
		{Type: adapter.EventText, Kitchen: "claude", Text: "Inspecting codebase..."},
		{Type: adapter.EventRateLimit, Kitchen: "claude", RateLimit: &adapter.RateLimitInfo{
			Status:        "exhausted",
			IsExhaustion:  true,
			DetectionKind: "stderr_text",
			RawText:       "You've hit your limit · resets 10pm (Europe/Stockholm)",
		}},
		{Type: adapter.EventDone, Kitchen: "claude", ExitCode: 1},
	}}
	codex := &stubAdapter{events: []adapter.Event{
		{Type: adapter.EventText, Kitchen: "codex", Text: "Continuing from restored context."},
		{Type: adapter.EventDone, Kitchen: "codex", ExitCode: 0},
	}}

	orch := orchestrator.Orchestrator{
		Factory: func(_ context.Context, _ string, exclude map[string]bool, _ string, _ map[string]string) (orchestrator.RouteResult, error) {
			if !exclude["claude"] {
				return orchestrator.RouteResult{
					Decision: sommelier.Decision{Kitchen: "claude", Tier: "keyword", Reason: "initial"},
					Adapter:  claude,
				}, nil
			}
			return orchestrator.RouteResult{
				Decision: sommelier.Decision{Kitchen: "codex", Tier: "fallback", Reason: "continuation"},
				Adapter:  codex,
			}, nil
		},
	}

	block := &tui.Block{
		ID:        "b1",
		Prompt:    "continue the platform admin engine",
		StartedAt: time.Now(),
		State:     tui.StateRouting,
	}

	conv, err := orch.Run(context.Background(), orchestrator.RunRequest{
		ConversationID: "conv-1",
		BlockID:        "b1",
		Prompt:         block.Prompt,
	}, func(res orchestrator.RouteResult) {
		block.ConversationID = "conv-1"
		block.Kitchen = res.Decision.Kitchen
		block.Decision = res.Decision
		if !containsChain(block.ProviderChain, res.Decision.Kitchen) {
			block.ProviderChain = append(block.ProviderChain, res.Decision.Kitchen)
		}
	}, func(evt adapter.Event) {
		block.AppendEvent(evt)
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	block.Complete(0, nil)

	if conv.Status != conversation.StatusDone {
		t.Fatalf("status = %q", conv.Status)
	}
	if got := strings.Join(block.ProviderChain, " -> "); got != "claude -> codex" {
		t.Fatalf("provider chain = %q", got)
	}
	rendered := block.RenderHeader()
	if !strings.Contains(rendered, "claude -> codex") {
		t.Fatalf("header missing provider chain: %q", rendered)
	}
	body := block.RenderBody(100, tui.RenderRaw)
	if !strings.Contains(body, "continuing with the next provider") {
		t.Fatalf("body missing continuity system message: %q", body)
	}
}

type stubAdapter struct {
	events []adapter.Event
}

func (s *stubAdapter) Exec(_ context.Context, _ kitchen.Task) (<-chan adapter.Event, error) {
	ch := make(chan adapter.Event, len(s.events))
	for _, evt := range s.events {
		ch <- evt
	}
	close(ch)
	return ch, nil
}

func (s *stubAdapter) Send(context.Context, string) error { return nil }
func (s *stubAdapter) SupportsResume() bool               { return false }
func (s *stubAdapter) SessionID() string                  { return "" }
func (s *stubAdapter) ProcessID() int                     { return 0 }
func (s *stubAdapter) Capabilities() adapter.Capabilities {
	return adapter.Capabilities{
		StructuredEvents:    true,
		InteractiveSend:     true,
		ExhaustionDetection: "structured",
	}
}

func containsChain(chain []string, provider string) bool {
	for _, item := range chain {
		if item == provider {
			return true
		}
	}
	return false
}
