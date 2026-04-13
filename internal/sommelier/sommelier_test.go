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
		"search for": "gemini", // longer match should win
	}
	s := New(keywords, "claude", "opencode", newTestRegistry())

	d := s.Route("search for code patterns")
	// "search for" (10 chars) should match before "search" (6) or "code" (4)
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

	// Run 100 times — non-deterministic map iteration would produce varying results
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
