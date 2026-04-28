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
