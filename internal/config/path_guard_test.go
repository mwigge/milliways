package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestModeManagerGuardWriteBlocksReadOnlyPath(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	mgr, err := NewModeManager()
	if err != nil {
		t.Fatalf("NewModeManager() error = %v", err)
	}
	defer func() { _ = mgr.Close() }()

	blocked := filepath.Join(homeDir, "dev", "src", "docs_local", "notes.md")
	err = mgr.GuardWrite(blocked)
	if err == nil {
		t.Fatal("GuardWrite() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "mode neutral") {
		t.Fatalf("GuardWrite() error = %q, want mode detail", err)
	}

	allowed := filepath.Join(homeDir, ".config", "milliways", "config.json")
	if err := mgr.GuardWrite(allowed); err != nil {
		t.Fatalf("GuardWrite(%q) error = %v, want nil", allowed, err)
	}
}

func TestGuardWritePathUsesCurrentModeFile(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	mgr, err := NewModeManager()
	if err != nil {
		t.Fatalf("NewModeManager() error = %v", err)
	}
	defer func() { _ = mgr.Close() }()

	if err := mgr.Set(string(ModeCompany)); err != nil {
		t.Fatalf("Set(company): %v", err)
	}

	blocked := filepath.Join(homeDir, "dev", "src", "pprojects", "milliways", "session.json")
	if err := GuardWritePath(blocked); err == nil {
		t.Fatal("GuardWritePath() error = nil, want error")
	}
}
