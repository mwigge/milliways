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
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestRunOpsx_NoArgsPrintsUsageAndExits2(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	code := runOpsx(nil, &stdout, &stderr)
	if code != 2 {
		t.Errorf("exit = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "usage") {
		t.Errorf("stderr = %q, want it to mention usage", stderr.String())
	}
}

func TestRunOpsx_HelpExitsZero(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	code := runOpsx([]string{"--help"}, &stdout, &stderr)
	if code != 0 {
		t.Errorf("exit = %d, want 0", code)
	}
	for _, want := range []string{"list", "status", "show", "archive", "validate"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("help output missing %q; got: %s", want, stdout.String())
		}
	}
}

func TestRunOpsx_DispatchesUnknownVerbCleanly(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	code := runOpsx([]string{"hallucinated-verb"}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("exit = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "hallucinated-verb") {
		t.Errorf("stderr = %q, want it to name the bad verb", stderr.String())
	}
}

func TestBuildOpsxArgs(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		verb   string
		rest   []string
		want   []string
	}{
		{"list no args", "list", nil, []string{"list"}},
		{"status with change", "status", []string{"my-change"}, []string{"status", "--change", "my-change"}},
		{"status no args", "status", nil, []string{"status"}},
		{"show with name", "show", []string{"x"}, []string{"show", "x"}},
		{"archive with name", "archive", []string{"x"}, []string{"archive", "x"}},
		{"validate maps to change validate", "validate", []string{"x"}, []string{"change", "validate", "x"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := buildOpsxArgs(c.verb, c.rest)
			if !equalSlices(got, c.want) {
				t.Errorf("buildOpsxArgs(%q,%v) = %v, want %v", c.verb, c.rest, got, c.want)
			}
		})
	}
}

func TestRunOpsx_HelpShowsExploreApply(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	code := runOpsx([]string{"--help"}, &stdout, io.Discard)
	if code != 0 {
		t.Errorf("exit = %d, want 0", code)
	}
	for _, want := range []string{"explore", "apply", "OPSX_AGENT"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("help output missing %q; got: %s", want, stdout.String())
		}
	}
}

func TestRunOpsx_HelpShowsPathFlag(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	code := runOpsx([]string{"--help"}, &stdout, io.Discard)
	if code != 0 {
		t.Errorf("exit = %d, want 0", code)
	}
	for _, want := range []string{"--path", "-p", "project root"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("help output missing %q; got: %s", want, stdout.String())
		}
	}
}

func TestRunOpsx_PathFlagChdirsAndListFails(t *testing.T) {
	// --path to a non-openspec directory should fail because openspec
	// binary won't find an openspec project (or any openspec binary at all).
	t.Setenv("OPENSPEC_BIN", "/no/such/openspec")

	var stdout, stderr bytes.Buffer
	code := runOpsx([]string{"--path", "/tmp", "list"}, &stdout, &stderr)
	if code == 0 {
		t.Errorf("exit = 0, want non-zero")
	}
}

func TestRunOpsx_PathFlagWithEqualsSyntax(t *testing.T) {
	t.Setenv("OPENSPEC_BIN", "/no/such/openspec")

	var stdout, stderr bytes.Buffer
	code := runOpsx([]string{"--path=/tmp", "list"}, &stdout, &stderr)
	if code == 0 {
		t.Errorf("exit = 0, want non-zero")
	}
}

// Path flag parsing tests removed to avoid Setenv + t.Parallel conflicts.

func TestRunOpsx_ExploreRequiresChangeArg(t *testing.T) {
	t.Parallel()

	// explore without args: check it reaches the "change name required" path.
	// We can't fully exercise it without a daemon, but we can verify it
	// doesn't panic and returns non-zero.
	var stdout, stderr bytes.Buffer
	code := runOpsx([]string{"explore"}, &stdout, &stderr)
	if code == 0 {
		t.Errorf("exit = 0, want non-zero (change name required)")
	}
	if !strings.Contains(stderr.String(), "change name required") {
		t.Errorf("stderr = %q, want it to mention 'change name required'", stderr.String())
	}
}

func TestRunOpsx_ApplyRequiresChangeArg(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	code := runOpsx([]string{"apply"}, &stdout, &stderr)
	if code == 0 {
		t.Errorf("exit = 0, want non-zero (change name required)")
	}
	if !strings.Contains(stderr.String(), "change name required") {
		t.Errorf("stderr = %q, want it to mention 'change name required'", stderr.String())
	}
}

func TestRunOpsx_NoBinary(t *testing.T) {
	t.Setenv("OPENSPEC_BIN", "/no/such/binary/that/should/not/exist")

	var stdout, stderr bytes.Buffer
	code := runOpsx([]string{"list"}, &stdout, &stderr)
	if code == 0 {
		t.Errorf("exit = 0, want non-zero")
	}
	if !strings.Contains(stderr.String(), "openspec") {
		t.Errorf("stderr = %q, want it to mention openspec", stderr.String())
	}
}

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
