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

package runners

// Environment scoping for subprocess CLI runners. Without this, daemon
// runners that spawn `claude`, `codex`, `copilot`, etc. inherit the full
// daemon env — including MINIMAX_API_KEY, MILLIWAYS_LOCAL_API_KEY, AWS_*,
// GITHUB_TOKEN, GH_TOKEN, and any other secrets the user happens to have
// in their shell. With the agentic tool loop wired into HTTP runners, a
// prompt-injected codex session can `printenv` or read /proc/self/environ
// and the agentic loop folds it back to a remote model.
//
// Same shape as internal/kitchen/adapter/adapter.go's safeEnv (duplicated
// rather than imported because adapter is a sibling package; consolidating
// into an internal/sandbox package is a follow-up).

import (
	"os"
	"strings"
)

// safeRunnerEnvKeys is the set of environment variables passed to runner
// subprocess execution. Mirrors the kitchen adapter list with the same
// trade-offs:
//   - PATH/HOME/USER/SHELL/TERM/LANG/LC_*/TMPDIR/XDG_*  → required for
//     basic CLI operation
//   - ANTHROPIC_API_KEY / OPENAI_API_KEY / GOOGLE_API_KEY / GEMINI_API_KEY
//     → required for the respective CLI to authenticate
//   - OLLAMA_HOST → required if the user's local CLI workflow involves it
//
// Notably absent: MINIMAX_API_KEY, MILLIWAYS_LOCAL_API_KEY, AWS_*,
// GITHUB_TOKEN, GH_TOKEN — these are not required by any of the CLIs we
// shell to, so withholding them prevents accidental exfil.
var safeRunnerEnvKeys = map[string]bool{
	"PATH": true, "HOME": true, "USER": true, "SHELL": true,
	"TERM": true, "LANG": true, "LC_ALL": true, "LC_CTYPE": true,
	"TMPDIR": true, "XDG_CONFIG_HOME": true, "XDG_DATA_HOME": true, "XDG_RUNTIME_DIR": true,
	"ANTHROPIC_API_KEY": true, "OPENAI_API_KEY": true,
	"GOOGLE_API_KEY": true, "GEMINI_API_KEY": true,
	"OLLAMA_HOST": true,
}

// safeRunnerEnv returns a filtered environment for runner subprocess
// execution. Uses os.Environ() as the source and keeps only entries
// whose key appears in safeRunnerEnvKeys.
func safeRunnerEnv() []string {
	var env []string
	for _, e := range os.Environ() {
		key := e
		if idx := strings.IndexByte(e, '='); idx >= 0 {
			key = e[:idx]
		}
		if safeRunnerEnvKeys[key] {
			env = append(env, e)
		}
	}
	return env
}
