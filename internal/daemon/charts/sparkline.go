package charts

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
)

// sparklineWidth and sparklineHeight are the fixed PNG dimensions for
// every sparkline. They match the cockpit pane's expected glyph size
// (roughly 32 cells wide x 4 cells tall on tokyonight-storm fonts).
const (
	sparklineWidth  = 256
	sparklineHeight = 64
)

// Sparkline renders a 256x64 PNG line plot of points using
// theme.Accent for the line and theme.Background for the fill. If
// points is empty the function still returns a well-formed blank PNG
// so the kitty-graphics escape stays valid (silent failure on the
// caller side is preferable to a malformed escape that locks the
// terminal).
func Sparkline(points []float64, theme Theme) []byte {
	img := image.NewRGBA(image.Rect(0, 0, sparklineWidth, sparklineHeight))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: theme.Background}, image.Point{}, draw.Src)
	if len(points) >= 2 {
		drawSparkPath(img, points, theme.Accent)
	} else if len(points) == 1 {
		// Single-point degenerate case: a horizontal line at midline so
		// the chart still communicates "we have one sample".
		y := sparklineHeight / 2
		for x := 0; x < sparklineWidth; x++ {
			img.SetRGBA(x, y, theme.Accent)
		}
	}
	return encodePNG(img)
}

// drawSparkPath plots a polyline through points across the full
// canvas, scaling y to [pad, height-pad]. Min and max are derived
// from the data; if all points are equal we centre on the midline.
//
// The line is drawn 1px wide using Bresenham's algorithm (no AA —
// AA at 256x64 is barely visible and adds a stdlib `image/x` import
// dependency this package was built to avoid).
func drawSparkPath(img *image.RGBA, points []float64, c color.RGBA) {
	const pad = 4
	innerH := sparklineHeight - 2*pad
	innerW := sparklineWidth - 2*pad

	minV, maxV := points[0], points[0]
	for _, p := range points {
		if p < minV {
			minV = p
		}
		if p > maxV {
			maxV = p
		}
	}
	span := maxV - minV
	flat := span < 1e-9

	// xs[i] is the canvas x for points[i]; nPoints-1 segments span the
	// inner width so the leftmost sample sits at pad and the rightmost
	// at width-pad-1.
	n := len(points)
	xs := make([]int, n)
	ys := make([]int, n)
	for i, v := range points {
		xs[i] = pad + int(math.Round(float64(i)*float64(innerW-1)/float64(n-1)))
		var yNorm float64
		if flat {
			yNorm = 0.5
		} else {
			yNorm = (v - minV) / span
		}
		// Invert: high values plot near the top (small y).
		ys[i] = pad + int(math.Round((1.0-yNorm)*float64(innerH-1)))
	}

	for i := 1; i < n; i++ {
		drawLine(img, xs[i-1], ys[i-1], xs[i], ys[i], c)
	}
}

// drawLine plots a 1px Bresenham line on img from (x0,y0) to (x1,y1).
// Out-of-bounds pixels are silently clipped by the SetRGBA call.
func drawLine(img *image.RGBA, x0, y0, x1, y1 int, c color.RGBA) {
	dx := x1 - x0
	if dx < 0 {
		dx = -dx
	}
	dy := y1 - y0
	if dy < 0 {
		dy = -dy
	}
	sx := 1
	if x0 > x1 {
		sx = -1
	}
	sy := 1
	if y0 > y1 {
		sy = -1
	}
	err := dx - dy
	for {
		img.SetRGBA(x0, y0, c)
		if x0 == x1 && y0 == y1 {
			return
		}
		e2 := 2 * err
		if e2 > -dy {
			err -= dy
			x0 += sx
		}
		if e2 < dx {
			err += dx
			y0 += sy
		}
	}
}

// encodePNG marshals img to the PNG wire format. Errors here are
// effectively impossible (writing to a bytes.Buffer cannot fail), so
// we discard the error rather than propagate it through every chart
// caller — a corrupt PNG would surface as a decode failure on the
// caller side.
func encodePNG(img image.Image) []byte {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		// Unreachable: bytes.Buffer.Write never returns a non-nil error
		// and png.Encode only fails on writer errors. Keep a fallback
		// so the caller still gets a well-formed (though empty) byte
		// slice instead of a panic.
		return nil
	}
	return buf.Bytes()
}
