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
	"image"
	"image/color"
)

// font5x7 is a tiny 5-pixels-wide, 7-pixels-tall bitmap font covering
// ASCII characters relevant to chart labels: digits, lowercase letters
// for "p", "ms", basic operators, and a fallback for anything else
// (the unknown glyph is a solid 5x7 rectangle, immediately visible).
//
// Each glyph is encoded as 7 bytes; bits 0..4 of byte i are pixel
// columns 0..4 of row i (LSB = column 0). Anything outside the ASCII
// range falls back to the unknown glyph.
//
// Why hand-roll a bitmap font? `golang.org/x/image/font/basicfont` is
// the standard answer, but it pulls in `golang.org/x/image` — a new
// module dependency this round explicitly forbids.
var font5x7 = map[rune][7]byte{
	'0': {0b01110, 0b10001, 0b10011, 0b10101, 0b11001, 0b10001, 0b01110},
	'1': {0b00100, 0b01100, 0b00100, 0b00100, 0b00100, 0b00100, 0b01110},
	'2': {0b01110, 0b10001, 0b00001, 0b00010, 0b00100, 0b01000, 0b11111},
	'3': {0b11111, 0b00010, 0b00100, 0b00010, 0b00001, 0b10001, 0b01110},
	'4': {0b00010, 0b00110, 0b01010, 0b10010, 0b11111, 0b00010, 0b00010},
	'5': {0b11111, 0b10000, 0b11110, 0b00001, 0b00001, 0b10001, 0b01110},
	'6': {0b00110, 0b01000, 0b10000, 0b11110, 0b10001, 0b10001, 0b01110},
	'7': {0b11111, 0b00001, 0b00010, 0b00100, 0b01000, 0b01000, 0b01000},
	'8': {0b01110, 0b10001, 0b10001, 0b01110, 0b10001, 0b10001, 0b01110},
	'9': {0b01110, 0b10001, 0b10001, 0b01111, 0b00001, 0b00010, 0b01100},
	'p': {0b00000, 0b00000, 0b11110, 0b10001, 0b11110, 0b10000, 0b10000},
	'm': {0b00000, 0b00000, 0b11010, 0b10101, 0b10101, 0b10101, 0b10101},
	's': {0b00000, 0b00000, 0b01111, 0b10000, 0b01110, 0b00001, 0b11110},
	'%': {0b11000, 0b11001, 0b00010, 0b00100, 0b01000, 0b10011, 0b00011},
	'.': {0b00000, 0b00000, 0b00000, 0b00000, 0b00000, 0b01100, 0b01100},
	'-': {0b00000, 0b00000, 0b00000, 0b11111, 0b00000, 0b00000, 0b00000},
	':': {0b00000, 0b01100, 0b01100, 0b00000, 0b01100, 0b01100, 0b00000},
}

// drawLabel writes label centred in a slot of width slotW, with its
// top-left at (x, y), using col. Glyphs that don't fit are dropped.
//
// Layout: 5px glyph + 1px tracking. So 3 chars = 17px, 2 chars = 11px.
// We centre by computing the laid-out width up front.
func drawLabel(img *image.RGBA, label string, x, y, slotW int, col color.RGBA) {
	if label == "" {
		return
	}
	const glyphW = 5
	const tracking = 1
	per := glyphW + tracking
	maxChars := (slotW + tracking) / per
	if maxChars <= 0 {
		return
	}
	if len(label) > maxChars {
		label = label[:maxChars]
	}
	textW := len(label)*per - tracking
	startX := x + (slotW-textW)/2
	for i, r := range label {
		drawGlyph(img, r, startX+i*per, y, col)
	}
}

// drawGlyph paints a single glyph at (x, y). The glyph reaches down
// 7 rows. Unknown runes render as a solid 5x7 rectangle so their
// presence is obvious during development.
func drawGlyph(img *image.RGBA, r rune, x, y int, col color.RGBA) {
	g, ok := font5x7[r]
	if !ok {
		// Unknown rune: render a 5x7 solid block. Easier to spot in
		// review than a silent gap.
		for row := 0; row < 7; row++ {
			for c := 0; c < 5; c++ {
				img.SetRGBA(x+c, y+row, col)
			}
		}
		return
	}
	for row := 0; row < 7; row++ {
		bits := g[row]
		for c := 0; c < 5; c++ {
			// MSB-first within the 5-bit row: bit 4 = column 0.
			if bits&(1<<(4-c)) != 0 {
				img.SetRGBA(x+c, y+row, col)
			}
		}
	}
}
