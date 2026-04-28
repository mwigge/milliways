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

// TestBars_TableDriven covers empty, single-bar and mixed-hint inputs.
// As with sparkline_test.go, we test structure (PNG magic + decode-clean
// + 256x96 dimensions) rather than pixels.
func TestBars_TableDriven(t *testing.T) {
	t.Parallel()
	th := DefaultTheme()

	tests := []struct {
		name string
		bars []Bar
	}{
		{"empty", nil},
		{"single ok", []Bar{{Value: 1.0, Hint: "ok", Label: "p50"}}},
		{
			"mixed hints",
			[]Bar{
				{Value: 0.4, Hint: "ok", Label: "p50"},
				{Value: 4.2, Hint: "warn", Label: "p95"},
				{Value: 13.0, Hint: "err", Label: "p99"},
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			out := Bars(tt.bars, th)
			if len(out) == 0 {
				t.Fatal("Bars returned 0 bytes; want a well-formed PNG")
			}
			if !bytes.HasPrefix(out, pngMagic) {
				t.Fatalf("output does not start with PNG magic: % x", out[:8])
			}
			img, err := png.Decode(bytes.NewReader(out))
			if err != nil {
				t.Fatalf("png.Decode: %v", err)
			}
			b := img.Bounds()
			if b.Dx() != 256 || b.Dy() != 96 {
				t.Errorf("size = %dx%d, want 256x96", b.Dx(), b.Dy())
			}
		})
	}
}
