package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSubstitutesEnvAndAppliesDefaults(t *testing.T) {
	t.Setenv("TEST_MINIMAX_API_KEY", "secret-key")
	t.Setenv("TEST_MEMPALACE_CMD", "mempalace")
	t.Setenv("TEST_MEMPALACE_ARGS", "serve --stdio")

	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.json")
	if err := os.WriteFile(configPath, []byte(`{
		"provider": "minimax",
		"minimax": {
			"api_key": "${TEST_MINIMAX_API_KEY}"
		},
		"memory": {
			"mempalace_mcp_cmd": "${TEST_MEMPALACE_CMD}",
			"mempalace_mcp_args": "${TEST_MEMPALACE_ARGS}",
			"session_dir": "~/.config/milliways/sessions"
		},
		"plugins": ["${TEST_MEMPALACE_CMD}"]
	}`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Provider != "minimax" {
		t.Fatalf("Provider = %q, want minimax", cfg.Provider)
	}
	if cfg.MiniMax.APIKey != "secret-key" {
		t.Fatalf("MiniMax.APIKey = %q, want secret-key", cfg.MiniMax.APIKey)
	}
	if cfg.MiniMax.BaseURL != "https://api.minimax.chat/v1" {
		t.Fatalf("MiniMax.BaseURL = %q", cfg.MiniMax.BaseURL)
	}
	if cfg.MiniMax.Model != "MiniMax-Text-01" {
		t.Fatalf("MiniMax.Model = %q", cfg.MiniMax.Model)
	}
	if cfg.Memory.MemPalaceMCPCmd != "mempalace" {
		t.Fatalf("Memory.MemPalaceMCPCmd = %q", cfg.Memory.MemPalaceMCPCmd)
	}
	if cfg.Memory.MemPalaceMCPArgs != "serve --stdio" {
		t.Fatalf("Memory.MemPalaceMCPArgs = %q", cfg.Memory.MemPalaceMCPArgs)
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir() error = %v", err)
	}
	wantSessionDir := filepath.Join(homeDir, ".config", "milliways", "sessions")
	if cfg.Memory.SessionDir != wantSessionDir {
		t.Fatalf("Memory.SessionDir = %q, want %q", cfg.Memory.SessionDir, wantSessionDir)
	}
	if len(cfg.Plugins) != 1 || cfg.Plugins[0] != "mempalace" {
		t.Fatalf("Plugins = %#v, want [mempalace]", cfg.Plugins)
	}
	if !cfg.AutoCompact {
		t.Fatal("AutoCompact = false, want true")
	}
	if cfg.CompactThreshold != 0.7 {
		t.Fatalf("CompactThreshold = %v, want 0.7", cfg.CompactThreshold)
	}
	if cfg.Viewport.ScrollLines != 3 {
		t.Fatalf("Viewport.ScrollLines = %d, want 3", cfg.Viewport.ScrollLines)
	}
	if cfg.Viewport.MaxBodyLines != 15 {
		t.Fatalf("Viewport.MaxBodyLines = %d, want 15", cfg.Viewport.MaxBodyLines)
	}
	if !cfg.Editor.VimMode {
		t.Fatal("Editor.VimMode = false, want true")
	}
	if !cfg.Editor.SyntaxHighlight {
		t.Fatal("Editor.SyntaxHighlight = false, want true")
	}
}

func TestLoadReturnsErrorForMissingFile(t *testing.T) {
	t.Parallel()

	_, err := Load(filepath.Join(t.TempDir(), "missing.json"))
	if err == nil {
		t.Fatal("Load() error = nil, want error")
	}
}
