package repl

import (
	"context"
	"io"
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
	args := []string{"-p", buildTextPrompt(req), "--allow-all-tools"}
	cmd := exec.CommandContext(ctx, r.binary, args...)
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
