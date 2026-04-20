package tools

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"
)

const defaultBashTimeout = 60 * time.Second

// handleBash executes a shell command and returns combined output.
func handleBash(ctx context.Context, args map[string]any) (string, error) {
	command, ok := stringArg(args, "command")
	if !ok || strings.TrimSpace(command) == "" {
		return "", errors.New("command is required")
	}
	timeout := durationArg(args, "timeout", defaultBashTimeout)
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	slog.Info("executing bash tool", "command", command, "timeout", timeout.String())

	cmd := exec.CommandContext(cmdCtx, "sh", "-c", command)
	output, err := cmd.CombinedOutput()
	if cmdCtx.Err() != nil {
		return string(output), fmt.Errorf("bash command timed out: %w", cmdCtx.Err())
	}
	if err != nil {
		return string(output), fmt.Errorf("bash command failed: %w", err)
	}
	return string(output), nil
}
