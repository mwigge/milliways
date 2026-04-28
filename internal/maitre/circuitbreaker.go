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

package maitre

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Mode represents the company/private circuit breaker state.
type Mode string

const (
	ModeCompany Mode = "company"
	ModePrivate Mode = "private"
)

// ReadMode reads the current mode from ~/.claude/mode.
// Returns ModePrivate if the file doesn't exist (safe default).
func ReadMode() Mode {
	home, err := os.UserHomeDir()
	if err != nil {
		return ModePrivate
	}

	data, err := os.ReadFile(filepath.Join(home, ".claude", "mode"))
	if err != nil {
		return ModePrivate
	}

	mode := Mode(strings.TrimSpace(string(data)))
	switch mode {
	case ModeCompany, ModePrivate:
		return mode
	default:
		return ModePrivate
	}
}

// PathAllowed checks if a path is writable in the current mode.
func PathAllowed(path string, mode Mode) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolving path: %w", err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolving home: %w", err)
	}

	// Normalize for prefix matching — resolve as many symlink components as possible.
	abs = resolvePathBestEffort(filepath.Clean(abs))

	// Neutral paths (always allowed regardless of mode)
	neutral := []string{
		filepath.Join(home, ".ssh"),
		filepath.Join(home, ".claude"),
		filepath.Join(home, ".config"),
		os.TempDir(),
	}
	for _, n := range neutral {
		if strings.HasPrefix(abs, n) {
			return nil
		}
	}

	// Mode-specific paths — configured via env vars.
	// Set MILLIWAYS_COMPANY_ROOTS and MILLIWAYS_PRIVATE_ROOTS to colon-separated
	// path lists to enable context separation.
	companyPaths := splitEnvPaths(os.Getenv("MILLIWAYS_COMPANY_ROOTS"), home)
	privatePaths := splitEnvPaths(os.Getenv("MILLIWAYS_PRIVATE_ROOTS"), home)

	switch mode {
	case ModeCompany:
		for _, p := range companyPaths {
			if strings.HasPrefix(abs, p) {
				return nil
			}
		}
		for _, p := range privatePaths {
			if strings.HasPrefix(abs, p) {
				return errors.New("path blocked in company mode — switch: mode private")
			}
		}
	case ModePrivate:
		for _, p := range privatePaths {
			if strings.HasPrefix(abs, p) {
				return nil
			}
		}
		for _, p := range companyPaths {
			if strings.HasPrefix(abs, p) {
				return errors.New("path blocked in private mode — switch: mode company")
			}
		}
	}

	// Paths not in any list: allow (non-project paths like /tmp)
	return nil
}

// resolvePathBestEffort resolves symlinks in as many path components as
// possible, walking up from the full path until EvalSymlinks succeeds, then
// appending the remaining non-existent components. This handles the common
// case where a path doesn't exist yet but its parent dir is a symlink (e.g.
// /var → /private/var on macOS).
func resolvePathBestEffort(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.Clean(resolved)
	}
	parent := filepath.Dir(path)
	if parent == path {
		return path
	}
	return filepath.Join(resolvePathBestEffort(parent), filepath.Base(path))
}

func splitEnvPaths(val, home string) []string {
	if val == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(val, string(os.PathListSeparator)) {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if strings.HasPrefix(p, "~/") {
			p = filepath.Join(home, p[2:])
		}
		out = append(out, resolvePathBestEffort(filepath.Clean(p)))
	}
	return out
}
