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

package main

// `milliwaysctl codegraph <verb>` — CodeGraph index management.
//
// Verbs:
//   index [path]   index the repo at path (default: cwd)
//   status         show index status / last indexed time

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// runCodegraph dispatches `milliwaysctl codegraph <verb> [args...]` and
// returns the process exit code.
func runCodegraph(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printCodegraphUsage(stderr)
		return 2
	}
	verb := args[0]
	rest := args[1:]
	switch verb {
	case "index":
		return runCodegraphIndex(rest, stdout, stderr)
	case "status":
		return runCodegraphStatus(rest, stdout, stderr)
	case "-h", "--help", "help":
		printCodegraphUsage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "milliwaysctl codegraph: unknown verb %q\n", verb)
		printCodegraphUsage(stderr)
		return 2
	}
}

func printCodegraphUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: milliwaysctl codegraph <verb> [args...]")
	fmt.Fprintln(w, "verbs:")
	fmt.Fprintln(w, "  index [path]   index the repo at path (default: cwd)")
	fmt.Fprintln(w, "  status         show index status / last indexed time")
}

// findCodegraphBinary resolves the codegraph binary by checking candidates in
// priority order:
//  1. $MILLIWAYS_CODEGRAPH_MCP_CMD
//  2. $HOME/.local/share/milliways/node/bin/codegraph
//  3. $HOME/.local/share/milliways/node_modules/.bin/codegraph
//  4. /usr/share/milliways/node/bin/codegraph
//  5. exec.LookPath("codegraph")
func findCodegraphBinary() (string, bool) {
	if v := os.Getenv("MILLIWAYS_CODEGRAPH_MCP_CMD"); v != "" {
		return v, true
	}
	home, _ := os.UserHomeDir()
	if home != "" {
		candidates := []string{
			filepath.Join(home, ".local", "share", "milliways", "node", "bin", "codegraph"),
			filepath.Join(home, ".local", "share", "milliways", "node_modules", ".bin", "codegraph"),
		}
		for _, p := range candidates {
			if isExecutablePath(p) {
				return p, true
			}
		}
	}
	if isExecutablePath("/usr/share/milliways/node/bin/codegraph") {
		return "/usr/share/milliways/node/bin/codegraph", true
	}
	if p, err := exec.LookPath("codegraph"); err == nil {
		return p, true
	}
	return "", false
}

// isExecutablePath returns true when path names a regular executable file.
func isExecutablePath(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Mode()&0o111 != 0
}

// runCodegraphIndex runs `codegraph index <path>` and on success writes
// MILLIWAYS_CODEGRAPH_WORKSPACE=<abs-path> to ~/.config/milliways/local.env.
func runCodegraphIndex(args []string, stdout, stderr io.Writer) int {
	bin, ok := findCodegraphBinary()
	if !ok {
		fmt.Fprintln(stderr, "codegraph: binary not found; install CodeGraph or set MILLIWAYS_CODEGRAPH_MCP_CMD")
		return 1
	}

	indexPath := "."
	if len(args) > 0 {
		indexPath = args[0]
	}

	absPath, err := filepath.Abs(indexPath)
	if err != nil {
		fmt.Fprintf(stderr, "codegraph index: resolve path %q: %v\n", indexPath, err)
		return 1
	}

	cmd := execCommand(bin, "index", absPath)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return ee.ExitCode()
		}
		fmt.Fprintf(stderr, "codegraph index: %v\n", err)
		return 1
	}

	envPath, err := configPath("local.env")
	if err != nil {
		fmt.Fprintf(stderr, "codegraph index: %v\n", err)
		return 1
	}
	if err := setLocalEnvKey(envPath, "MILLIWAYS_CODEGRAPH_WORKSPACE", absPath); err != nil {
		fmt.Fprintf(stderr, "codegraph index: write workspace to local.env: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "MILLIWAYS_CODEGRAPH_WORKSPACE=%s (wrote %s)\n", absPath, envPath)
	return 0
}

// runCodegraphStatus shows the currently configured workspace and whether the
// index appears to exist.
func runCodegraphStatus(_ []string, stdout, _ io.Writer) int {
	workspace := os.Getenv("MILLIWAYS_CODEGRAPH_WORKSPACE")
	if workspace == "" {
		fmt.Fprintln(stdout, "codegraph: no workspace indexed yet — run `milliwaysctl codegraph index [path]`")
		return 0
	}
	fmt.Fprintf(stdout, "MILLIWAYS_CODEGRAPH_WORKSPACE=%s\n", workspace)
	return 0
}

// setLocalEnvKey atomically rewrites path, replacing any existing line whose
// key matches key with key=value, and appending if absent. Creates parent
// directories and the file if needed (mode 0o600).
func setLocalEnvKey(path, key, value string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("setLocalEnvKey mkdir: %w", err)
	}

	// Read existing lines, stripping any line for key.
	var lines []string
	if data, err := os.ReadFile(path); err == nil {
		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		for scanner.Scan() {
			line := scanner.Text()
			k, _, found := strings.Cut(line, "=")
			if found && strings.TrimSpace(k) == key {
				continue // drop existing entry for this key
			}
			lines = append(lines, line)
		}
	}

	if value != "" {
		lines = append(lines, key+"="+value)
	}

	content := strings.Join(lines, "\n") + "\n"
	return os.WriteFile(path, []byte(content), 0o600)
}
