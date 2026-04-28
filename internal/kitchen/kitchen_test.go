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

package kitchen

import (
	"context"
	"testing"
	"time"
)

func TestCostTier_String(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		tier CostTier
		want string
	}{
		{"cloud", Cloud, "cloud"},
		{"local", Local, "local"},
		{"free", Free, "free"},
		{"unknown", CostTierUnknown, "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.tier.String(); got != tt.want {
				t.Errorf("CostTier(%d).String() = %q, want %q", tt.tier, got, tt.want)
			}
		})
	}
}

func TestParseCostTier(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  CostTier
	}{
		{"cloud", "cloud", Cloud},
		{"local", "local", Local},
		{"free", "free", Free},
		{"unknown string returns unknown", "typo", CostTierUnknown},
		{"empty string returns unknown", "", CostTierUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ParseCostTier(tt.input); got != tt.want {
				t.Errorf("ParseCostTier(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestStatus_Symbol(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		status Status
		want   string
	}{
		{"ready", Ready, "✓"},
		{"needs-auth", NeedsAuth, "!"},
		{"not-installed", NotInstalled, "✗"},
		{"disabled", Disabled, "⊘"},
		{"unknown", StatusUnknown, "?"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.status.Symbol(); got != tt.want {
				t.Errorf("Status(%d).Symbol() = %q, want %q", tt.status, got, tt.want)
			}
		})
	}
}

func TestRegistry(t *testing.T) {
	t.Parallel()
	reg := NewRegistry()

	k := NewGeneric(GenericConfig{Name: "test", Cmd: "echo", Stations: []string{"greet"}, Tier: Local, Enabled: true})
	reg.Register(k)

	got, ok := reg.Get("test")
	if !ok {
		t.Fatal("expected to find 'test' kitchen")
	}
	if got.Name() != "test" {
		t.Errorf("expected name 'test', got %q", got.Name())
	}

	_, ok = reg.Get("nonexistent")
	if ok {
		t.Error("expected not to find 'nonexistent' kitchen")
	}
}

func TestRegistry_GetByStation(t *testing.T) {
	t.Parallel()
	reg := NewRegistry()

	k := NewGeneric(GenericConfig{Name: "test", Cmd: "echo", Stations: []string{"greet", "farewell"}, Tier: Local, Enabled: true})
	reg.Register(k)

	got, ok := reg.GetByStation("greet")
	if !ok {
		t.Fatal("expected to find kitchen for 'greet' station")
	}
	if got.Name() != "test" {
		t.Errorf("expected kitchen 'test', got %q", got.Name())
	}

	_, ok = reg.GetByStation("nonexistent")
	if ok {
		t.Error("expected not to find kitchen for 'nonexistent' station")
	}
}

func TestRegistry_Ready(t *testing.T) {
	t.Parallel()
	reg := NewRegistry()

	ready := NewGeneric(GenericConfig{Name: "available", Cmd: "echo", Stations: []string{"greet"}, Tier: Local, Enabled: true})
	disabled := NewGeneric(GenericConfig{Name: "disabled", Cmd: "echo", Stations: []string{"farewell"}, Tier: Local, Enabled: false})

	reg.Register(ready)
	reg.Register(disabled)

	readyList := reg.Ready()
	if len(readyList) != 1 {
		t.Errorf("expected 1 ready kitchen, got %d", len(readyList))
	}
	if len(readyList) > 0 && readyList[0].Name() != "available" {
		t.Errorf("expected 'available' kitchen, got %q", readyList[0].Name())
	}
}

func TestRegistry_AllReturnsDefensiveCopy(t *testing.T) {
	t.Parallel()
	reg := NewRegistry()
	reg.Register(NewGeneric(GenericConfig{Name: "test", Cmd: "echo", Enabled: true}))

	all := reg.All()
	all["injected"] = nil // mutate the copy

	_, ok := reg.Get("injected")
	if ok {
		t.Error("mutating All() result should not affect registry internals")
	}
}

func TestGenericKitchen_Status(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		cfg  GenericConfig
		want Status
	}{
		{"ready with echo", GenericConfig{Cmd: "echo", Enabled: true}, Ready},
		{"disabled", GenericConfig{Cmd: "echo", Enabled: false}, Disabled},
		{"not installed", GenericConfig{Cmd: "nonexistent-binary-xyz", Enabled: true}, NotInstalled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			k := NewGeneric(tt.cfg)
			if got := k.Status(); got != tt.want {
				t.Errorf("Status() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestGenericKitchen_StationsDefensiveCopy(t *testing.T) {
	t.Parallel()
	k := NewGeneric(GenericConfig{Name: "test", Stations: []string{"a", "b"}, Enabled: true})

	stations := k.Stations()
	stations[0] = "mutated"

	original := k.Stations()
	if original[0] == "mutated" {
		t.Error("Stations() should return a defensive copy")
	}
}

func TestGenericKitchen_Exec(t *testing.T) {
	t.Parallel()
	k := NewGeneric(GenericConfig{Name: "echo-test", Cmd: "echo", Enabled: true})

	var lines []string
	task := Task{
		Prompt: "hello world",
		OnLine: func(line string) { lines = append(lines, line) },
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := k.Exec(ctx, task)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", result.ExitCode)
	}
	if result.Output != "hello world\n" {
		t.Errorf("expected output 'hello world\\n', got %q", result.Output)
	}
	if len(lines) != 1 || lines[0] != "hello world" {
		t.Errorf("expected OnLine called with 'hello world', got %v", lines)
	}
	if result.Duration <= 0 {
		t.Error("expected positive duration")
	}
}

func TestGenericKitchen_ExecDisallowedCmd(t *testing.T) {
	t.Parallel()
	k := NewGeneric(GenericConfig{Name: "evil", Cmd: "rm", Enabled: true})

	_, err := k.Exec(context.Background(), Task{Prompt: "-rf /"})
	if err == nil {
		t.Fatal("expected error for disallowed command")
	}
}

func TestGenericKitchen_ExecNotReady(t *testing.T) {
	t.Parallel()
	k := NewGeneric(GenericConfig{Name: "test", Cmd: "nonexistent-xyz", Enabled: true})

	_, err := k.Exec(context.Background(), Task{Prompt: "hello"})
	if err == nil {
		t.Fatal("expected error for not-ready kitchen")
	}
}
