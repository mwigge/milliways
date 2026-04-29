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

package repl

// REPL-side aliases for the milliwaysctl `local` subcommand tree, so users
// of the legacy --repl line-reader can run /install-local-server etc. just
// like the milliways-term Leader+/ palette. Throwaway code: the entire REPL
// package is being deleted in the decommission-repl-into-daemon change.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// runMilliwaysctlLocal shells out `milliwaysctl local <verb> <args...>` and
// streams output to the REPL stdout. Mirrors the runOpsxCommand pattern.
func (r *REPL) runMilliwaysctlLocal(ctx context.Context, verb string, rawArgs string) error {
	bin := lookupMilliwaysctl()
	if bin == "" {
		return fmt.Errorf("milliwaysctl not found — install from this repo (`go install ./cmd/milliwaysctl/`) or set MILLIWAYSCTL_BIN")
	}
	args := []string{"local", verb}
	for _, w := range strings.Fields(rawArgs) {
		args = append(args, w)
	}
	r.println(fmt.Sprintf("[%s] %s", AccentColorText(r.scheme, "ctl"), strings.Join(args, " ")))
	cmd := exec.CommandContext(ctx, bin, args...)
	return streamCmdOutput(ctx, cmd, r.stdout)
}

func lookupMilliwaysctl() string {
	if env := strings.TrimSpace(os.Getenv("MILLIWAYSCTL_BIN")); env != "" {
		return env
	}
	if path, err := exec.LookPath("milliwaysctl"); err == nil {
		return path
	}
	return ""
}

// localCtlAlias registers a single REPL slash-command alias mapping to a
// `milliwaysctl local <verb>` invocation.
func localCtlAlias(verb string) func(ctx context.Context, r *REPL, args string) error {
	return func(ctx context.Context, r *REPL, args string) error {
		return r.runMilliwaysctlLocal(ctx, verb, args)
	}
}
