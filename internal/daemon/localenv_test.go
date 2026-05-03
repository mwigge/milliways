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
