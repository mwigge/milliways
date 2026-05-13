package main

import (
	"strings"
	"testing"
)

func TestDeckSwitchControlPoller(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	poll := newDeckSwitchControlPoller()
	if line, ok := poll(); ok || line != "" {
		t.Fatalf("initial poll = (%q,%t), want empty", line, ok)
	}
	if err := writeDeckSwitchControl("minimax"); err != nil {
		t.Fatalf("write control: %v", err)
	}
	line, ok := poll()
	if !ok || line != "/switch minimax" {
		t.Fatalf("poll = (%q,%t), want /switch minimax", line, ok)
	}
	if line, ok := poll(); ok || line != "" {
		t.Fatalf("duplicate poll = (%q,%t), want empty", line, ok)
	}
}

func TestDeckSwitchControlIgnoresInvalidAgent(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	if err := writeDeckSwitchControl("not-a-client"); err != nil {
		t.Fatalf("write control: %v", err)
	}
	line, ok := newDeckSwitchControlPoller()()
	if ok || strings.TrimSpace(line) != "" {
		t.Fatalf("invalid poll = (%q,%t), want empty", line, ok)
	}
}

func TestDeckSwitchControlPollerIgnoresStaleControlFile(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	if err := writeDeckSwitchControl("minimax"); err != nil {
		t.Fatalf("write stale control: %v", err)
	}
	poll := newDeckSwitchControlPoller()
	if line, ok := poll(); ok || line != "" {
		t.Fatalf("stale poll = (%q,%t), want empty", line, ok)
	}
	if err := writeDeckSwitchControl("codex"); err != nil {
		t.Fatalf("write fresh control: %v", err)
	}
	line, ok := poll()
	if !ok || line != "/switch codex" {
		t.Fatalf("fresh poll = (%q,%t), want /switch codex", line, ok)
	}
}
