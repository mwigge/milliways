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

package maitre

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadMode_Default(t *testing.T) {
	t.Parallel()
	mode := ReadMode()
	if mode != ModeCompany && mode != ModePrivate {
		t.Errorf("ReadMode() = %q, want company or private", mode)
	}
}

func TestPathAllowed_CompanyMode(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	companyDir := filepath.Join(home, "work")
	privateDir := filepath.Join(home, "personal")
	// Create dirs so EvalSymlinks resolves consistently (macOS /var → /private/var)
	if err := os.MkdirAll(companyDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.MkdirAll(privateDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	t.Setenv("MILLIWAYS_COMPANY_ROOTS", companyDir)
	t.Setenv("MILLIWAYS_PRIVATE_ROOTS", privateDir)

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{"company path allowed", filepath.Join(companyDir, "project/foo"), false},
		{"private path blocked", filepath.Join(privateDir, "project"), true},
		{"ssh neutral", filepath.Join(home, ".ssh/config"), false},
		{"claude config neutral", filepath.Join(home, ".claude/settings.json"), false},
		{"tmp allowed", "/tmp/foo", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := PathAllowed(tt.path, ModeCompany)
			if tt.wantErr && err == nil {
				t.Errorf("expected error for %s in company mode", tt.path)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error for %s in company mode: %v", tt.path, err)
			}
		})
	}
}

func TestPathAllowed_PrivateMode(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	companyDir := filepath.Join(home, "work")
	privateDir := filepath.Join(home, "personal")
	if err := os.MkdirAll(companyDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.MkdirAll(privateDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	t.Setenv("MILLIWAYS_COMPANY_ROOTS", companyDir)
	t.Setenv("MILLIWAYS_PRIVATE_ROOTS", privateDir)

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{"private path allowed", filepath.Join(privateDir, "project"), false},
		{"company path blocked", filepath.Join(companyDir, "project/foo"), true},
		{"tmp allowed", "/tmp/foo", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := PathAllowed(tt.path, ModePrivate)
			if tt.wantErr && err == nil {
				t.Errorf("expected error for %s in private mode", tt.path)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error for %s in private mode: %v", tt.path, err)
			}
		})
	}
}

func TestPathAllowed_ResolvesSymlinksBeforeCheckingPrefixes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	companyDir := filepath.Join(home, "work")
	privateDir := filepath.Join(home, "personal")
	t.Setenv("MILLIWAYS_COMPANY_ROOTS", companyDir)
	t.Setenv("MILLIWAYS_PRIVATE_ROOTS", privateDir)

	blockedTarget := filepath.Join(companyDir, "blocked-project")
	allowedAlias := filepath.Join(home, ".config", "milliways", "allowed-link")

	if err := os.MkdirAll(blockedTarget, 0o755); err != nil {
		t.Fatalf("mkdir blocked target: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(allowedAlias), 0o755); err != nil {
		t.Fatalf("mkdir allowed alias parent: %v", err)
	}
	if err := os.Symlink(blockedTarget, allowedAlias); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	err := PathAllowed(allowedAlias, ModePrivate)
	if err == nil {
		t.Fatal("expected symlinked blocked path to be rejected")
	}
	if got, want := err.Error(), "path blocked in private mode — switch: mode company"; got != want {
		t.Fatalf("PathAllowed() error = %q, want %q", got, want)
	}
	if strings.Contains(err.Error(), blockedTarget) || strings.Contains(err.Error(), allowedAlias) {
		t.Fatalf("error %q should not leak filesystem paths", err)
	}
}

func TestPathAllowed_DoesNotLeakBlockedPathInError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	companyDir := filepath.Join(home, "work")
	privateDir := filepath.Join(home, "personal")
	t.Setenv("MILLIWAYS_COMPANY_ROOTS", companyDir)
	t.Setenv("MILLIWAYS_PRIVATE_ROOTS", privateDir)

	blockedPath := filepath.Join(privateDir, "milliways-test-blocked")
	if err := os.MkdirAll(blockedPath, 0o755); err != nil {
		t.Fatalf("mkdir blocked path: %v", err)
	}

	err := PathAllowed(blockedPath, ModeCompany)
	if err == nil {
		t.Fatal("expected blocked path to be rejected")
	}
	if got, want := err.Error(), "path blocked in company mode — switch: mode private"; got != want {
		t.Fatalf("PathAllowed() error = %q, want %q", got, want)
	}
	if strings.Contains(err.Error(), blockedPath) {
		t.Fatalf("error %q should not leak filesystem paths", err)
	}
}
