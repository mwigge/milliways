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
