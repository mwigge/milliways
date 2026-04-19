package maitre

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	t.Parallel()
	cfg := defaultConfig()

	if len(cfg.Kitchens) != 9 {
		t.Errorf("expected 9 default kitchens, got %d", len(cfg.Kitchens))
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
	if cfg.ProjectContextLimit != 3 {
		t.Errorf("expected project context limit 3, got %d", cfg.ProjectContextLimit)
	}
}

func TestLoadConfig_FileNotFound(t *testing.T) {
	t.Parallel()
	cfg, err := LoadConfig("/tmp/nonexistent-milliways-carte.yaml")
	if err != nil {
		t.Fatalf("expected no error for missing file, got %v", err)
	}
	if len(cfg.Kitchens) != 9 {
		t.Errorf("expected defaults when file missing, got %d kitchens", len(cfg.Kitchens))
	}
}

func TestLoadConfig_ValidYAML(t *testing.T) {
	t.Parallel()
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
project_context_limit: 5
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
	if cfg.ProjectContextLimit != 5 {
		t.Errorf("expected project context limit 5, got %d", cfg.ProjectContextLimit)
	}
}

func TestLoadConfig_InvalidYAML(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
			t.Parallel()
			kc := KitchenConfig{Enabled: tt.enabled}
			if got := kc.IsEnabled(); got != tt.want {
				t.Errorf("IsEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLoadConfig_HTTPClientKitchen(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "carte.yaml")

	yaml := `
kitchens:
  api-kitchen:
    stations: [code]
    http_client:
      base_url: https://api.example.test
      auth_key: TEST_API_KEY
      model: gpt-4.1
      auth_type: apikey
      response_format: anthropic
      timeout_seconds: 42
      tier: cloud
      stations: [review]
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	kitchenCfg, ok := cfg.Kitchens["api-kitchen"]
	if !ok {
		t.Fatal("expected api-kitchen in config")
	}
	if kitchenCfg.HTTPClient == nil {
		t.Fatal("expected http_client config")
	}
	if kitchenCfg.HTTPClient.BaseURL != "https://api.example.test" {
		t.Fatalf("BaseURL = %q, want https://api.example.test", kitchenCfg.HTTPClient.BaseURL)
	}
	if kitchenCfg.HTTPClient.AuthKey != "TEST_API_KEY" {
		t.Fatalf("AuthKey = %q, want TEST_API_KEY", kitchenCfg.HTTPClient.AuthKey)
	}
	if kitchenCfg.HTTPClient.AuthType != "apikey" {
		t.Fatalf("AuthType = %q, want apikey", kitchenCfg.HTTPClient.AuthType)
	}
	if kitchenCfg.HTTPClient.Model != "gpt-4.1" {
		t.Fatalf("Model = %q, want gpt-4.1", kitchenCfg.HTTPClient.Model)
	}
	if kitchenCfg.HTTPClient.ResponseFormat != "anthropic" {
		t.Fatalf("ResponseFormat = %q, want anthropic", kitchenCfg.HTTPClient.ResponseFormat)
	}
	if kitchenCfg.HTTPClient.Timeout != 42 {
		t.Fatalf("Timeout = %d, want 42", kitchenCfg.HTTPClient.Timeout)
	}
	if kitchenCfg.HTTPClient.Tier != "cloud" {
		t.Fatalf("Tier = %q, want cloud", kitchenCfg.HTTPClient.Tier)
	}
	if len(kitchenCfg.HTTPClient.Stations) != 1 || kitchenCfg.HTTPClient.Stations[0] != "review" {
		t.Fatalf("Stations = %v, want [review]", kitchenCfg.HTTPClient.Stations)
	}
}

func boolPtr(b bool) *bool { return &b }
