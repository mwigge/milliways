package maitre

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig_ParsesRoutingWeightOn(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "carte.yaml")

	yaml := `
routing:
  weight_on:
    claude:
      lsp_errors: 0.5
      dirty: 0.3
    goose:
      language_sql: 0.5
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	if got := cfg.Routing.WeightOn["claude"]["lsp_errors"]; got != 0.5 {
		t.Fatalf("claude lsp_errors weight = %v, want 0.5", got)
	}
	if got := cfg.Routing.WeightOn["claude"]["dirty"]; got != 0.3 {
		t.Fatalf("claude dirty weight = %v, want 0.3", got)
	}
	if got := cfg.Routing.WeightOn["goose"]["language_sql"]; got != 0.5 {
		t.Fatalf("goose language_sql weight = %v, want 0.5", got)
	}
}

func TestDefaultConfig_IncludesRoutingWeightOnDefaults(t *testing.T) {
	t.Parallel()

	cfg := defaultConfig()

	if got := cfg.Routing.WeightOn["claude"]["lsp_errors"]; got != 0.5 {
		t.Fatalf("claude lsp_errors weight = %v, want 0.5", got)
	}
	if got := cfg.Routing.WeightOn["claude"]["dirty"]; got != 0.3 {
		t.Fatalf("claude dirty weight = %v, want 0.3", got)
	}
	if got := cfg.Routing.WeightOn["opencode"]["in_test_file"]; got != 0.4 {
		t.Fatalf("opencode in_test_file weight = %v, want 0.4", got)
	}
	if got := cfg.Routing.WeightOn["goose"]["language_sql"]; got != 0.5 {
		t.Fatalf("goose language_sql weight = %v, want 0.5", got)
	}
}
