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

package charts

import (
	"image/color"
	"testing"
)

// TestDefaultTheme_TokyonightStorm pins the default palette to the
// hex values in crates/milliways-term/milliways/etc/milliways.lua so
// charts visually match the wezterm status bar.
func TestDefaultTheme_TokyonightStorm(t *testing.T) {
	t.Parallel()
	th := DefaultTheme()
	cases := []struct {
		name string
		got  color.RGBA
		want color.RGBA
	}{
		{"ok", th.Ok, color.RGBA{0x85, 0xb9, 0x4e, 0xff}},
		{"warn", th.Warn, color.RGBA{0xe0, 0xaf, 0x68, 0xff}},
		{"err", th.Err, color.RGBA{0xf7, 0x76, 0x8e, 0xff}},
		{"accent", th.Accent, color.RGBA{0x7a, 0xa2, 0xf7, 0xff}},
		{"dim", th.Dim, color.RGBA{0x5c, 0x63, 0x70, 0xff}},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if c.got != c.want {
				t.Errorf("theme.%s = %#v, want %#v", c.name, c.got, c.want)
			}
		})
	}
	// Background and foreground must be opaque (alpha = 0xff) so they
	// composite cleanly under `image/draw.Over`.
	if th.Background.A != 0xff {
		t.Errorf("Background alpha = %d, want 0xff", th.Background.A)
	}
	if th.Foreground.A != 0xff {
		t.Errorf("Foreground alpha = %d, want 0xff", th.Foreground.A)
	}
}
