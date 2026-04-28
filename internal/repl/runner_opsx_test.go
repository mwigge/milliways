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

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// TestLookupOpenspec_EnvVar verifies OPENSPEC_BIN env var takes priority.
// Cannot run in parallel because it mutates an environment variable.
func TestLookupOpenspec_EnvVar(t *testing.T) {
	t.Setenv("OPENSPEC_BIN", "/nonexistent/openspec")

	got := lookupOpenspec()
	if got != "/nonexistent/openspec" {
		t.Errorf("lookupOpenspec() = %q, want %q", got, "/nonexistent/openspec")
	}
}

// TestLookupOpenspec_NotFound verifies that clearing OPENSPEC_BIN does not
// return the env-var value. Cannot run in parallel — mutates env.
func TestLookupOpenspec_NotFound(t *testing.T) {
	t.Setenv("OPENSPEC_BIN", "")

	got := lookupOpenspec()
	// The env var path must not be returned when the var is empty.
	if got == "/nonexistent/openspec" {
		t.Errorf("lookupOpenspec() returned stale env value %q", got)
	}
}

// TestHandleOpsxList_NoBinary verifies the "not found" error when the binary
// is absent. Cannot run in parallel — mutates env.
func TestHandleOpsxList_NoBinary(t *testing.T) {
	t.Setenv("OPENSPEC_BIN", "")

	if lookupOpenspec() != "" {
		t.Skip("openspec found on PATH — skipping no-binary test")
	}

	buf := &bytes.Buffer{}
	r := NewREPL(buf)

	err := handleOpsxList(context.Background(), r, "")
	if err == nil {
		t.Fatal("expected error when openspec binary is absent, got nil")
	}
	if !strings.Contains(err.Error(), "openspec not found") {
		t.Errorf("error = %q, want to contain %q", err.Error(), "openspec not found")
	}
}

// TestHandleOpsxApply_NoRunner verifies the "no runner" error path.
func TestHandleOpsxApply_NoRunner(t *testing.T) {
	t.Parallel()

	buf := &bytes.Buffer{}
	r := NewREPL(buf)
	// runner is nil by default — NewREPL does not set one.

	err := handleOpsxApply(context.Background(), r, "my-change")
	if err == nil {
		t.Fatal("expected error when no runner selected, got nil")
	}
	if !strings.Contains(err.Error(), "no runner") {
		t.Errorf("error = %q, want to contain %q", err.Error(), "no runner")
	}
}

// TestHandleOpsxStatus_NoArgs_NoCurrentChange verifies the usage error when
// no args are given and currentChange is empty.
func TestHandleOpsxStatus_NoArgs_NoCurrentChange(t *testing.T) {
	t.Parallel()

	buf := &bytes.Buffer{}
	r := NewREPL(buf)
	r.currentChange = ""

	err := handleOpsxStatus(context.Background(), r, "")
	if err == nil {
		t.Fatal("expected usage error when no args and no currentChange, got nil")
	}
	if !strings.Contains(err.Error(), "usage") {
		t.Errorf("error = %q, want to contain %q", err.Error(), "usage")
	}
}

// TestHandleOpsxStatus_UsesCurrentChange verifies that when currentChange is
// set, the handler does not return the usage error (it falls through to the
// binary lookup). Cannot run in parallel — mutates env.
func TestHandleOpsxStatus_UsesCurrentChange(t *testing.T) {
	t.Setenv("OPENSPEC_BIN", "")

	if lookupOpenspec() != "" {
		t.Skip("openspec found on PATH — skipping binary-not-found path test")
	}

	buf := &bytes.Buffer{}
	r := NewREPL(buf)
	r.currentChange = "my-change"

	err := handleOpsxStatus(context.Background(), r, "")
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if strings.Contains(err.Error(), "usage") {
		t.Errorf("got usage error but should have fallen through to binary lookup: %q", err.Error())
	}
	if !strings.Contains(err.Error(), "openspec not found") {
		t.Errorf("error = %q, want to contain %q", err.Error(), "openspec not found")
	}
}
