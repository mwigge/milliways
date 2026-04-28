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
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mwigge/milliways/internal/config"
	"github.com/mwigge/milliways/internal/maitre"
	"github.com/spf13/cobra"
)

// RunInit bootstraps the milliways config directory, mode file, and rules.
func RunInit() error {
	modeManager, err := config.NewModeManager()
	if err != nil {
		return err
	}
	defer func() { _ = modeManager.Close() }()

	configDir := maitre.DefaultConfigDir()
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	if err := modeManager.Set(string(config.ModeNeutral)); err != nil {
		return err
	}

	rulesDir := filepath.Join(configDir, "rules")
	if err := os.MkdirAll(rulesDir, 0o755); err != nil {
		return fmt.Errorf("create rules dir: %w", err)
	}

	content, err := loadDefaultRulesContent()
	if err != nil {
		return err
	}

	rulesPath := filepath.Join(rulesDir, "global.md")
	if err := os.WriteFile(rulesPath, []byte(content), 0o600); err != nil {
		return fmt.Errorf("write global rules: %w", err)
	}

	return nil
}

func initCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Initialize milliways config, rules, and neutral mode",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := RunInit(); err != nil {
				return err
			}
			_, err := fmt.Fprintln(cmd.OutOrStdout(), "initialized milliways in neutral mode")
			return err
		},
	}
}

func modeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mode [neutral|company|private]",
		Short: "Show or set the milliways write mode",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, err := config.NewModeManager()
			if err != nil {
				return err
			}
			defer func() { _ = mgr.Close() }()

			if len(args) == 0 {
				_, err := fmt.Fprintln(cmd.OutOrStdout(), mgr.Current())
				return err
			}
			if err := mgr.Set(args[0]); err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), mgr.Current())
			return err
		},
	}
	return cmd
}

func loadDefaultRulesContent() (string, error) {
	paths := []string{
		expandUserPath("~/.config/milliways/AGENTS.md"),
		expandUserPath("~/.config/milliways/CLAUDE.md"),
	}
	if extra := os.Getenv("MILLIWAYS_RULES_PATH"); extra != "" {
		paths = append([]string{extra}, paths...)
	}
	for _, path := range paths {
		if strings.TrimSpace(path) == "" {
			continue
		}
		data, err := os.ReadFile(path)
		if err == nil {
			return string(data), nil
		}
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("read default rules %q: %w", path, err)
		}
	}
	return "# Global Rules\n\n- No AI attribution.\n- Use conventional commits.\n", nil
}

func expandUserPath(path string) string {
	if !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[2:])
}
