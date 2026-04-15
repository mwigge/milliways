package tui

import "testing"

func TestFilterPalette_Empty(t *testing.T) {
	t.Parallel()
	matches := FilterPalette("")
	if len(matches) != len(paletteItems) {
		t.Errorf("empty query should return all items, got %d", len(matches))
	}
}

func TestFilterPalette_Exact(t *testing.T) {
	t.Parallel()
	matches := FilterPalette("cancel")
	found := false
	for _, m := range matches {
		if m.Command == "cancel" {
			found = true
			break
		}
	}
	if !found {
		t.Error("should match 'cancel' command")
	}
}

func TestFilterPalette_Fuzzy(t *testing.T) {
	t.Parallel()
	matches := FilterPalette("cncl")
	found := false
	for _, m := range matches {
		if m.Command == "cancel" {
			found = true
			break
		}
	}
	if !found {
		t.Error("fuzzy 'cncl' should match 'cancel'")
	}
}

func TestFilterPalette_NoMatch(t *testing.T) {
	t.Parallel()
	matches := FilterPalette("zzzzzzz")
	if len(matches) != 0 {
		t.Errorf("nonsense query should return 0 matches, got %d", len(matches))
	}
}

func TestFuzzyMatch(t *testing.T) {
	t.Parallel()
	tests := []struct {
		s, pattern string
		want       bool
	}{
		{"cancel", "cnl", true},
		{"collapse", "clps", true},
		{"expand", "xyz", false},
		{"", "a", false},
		{"abc", "", false},
	}
	for _, tt := range tests {
		got := fuzzyMatch(tt.s, tt.pattern)
		if got != tt.want {
			t.Errorf("fuzzyMatch(%q, %q) = %v, want %v", tt.s, tt.pattern, got, tt.want)
		}
	}
}

func TestRenderPalette_NonEmpty(t *testing.T) {
	t.Parallel()
	matches := FilterPalette("")
	result := RenderPalette(matches, 0, "", 60)
	if result == "" {
		t.Error("palette render should not be empty")
	}
}

func TestRenderPalette_WithSelection(t *testing.T) {
	t.Parallel()
	matches := FilterPalette("")
	result := RenderPalette(matches, 2, "exp", 60)
	if !containsPlain(result, ">") {
		t.Error("should show selection indicator")
	}
}
