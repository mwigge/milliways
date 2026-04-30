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
// pre-cobra dispatch hook that decides whether to launch milliways-term or
// fall through to cobra.
func TestModeDispatch(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want launcherMode
	}{
		{
			name: "no args runs cockpit",
			args: []string{},
			want: modeCockpit,
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
			got := parseLauncherMode(tt.args)
			if got != tt.want {
				t.Errorf("parseLauncherMode(%v) = %v, want %v", tt.args, got, tt.want)
			}
		})
	}
}
