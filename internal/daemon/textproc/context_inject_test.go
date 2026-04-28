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

package textproc

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestExpandContext_NoTokens verifies passthrough when nothing to expand.
func TestExpandContext_NoTokens(t *testing.T) {
	t.Parallel()

	in := []byte("just a plain prompt with no tokens")
	got := ExpandContext(context.Background(), in)
	if string(got) != string(in) {
		t.Errorf("ExpandContext(plain) = %q, want %q", string(got), string(in))
	}
}

// TestExpandContext_AtFile verifies @file <path> inlines file content.
func TestExpandContext_AtFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(path, []byte("file content here"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	in := []byte("look at @file " + path + " please")
	got := string(ExpandContext(context.Background(), in))

	if strings.Contains(got, "@file "+path) {
		t.Errorf("token not expanded; got %q", got)
	}
	if !strings.Contains(got, "file content here") {
		t.Errorf("file content missing; got %q", got)
	}
	if !strings.Contains(got, "look at ") {
		t.Errorf("surrounding text lost; got %q", got)
	}
	if !strings.Contains(got, " please") {
		t.Errorf("trailing text lost; got %q", got)
	}
}

// TestExpandContext_AtFile_NotFound surfaces an error marker without aborting.
func TestExpandContext_AtFile_NotFound(t *testing.T) {
	t.Parallel()

	in := []byte("read @file /definitely/not/a/real/path/xyz123.txt and continue")
	got := string(ExpandContext(context.Background(), in))

	if !strings.Contains(got, "[milliways:") {
		t.Errorf("missing error marker; got %q", got)
	}
	if !strings.Contains(got, "and continue") {
		t.Errorf("trailing text lost; got %q", got)
	}
}

// TestExpandContext_AtShell runs a tiny external command and inlines stdout.
func TestExpandContext_AtShell(t *testing.T) {
	t.Parallel()

	in := []byte("output: @shell echo hi")
	got := string(ExpandContext(context.Background(), in))

	if strings.Contains(got, "@shell echo hi") {
		t.Errorf("token not expanded; got %q", got)
	}
	if !strings.Contains(got, "hi") {
		t.Errorf("shell stdout missing; got %q", got)
	}
}

// TestExpandContext_AtBranch verifies @branch is inlined; either a real
// branch name or an error marker if not in a repo.
func TestExpandContext_AtBranch(t *testing.T) {
	t.Parallel()

	in := []byte("on @branch now")
	got := string(ExpandContext(context.Background(), in))

	if strings.Contains(got, "@branch ") {
		t.Errorf("token not expanded; got %q", got)
	}
	if !strings.Contains(got, "on ") || !strings.Contains(got, "now") {
		t.Errorf("surrounding text lost; got %q", got)
	}
}

// TestExpandContext_MultipleTokens verifies several tokens in one prompt.
func TestExpandContext_MultipleTokens(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	if err := os.WriteFile(a, []byte("AAA"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("BBB"), 0o644); err != nil {
		t.Fatal(err)
	}

	in := []byte("first @file " + a + " then @file " + b + " end")
	got := string(ExpandContext(context.Background(), in))

	if !strings.Contains(got, "AAA") {
		t.Errorf("first file content missing; got %q", got)
	}
	if !strings.Contains(got, "BBB") {
		t.Errorf("second file content missing; got %q", got)
	}
	if strings.Contains(got, "@file") {
		t.Errorf("@file token still present; got %q", got)
	}
}

// TestExpandContext_AtFileColon verifies @file:<path> form.
func TestExpandContext_AtFileColon(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "colon.txt")
	if err := os.WriteFile(path, []byte("colon content"), 0o644); err != nil {
		t.Fatal(err)
	}

	in := []byte("check @file:" + path + " please")
	got := string(ExpandContext(context.Background(), in))

	if !strings.Contains(got, "colon content") {
		t.Errorf("colon-form file content missing; got %q", got)
	}
	if strings.Contains(got, "@file:") {
		t.Errorf("@file: token still present; got %q", got)
	}
}

// TestExpandContext_TokenAtStart verifies a token at the very start works.
func TestExpandContext_TokenAtStart(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "start.txt")
	if err := os.WriteFile(path, []byte("at-start"), 0o644); err != nil {
		t.Fatal(err)
	}

	in := []byte("@file " + path + " trailing")
	got := string(ExpandContext(context.Background(), in))

	if !strings.Contains(got, "at-start") {
		t.Errorf("file content missing; got %q", got)
	}
	if !strings.Contains(got, "trailing") {
		t.Errorf("trailing text lost; got %q", got)
	}
}
