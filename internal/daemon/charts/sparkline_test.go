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
	"bytes"
	"image/png"
	"testing"
)

// pngMagic is the eight-byte PNG file signature.
var pngMagic = []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}

// TestSparkline_TableDriven exercises the three meaningful inputs:
// no data (blank PNG so the kitty-graphics escape stays well-formed),
// a single point (degenerate horizontal line), and an ascending series
// (the common case). Pixel-level golden comparisons are flaky across
// Go versions, so we assert structure: PNG magic + decode-clean +
// expected dimensions.
func TestSparkline_TableDriven(t *testing.T) {
	t.Parallel()
	th := DefaultTheme()

	tests := []struct {
		name   string
		points []float64
	}{
		{"empty", nil},
		{"single point", []float64{42}},
		{"ascending", []float64{1, 2, 3, 4, 5}},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			out := Sparkline(tt.points, th)
			if len(out) == 0 {
				t.Fatal("Sparkline returned 0 bytes; want a well-formed PNG")
			}
			if !bytes.HasPrefix(out, pngMagic) {
				t.Fatalf("output does not start with PNG magic: % x", out[:8])
			}
			img, err := png.Decode(bytes.NewReader(out))
			if err != nil {
				t.Fatalf("png.Decode: %v", err)
			}
			b := img.Bounds()
			if b.Dx() != 256 || b.Dy() != 64 {
				t.Errorf("size = %dx%d, want 256x64", b.Dx(), b.Dy())
			}
		})
	}
}
