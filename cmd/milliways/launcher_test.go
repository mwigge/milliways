package main

import (
	"net"
	"path/filepath"
	"testing"
	"time"
)

// TestSocketReachable verifies socketReachable returns true for a live UDS
// listener and false for a missing path within the polling deadline.
func TestSocketReachable(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	sockPath := filepath.Join(dir, "sock")

	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	// Drain accepts so the listener doesn't fill its backlog (we don't care
	// about the data — just that Dial succeeds).
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()

	if !socketReachable(sockPath, 200*time.Millisecond) {
		t.Errorf("socketReachable(live socket) = false, want true")
	}

	missing := filepath.Join(dir, "does-not-exist")
	if socketReachable(missing, 200*time.Millisecond) {
		t.Errorf("socketReachable(missing) = true, want false")
	}
}

// TestModeDispatch covers the parseLauncherMode argument parser. It is the
// pre-cobra dispatch hook that decides whether to run the cockpit launcher,
// the legacy REPL, or fall through to cobra.
func TestModeDispatch(t *testing.T) {
	tests := []struct {
		name string
		args []string
		env  map[string]string
		want launcherMode
	}{
		{
			name: "no args runs cockpit",
			args: []string{},
			want: modeCockpit,
		},
		{
			name: "single --repl runs legacy",
			args: []string{"--repl"},
			want: modeREPL,
		},
		{
			name: "--repl with subcommand still legacy",
			args: []string{"--repl", "subcommand"},
			want: modeREPL,
		},
		{
			name: "--version delegates to cobra",
			args: []string{"--version"},
			want: modeCobra,
		},
		{
			name: "subcommand delegates to cobra",
			args: []string{"login", "claude"},
			want: modeCobra,
		},
		{
			name: "MILLIWAYS_REPL=1 forces legacy",
			args: []string{},
			env:  map[string]string{"MILLIWAYS_REPL": "1"},
			want: modeREPL,
		},
		{
			name: "MILLIWAYS_REPL=0 ignored",
			args: []string{},
			env:  map[string]string{"MILLIWAYS_REPL": "0"},
			want: modeCockpit,
		},
		{
			name: "prompt args delegate to cobra",
			args: []string{"explain", "the", "auth", "flow"},
			want: modeCobra,
		},
		{
			name: "--help delegates to cobra",
			args: []string{"--help"},
			want: modeCobra,
		},
		{
			name: "-h delegates to cobra",
			args: []string{"-h"},
			want: modeCobra,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			got := parseLauncherMode(tt.args, tt.env["MILLIWAYS_REPL"])
			if got != tt.want {
				t.Errorf("parseLauncherMode(%v) = %v, want %v", tt.args, got, tt.want)
			}
		})
	}
}
