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

package tools

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"
)

const defaultBashTimeout = 60 * time.Second

// handleBash executes a shell command and returns combined output.
//
// Safety guardrails:
//   - cwd pinned to the workspace root (MILLIWAYS_WORKSPACE_ROOT or
//     process cwd) so commands operate within the same jail as file tools.
//   - The command string is NOT logged (only the command length and a
//     sha256 prefix). Models can be induced to construct commands that
//     contain secrets via env-var interpolation; logging the command at
//     INFO would leak them into the daemon log on every invocation.
func handleBash(ctx context.Context, args map[string]any) (string, error) {
	command, ok := stringArg(args, "command")
	if !ok || strings.TrimSpace(command) == "" {
		return "", errors.New("command is required")
	}
	timeout := durationArg(args, "timeout", defaultBashTimeout)
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cwd := WorkspaceRoot()
	if cwd == "" {
		return "", fmt.Errorf("bash refused: workspace root unresolvable")
	}

	digest := sha256.Sum256([]byte(command))
	slog.Info("executing bash tool",
		"cwd", cwd,
		"length", len(command),
		"sha256_prefix", fmt.Sprintf("%x", digest[:6]),
		"timeout", timeout.String(),
	)

	cmd := exec.CommandContext(cmdCtx, "sh", "-c", command)
	cmd.Dir = cwd
	output, err := cmd.CombinedOutput()
	if cmdCtx.Err() != nil {
		return string(output), fmt.Errorf("bash command timed out: %w", cmdCtx.Err())
	}
	if err != nil {
		return string(output), fmt.Errorf("bash command failed: %w", err)
	}
	return string(output), nil
}
