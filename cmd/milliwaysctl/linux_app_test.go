package main

import (
	"os"
	"regexp"
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
		"key = '1', mods = 'LEADER'",
		"args = { mwctl_bin, 'open', '--agent', 'minimax' }",
		"key = '2', mods = 'LEADER'",
		"args = { mwctl_bin, 'open', '--agent', 'berget' }",
		"key = '0', mods = 'LEADER'",
		"args = { mwctl_bin, 'open', '--agent', 'pool' }",
		"window_background_opacity = 0.85",
		"direction = 'Left'",
		"size = 0.20",
		"args = { mw_bin, 'attach', '--deck', '--right-pane', main_pane_id }",
		"direction = 'Bottom'",
		"size = 0.50",
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
		"window_background_appearance",
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

func TestLinuxBergetChromeInvariants(t *testing.T) {
	raw, err := os.ReadFile("milliways.lua")
	if err != nil {
		t.Fatalf("read milliways.lua: %v", err)
	}
	lua := string(raw)
	for _, want := range []string{
		"berget  = 'B'",
		"berget  = { accent='#e0af68'",
	} {
		if !strings.Contains(lua, want) {
			t.Fatalf("milliways.lua missing berget invariant %q", want)
		}
	}
}

func TestMilliwaysLuaConfigAssignmentsDoNotUseTrailingCommas(t *testing.T) {
	raw, err := os.ReadFile("milliways.lua")
	if err != nil {
		t.Fatalf("read milliways.lua: %v", err)
	}
	badAssignment := regexp.MustCompile(`(?m)^config\.[A-Za-z0-9_]+\s*=.*,\s*$`)
	if match := badAssignment.FindString(string(raw)); match != "" {
		t.Fatalf("milliways.lua has Lua-invalid trailing comma in config assignment: %q", match)
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
