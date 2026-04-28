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

package commands

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAndExecuteCommand(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	commandText := "---\n" +
		"name: commit\n" +
		"description: Create a git commit\n" +
		"---\n\n" +
		"```bash\n" +
		"printf '%s' \"$COMMIT_MESSAGE\"\n" +
		"```\n"
	if err := os.WriteFile(filepath.Join(dir, "commit.md"), []byte(commandText), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	loaded, err := LoadCommands(dir)
	if err != nil {
		t.Fatalf("LoadCommands: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("len(commands) = %d, want 1", len(loaded))
	}

	output, err := NewExecutor(dir).Execute(loaded[0], map[string]string{"COMMIT_MESSAGE": "feat(commands): add integration test"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if output != "feat(commands): add integration test" {
		t.Fatalf("output = %q", output)
	}
}
