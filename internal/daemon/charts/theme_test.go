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
