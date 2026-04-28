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

package maitre

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mwigge/milliways/internal/kitchen"
	"github.com/mwigge/milliways/internal/kitchen/adapter"
	"github.com/mwigge/milliways/internal/pantry"
	"github.com/mwigge/milliways/internal/sommelier"
)

// Integration test: config → registry → sommelier → exec → ledger.
// Uses "echo" as the kitchen command to avoid external dependencies.

func TestDispatchPipeline_HappyPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Write a minimal carte.yaml
	configPath := filepath.Join(dir, "carte.yaml")
	configYAML := `
kitchens:
  echo-kitchen:
    cmd: echo
    args: []
    stations: [think, code]
    cost_tier: local
routing:
  keywords:
    think: echo-kitchen
    code: echo-kitchen
  default: echo-kitchen
`
	if err := os.WriteFile(configPath, []byte(configYAML), 0o600); err != nil {
		t.Fatal(err)
	}

	// Load config
	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	// Build registry
	reg := kitchen.NewRegistry()
	for name, kc := range cfg.Kitchens {
		if kc.HTTPClient != nil {
			httpKitchen, err := adapter.NewHTTPKitchen(name, adapter.HTTPKitchenConfig{
				BaseURL:        kc.HTTPClient.BaseURL,
				AuthKey:        kc.HTTPClient.AuthKey,
				AuthType:       kc.HTTPClient.AuthType,
				Model:          kc.HTTPClient.Model,
				Stations:       kc.HTTPClient.Stations,
				Tier:           kitchen.ParseCostTier(kc.HTTPClient.Tier),
				ResponseFormat: kc.HTTPClient.ResponseFormat,
				Timeout:        time.Duration(kc.HTTPClient.Timeout) * time.Second,
			}, kc.Stations, kitchen.ParseCostTier(kc.CostTier))
			if err != nil {
				t.Fatalf("NewHTTPKitchen: %v", err)
			}
			reg.Register(httpKitchen)
			continue
		}

		reg.Register(kitchen.NewGeneric(kitchen.GenericConfig{
			Name:     name,
			Cmd:      kc.Cmd,
			Args:     kc.Args,
			Stations: kc.Stations,
			Tier:     kitchen.ParseCostTier(kc.CostTier),
			Enabled:  kc.IsEnabled(),
		}))
	}

	// Route
	som := sommelier.New(cfg.Routing.Keywords, cfg.Routing.Default, "", cfg.Routing.WeightOn, reg)
	decision := som.Route("think about this")

	if decision.Kitchen != "echo-kitchen" {
		t.Fatalf("expected echo-kitchen, got %q", decision.Kitchen)
	}
	if decision.Tier != "keyword" {
		t.Errorf("expected keyword tier, got %q", decision.Tier)
	}

	// Execute
	k, ok := reg.Get(decision.Kitchen)
	if !ok {
		t.Fatal("kitchen not found in registry")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var lines []string
	task := kitchen.Task{
		Prompt: "hello from integration test",
		OnLine: func(line string) { lines = append(lines, line) },
	}

	result, err := k.Exec(ctx, task)
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("exit code: %d", result.ExitCode)
	}
	if len(lines) == 0 {
		t.Error("expected output lines")
	}

	// Write to PantryDB
	dbPath := filepath.Join(dir, "milliways.db")
	pdb, err := pantry.Open(dbPath)
	if err != nil {
		t.Fatalf("pantry.Open: %v", err)
	}
	defer func() { _ = pdb.Close() }()

	entry := pantry.LedgerEntry{
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
		TaskHash:    "test-hash",
		Kitchen:     decision.Kitchen,
		DurationSec: result.Duration.Seconds(),
		ExitCode:    result.ExitCode,
		Outcome:     "success",
	}
	if _, err := pdb.Ledger().Insert(entry); err != nil {
		t.Fatalf("Ledger.Insert: %v", err)
	}

	// Verify ledger
	total, err := pdb.Ledger().Total()
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 {
		t.Errorf("expected 1 ledger entry, got %d", total)
	}

	stats, err := pdb.Ledger().Stats()
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 1 || stats[0].Kitchen != "echo-kitchen" {
		t.Errorf("unexpected stats: %+v", stats)
	}
	if stats[0].SuccessRate != 100.0 {
		t.Errorf("expected 100%% success rate, got %.1f%%", stats[0].SuccessRate)
	}
}

func TestDispatchPipeline_NoKeywordFallsToDefault(t *testing.T) {
	t.Parallel()

	reg := kitchen.NewRegistry()
	reg.Register(kitchen.NewGeneric(kitchen.GenericConfig{
		Name: "fallback", Cmd: "echo", Enabled: true,
	}))

	som := sommelier.New(map[string]string{"think": "missing"}, "fallback", "", nil, reg)
	decision := som.Route("something without keywords")

	if decision.Kitchen != "fallback" {
		t.Errorf("expected fallback kitchen, got %q", decision.Kitchen)
	}
	if decision.Tier != "fallback" {
		t.Errorf("expected fallback tier, got %q", decision.Tier)
	}
}

func TestDispatchPipeline_SingleKitchenMode(t *testing.T) {
	t.Parallel()

	reg := kitchen.NewRegistry()
	reg.Register(kitchen.NewGeneric(kitchen.GenericConfig{
		Name: "only-one", Cmd: "echo", Stations: []string{"everything"}, Enabled: true,
	}))

	som := sommelier.New(nil, "only-one", "", nil, reg)
	decision := som.Route("any task at all")

	if decision.Kitchen != "only-one" {
		t.Errorf("expected only-one kitchen, got %q", decision.Kitchen)
	}
}

func TestDispatchPipeline_ExplainMode(t *testing.T) {
	t.Parallel()

	reg := kitchen.NewRegistry()
	reg.Register(kitchen.NewGeneric(kitchen.GenericConfig{
		Name: "claude", Cmd: "echo", Stations: []string{"think"}, Enabled: true,
	}))

	som := sommelier.New(map[string]string{"explain": "claude"}, "claude", "", nil, reg)
	decision := som.Route("explain the auth flow")

	// Explain mode: we get the decision without executing
	if decision.Kitchen != "claude" {
		t.Errorf("expected claude, got %q", decision.Kitchen)
	}
	if decision.Reason == "" {
		t.Error("expected non-empty reason")
	}
}

func TestDispatchPipeline_ForceKitchen(t *testing.T) {
	t.Parallel()

	reg := kitchen.NewRegistry()
	reg.Register(kitchen.NewGeneric(kitchen.GenericConfig{Name: "claude", Cmd: "echo", Enabled: true}))
	reg.Register(kitchen.NewGeneric(kitchen.GenericConfig{Name: "opencode", Cmd: "echo", Enabled: true}))

	som := sommelier.New(map[string]string{"explain": "claude"}, "claude", "", nil, reg)

	// Force opencode even though "explain" matches claude
	decision := som.ForceRoute("opencode")
	if decision.Kitchen != "opencode" {
		t.Errorf("expected forced opencode, got %q", decision.Kitchen)
	}
}
