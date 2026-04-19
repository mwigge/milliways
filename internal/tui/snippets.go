package tui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

type snippet struct {
	Name string   `toml:"name"`
	Body string   `toml:"body"`
	Tags []string `toml:"tags"`
	Lang string   `toml:"lang"`
}

type snippetConfig struct {
	Snippets []snippet `toml:"snippet"`
}

var defaultSnippets = []snippet{
	{
		Name: "explain",
		Body: "Explain this code:\n$FILE",
		Tags: []string{"read", "explain"},
		Lang: "en",
	},
	{
		Name: "test for",
		Body: "Write pytest tests for:\n$CODE\n---\nRequirements:\n$REQ",
		Tags: []string{"test", "pytest"},
		Lang: "en",
	},
	{
		Name: "refactor",
		Body: "Refactor this code:\n$CODE\n---\nGoals:\n$GOALS",
		Tags: []string{"refactor"},
		Lang: "en",
	},
	{
		Name: "review",
		Body: "Review this code for bugs and style:\n$FILE",
		Tags: []string{"review", "security"},
		Lang: "en",
	},
}

func loadAllSnippets() []snippet {
	return loadSnippets(configDir("snippets.toml"))
}

func loadSnippets(path string) []snippet {
	var cfg snippetConfig
	if _, err := toml.DecodeFile(path, &cfg); err == nil {
		merged := mergeSnippets(defaultSnippets, cfg.Snippets)
		if len(merged) > 0 {
			return merged
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		_ = writeDefaultSnippets(path)
		return cloneSnippets(defaultSnippets)
	}

	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		_ = writeDefaultSnippets(path)
	}

	return cloneSnippets(defaultSnippets)
}

func writeDefaultSnippets(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create snippet config dir: %w", err)
	}

	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create snippet config: %w", err)
	}
	defer file.Close()

	cfg := snippetConfig{Snippets: cloneSnippets(defaultSnippets)}
	if err := toml.NewEncoder(file).Encode(cfg); err != nil {
		return fmt.Errorf("encode snippet config: %w", err)
	}

	return nil
}

func filterSnippets(all []snippet, filter string) []snippet {
	if filter == "" {
		return cloneSnippets(all)
	}

	needle := strings.ToLower(filter)
	filtered := make([]snippet, 0, len(all))
	for _, item := range all {
		if strings.Contains(strings.ToLower(item.Name), needle) {
			filtered = append(filtered, cloneSnippet(item))
			continue
		}
		for _, tag := range item.Tags {
			if strings.Contains(strings.ToLower(tag), needle) {
				filtered = append(filtered, cloneSnippet(item))
				break
			}
		}
	}

	return filtered
}

func configDir(filename string) string {
	if dir := os.Getenv("MILLIWAYS_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, filename)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return filename
	}

	return filepath.Join(home, ".config", "milliways", filename)
}

func mergeSnippets(defaults, custom []snippet) []snippet {
	if len(custom) == 0 {
		return cloneSnippets(defaults)
	}

	merged := make([]snippet, 0, len(custom)+len(defaults))
	seen := make(map[string]struct{}, len(custom))
	for _, item := range custom {
		merged = append(merged, cloneSnippet(item))
		seen[strings.ToLower(item.Name)] = struct{}{}
	}
	for _, item := range defaults {
		if _, ok := seen[strings.ToLower(item.Name)]; ok {
			continue
		}
		merged = append(merged, cloneSnippet(item))
	}

	return merged
}

func cloneSnippets(in []snippet) []snippet {
	if len(in) == 0 {
		return nil
	}
	out := make([]snippet, 0, len(in))
	for _, item := range in {
		out = append(out, cloneSnippet(item))
	}
	return out
}

func cloneSnippet(in snippet) snippet {
	return snippet{
		Name: in.Name,
		Body: in.Body,
		Tags: append([]string(nil), in.Tags...),
		Lang: in.Lang,
	}
}
