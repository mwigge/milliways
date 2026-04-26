package config

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewModeManagerCreatesNeutralModeFile(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	mgr, err := NewModeManager()
	if err != nil {
		t.Fatalf("NewModeManager() error = %v", err)
	}
	defer func() { _ = mgr.Close() }()

	if got := mgr.Current(); got != string(ModeNeutral) {
		t.Fatalf("Current() = %q, want %q", got, ModeNeutral)
	}

	data, err := os.ReadFile(filepath.Join(homeDir, ".config", "milliways", "mode"))
	if err != nil {
		t.Fatalf("ReadFile(mode): %v", err)
	}
	if got := string(data); got != "neutral\n" {
		t.Fatalf("mode file = %q, want %q", got, "neutral\\n")
	}
}

func TestModeManagerCanWriteByMode(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	mgr, err := NewModeManager()
	if err != nil {
		t.Fatalf("NewModeManager() error = %v", err)
	}
	defer func() { _ = mgr.Close() }()

	projectPath := filepath.Join(homeDir, "dev", "src", "pprojects", "milliways")
	if err := mgr.Set(string(ModeCompany)); err != nil {
		t.Fatalf("Set(company): %v", err)
	}
	if mgr.CanWrite(projectPath) {
		t.Fatalf("CanWrite(%q) = true, want false in company mode", projectPath)
	}

	if err := mgr.Set(string(ModePrivate)); err != nil {
		t.Fatalf("Set(private): %v", err)
	}
	if !mgr.CanWrite(projectPath) {
		t.Fatalf("CanWrite(%q) = false, want true in private mode", projectPath)
	}

	configPath := filepath.Join(homeDir, ".config", "milliways", "config.json")
	if !mgr.CanWrite(configPath) {
		t.Fatalf("CanWrite(%q) = false, want true for config dir", configPath)
	}
	if !mgr.CanRead(projectPath) {
		t.Fatalf("CanRead(%q) = false, want true", projectPath)
	}
}

func TestModeManagerWatchNotifiesOnModeChange(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	mgr, err := NewModeManager()
	if err != nil {
		t.Fatalf("NewModeManager() error = %v", err)
	}
	defer func() { _ = mgr.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	changes := make(chan string, 1)
	mgr.Watch(ctx, func(mode string) {
		select {
		case changes <- mode:
		default:
		}
	})

	if err := mgr.Set(string(ModeCompany)); err != nil {
		t.Fatalf("Set(company): %v", err)
	}

	select {
	case got := <-changes:
		if got != string(ModeCompany) {
			t.Fatalf("watch callback mode = %q, want %q", got, ModeCompany)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for mode watch callback")
	}
}
