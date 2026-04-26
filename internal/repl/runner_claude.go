package repl

import (
	"context"
	"io"
	"os/exec"
)

type ClaudeRunner struct {
	binary string
	args   []string
}

func NewClaudeRunner() *ClaudeRunner {
	return &ClaudeRunner{
		binary: "claude",
		args:   []string{},
	}
}

func (r *ClaudeRunner) Name() string { return "claude" }

func (r *ClaudeRunner) Execute(ctx context.Context, prompt string, out io.Writer) error {
	args := append(r.args, "--print", prompt)
	cmd := exec.CommandContext(ctx, r.binary, args...)
	return streamCmdOutput(ctx, cmd, out)
}

func (r *ClaudeRunner) AuthStatus() (bool, error) {
	return true, nil
}

func (r *ClaudeRunner) Login() error {
	cmd := exec.Command("claude", "auth", "login")
	_, err := runPTY(cmd)
	return err
}

func (r *ClaudeRunner) Logout() error {
	cmd := exec.Command("claude", "auth", "logout")
	_, err := runPTY(cmd)
	return err
}

func (r *ClaudeRunner) Quota() (*QuotaInfo, error) {
	return nil, nil
}