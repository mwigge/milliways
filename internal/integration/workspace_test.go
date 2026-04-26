package integration

import (
	"testing"

	"github.com/mwigge/milliways/internal/kitchen"
	"github.com/mwigge/milliways/internal/sommelier"
)

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
