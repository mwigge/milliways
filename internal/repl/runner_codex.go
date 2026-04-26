package repl

import (
	"context"
	"io"
	"os/exec"
)

type CodexRunner struct {
	binary string
}

func NewCodexRunner() *CodexRunner {
	return &CodexRunner{
		binary: "codex",
	}
}

func (r *CodexRunner) Name() string { return "codex" }

func (r *CodexRunner) Execute(ctx context.Context, prompt string, out io.Writer) error {
	cmd := exec.CommandContext(ctx, r.binary, "exec", "--skip-git-repo-check", "--", prompt)
	captured, err := runPTYWithContext(cmd, ctx)
	out.Write([]byte(captured))
	return err
}

func (r *CodexRunner) AuthStatus() (bool, error) {
	return true, nil
}

func (r *CodexRunner) Login() error {
	cmd := exec.Command("codex", "login")
	_, err := runPTY(cmd)
	return err
}

func (r *CodexRunner) Logout() error {
	cmd := exec.Command("codex", "logout")
	_, err := runPTY(cmd)
	return err
}

func (r *CodexRunner) Quota() (*QuotaInfo, error) {
	return nil, nil
}