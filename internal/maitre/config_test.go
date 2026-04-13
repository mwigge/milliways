package maitre

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := defaultConfig()

	if len(cfg.Kitchens) != 6 {
		t.Errorf("expected 6 default kitchens, got %d", len(cfg.Kitchens))
	}

	claude, ok := cfg.Kitchens["claude"]
	if !ok {
		t.Fatal("expected claude kitchen in defaults")
	}
	if claude.Cmd != "claude" {
		t.Errorf("expected cmd 'claude', got %q", claude.Cmd)
	}
	if !claude.IsEnabled() {
		t.Error("expected claude enabled by default")
	}

	if cfg.Routing.Default != "claude" {
		t.Errorf("expected default routing to claude, got %q", cfg.Routing.Default)
	}
	if cfg.Routing.BudgetFallback != "opencode" {
		t.Errorf("expected budget fallback to opencode, got %q", cfg.Routing.BudgetFallback)
	}
}

func TestLoadConfig_FileNotFound(t *testing.T) {
	cfg, err := LoadConfig("/tmp/nonexistent-milliways-carte.yaml")
	if err != nil {
		t.Fatalf("expected no error for missing file, got %v", err)
	}
	if len(cfg.Kitchens) != 6 {
		t.Errorf("expected defaults when file missing, got %d kitchens", len(cfg.Kitchens))
	}
}

func TestLoadConfig_ValidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "carte.yaml")

	yaml := `
kitchens:
  claude:
    cmd: claude
    args: ["-p"]
    stations: [think]
    cost_tier: cloud
  opencode:
    cmd: opencode
    args: ["run"]
    stations: [code]
    cost_tier: local
    enabled: false
routing:
  keywords:
    think: claude
    code: opencode
  default: claude
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(cfg.Kitchens) != 2 {
		t.Errorf("expected 2 kitchens from yaml, got %d", len(cfg.Kitchens))
	}

	oc := cfg.Kitchens["opencode"]
	if oc.IsEnabled() {
		t.Error("expected opencode disabled")
	}

	if cfg.Routing.Keywords["think"] != "claude" {
		t.Errorf("expected think→claude routing, got %q", cfg.Routing.Keywords["think"])
	}
}

func TestLoadConfig_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "carte.yaml")

	if err := os.WriteFile(path, []byte("{{invalid"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("expected error for invalid yaml")
	}
}

func TestKitchenConfig_IsEnabled(t *testing.T) {
	tests := []struct {
		name    string
		enabled *bool
		want    bool
	}{
		{"nil defaults to true", nil, true},
		{"explicit true", boolPtr(true), true},
		{"explicit false", boolPtr(false), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kc := KitchenConfig{Enabled: tt.enabled}
			if got := kc.IsEnabled(); got != tt.want {
				t.Errorf("IsEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func boolPtr(b bool) *bool { return &b }
