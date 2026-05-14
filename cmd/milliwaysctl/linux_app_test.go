package main

import (
	"os"
	"strings"
	"testing"
)

func TestLinuxDeckLayoutInvariants(t *testing.T) {
	raw, err := os.ReadFile("milliways.lua")
	if err != nil {
		t.Fatalf("read milliways.lua: %v", err)
	}
	lua := string(raw)
	for _, want := range []string{
		"direction = 'Left'",
		"size = 0.25",
		"args = { mw_bin, 'attach', '--deck', '--right-pane', main_pane_id }",
		"direction = 'Bottom'",
		"args = { mwctl_bin, 'observe-render' }",
		"apply_startup_window_state(window)",
		":maximize()",
		"user-var-changed",
		"milliways_exit",
		"CloseCurrentTab { confirm = false }",
		"uri = tostring(uri)",
	} {
		if !strings.Contains(lua, want) {
			t.Fatalf("milliways.lua missing invariant %q", want)
		}
	}
	for _, blocked := range []string{
		"toggle_fullscreen",
		"MILLIWAYS_NO_FULLSCREEN",
	} {
		if strings.Contains(lua, blocked) {
			t.Fatalf("milliways.lua uses blocking fullscreen invariant %q", blocked)
		}
	}
	if strings.Contains(lua, "MILLIWAYS_WEZTERM_CLI") {
		t.Fatalf("milliways.lua still depends on MILLIWAYS_WEZTERM_CLI")
	}
}

func TestLinuxSecurityChromeInvariants(t *testing.T) {
	raw, err := os.ReadFile("milliways.lua")
	if err != nil {
		t.Fatalf("read milliways.lua: %v", err)
	}
	lua := string(raw)
	for _, want := range []string{
		"local function security_badge(sec)",
		"SEC OK",
		"SEC WARN",
		"SEC BLOCK",
		"window:toast_notification('MilliWays security'",
		"last_security_banner_key",
	} {
		if !strings.Contains(lua, want) {
			t.Fatalf("milliways.lua missing security chrome invariant %q", want)
		}
	}
}

func TestLinuxDesktopEntryUsesExplicitConfig(t *testing.T) {
	raw, err := os.ReadFile("../../bundle/linux/dev.milliways.MilliWays.desktop")
	if err != nil {
		t.Fatalf("read desktop entry: %v", err)
	}
	desktop := string(raw)
	if !strings.Contains(desktop, "Exec=milliways-term --config-file /usr/share/milliways/wezterm.lua") {
		t.Fatalf("desktop entry does not launch milliways-term with explicit config")
	}
	if !strings.Contains(desktop, "TryExec=milliways-term") {
		t.Fatalf("desktop entry missing TryExec=milliways-term")
	}
}
