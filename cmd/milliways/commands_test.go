package main

import (
	"testing"

	"github.com/mwigge/milliways/internal/kitchen"
	"github.com/mwigge/milliways/internal/maitre"
	"github.com/mwigge/milliways/internal/sommelier"
)

func TestBestContinuationKitchen_PrefersResumeCapableProvider(t *testing.T) {
	t.Parallel()

	reg := kitchen.NewRegistry()
	reg.Register(kitchen.NewGeneric(kitchen.GenericConfig{
		Name:     "claude",
		Cmd:      "claude",
		Stations: []string{"review"},
		Tier:     kitchen.Cloud,
		Enabled:  true,
	}))
	reg.Register(kitchen.NewGeneric(kitchen.GenericConfig{
		Name:     "codex",
		Cmd:      "codex",
		Stations: []string{"code"},
		Tier:     kitchen.Cloud,
		Enabled:  true,
	}))

	best, caps := bestContinuationKitchen(reg, map[string]bool{"claude": true})
	if best != "codex" {
		t.Fatalf("bestContinuationKitchen = %q, want codex", best)
	}
	if caps.StructuredEvents != true {
		t.Fatalf("expected structured continuity capabilities for codex")
	}
}

func TestSelectDecision_ContinuationOverridesWeakerRoute(t *testing.T) {
	t.Parallel()

	cfg := &maitre.Config{
		Routing: maitre.RoutingConfig{
			Keywords: map[string]string{"fix": "gemini"},
			Default:  "gemini",
		},
	}
	reg := kitchen.NewRegistry()
	reg.Register(kitchen.NewGeneric(kitchen.GenericConfig{
		Name:     "gemini",
		Cmd:      "gemini",
		Stations: []string{"research"},
		Tier:     kitchen.Free,
		Enabled:  true,
	}))
	reg.Register(kitchen.NewGeneric(kitchen.GenericConfig{
		Name:     "codex",
		Cmd:      "codex",
		Stations: []string{"code"},
		Tier:     kitchen.Cloud,
		Enabled:  true,
	}))
	som := sommelier.New(cfg.Routing.Keywords, cfg.Routing.Default, cfg.Routing.BudgetFallback, reg)

	decision := selectDecision(cfg, reg, som, nil, "fix continuity", "", map[string]bool{"claude": true})
	if decision.Kitchen != "codex" {
		t.Fatalf("selectDecision kitchen = %q, want codex", decision.Kitchen)
	}
	if decision.Tier != "continuation" {
		t.Fatalf("selectDecision tier = %q, want continuation", decision.Tier)
	}
}
