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
	"context"
	"errors"
	"testing"
)

// compile-time interface check
var _ Pane = (*REPLPane)(nil)

func TestREPLPane_ImplementsPane(t *testing.T) {
	t.Parallel()
	// Satisfied by the package-level var above; this test documents the intent.
}

func TestREPLPane_Title_Default(t *testing.T) {
	t.Parallel()
	p := NewREPLPane(&REPL{}, "")
	if p.Title() != "repl" {
		t.Errorf("Title() = %q, want %q", p.Title(), "repl")
	}
}

func TestREPLPane_Title_Custom(t *testing.T) {
	t.Parallel()
	p := NewREPLPane(&REPL{}, "my-pane")
	if p.Title() != "my-pane" {
		t.Errorf("Title() = %q, want %q", p.Title(), "my-pane")
	}
}

func TestShell_NoPane_ReturnsError(t *testing.T) {
	t.Parallel()
	s := NewShell(nil, nil)
	err := s.Run(context.Background())
	if err == nil {
		t.Fatal("Run() on empty shell must return a non-nil error")
	}
}

// mockPane records whether Run was called and returns a fixed error.
type mockPane struct {
	runCalled bool
	runErr    error
	title     string
}

func (m *mockPane) Run(_ context.Context) error {
	m.runCalled = true
	return m.runErr
}

func (m *mockPane) Title() string { return m.title }

func TestShell_RunsDelegateToPane(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("pane ran")
	mock := &mockPane{runErr: sentinel, title: "test"}

	s := NewShell(nil, nil)
	s.AddPane(mock)

	err := s.Run(context.Background())
	if !mock.runCalled {
		t.Fatal("Shell.Run did not call the active pane's Run method")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("Shell.Run returned %v, want %v", err, sentinel)
	}
}
