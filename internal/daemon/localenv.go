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

package daemon

import (
	"bufio"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// allowedSetenvKeys is the allowlist for config.setenv and local.env.
// Only these keys may be injected into the daemon process.
var allowedSetenvKeys = map[string]bool{
	// Auth keys
	"MINIMAX_API_KEY":   true,
	"GEMINI_API_KEY":    true,
	"OPENAI_API_KEY":    true,
	"ANTHROPIC_API_KEY": true,
	// Model selection (live-switchable via /model <name>)
	"MINIMAX_MODEL":         true,
	"MILLIWAYS_LOCAL_MODEL": true,
	"ANTHROPIC_MODEL":       true,
	"CLAUDE_MODEL":          true,
	"OPENAI_MODEL":          true,
	"CODEX_MODEL":           true,
	"GEMINI_MODEL":          true,
	"GOOGLE_MODEL":          true,
	// Endpoint overrides
	"MINIMAX_API_URL":          true,
	"MINIMAX_ENDPOINT":         true,
	"MILLIWAYS_LOCAL_ENDPOINT": true,
	// Tuning
	"MILLIWAYS_MAX_TURNS": true,
}

// LocalEnvPath returns ~/.config/milliways/local.env.
func LocalEnvPath() string {
	return localEnvDefaultPath()
}

// localEnvDefaultPath returns ~/.config/milliways/local.env.
func localEnvDefaultPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "milliways", "local.env")
}

// LoadLocalEnv reads path and calls os.Setenv for each KEY=VALUE line
// whose key is in allowedSetenvKeys. Called once at daemon startup so
// keys persisted by /login survive restarts. A missing file is silently
// ignored; parse errors are logged and skipped.
func LoadLocalEnv(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Warn("local.env: read error", "path", path, "err", err)
		}
		return
	}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		k = strings.TrimSpace(k)
		if !allowedSetenvKeys[k] {
			continue
		}
		if err := os.Setenv(k, strings.TrimSpace(v)); err != nil {
			slog.Warn("local.env: setenv failed", "key", k, "err", err)
		}
	}
}

// persistLocalEnv writes key=value to path, replacing any existing entry
// for that key. Creates the file with 0o600 if it does not exist.
// Returns an error but is treated as non-fatal by callers — the key is
// already set in the daemon process for the current session.
func persistLocalEnv(path, key, value string) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}

	var lines []string
	if data, err := os.ReadFile(path); err == nil {
		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		for scanner.Scan() {
			line := scanner.Text()
			k, _, found := strings.Cut(line, "=")
			if found && strings.TrimSpace(k) == key {
				continue
			}
			lines = append(lines, line)
		}
	}
	lines = append(lines, key+"="+value)
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600)
}
