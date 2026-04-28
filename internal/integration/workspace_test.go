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
