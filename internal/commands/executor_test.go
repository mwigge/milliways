package commands

import (
	"path/filepath"
	"testing"
)

func TestExecutorExecute_SubstitutesArgumentsAndRunsFences(t *testing.T) {
	t.Parallel()

	executor := NewExecutor(t.TempDir())
	command := Command{
		Name: "commit",
		Content: "# /commit\n\n" +
			"```bash\n" +
			"printf '%s' \"$COMMIT_MESSAGE\"\n" +
			"```\n\n" +
			"```bash\n" +
			"printf '%s' \"$BRANCH_NAME\"\n" +
			"```\n",
	}

	output, err := executor.Execute(command, map[string]string{
		"COMMIT_MESSAGE": "feat(commands): run executor",
		"BRANCH_NAME":    "feat/test-branch",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	want := "feat(commands): run executor\n---\nfeat/test-branch"
	if output != want {
		t.Fatalf("output = %q, want %q", output, want)
	}
}

func TestExecutorExecute_MissingArguments(t *testing.T) {
	t.Parallel()

	executor := NewExecutor(filepath.Dir(t.TempDir()))
	_, err := executor.Execute(Command{Name: "commit", Content: "$COMMIT_MESSAGE"}, nil)
	if err == nil {
		t.Fatal("Execute error = nil, want error")
	}
}
