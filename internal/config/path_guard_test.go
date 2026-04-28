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

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestModeManagerGuardWriteBlocksReadOnlyPath(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	companyDir := filepath.Join(homeDir, "work")
	privateDir := filepath.Join(homeDir, "personal")
	if err := os.MkdirAll(companyDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.MkdirAll(privateDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	t.Setenv("MILLIWAYS_COMPANY_ROOTS", companyDir)
	t.Setenv("MILLIWAYS_PRIVATE_ROOTS", privateDir)

	mgr, err := NewModeManager()
	if err != nil {
		t.Fatalf("NewModeManager() error = %v", err)
	}
	defer func() { _ = mgr.Close() }()

	blocked := filepath.Join(companyDir, "notes.md")
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
	privateDir := filepath.Join(homeDir, "personal")
	companyDir := filepath.Join(homeDir, "work")
	if err := os.MkdirAll(privateDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.MkdirAll(companyDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	t.Setenv("MILLIWAYS_PRIVATE_ROOTS", privateDir)
	t.Setenv("MILLIWAYS_COMPANY_ROOTS", companyDir)

	mgr, err := NewModeManager()
	if err != nil {
		t.Fatalf("NewModeManager() error = %v", err)
	}
	defer func() { _ = mgr.Close() }()

	if err := mgr.Set(string(ModeCompany)); err != nil {
		t.Fatalf("Set(company): %v", err)
	}

	blocked := filepath.Join(privateDir, "milliways", "session.json")
	if err := GuardWritePath(blocked); err == nil {
		t.Fatal("GuardWritePath() error = nil, want error")
	}
}
