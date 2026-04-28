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
	"io"
	"os"
	"path/filepath"
	"strings"
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

func TestUpdateKitchenAuth(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	configDir := filepath.Join(tmpDir, ".config", "milliways")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	original := `kitchens:
  minimax:
    http_client:
      base_url: https://api.minimaxi.com/v1/text
      auth_key: OLD_KEY
      auth_type: bearer
      model: M2-her
      stations: [reason]
      tier: cloud
      response_format: minimax
      timeout_seconds: 300
    enabled: true
  claude:
    cmd: claude
    args: ["-p"]
routing:
  default: claude
`

	configPath := DefaultConfigPath()
	if err := os.WriteFile(configPath, []byte(original), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := UpdateKitchenAuth("minimax", "NEW_KEY"); err != nil {
		t.Fatalf("UpdateKitchenAuth: %v", err)
	}

	updatedCfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if got := updatedCfg.Kitchens["minimax"].HTTPClient.AuthKey; got != "NEW_KEY" {
		t.Fatalf("AuthKey = %q, want NEW_KEY", got)
	}
	if got := updatedCfg.Kitchens["claude"].Cmd; got != "claude" {
		t.Fatalf("claude cmd = %q, want claude", got)
	}

	backup, err := os.ReadFile(configPath + ".bak")
	if err != nil {
		t.Fatalf("ReadFile backup: %v", err)
	}
	if string(backup) != original {
		t.Fatalf("backup mismatch\n got: %s\nwant: %s", string(backup), original)
	}
}

func TestUpdateKitchenAuth_ReturnsErrorForNonHTTPKitchen(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	configDir := filepath.Join(tmpDir, ".config", "milliways")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	configPath := DefaultConfigPath()
	config := `kitchens:
  claude:
    cmd: claude
`
	if err := os.WriteFile(configPath, []byte(config), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	err := UpdateKitchenAuth("claude", "NEW_KEY")
	if err == nil {
		t.Fatal("expected error for non-HTTP kitchen")
	}
	if !strings.Contains(err.Error(), "is not an HTTPClient type") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoginKitchen_MinimaxNonTTY(t *testing.T) {
	if isTTY() {
		t.Skip("requires non-interactive stdout")
	}

	output := captureStdout(t, func() {
		if err := LoginKitchen("minimax"); err != nil {
			t.Fatalf("LoginKitchen: %v", err)
		}
	})

	if !strings.Contains(output, "minimax") {
		t.Fatalf("expected minimax instructions, got %q", output)
	}
}

func TestLoginKitchen_UnknownKitchen(t *testing.T) {
	err := LoginKitchen("unknown")
	if err == nil {
		t.Fatal("expected error for unknown kitchen")
	}
	if got, want := err.Error(), "unknown kitchen: unknown"; got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	originalStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe: %v", err)
	}

	os.Stdout = w
	defer func() {
		os.Stdout = originalStdout
	}()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("Close writer: %v", err)
	}

	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}

	return string(data)
}
