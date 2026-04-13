package maitre

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config represents the carte.yaml configuration.
type Config struct {
	Kitchens map[string]KitchenConfig `yaml:"kitchens"`
	Routing  RoutingConfig            `yaml:"routing"`
	Ledger   LedgerConfig             `yaml:"ledger"`
	Recipes  map[string][]RecipeStep  `yaml:"recipes"`
}

// KitchenConfig defines a kitchen's CLI command and capabilities.
type KitchenConfig struct {
	Cmd      string            `yaml:"cmd"`
	Args     []string          `yaml:"args"`
	Stations []string          `yaml:"stations"`
	CostTier string            `yaml:"cost_tier"`
	Enabled  *bool             `yaml:"enabled"`
	Env      map[string]string `yaml:"env"`
}

// IsEnabled returns true if the kitchen is enabled (default: true).
func (kc KitchenConfig) IsEnabled() bool {
	if kc.Enabled == nil {
		return true
	}
	return *kc.Enabled
}

// RoutingConfig defines keyword-to-kitchen routing rules.
type RoutingConfig struct {
	Keywords       map[string]string `yaml:"keywords"`
	Default        string            `yaml:"default"`
	BudgetFallback string            `yaml:"budget_fallback"`
}

// LedgerConfig defines ledger file paths.
type LedgerConfig struct {
	NDJSON string `yaml:"ndjson"`
	DB     string `yaml:"db"`
}

// RecipeStep defines one course in a recipe.
type RecipeStep struct {
	Station string `yaml:"station"`
	Kitchen string `yaml:"kitchen"`
}

// DefaultConfigDir returns ~/.config/milliways.
func DefaultConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".milliways"
	}
	return filepath.Join(home, ".config", "milliways")
}

// DefaultConfigPath returns the path to carte.yaml.
func DefaultConfigPath() string {
	return filepath.Join(DefaultConfigDir(), "carte.yaml")
}

// LoadConfig reads and parses carte.yaml, merging with defaults.
// When a config file exists, its kitchens replace the defaults entirely
// (user controls which kitchens are on the menu). Routing and ledger
// fields merge with defaults (user overrides only what they specify).
func LoadConfig(path string) (*Config, error) {
	defaults := defaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return defaults, nil
		}
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}

	var fileCfg Config
	if err := yaml.Unmarshal(data, &fileCfg); err != nil {
		return nil, fmt.Errorf("parsing config %s: %w", path, err)
	}

	// Kitchens: file replaces defaults entirely if specified
	if len(fileCfg.Kitchens) > 0 {
		defaults.Kitchens = fileCfg.Kitchens
	}

	// Routing: file overrides individual fields
	if len(fileCfg.Routing.Keywords) > 0 {
		defaults.Routing.Keywords = fileCfg.Routing.Keywords
	}
	if fileCfg.Routing.Default != "" {
		defaults.Routing.Default = fileCfg.Routing.Default
	}
	if fileCfg.Routing.BudgetFallback != "" {
		defaults.Routing.BudgetFallback = fileCfg.Routing.BudgetFallback
	}

	// Ledger: file overrides paths
	if fileCfg.Ledger.NDJSON != "" {
		defaults.Ledger.NDJSON = fileCfg.Ledger.NDJSON
	}
	if fileCfg.Ledger.DB != "" {
		defaults.Ledger.DB = fileCfg.Ledger.DB
	}

	// Recipes: file replaces defaults if specified
	if len(fileCfg.Recipes) > 0 {
		defaults.Recipes = fileCfg.Recipes
	}

	return defaults, nil
}

func defaultConfig() *Config {
	return &Config{
		Kitchens: map[string]KitchenConfig{
			"claude": {
				Cmd:      "claude",
				Args:     []string{"-p"},
				Stations: []string{"think", "plan", "review", "explore", "sign-off"},
				CostTier: "cloud",
			},
			"opencode": {
				Cmd:      "opencode",
				Args:     []string{"run"},
				Stations: []string{"code", "test", "refactor", "lint", "commit"},
				CostTier: "local",
			},
			"gemini": {
				Cmd:      "gemini",
				Args:     []string{},
				Stations: []string{"search", "compare", "docs", "research"},
				CostTier: "free",
			},
			"aider": {
				Cmd:      "aider",
				Args:     []string{"--message", "--yes-always", "--no-suggest-shell-commands"},
				Stations: []string{"multi-file", "git-commit"},
				CostTier: "cloud",
			},
			"goose": {
				Cmd:      "goose",
				Args:     []string{},
				Stations: []string{"tools", "database", "api", "mcp"},
				CostTier: "local",
			},
			"cline": {
				Cmd:      "cline",
				Args:     []string{"-y", "--json"},
				Stations: []string{"fleet", "parallel"},
				CostTier: "cloud",
			},
		},
		Routing: RoutingConfig{
			Keywords: map[string]string{
				"think": "claude", "plan": "claude", "explain": "claude",
				"explore": "claude", "review": "claude", "design": "claude",
				"code": "opencode", "implement": "opencode", "test": "opencode",
				"build": "opencode", "fix": "opencode",
				"refactor": "aider",
				"search":   "gemini", "research": "gemini", "compare": "gemini",
				"tools": "goose", "database": "goose",
			},
			Default:        "claude",
			BudgetFallback: "opencode",
		},
		Ledger: LedgerConfig{
			NDJSON: filepath.Join(DefaultConfigDir(), "ledger.ndjson"),
			DB:     filepath.Join(DefaultConfigDir(), "ledger.db"),
		},
		Recipes: map[string][]RecipeStep{
			"implement-feature": {
				{Station: "think", Kitchen: "claude"},
				{Station: "code", Kitchen: "opencode"},
				{Station: "test", Kitchen: "opencode"},
				{Station: "review", Kitchen: "claude"},
				{Station: "git-commit", Kitchen: "aider"},
			},
			"fix-bug": {
				{Station: "research", Kitchen: "gemini"},
				{Station: "think", Kitchen: "claude"},
				{Station: "code", Kitchen: "opencode"},
				{Station: "test", Kitchen: "opencode"},
				{Station: "git-commit", Kitchen: "aider"},
			},
		},
	}
}
