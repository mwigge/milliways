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

package parallel

import (
	"os"
	"strings"
	"testing"
)

func TestProviderColor_KnownProviders(t *testing.T) {
	withoutNoColor(t)

	tests := []struct {
		provider  string
		wantColor string
	}{
		{"claude", "\033[97m"},
		{"codex", "\033[38;5;214m"},
		{"copilot", "\033[38;5;69m"},
		{"gemini", "\033[38;5;208m"},
		{"local", "\033[38;5;160m"},
		{"minimax", "\033[38;5;141m"},
		{"pool", "\033[38;5;117m"},
	}

	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			got := ProviderColor(tt.provider)
			if got == "" {
				t.Errorf("ProviderColor(%q) = %q, want non-empty color code", tt.provider, got)
			}
			if got != tt.wantColor {
				t.Errorf("ProviderColor(%q) = %q, want %q", tt.provider, got, tt.wantColor)
			}
		})
	}
}

func TestProviderColor_UnknownProvider(t *testing.T) {
	got := ProviderColor("unknown-provider")
	if got != "" {
		t.Errorf("ProviderColor(%q) = %q, want empty string", "unknown-provider", got)
	}
}

func TestColorProvider_ContainsProviderName(t *testing.T) {
	got := ColorProvider("claude")
	if !strings.Contains(got, "claude") {
		t.Errorf("ColorProvider(%q) = %q, does not contain provider name", "claude", got)
	}
}

func TestColorProvider_ContainsColorCode(t *testing.T) {
	withoutNoColor(t)

	got := ColorProvider("claude")
	if !strings.Contains(got, "\033[97m") {
		t.Errorf("ColorProvider(%q) = %q, does not contain pearl color code \\033[97m", "claude", got)
	}
}

func TestColorProvider_ContainsReset(t *testing.T) {
	withoutNoColor(t)

	got := ColorProvider("claude")
	if !strings.Contains(got, "\033[0m") {
		t.Errorf("ColorProvider(%q) = %q, does not contain reset code \\033[0m", "claude", got)
	}
}

func TestColorProvider_UnknownReturnsNameUndecorated(t *testing.T) {
	provider := "unknown-xyz"
	got := ColorProvider(provider)
	if got != provider {
		t.Errorf("ColorProvider(%q) = %q, want undecorated name %q", provider, got, provider)
	}
}

func TestColorProvider_RespectsNoColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	if got := ProviderColor("claude"); got != "" {
		t.Fatalf("ProviderColor() with NO_COLOR = %q, want empty", got)
	}
	if got := ColorProvider("claude"); got != "claude" {
		t.Fatalf("ColorProvider() with NO_COLOR = %q, want undecorated provider", got)
	}
}

func TestColorProvider_RespectsTerminalCapability(t *testing.T) {
	oldNoColor, hadNoColor := os.LookupEnv("NO_COLOR")
	oldTerm, hadTerm := os.LookupEnv("TERM")
	oldColorTerm, hadColorTerm := os.LookupEnv("COLORTERM")
	oldTermProgram, hadTermProgram := os.LookupEnv("TERM_PROGRAM")
	t.Cleanup(func() {
		restoreTestEnv("NO_COLOR", oldNoColor, hadNoColor)
		restoreTestEnv("TERM", oldTerm, hadTerm)
		restoreTestEnv("COLORTERM", oldColorTerm, hadColorTerm)
		restoreTestEnv("TERM_PROGRAM", oldTermProgram, hadTermProgram)
	})
	_ = os.Unsetenv("NO_COLOR")
	_ = os.Unsetenv("COLORTERM")
	_ = os.Unsetenv("TERM_PROGRAM")
	_ = os.Setenv("TERM", "dumb")

	if got := ProviderColor("claude"); got != "" {
		t.Fatalf("ProviderColor() with TERM=dumb = %q, want empty", got)
	}
	if got := ColorProvider("claude"); got != "claude" {
		t.Fatalf("ColorProvider() with TERM=dumb = %q, want undecorated provider", got)
	}
}

func restoreTestEnv(key, value string, ok bool) {
	if ok {
		_ = os.Setenv(key, value)
		return
	}
	_ = os.Unsetenv(key)
}

func withoutNoColor(t *testing.T) {
	t.Helper()
	old, ok := os.LookupEnv("NO_COLOR")
	oldTerm, hadTerm := os.LookupEnv("TERM")
	if err := os.Unsetenv("NO_COLOR"); err != nil {
		t.Fatalf("unset NO_COLOR: %v", err)
	}
	if err := os.Setenv("TERM", "xterm-256color"); err != nil {
		t.Fatalf("set TERM: %v", err)
	}
	t.Cleanup(func() {
		if ok {
			_ = os.Setenv("NO_COLOR", old)
		} else {
			_ = os.Unsetenv("NO_COLOR")
		}
		if hadTerm {
			_ = os.Setenv("TERM", oldTerm)
		} else {
			_ = os.Unsetenv("TERM")
		}
	})
}
