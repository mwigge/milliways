package repl

import (
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
)

type CopilotRunner struct {
	binary string
}

func NewCopilotRunner() *CopilotRunner {
	return &CopilotRunner{
		binary: "copilot",
	}
}

func (r *CopilotRunner) Name() string { return "copilot" }

func (r *CopilotRunner) Execute(ctx context.Context, req DispatchRequest, out io.Writer) error {
	if len(req.Attachments) > 0 {
		slog.Warn("copilot: image attachments not supported, proceeding with text only",
			"count", len(req.Attachments))
	}
	cwd, _ := os.Getwd()
	// --add-dir scopes file search to the project directory, avoiding macOS
	// system paths that produce permission errors when copilot searches broadly.
	args := []string{"-p", buildTextPrompt(req), "--allow-all-tools", "--add-dir", cwd}
	cmd := exec.CommandContext(ctx, r.binary, args...)
	cmd.Dir = cwd
	return streamCmdOutput(ctx, cmd, out)
}

func (r *CopilotRunner) AuthStatus() (bool, error) {
	return true, nil
}

func (r *CopilotRunner) Login() error {
	cmd := exec.Command("copilot", "auth", "login")
	_, err := runPTY(cmd)
	return err
}

func (r *CopilotRunner) Logout() error {
	cmd := exec.Command("copilot", "auth", "logout")
	_, err := runPTY(cmd)
	return err
}

func (r *CopilotRunner) Quota() (*QuotaInfo, error) {
	return nil, nil
}
