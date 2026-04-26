package main

import (
	"os"
	"path/filepath"
	"sync"

	"github.com/mwigge/milliways/internal/plugins"
	"github.com/mwigge/milliways/internal/rules"
)

var bootstrapRulesOnce sync.Once

// Run executes milliways with the provided CLI arguments.
func Run(args []string) error {
	bootstrapRulesLoader()
	cmd := rootCmd()
	cmd.SetArgs(args)
	return cmd.Execute()
}

func bootstrapRulesLoader() {
	bootstrapRulesOnce.Do(func() {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return
		}
		loader := rules.NewRulesLoader(
			filepath.Join(homeDir, "dev", "src", "ai_local"),
			filepath.Join(homeDir, ".config", "milliways", "rules"),
		)
		_ = loader.EnsureDefaultRuleFiles()
		if err := loader.LoadAgents(); err != nil {
			return
		}
		if err := loader.LoadSkills(); err != nil {
			return
		}
		plugins.SetRulesLoader(loader)
	})
}
