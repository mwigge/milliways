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

package kitchen

import "testing"

func TestIsQuestion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		line string
		want bool
	}{
		{"?MW> Which runner?", true},
		{"?MW> ", true},
		{"?MW>", true},
		{"?MW", false},
		{"!MW> Which runner?", false},
		{"", false},
		{"?MW>more text", true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.line, func(t *testing.T) {
			t.Parallel()
			if got := IsQuestion(tt.line); got != tt.want {
				t.Errorf("IsQuestion(%q) = %v, want %v", tt.line, got, tt.want)
			}
		})
	}
}

func TestIsConfirm(t *testing.T) {
	t.Parallel()

	tests := []struct {
		line string
		want bool
	}{
		{"!MW> Delete 14 files?", true},
		{"!MW> ", true},
		{"!MW>", true},
		{"?MW> Delete?", false},
		{"", false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.line, func(t *testing.T) {
			t.Parallel()
			if got := IsConfirm(tt.line); got != tt.want {
				t.Errorf("IsConfirm(%q) = %v, want %v", tt.line, got, tt.want)
			}
		})
	}
}

func TestStripPrefix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		line string
		want string
	}{
		{"?MW> Which runner?", "Which runner?"},
		{"!MW> Delete files?", "Delete files?"},
		{"some other line", "some other line"},
		{"?MW> ", ""},
		{"", ""},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.line, func(t *testing.T) {
			t.Parallel()
			if got := StripPrefix(tt.line); got != tt.want {
				t.Errorf("StripPrefix(%q) = %q, want %q", tt.line, got, tt.want)
			}
		})
	}
}
