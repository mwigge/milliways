package main

import (
	"bytes"
	"strings"
	"testing"
)

// TestRenderChart_TableDriven covers the chart subcommand's two
// supported kinds. The asserted output is a kitty-graphics escape
// (ESC_G ... ESC\) wrapping a PNG. Bad input surfaces as an error
// rather than a panic — see the malformed cases.
func TestRenderChart_TableDriven(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		kind    string
		data    string
		wantErr bool
	}{
		{
			name: "sparkline ascending",
			kind: "sparkline",
			data: `{"points":[1,2,3,4,5]}`,
		},
		{
			name: "sparkline empty points",
			kind: "sparkline",
			data: `{"points":[]}`,
		},
		{
			name: "bars three percentiles",
			kind: "bars",
			data: `{"bars":[
				{"value":0.4,"hint":"ok","label":"p50"},
				{"value":4.2,"hint":"warn","label":"p95"},
				{"value":13.0,"hint":"err","label":"p99"}
			]}`,
		},
		{
			name: "bars empty",
			kind: "bars",
			data: `{"bars":[]}`,
		},
		{
			name:    "unknown kind",
			kind:    "donut",
			data:    `{}`,
			wantErr: true,
		},
		{
			name:    "malformed json",
			kind:    "sparkline",
			data:    `{"points":`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			err := renderChart(&buf, tt.kind, tt.data)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("renderChart: %v", err)
			}
			out := buf.String()
			if !strings.HasPrefix(out, "\x1b_G") {
				t.Errorf("output missing ESC_G prefix")
			}
			if !strings.HasSuffix(out, "\x1b\\") {
				t.Errorf("output missing ESC\\ suffix")
			}
		})
	}
}
