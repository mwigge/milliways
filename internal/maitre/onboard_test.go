package maitre

import (
	"testing"

	"github.com/mwigge/milliways/internal/kitchen"
)

func TestDiagnose(t *testing.T) {
	t.Parallel()
	reg := kitchen.NewRegistry()
	reg.Register(kitchen.NewGeneric(kitchen.GenericConfig{
		Name: "echo-kitchen", Cmd: "echo", Stations: []string{"greet"}, Tier: kitchen.Local, Enabled: true,
		InstallCmd: "brew install echo", AuthCmd: "echo auth",
	}))
	reg.Register(kitchen.NewGeneric(kitchen.GenericConfig{
		Name: "missing-kitchen", Cmd: "nonexistent-xyz", Stations: []string{"fail"}, Tier: kitchen.Cloud, Enabled: true,
		InstallCmd: "brew install missing", AuthCmd: "",
	}))
	reg.Register(kitchen.NewGeneric(kitchen.GenericConfig{
		Name: "disabled-kitchen", Cmd: "echo", Stations: []string{"off"}, Tier: kitchen.Free, Enabled: false,
	}))

	health := Diagnose(reg)
	if len(health) != 3 {
		t.Fatalf("expected 3 health entries, got %d", len(health))
	}

	healthMap := make(map[string]KitchenHealth)
	for _, h := range health {
		healthMap[h.Name] = h
	}

	if h := healthMap["echo-kitchen"]; h.Status != kitchen.Ready {
		t.Errorf("echo-kitchen: expected Ready, got %s", h.Status)
	}
	if h := healthMap["missing-kitchen"]; h.Status != kitchen.NotInstalled {
		t.Errorf("missing-kitchen: expected NotInstalled, got %s", h.Status)
	}
	if h := healthMap["disabled-kitchen"]; h.Status != kitchen.Disabled {
		t.Errorf("disabled-kitchen: expected Disabled, got %s", h.Status)
	}
}

func TestReadyCounts(t *testing.T) {
	t.Parallel()
	health := []KitchenHealth{
		{Status: kitchen.Ready},
		{Status: kitchen.Ready},
		{Status: kitchen.NotInstalled},
		{Status: kitchen.Disabled},
	}

	ready, total := ReadyCounts(health)
	if ready != 2 {
		t.Errorf("expected 2 ready, got %d", ready)
	}
	if total != 4 {
		t.Errorf("expected 4 total, got %d", total)
	}
}

func TestReadyCounts_Empty(t *testing.T) {
	t.Parallel()
	ready, total := ReadyCounts(nil)
	if ready != 0 || total != 0 {
		t.Errorf("expected 0/0, got %d/%d", ready, total)
	}
}

func TestSetupKitchen_AlreadyReady(t *testing.T) {
	t.Parallel()
	k := kitchen.NewGeneric(kitchen.GenericConfig{Name: "echo", Cmd: "echo", Enabled: true})

	err := SetupKitchen(k)
	if err != nil {
		t.Errorf("expected no error for ready kitchen, got %v", err)
	}
}

func TestSetupKitchen_Disabled(t *testing.T) {
	t.Parallel()
	k := kitchen.NewGeneric(kitchen.GenericConfig{Name: "off", Cmd: "echo", Enabled: false})

	err := SetupKitchen(k)
	if err == nil {
		t.Error("expected error for disabled kitchen")
	}
}
