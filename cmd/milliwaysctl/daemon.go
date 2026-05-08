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

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

var (
	daemonSignalProcess = func(pid int, sig os.Signal) error {
		proc, err := os.FindProcess(pid)
		if err != nil {
			return err
		}
		return proc.Signal(sig)
	}
	daemonProbeProcess = func(pid int) error {
		return syscall.Kill(pid, 0)
	}
	daemonProcessAlive = func(pid int) bool {
		err := daemonProbeProcess(pid)
		return err == nil || errors.Is(err, syscall.EPERM)
	}
)

func runDaemon(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("daemon", flag.ContinueOnError)
	fs.SetOutput(stderr)
	stateDir := fs.String("state", filepath.Dir(defaultSocket()), "state directory (default: XDG_RUNTIME_DIR or ~/.local/state/milliways)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	rest := fs.Args()
	if len(rest) == 0 {
		daemonUsage(stderr)
		return 2
	}
	switch rest[0] {
	case "stop":
		return runDaemonStop(*stateDir, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "milliwaysctl daemon: unknown verb %q\n", rest[0])
		daemonUsage(stderr)
		return 2
	}
}

func daemonUsage(stderr io.Writer) {
	fmt.Fprintln(stderr, "usage: milliwaysctl daemon <stop> [--state PATH]")
}

func runDaemonStop(stateDir string, stdout, stderr io.Writer) int {
	pidPath := filepath.Join(stateDir, "pid")
	data, err := os.ReadFile(pidPath)
	if errors.Is(err, os.ErrNotExist) {
		fmt.Fprintln(stdout, "milliwaysd not running")
		return 0
	}
	if err != nil {
		fmt.Fprintf(stderr, "read pid file: %v\n", err)
		return 1
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		fmt.Fprintf(stderr, "invalid pid file %s\n", pidPath)
		return 1
	}
	if !daemonProcessAlive(pid) {
		_ = os.Remove(pidPath)
		fmt.Fprintln(stdout, "milliwaysd not running")
		return 0
	}
	if err := daemonSignalProcess(pid, syscall.SIGTERM); err != nil {
		fmt.Fprintf(stderr, "stop milliwaysd pid %d: %v\n", pid, err)
		return 1
	}
	for i := 0; i < 50; i++ {
		if !daemonProcessAlive(pid) {
			fmt.Fprintf(stdout, "stopped milliwaysd pid %d\n", pid)
			return 0
		}
		time.Sleep(100 * time.Millisecond)
	}
	fmt.Fprintf(stderr, "milliwaysd pid %d did not stop within 5s\n", pid)
	return 1
}
