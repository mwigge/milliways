package sommelier

import (
	"testing"

	"github.com/mwigge/milliways/internal/kitchen"
)

func newTestRegistry() *kitchen.Registry {
	reg := kitchen.NewRegistry()
	reg.Register(kitchen.NewGeneric(kitchen.GenericConfig{Name: "claude", Cmd: "echo", Stations: []string{"think", "plan", "review"}, Tier: kitchen.Cloud, Enabled: true}))
	reg.Register(kitchen.NewGeneric(kitchen.GenericConfig{Name: "opencode", Cmd: "echo", Stations: []string{"code", "test"}, Tier: kitchen.Local, Enabled: true}))
	reg.Register(kitchen.NewGeneric(kitchen.GenericConfig{Name: "gemini", Cmd: "echo", Stations: []string{"search"}, Tier: kitchen.Free, Enabled: true}))
	return reg
}

func TestRoute_KeywordMatch(t *testing.T) {
	t.Parallel()
	keywords := map[string]string{"explain": "claude", "code": "opencode", "search": "gemini"}
	s := New(keywords, "claude", "opencode", newTestRegistry())

	tests := []struct {
		name, prompt, wantKitchen, wantTier string
	}{
		{"explain", "explain the auth flow", "claude", "keyword"},
		{"code", "code a new handler", "opencode", "keyword"},
		{"search", "search for DORA-EU docs", "gemini", "keyword"},
		{"fallback", "do something random", "claude", "fallback"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			d := s.Route(tt.prompt)
			if d.Kitchen != tt.wantKitchen {
				t.Errorf("Kitchen = %q, want %q", d.Kitchen, tt.wantKitchen)
			}
			if d.Tier != tt.wantTier {
				t.Errorf("Tier = %q, want %q", d.Tier, tt.wantTier)
			}
		})
	}
}

func TestRoute_LongestMatchWins(t *testing.T) {
	t.Parallel()
	keywords := map[string]string{"search": "gemini", "code": "opencode", "search for": "gemini"}
	s := New(keywords, "claude", "opencode", newTestRegistry())
	d := s.Route("search for code patterns")
	if d.Kitchen != "gemini" {
		t.Errorf("expected gemini (longest match), got %q", d.Kitchen)
	}
}

func TestRoute_DeterministicOrder(t *testing.T) {
	t.Parallel()
	s := New(map[string]string{"auth": "claude", "code": "opencode"}, "claude", "opencode", newTestRegistry())
	results := make(map[string]int)
	for range 100 {
		results[s.Route("auth code both").Kitchen]++
	}
	if len(results) != 1 {
		t.Errorf("non-deterministic: %v", results)
	}
}

func TestRoute_UnavailableKitchen(t *testing.T) {
	t.Parallel()
	reg := kitchen.NewRegistry()
	reg.Register(kitchen.NewGeneric(kitchen.GenericConfig{Name: "claude", Cmd: "nonexistent-xyz", Enabled: true}))
	reg.Register(kitchen.NewGeneric(kitchen.GenericConfig{Name: "opencode", Cmd: "echo", Enabled: true}))
	s := New(map[string]string{"think": "claude"}, "claude", "opencode", reg)
	d := s.Route("think about this")
	if d.Kitchen == "claude" {
		t.Error("should skip unavailable claude")
	}
}

func TestRoute_NoKitchensAvailable(t *testing.T) {
	t.Parallel()
	s := New(nil, "claude", "opencode", kitchen.NewRegistry())
	if d := s.Route("anything"); d.Kitchen != "" {
		t.Errorf("expected empty, got %q", d.Kitchen)
	}
}

func TestForceRoute(t *testing.T) {
	t.Parallel()
	s := New(nil, "claude", "opencode", newTestRegistry())
	d := s.ForceRoute("gemini")
	if d.Kitchen != "gemini" || d.Tier != "forced" {
		t.Errorf("got %q/%q", d.Kitchen, d.Tier)
	}
}

func TestForceRoute_Unknown(t *testing.T) {
	t.Parallel()
	s := New(nil, "claude", "opencode", newTestRegistry())
	d := s.ForceRoute("nonexistent")
	if d.Kitchen != "nonexistent" {
		t.Errorf("expected passthrough, got %q", d.Kitchen)
	}
}

// === Tier 2: Enriched ===

func TestRouteEnriched_HighRiskOverrides(t *testing.T) {
	t.Parallel()
	s := New(map[string]string{"refactor": "opencode"}, "claude", "opencode", newTestRegistry())
	signals := &Signals{FileStability: "volatile", FileChurn90d: 45, Complexity: 34, Coverage: 30}
	d := s.RouteEnriched("refactor the auth module", signals, nil)
	if d.Kitchen != "claude" {
		t.Errorf("high risk: expected claude, got %q", d.Kitchen)
	}
	if d.Tier != "enriched" || d.Risk != "high" {
		t.Errorf("tier=%q risk=%q", d.Tier, d.Risk)
	}
}

func TestRouteEnriched_LowRiskKeepsKeyword(t *testing.T) {
	t.Parallel()
	s := New(map[string]string{"refactor": "opencode"}, "claude", "opencode", newTestRegistry())
	signals := &Signals{FileStability: "stable", FileChurn90d: 1, Complexity: 5, Coverage: 90}
	d := s.RouteEnriched("refactor the auth module", signals, nil)
	if d.Kitchen != "opencode" || d.Tier != "keyword" {
		t.Errorf("low risk: got %q/%q", d.Kitchen, d.Tier)
	}
}

func TestRouteEnriched_NilSignals(t *testing.T) {
	t.Parallel()
	s := New(map[string]string{"code": "opencode"}, "claude", "opencode", newTestRegistry())
	d := s.RouteEnriched("code a handler", nil, nil)
	if d.Kitchen != "opencode" || d.Signals != nil {
		t.Errorf("nil signals: got %q, signals=%v", d.Kitchen, d.Signals)
	}
}

// === Tier 3: Learned ===

func TestRouteEnriched_LearnedOverrides(t *testing.T) {
	t.Parallel()
	s := New(map[string]string{"refactor": "opencode"}, "claude", "opencode", newTestRegistry())
	signals := &Signals{FileStability: "active", Complexity: 10, Coverage: 80, LearnedKitchen: "claude", LearnedRate: 95.0}
	d := s.RouteEnriched("refactor the module", signals, nil)
	if d.Kitchen != "claude" || d.Tier != "learned" {
		t.Errorf("learned: got %q/%q", d.Kitchen, d.Tier)
	}
}

func TestRouteEnriched_LearnedUnavailable(t *testing.T) {
	t.Parallel()
	reg := kitchen.NewRegistry()
	reg.Register(kitchen.NewGeneric(kitchen.GenericConfig{Name: "claude", Cmd: "nonexistent-xyz", Enabled: true}))
	reg.Register(kitchen.NewGeneric(kitchen.GenericConfig{Name: "opencode", Cmd: "echo", Enabled: true}))
	s := New(map[string]string{"refactor": "opencode"}, "opencode", "opencode", reg)
	signals := &Signals{LearnedKitchen: "claude", LearnedRate: 90.0}
	d := s.RouteEnriched("refactor module", signals, nil)
	if d.Kitchen != "opencode" {
		t.Errorf("fallthrough: expected opencode, got %q", d.Kitchen)
	}
}

// === Skill Hint ===

func TestRouteEnriched_SkillHintBoost(t *testing.T) {
	t.Parallel()
	s := New(map[string]string{"review": "opencode"}, "claude", "opencode", newTestRegistry())
	hint := &SkillHint{Kitchen: "claude", SkillName: "security-review"}
	d := s.RouteEnriched("review the security changes", nil, hint)
	if d.Kitchen != "claude" {
		t.Errorf("skill hint: expected claude, got %q", d.Kitchen)
	}
	if d.Tier != "enriched" {
		t.Errorf("expected tier 'enriched', got %q", d.Tier)
	}
}

func TestRouteEnriched_SkillHintKitchenUnavailable(t *testing.T) {
	t.Parallel()
	reg := kitchen.NewRegistry()
	reg.Register(kitchen.NewGeneric(kitchen.GenericConfig{Name: "claude", Cmd: "nonexistent-xyz", Enabled: true}))
	reg.Register(kitchen.NewGeneric(kitchen.GenericConfig{Name: "opencode", Cmd: "echo", Enabled: true}))
	s := New(map[string]string{"review": "opencode"}, "opencode", "opencode", reg)
	hint := &SkillHint{Kitchen: "claude", SkillName: "security-review"}
	d := s.RouteEnriched("review security", nil, hint)
	// claude unavailable → fall through to keyword
	if d.Kitchen != "opencode" {
		t.Errorf("expected fallthrough, got %q", d.Kitchen)
	}
}

func TestRouteEnriched_NilSkillHint(t *testing.T) {
	t.Parallel()
	s := New(map[string]string{"code": "opencode"}, "claude", "opencode", newTestRegistry())
	d := s.RouteEnriched("code something", nil, nil)
	if d.Kitchen != "opencode" {
		t.Errorf("nil hint: expected opencode, got %q", d.Kitchen)
	}
}

// mockQuotaChecker implements QuotaChecker for testing.
type mockQuotaChecker struct {
	exhausted map[string]bool
}

func (m *mockQuotaChecker) IsExhausted(kitchen string, _ int) (bool, error) {
	return m.exhausted[kitchen], nil
}

func TestRoute_QuotaExhausted_SkipsToFallback(t *testing.T) {
	t.Parallel()

	keywords := map[string]string{"explain": "claude", "code": "opencode"}
	s := New(keywords, "claude", "opencode", newTestRegistry())
	s.SetQuotaChecker(&mockQuotaChecker{exhausted: map[string]bool{"claude": true}}, nil)

	d := s.Route("explain the auth flow")
	// claude is exhausted, should fall back
	if d.Kitchen == "claude" {
		t.Error("should not route to exhausted claude")
	}
	if d.Kitchen == "" {
		t.Error("should find a fallback kitchen")
	}
}

func TestRoute_QuotaExhausted_AllExhausted(t *testing.T) {
	t.Parallel()

	keywords := map[string]string{"explain": "claude"}
	s := New(keywords, "claude", "opencode", newTestRegistry())
	s.SetQuotaChecker(&mockQuotaChecker{exhausted: map[string]bool{
		"claude":   true,
		"opencode": true,
		"gemini":   true,
	}}, nil)

	d := s.Route("explain something")
	if d.Kitchen != "" {
		t.Errorf("all exhausted: expected empty kitchen, got %q", d.Kitchen)
	}
}

func TestRoute_QuotaNotSet_NoEffect(t *testing.T) {
	t.Parallel()

	keywords := map[string]string{"explain": "claude"}
	s := New(keywords, "claude", "opencode", newTestRegistry())
	// No SetQuotaChecker call — nil checker

	d := s.Route("explain the flow")
	if d.Kitchen != "claude" {
		t.Errorf("with nil checker: expected claude, got %q", d.Kitchen)
	}
}

func TestRoute_QuotaLimitsFromConfig(t *testing.T) {
	t.Parallel()

	keywords := map[string]string{"explain": "claude"}
	s := New(keywords, "claude", "opencode", newTestRegistry())
	// Checker that checks limits
	checker := &mockQuotaChecker{exhausted: map[string]bool{"claude": true}}
	s.SetQuotaChecker(checker, map[string]int{"claude": 50})

	d := s.Route("explain something")
	if d.Kitchen == "claude" {
		t.Error("should skip exhausted claude")
	}
}
