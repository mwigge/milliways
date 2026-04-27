package main

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/mwigge/milliways/internal/daemon/charts"
)

// chartSparklineInput is the JSON wire shape for `chart --kind sparkline`.
type chartSparklineInput struct {
	Points []float64 `json:"points"`
}

// chartBarInput is one entry in `chart --kind bars`.
type chartBarInput struct {
	Value float64 `json:"value"`
	Hint  string  `json:"hint"`
	Label string  `json:"label"`
}

// chartBarsInput is the JSON wire shape for `chart --kind bars`.
type chartBarsInput struct {
	Bars []chartBarInput `json:"bars"`
}

// renderChart parses data as JSON, dispatches to the appropriate
// chart renderer in internal/daemon/charts, and writes a single
// kitty-graphics escape to w. Returns a non-nil error for
// unknown kinds or malformed JSON; the caller (main()) maps it to
// a non-zero exit.
//
// Lives here so it can be table-driven tested without touching the
// process lifecycle, and so the CLI surface stays tiny in main.go.
func renderChart(w io.Writer, kind, data string) error {
	theme := charts.DefaultTheme()
	switch kind {
	case "sparkline":
		var in chartSparklineInput
		if err := json.Unmarshal([]byte(data), &in); err != nil {
			return fmt.Errorf("decoding sparkline data: %w", err)
		}
		png := charts.Sparkline(in.Points, theme)
		_, err := io.WriteString(w, charts.KittyEscape(png, 0))
		return err
	case "bars":
		var in chartBarsInput
		if err := json.Unmarshal([]byte(data), &in); err != nil {
			return fmt.Errorf("decoding bars data: %w", err)
		}
		bars := make([]charts.Bar, len(in.Bars))
		for i, b := range in.Bars {
			bars[i] = charts.Bar{Value: b.Value, Hint: b.Hint, Label: b.Label}
		}
		png := charts.Bars(bars, theme)
		_, err := io.WriteString(w, charts.KittyEscape(png, 0))
		return err
	default:
		return fmt.Errorf("unknown chart kind %q (want sparkline|bars)", kind)
	}
}
