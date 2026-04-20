package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	defaultConfigPath        = "~/.config/milliways/config.json"
	defaultMiniMaxBaseURL    = "https://api.minimax.chat/v1"
	defaultMiniMaxModel      = "MiniMax-Text-01"
	defaultCompactThreshold  = 0.7
	defaultViewportScroll    = 3
	defaultViewportMaxBody   = 15
	defaultSessionDir        = "~/.config/milliways/sessions"
	defaultConfigPermissions = 0o755
)

var envPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// Config stores milliways runtime configuration.
type Config struct {
	Schema           string                     `json:"$schema,omitempty"`
	Provider         string                     `json:"provider"`
	MiniMax          MiniMaxConfig              `json:"minimax"`
	Memory           MemoryConfig               `json:"memory"`
	MCPServers       map[string]MCPServerConfig `json:"mcpServers,omitempty"`
	Plugins          []string                   `json:"plugins,omitempty"`
	AutoCompact      bool                       `json:"autoCompact"`
	CompactThreshold float64                    `json:"compactThreshold"`
	Viewport         ViewportConfig             `json:"viewport"`
	Editor           EditorConfig               `json:"editor"`
}

// MiniMaxConfig contains MiniMax provider settings.
type MiniMaxConfig struct {
	APIKey    string `json:"api_key"`
	BaseURL   string `json:"base_url"`
	Model     string `json:"model"`
	MaxTokens int    `json:"max_tokens,omitempty"`
}

// MemoryConfig contains session and MemPalace configuration.
type MemoryConfig struct {
	MemPalaceMCPCmd  string `json:"mempalace_mcp_cmd"`
	MemPalaceMCPArgs string `json:"mempalace_mcp_args"`
	SessionDir       string `json:"session_dir"`
}

// MCPServerConfig configures one MCP server entry.
type MCPServerConfig struct {
	Type    string            `json:"type"`
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

// ViewportConfig configures block stack scrolling.
type ViewportConfig struct {
	ScrollLines  int `json:"scrollLines"`
	MaxBodyLines int `json:"maxBodyLines"`
}

// EditorConfig configures input and rendering behavior.
type EditorConfig struct {
	VimMode         bool `json:"vimMode"`
	SyntaxHighlight bool `json:"syntaxHighlight"`
}

// DefaultPath returns the default milliways config path.
func DefaultPath() string {
	return expandHome(defaultConfigPath)
}

// Load loads milliways JSON configuration from path.
func Load(path string) (Config, error) {
	resolvedPath := path
	if strings.TrimSpace(resolvedPath) == "" {
		resolvedPath = defaultConfigPath
	}
	resolvedPath = expandHome(resolvedPath)

	data, err := os.ReadFile(resolvedPath)
	if err != nil {
		return Config{}, fmt.Errorf("read config %q: %w", resolvedPath, err)
	}

	// JSON config supports env substitution across arbitrary nested values, so a
	// dynamic intermediate representation is required before decoding to structs.
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return Config{}, fmt.Errorf("decode config %q: %w", resolvedPath, err)
	}

	substituted := substituteEnv(raw)
	normalized, err := json.Marshal(substituted)
	if err != nil {
		return Config{}, fmt.Errorf("normalize config %q: %w", resolvedPath, err)
	}

	var cfg Config
	if err := json.Unmarshal(normalized, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config %q: %w", resolvedPath, err)
	}

	applyDefaults(&cfg)
	return cfg, nil
}

func applyDefaults(cfg *Config) {
	if cfg == nil {
		return
	}
	if cfg.MiniMax.BaseURL == "" {
		cfg.MiniMax.BaseURL = defaultMiniMaxBaseURL
	}
	if cfg.MiniMax.Model == "" {
		cfg.MiniMax.Model = defaultMiniMaxModel
	}
	if cfg.Memory.SessionDir == "" {
		cfg.Memory.SessionDir = defaultSessionDir
	}
	cfg.Memory.SessionDir = expandHome(cfg.Memory.SessionDir)
	if cfg.CompactThreshold == 0 {
		cfg.CompactThreshold = defaultCompactThreshold
	}
	if cfg.Viewport.ScrollLines == 0 {
		cfg.Viewport.ScrollLines = defaultViewportScroll
	}
	if cfg.Viewport.MaxBodyLines == 0 {
		cfg.Viewport.MaxBodyLines = defaultViewportMaxBody
	}
	if !cfg.AutoCompact {
		cfg.AutoCompact = true
	}
	if !cfg.Editor.VimMode {
		cfg.Editor.VimMode = true
	}
	if !cfg.Editor.SyntaxHighlight {
		cfg.Editor.SyntaxHighlight = true
	}
	for index, plugin := range cfg.Plugins {
		cfg.Plugins[index] = expandHome(plugin)
	}
	for name, server := range cfg.MCPServers {
		server.Command = expandHome(server.Command)
		cfg.MCPServers[name] = server
	}
}

func substituteEnv(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, nested := range typed {
			result[key] = substituteEnv(nested)
		}
		return result
	case []any:
		result := make([]any, len(typed))
		for index, nested := range typed {
			result[index] = substituteEnv(nested)
		}
		return result
	case string:
		return envPattern.ReplaceAllStringFunc(typed, func(match string) string {
			parts := envPattern.FindStringSubmatch(match)
			if len(parts) != 2 {
				return match
			}
			return os.Getenv(parts[1])
		})
	default:
		return value
	}
}

func expandHome(path string) string {
	if path == "" || !strings.HasPrefix(path, "~/") {
		return path
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(homeDir, path[2:])
}

// EnsureConfigDir creates the default config directory if needed.
func EnsureConfigDir() error {
	return os.MkdirAll(filepath.Dir(DefaultPath()), defaultConfigPermissions)
}
