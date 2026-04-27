package charts

import (
	"image"
	"image/color"
	"image/draw"
)

// barsWidth and barsHeight are the fixed PNG dimensions for every
// bars chart.
const (
	barsWidth  = 256
	barsHeight = 96
)

// Bar is one entry in a Bars chart. Hint maps to the theme palette
// (see hintColor) and Label is drawn under the bar when there is
// enough horizontal room.
type Bar struct {
	Value float64
	Hint  string // "ok"|"warn"|"err"|"accent"|"dim"
	Label string
}

// Bars renders a 256x96 PNG with one vertical bar per entry in bars,
// coloured per Bar.Hint via the theme. Labels are drawn below each
// bar when each bar is at least 12px wide; otherwise they are
// dropped to keep the chart legible.
//
// Empty input returns a blank background PNG so the kitty-graphics
// escape remains well-formed and the cockpit pane keeps redrawing.
func Bars(bars []Bar, theme Theme) []byte {
	img := image.NewRGBA(image.Rect(0, 0, barsWidth, barsHeight))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: theme.Background}, image.Point{}, draw.Src)
	if len(bars) == 0 {
		return encodePNG(img)
	}

	const (
		paddingX = 8
		paddingY = 6
		labelH   = 8 // reserved at the bottom for labels
		barGap   = 4
	)
	innerW := barsWidth - 2*paddingX
	innerH := barsHeight - 2*paddingY - labelH
	if innerW <= 0 || innerH <= 0 {
		return encodePNG(img)
	}

	// Compute bar geometry. Width is proportional, with a minimum of
	// 1px so even a 256-bar plot is still drawable (degenerate but
	// valid).
	totalGap := barGap * (len(bars) - 1)
	barW := (innerW - totalGap) / len(bars)
	if barW < 1 {
		barW = 1
	}

	// Find the maximum value (use 1.0 for an all-zero or all-negative
	// dataset so we still draw a visible empty axis).
	maxV := 0.0
	for _, b := range bars {
		if b.Value > maxV {
			maxV = b.Value
		}
	}
	if maxV <= 0 {
		maxV = 1.0
	}

	x := paddingX
	for _, b := range bars {
		c := hintColor(b.Hint, theme)
		// Clamp negative values to 0 — bars chart is for non-negative
		// magnitudes (latency, counts).
		v := b.Value
		if v < 0 {
			v = 0
		}
		h := int((v / maxV) * float64(innerH))
		if h < 1 && b.Value > 0 {
			h = 1 // always show at least 1px for non-zero values
		}
		topY := paddingY + (innerH - h)
		botY := paddingY + innerH
		fillRect(img, x, topY, x+barW, botY, c)

		// Label slot — only render if the bar is wide enough for the
		// label to be readable. The 5x7 ASCII bitmap (see drawLabel)
		// fits ~3 chars in a 16px-wide bar.
		if barW >= 12 && b.Label != "" {
			labelY := paddingY + innerH + 1
			drawLabel(img, b.Label, x, labelY, barW, theme.Foreground)
		}
		x += barW + barGap
	}
	return encodePNG(img)
}

// fillRect paints the half-open rectangle [x0, x1) x [y0, y1) on img.
// Out-of-bounds pixels are silently clipped by the SetRGBA call.
func fillRect(img *image.RGBA, x0, y0, x1, y1 int, c color.RGBA) {
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			img.SetRGBA(x, y, c)
		}
	}
}
