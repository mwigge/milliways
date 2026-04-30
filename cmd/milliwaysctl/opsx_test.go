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
