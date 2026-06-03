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
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPersistLocalEnvReplacesAndLoadsMiniMaxAPIKey(t *testing.T) {
	t.Setenv("MINIMAX_API_KEY", "")
	path := filepath.Join(t.TempDir(), "milliways", "local.env")

	if err := persistLocalEnv(path, "MINIMAX_API_KEY", "old-key"); err != nil {
		t.Fatalf("persist old key: %v", err)
	}
	if err := persistLocalEnv(path, "MINIMAX_API_KEY", "new-key"); err != nil {
		t.Fatalf("persist new key: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read local.env: %v", err)
	}
	content := string(data)
	if strings.Count(content, "MINIMAX_API_KEY=") != 1 {
		t.Fatalf("local.env should contain one MINIMAX_API_KEY entry, got:\n%s", content)
	}
	if !strings.Contains(content, "MINIMAX_API_KEY=new-key") {
		t.Fatalf("local.env missing new key, got:\n%s", content)
	}

	LoadLocalEnv(path)
	if got := os.Getenv("MINIMAX_API_KEY"); got != "new-key" {
		t.Fatalf("MINIMAX_API_KEY = %q, want new-key", got)
	}
}

func TestLoadLocalEnvLoadsLocalRunnerAPIKey(t *testing.T) {
	t.Setenv("MILLIWAYS_LOCAL_ENDPOINT", "")
	t.Setenv("MILLIWAYS_LOCAL_MODEL", "")
	t.Setenv("MILLIWAYS_LOCAL_API_KEY", "")
	t.Setenv("KIMI_TOOLS", "")
	t.Setenv("DEEPSEEK_TOOLS", "")
	t.Setenv("MINIMAX_TOOLS", "")
	t.Setenv("MILLIWAYS_LOCAL_TOOLS", "")
	path := filepath.Join(t.TempDir(), "milliways", "local.env")

	content := strings.Join([]string{
		"MILLIWAYS_LOCAL_ENDPOINT=http://127.0.0.1:8765/v1",
		"MILLIWAYS_LOCAL_MODEL=qwen",
		"MILLIWAYS_LOCAL_API_KEY=local-secret",
		"KIMI_TOOLS=off",
		"DEEPSEEK_TOOLS=off",
		"MINIMAX_TOOLS=off",
		"MILLIWAYS_LOCAL_TOOLS=off",
		"UNSAFE_KEY=ignored",
		"",
	}, "\n")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	LoadLocalEnv(path)
	if got := os.Getenv("MILLIWAYS_LOCAL_ENDPOINT"); got != "http://127.0.0.1:8765/v1" {
		t.Fatalf("MILLIWAYS_LOCAL_ENDPOINT = %q", got)
	}
	if got := os.Getenv("MILLIWAYS_LOCAL_MODEL"); got != "qwen" {
		t.Fatalf("MILLIWAYS_LOCAL_MODEL = %q", got)
	}
	if got := os.Getenv("MILLIWAYS_LOCAL_API_KEY"); got != "local-secret" {
		t.Fatalf("MILLIWAYS_LOCAL_API_KEY = %q", got)
	}
	if got := os.Getenv("KIMI_TOOLS"); got != "off" {
		t.Fatalf("KIMI_TOOLS = %q", got)
	}
	if got := os.Getenv("DEEPSEEK_TOOLS"); got != "off" {
		t.Fatalf("DEEPSEEK_TOOLS = %q", got)
	}
	if got := os.Getenv("MINIMAX_TOOLS"); got != "off" {
		t.Fatalf("MINIMAX_TOOLS = %q", got)
	}
	if got := os.Getenv("MILLIWAYS_LOCAL_TOOLS"); got != "off" {
		t.Fatalf("MILLIWAYS_LOCAL_TOOLS = %q", got)
	}
	if got := os.Getenv("UNSAFE_KEY"); got != "" {
		t.Fatalf("UNSAFE_KEY should not be loaded, got %q", got)
	}
}

func TestLocalEnvPathUsesXDGConfigHome(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "xdg"))
	t.Setenv("HOME", filepath.Join(tmp, "home"))

	want := filepath.Join(tmp, "xdg", "milliways", "local.env")
	if got := LocalEnvPath(); got != want {
		t.Fatalf("LocalEnvPath() = %q, want %q", got, want)
	}
}
