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

// Package main is the milliwaysd daemon: long-running JSON-RPC 2.0 server
// hosting runners, sessions, MCP, MemPalace, sommelier, pantry, and the OTel
// SDK. See openspec/changes/milliways-emulator-fork/specs/milliwaysd/spec.md.
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/mwigge/milliways/internal/daemon"
)

var version = "dev"

func main() {
	var (
		socket      = flag.String("socket", "", "UDS path (default: ${state}/sock)")
		stateDir    = flag.String("state-dir", "", "state dir (default: ${XDG_RUNTIME_DIR:-$HOME/.local/state/milliways})")
		logLevel    = flag.String("log-level", "info", "debug|info|warn|error")
		showVersion = flag.Bool("version", false, "print version and exit")
	)
	flag.Parse()
	daemon.Version = version
	if *showVersion {
		fmt.Printf("milliwaysd version %s\n", version)
		return
	}

	state, err := resolveStateDir(*stateDir)
	if err != nil {
		die("state dir: %v", err)
	}
	if *socket == "" {
		*socket = filepath.Join(state, "sock")
	}

	setupLogger(*logLevel)

	localEnvPath := daemon.LocalEnvPath()
	daemon.LoadLocalEnv(localEnvPath)

	// Watch local.env for changes and reload it automatically.
	// This lets scripts and installers write new env vars without requiring
	// a daemon restart.
	go watchLocalEnv(localEnvPath)

	pidPath := filepath.Join(state, "pid")
	lock, err := daemon.AcquireLock(pidPath)
	if err != nil {
		die("acquire lock: %v", err)
	}
	defer lock.Release()

	srv, err := daemon.NewServer(*socket)
	if err != nil {
		die("new server: %v", err)
	}
	defer srv.Close()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-sigs
		slog.Info("shutdown signal received")
		srv.Shutdown()
	}()

	slog.Info("milliwaysd up", "socket", *socket, "pid", os.Getpid())
	if err := srv.Serve(); err != nil {
		die("serve: %v", err)
	}
}

func resolveStateDir(s string) (string, error) {
	if s == "" {
		if x := os.Getenv("XDG_RUNTIME_DIR"); x != "" {
			s = filepath.Join(x, "milliways")
		} else {
			h, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			s = filepath.Join(h, ".local", "state", "milliways")
		}
	}
	return s, os.MkdirAll(s, 0o700)
}

func setupLogger(level string) {
	var l slog.Level
	switch level {
	case "debug":
		l = slog.LevelDebug
	case "warn":
		l = slog.LevelWarn
	case "error":
		l = slog.LevelError
	default:
		l = slog.LevelInfo
	}
	h := slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: l})
	slog.SetDefault(slog.New(h))
}

func die(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "milliwaysd: "+format+"\n", args...)
	os.Exit(1)
}

// watchLocalEnv polls local.env every 5 seconds and reloads it when the
// file's modification time changes. This lets install scripts, milliwaysctl
// upgrade, and /install-local-server write new env vars (e.g.
// MILLIWAYS_LOCAL_ENDPOINT) without requiring a daemon restart.
func watchLocalEnv(path string) {
	var lastMod time.Time
	for {
		time.Sleep(5 * time.Second)
		info, err := os.Stat(path)
		if err != nil {
			continue // file may not exist yet
		}
		if info.ModTime().After(lastMod) {
			lastMod = info.ModTime()
			daemon.LoadLocalEnv(path)
			slog.Debug("local.env reloaded", "path", path)
		}
	}
}
