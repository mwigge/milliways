package commands

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadCommands_ParsesFrontmatterAndArguments(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	commandPath := filepath.Join(dir, "commit.md")
	content := "---\n" +
		"name: commit\n" +
		"description: Commit current changes\n" +
		"arguments:\n" +
		"  - name: COMMIT_MESSAGE\n" +
		"    description: Commit message to use\n" +
		"    required: true\n" +
		"---\n\n" +
		"# /commit\n\n" +
		"Use the message:\n\n" +
		"```bash\n" +
		"printf '%s' \"$COMMIT_MESSAGE\"\n" +
		"```\n\n" +
		"Optional branch: $BRANCH_NAME\n"
	if err := os.WriteFile(commandPath, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	loaded, err := LoadCommands(dir)
	if err != nil {
		t.Fatalf("LoadCommands: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("len(commands) = %d, want 1", len(loaded))
	}

	command := loaded[0]
	if command.Name != "commit" {
		t.Fatalf("Name = %q, want commit", command.Name)
	}
	if command.Namespace != "user" {
		t.Fatalf("Namespace = %q, want user", command.Namespace)
	}
	if command.Description != "Commit current changes" {
		t.Fatalf("Description = %q", command.Description)
	}
	if len(command.Arguments) != 2 {
		t.Fatalf("len(arguments) = %d, want 2", len(command.Arguments))
	}
	if command.Arguments[0].Name != "BRANCH_NAME" {
		t.Fatalf("first arg = %q, want BRANCH_NAME", command.Arguments[0].Name)
	}
	if command.Arguments[1].Name != "COMMIT_MESSAGE" {
		t.Fatalf("second arg = %q, want COMMIT_MESSAGE", command.Arguments[1].Name)
	}
}

func TestLoadCommands_MissingDirectoryReturnsNil(t *testing.T) {
	t.Parallel()

	loaded, err := LoadCommands(filepath.Join(t.TempDir(), "missing"))
	if err != nil {
		t.Fatalf("LoadCommands: %v", err)
	}
	if loaded != nil {
		t.Fatalf("commands = %#v, want nil", loaded)
	}
}
