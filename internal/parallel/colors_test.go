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
	"strings"
	"testing"
)

func TestProviderColor_KnownProviders(t *testing.T) {
	t.Parallel()

	tests := []struct {
		provider  string
		wantColor string
	}{
		{"claude", "\033[34m"},
		{"codex", "\033[33m"},
		{"copilot", "\033[36m"},
		{"gemini", "\033[35m"},
		{"local", "\033[32m"},
		{"minimax", "\033[95m"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.provider, func(t *testing.T) {
			t.Parallel()
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
	t.Parallel()

	got := ProviderColor("unknown-provider")
	if got != "" {
		t.Errorf("ProviderColor(%q) = %q, want empty string", "unknown-provider", got)
	}
}

func TestColorProvider_ContainsProviderName(t *testing.T) {
	t.Parallel()

	got := ColorProvider("claude")
	if !strings.Contains(got, "claude") {
		t.Errorf("ColorProvider(%q) = %q, does not contain provider name", "claude", got)
	}
}

func TestColorProvider_ContainsColorCode(t *testing.T) {
	t.Parallel()

	got := ColorProvider("claude")
	if !strings.Contains(got, "\033[34m") {
		t.Errorf("ColorProvider(%q) = %q, does not contain blue color code \\033[34m", "claude", got)
	}
}

func TestColorProvider_ContainsReset(t *testing.T) {
	t.Parallel()

	got := ColorProvider("claude")
	if !strings.Contains(got, "\033[0m") {
		t.Errorf("ColorProvider(%q) = %q, does not contain reset code \\033[0m", "claude", got)
	}
}

func TestColorProvider_UnknownReturnsNameUndecorated(t *testing.T) {
	t.Parallel()

	provider := "unknown-xyz"
	got := ColorProvider(provider)
	if got != provider {
		t.Errorf("ColorProvider(%q) = %q, want undecorated name %q", provider, got, provider)
	}
}
