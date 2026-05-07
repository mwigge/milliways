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

import (
	"os"
	"strings"
	"testing"
)

func TestEnsureRunnerSystemPathAddsBashPath(t *testing.T) {
	path := ensureRunnerSystemPath("/tmp/copilot-bin")

	if !pathContains(path, "/tmp/copilot-bin") {
		t.Fatalf("custom path missing from %q", path)
	}
	if !pathContains(path, "/bin") {
		t.Fatalf("/bin missing from %q", path)
	}
	if !pathContains(path, "/usr/bin") {
		t.Fatalf("/usr/bin missing from %q", path)
	}
}

func TestSafeRunnerEnvMILLIWAYSPathKeepsSystemFallbacks(t *testing.T) {
	t.Setenv("PATH", "/should/not/win")
	t.Setenv("MILLIWAYS_PATH", "/tmp/copilot-bin")

	env := safeRunnerEnv()
	path := envValue(env, "PATH")
	if !pathContains(path, "/tmp/copilot-bin") {
		t.Fatalf("MILLIWAYS_PATH missing from PATH=%q", path)
	}
	if pathContains(path, "/should/not/win") {
		t.Fatalf("inherited PATH leaked despite MILLIWAYS_PATH override: %q", path)
	}
	if !pathContains(path, "/bin") {
		t.Fatalf("/bin missing from PATH=%q", path)
	}
}

func envValue(env []string, key string) string {
	prefix := key + "="
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			return strings.TrimPrefix(entry, prefix)
		}
	}
	return ""
}

func pathContains(path, want string) bool {
	for _, part := range strings.Split(path, string(os.PathListSeparator)) {
		if part == want {
			return true
		}
	}
	return false
}
