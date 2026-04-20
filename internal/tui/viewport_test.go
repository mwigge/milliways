package tui

import "testing"

func TestNewViewport(t *testing.T) {
	t.Parallel()

	vp := NewViewport(72, 18)

	if vp.Width != 72 {
		t.Fatalf("Width = %d, want 72", vp.Width)
	}
	if vp.Height != 18 {
		t.Fatalf("Height = %d, want 18", vp.Height)
	}
	if vp.TotalLineCount() != 1 {
		t.Fatalf("TotalLineCount() = %d, want 1", vp.TotalLineCount())
	}
}
