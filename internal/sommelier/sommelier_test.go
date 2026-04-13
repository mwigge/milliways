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
	keywords := map[string]string{
		"explain": "claude",
		"code":    "opencode",
		"search":  "gemini",
	}
	s := New(keywords, "claude", "opencode", newTestRegistry())

	tests := []struct {
		name        string
		prompt      string
		wantKitchen string
		wantTier    string
	}{
		{"explain routes to claude", "explain the auth flow", "claude", "keyword"},
		{"code routes to opencode", "code a new handler", "opencode", "keyword"},
		{"search routes to gemini", "search for DORA-EU docs", "gemini", "keyword"},
		{"no keyword falls back", "do something random", "claude", "fallback"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			d := s.Route(tt.prompt)
			if d.Kitchen != tt.wantKitchen {
				t.Errorf("Route(%q).Kitchen = %q, want %q", tt.prompt, d.Kitchen, tt.wantKitchen)
			}
			if d.Tier != tt.wantTier {
				t.Errorf("Route(%q).Tier = %q, want %q", tt.prompt, d.Tier, tt.wantTier)
			}
		})
	}
}

func TestRoute_LongestMatchWins(t *testing.T) {
	t.Parallel()
	keywords := map[string]string{
		"search":     "gemini",
		"code":       "opencode",
		"search for": "gemini",
	}
	s := New(keywords, "claude", "opencode", newTestRegistry())

	d := s.Route("search for code patterns")
	if d.Kitchen != "gemini" {
		t.Errorf("expected longest match 'search for' → gemini, got %q via %q", d.Kitchen, d.Reason)
	}
}

func TestRoute_DeterministicOrder(t *testing.T) {
	t.Parallel()
	keywords := map[string]string{
		"auth": "claude",
		"code": "opencode",
	}
	s := New(keywords, "claude", "opencode", newTestRegistry())

	results := make(map[string]int)
	for range 100 {
		d := s.Route("auth code both keywords")
		results[d.Kitchen]++
	}

	if len(results) != 1 {
		t.Errorf("expected deterministic routing, got multiple results: %v", results)
	}
}

func TestRoute_UnavailableKitchen(t *testing.T) {
	t.Parallel()
	reg := kitchen.NewRegistry()
	reg.Register(kitchen.NewGeneric(kitchen.GenericConfig{Name: "claude", Cmd: "nonexistent-binary-xyz", Stations: []string{"think"}, Tier: kitchen.Cloud, Enabled: true}))
	reg.Register(kitchen.NewGeneric(kitchen.GenericConfig{Name: "opencode", Cmd: "echo", Stations: []string{"code"}, Tier: kitchen.Local, Enabled: true}))

	keywords := map[string]string{"think": "claude"}
	s := New(keywords, "claude", "opencode", reg)

	d := s.Route("think about this")
	if d.Kitchen == "claude" {
		t.Error("expected to skip unavailable claude kitchen")
	}
}

func TestRoute_NoKitchensAvailable(t *testing.T) {
	t.Parallel()
	reg := kitchen.NewRegistry()
	s := New(nil, "claude", "opencode", reg)

	d := s.Route("anything")
	if d.Kitchen != "" {
		t.Errorf("expected empty kitchen when none available, got %q", d.Kitchen)
	}
}

func TestForceRoute(t *testing.T) {
	t.Parallel()
	s := New(nil, "claude", "opencode", newTestRegistry())

	d := s.ForceRoute("gemini")
	if d.Kitchen != "gemini" {
		t.Errorf("ForceRoute('gemini') = %q, want 'gemini'", d.Kitchen)
	}
	if d.Tier != "forced" {
		t.Errorf("ForceRoute tier = %q, want 'forced'", d.Tier)
	}
}

func TestForceRoute_UnknownKitchen(t *testing.T) {
	t.Parallel()
	s := New(nil, "claude", "opencode", newTestRegistry())

	d := s.ForceRoute("nonexistent")
	if d.Kitchen != "nonexistent" {
		t.Errorf("expected kitchen name passed through, got %q", d.Kitchen)
	}
}

// === Tier 2: Enriched Routing ===

func TestRouteEnriched_HighRiskOverridesToClaude(t *testing.T) {
	t.Parallel()
	keywords := map[string]string{"refactor": "opencode"}
	s := New(keywords, "claude", "opencode", newTestRegistry())

	// Without signals: keyword routes to opencode
	d1 := s.Route("refactor the auth module")
	if d1.Kitchen != "opencode" {
		t.Errorf("without signals: expected opencode, got %q", d1.Kitchen)
	}

	// With HIGH risk signals: override to claude
	signals := &Signals{
		FileStability: "volatile",
		FileChurn90d:  45,
		Complexity:    34,
		Coverage:      30,
	}
	d2 := s.RouteEnriched("refactor the auth module", signals)
	if d2.Kitchen != "claude" {
		t.Errorf("with high risk: expected claude override, got %q (reason: %s)", d2.Kitchen, d2.Reason)
	}
	if d2.Tier != "enriched" {
		t.Errorf("expected tier 'enriched', got %q", d2.Tier)
	}
	if d2.Risk != "high" {
		t.Errorf("expected risk 'high', got %q", d2.Risk)
	}
}

func TestRouteEnriched_LowRiskKeepsKeyword(t *testing.T) {
	t.Parallel()
	keywords := map[string]string{"refactor": "opencode"}
	s := New(keywords, "claude", "opencode", newTestRegistry())

	signals := &Signals{
		FileStability: "stable",
		FileChurn90d:  1,
		Complexity:    5,
		Coverage:      90,
	}
	d := s.RouteEnriched("refactor the auth module", signals)
	if d.Kitchen != "opencode" {
		t.Errorf("low risk should keep keyword routing to opencode, got %q", d.Kitchen)
	}
	if d.Tier != "keyword" {
		t.Errorf("expected tier 'keyword', got %q", d.Tier)
	}
}

func TestRouteEnriched_MediumRiskKeepsKeyword(t *testing.T) {
	t.Parallel()
	keywords := map[string]string{"code": "opencode"}
	s := New(keywords, "claude", "opencode", newTestRegistry())

	signals := &Signals{
		FileStability: "active",
		Complexity:    20,
		Coverage:      60,
	}
	d := s.RouteEnriched("code a handler", signals)
	// Medium risk doesn't override — only HIGH does
	if d.Kitchen != "opencode" {
		t.Errorf("medium risk should keep keyword, got %q", d.Kitchen)
	}
}

func TestRouteEnriched_NilSignalsGraceful(t *testing.T) {
	t.Parallel()
	keywords := map[string]string{"code": "opencode"}
	s := New(keywords, "claude", "opencode", newTestRegistry())

	// nil signals = keyword-only (pantry unavailable)
	d := s.RouteEnriched("code a handler", nil)
	if d.Kitchen != "opencode" {
		t.Errorf("nil signals: expected keyword routing, got %q", d.Kitchen)
	}
	if d.Signals != nil {
		t.Error("expected nil signals in decision")
	}
}

// === Tier 3: Learned Routing ===

func TestRouteEnriched_LearnedOverridesKeyword(t *testing.T) {
	t.Parallel()
	keywords := map[string]string{"refactor": "opencode"}
	s := New(keywords, "claude", "opencode", newTestRegistry())

	signals := &Signals{
		FileStability:  "active",
		Complexity:     10,
		Coverage:       80,
		LearnedKitchen: "claude",
		LearnedRate:    95.0,
	}
	d := s.RouteEnriched("refactor the module", signals)
	if d.Kitchen != "claude" {
		t.Errorf("learned routing should override keyword, got %q", d.Kitchen)
	}
	if d.Tier != "learned" {
		t.Errorf("expected tier 'learned', got %q", d.Tier)
	}
}

func TestRouteEnriched_LearnedKitchenUnavailable(t *testing.T) {
	t.Parallel()
	reg := kitchen.NewRegistry()
	reg.Register(kitchen.NewGeneric(kitchen.GenericConfig{Name: "claude", Cmd: "nonexistent-xyz", Enabled: true}))
	reg.Register(kitchen.NewGeneric(kitchen.GenericConfig{Name: "opencode", Cmd: "echo", Enabled: true}))

	keywords := map[string]string{"refactor": "opencode"}
	s := New(keywords, "opencode", "opencode", reg)

	signals := &Signals{
		LearnedKitchen: "claude", // claude not installed
		LearnedRate:    90.0,
	}
	d := s.RouteEnriched("refactor module", signals)
	// Learned kitchen unavailable → fall through to keyword
	if d.Kitchen != "opencode" {
		t.Errorf("expected fallthrough to keyword, got %q", d.Kitchen)
	}
}
