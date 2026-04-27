// Package charts renders small PNG chart primitives (sparklines, bars)
// from pure stdlib (`image`, `image/color`, `image/draw`, `image/png`)
// for embedding in cockpit pane frames via the kitty graphics protocol.
//
// The package intentionally avoids any third-party plotting library:
// the cockpit only needs two simple shapes at fixed sizes (256x64 for
// sparklines, 256x96 for bars) and the existing palette must match
// the wezterm status bar (tokyonight-storm), so the cost of a full
// plotter would dwarf the value.
package charts

import "image/color"

// Theme is the semantic palette used by every chart in this package.
// Colours mirror crates/milliways-term/milliways/etc/milliways.lua.
type Theme struct {
	Background color.RGBA
	Foreground color.RGBA
	Ok         color.RGBA
	Warn       color.RGBA
	Err        color.RGBA
	Accent     color.RGBA
	Dim        color.RGBA
}

// DefaultTheme returns the tokyonight-storm-flavoured palette used by
// the wezterm status bar so cockpit charts visually match the chrome.
//
// Decision: a fully opaque dark background (0x1a1b26) keeps PNGs
// composable with terminals that ignore the alpha channel (kitty
// graphics is RGBA but some renderers flatten to RGB).
func DefaultTheme() Theme {
	return Theme{
		Background: color.RGBA{0x1a, 0x1b, 0x26, 0xff},
		Foreground: color.RGBA{0xc0, 0xca, 0xf5, 0xff},
		Ok:         color.RGBA{0x85, 0xb9, 0x4e, 0xff},
		Warn:       color.RGBA{0xe0, 0xaf, 0x68, 0xff},
		Err:        color.RGBA{0xf7, 0x76, 0x8e, 0xff},
		Accent:     color.RGBA{0x7a, 0xa2, 0xf7, 0xff},
		Dim:        color.RGBA{0x5c, 0x63, 0x70, 0xff},
	}
}

// hintColor maps a Bar.Hint string to a Theme entry. Unknown hints
// fall back to Foreground so a typo does not produce an invisible
// (background-coloured) bar.
func hintColor(hint string, th Theme) color.RGBA {
	switch hint {
	case "ok":
		return th.Ok
	case "warn":
		return th.Warn
	case "err":
		return th.Err
	case "accent":
		return th.Accent
	case "dim":
		return th.Dim
	default:
		return th.Foreground
	}
}
