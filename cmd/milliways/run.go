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
		agentsDir := filepath.Join(homeDir, ".config", "milliways")
		if extra := os.Getenv("MILLIWAYS_AGENTS_DIR"); extra != "" {
			agentsDir = extra
		}
		rulesDir := filepath.Join(homeDir, ".config", "milliways", "rules")
		if extra := os.Getenv("MILLIWAYS_RULES_DIR"); extra != "" {
			rulesDir = extra
		}
		loader := rules.NewRulesLoader(agentsDir, rulesDir)
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
