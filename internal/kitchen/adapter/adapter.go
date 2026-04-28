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

package adapter

import (
	"context"
	"errors"
	"os"
	"strings"

	"github.com/mwigge/milliways/internal/kitchen"
)

// ErrNotInteractive is returned by Send when the adapter does not support
// bidirectional communication (e.g. GenericAdapter).
var ErrNotInteractive = errors.New("kitchen adapter does not support interactive dialogue")

// Capabilities describes continuity-relevant adapter features.
type Capabilities struct {
	NativeResume        bool
	InteractiveSend     bool
	StructuredEvents    bool
	ExhaustionDetection string
}

// Adapter translates a kitchen's native protocol to the Event stream.
type Adapter interface {
	// Exec starts the kitchen process and returns an event channel.
	// The channel is closed when the kitchen process exits.
	// The caller MUST drain the channel to avoid goroutine leaks.
	Exec(ctx context.Context, task kitchen.Task) (<-chan Event, error)

	// Send writes a message to the kitchen's stdin (for dialogue).
	// Returns ErrNotInteractive if the adapter doesn't support it.
	Send(ctx context.Context, msg string) error

	// SupportsResume returns true if the kitchen supports session continuity.
	SupportsResume() bool

	// SessionID returns the current session ID for resume, or "" if none.
	SessionID() string

	// ProcessID returns the underlying subprocess pid when available, or 0.
	ProcessID() int

	// Capabilities returns continuity-relevant adapter capabilities.
	Capabilities() Capabilities
}

// AdapterOpts configures adapter behaviour.
type AdapterOpts struct {
	// AllowedTools restricts which tools the kitchen may use (claude-specific).
	AllowedTools []string

	// ResumeSessionID resumes a previous session if the adapter supports it.
	ResumeSessionID string

	// Verbose enables extra logging on the adapter.
	Verbose bool
}

// safeEnvKeys is the set of environment variables passed to subprocess execution.
var safeEnvKeys = map[string]bool{
	"PATH": true, "HOME": true, "USER": true, "SHELL": true,
	"TERM": true, "LANG": true, "LC_ALL": true, "LC_CTYPE": true,
	"TMPDIR": true, "XDG_CONFIG_HOME": true, "XDG_DATA_HOME": true,
	"ANTHROPIC_API_KEY": true, "OPENAI_API_KEY": true,
	"GOOGLE_API_KEY": true, "GEMINI_API_KEY": true,
	"OLLAMA_HOST": true, "OPENCODE_MODEL": true,
}

// safeEnv returns a filtered environment for subprocess execution.
// Only includes known-safe variables plus any task-specific env vars.
func safeEnv(extra map[string]string) []string {
	var env []string
	for _, e := range os.Environ() {
		key := e
		if idx := strings.IndexByte(e, '='); idx >= 0 {
			key = e[:idx]
		}
		if safeEnvKeys[key] {
			env = append(env, e)
		}
	}
	for k, v := range extra {
		env = append(env, k+"="+v)
	}
	return env
}
