package repl

import (
	"bytes"
	"strings"
	"testing"
)

// TestPickFromList_NonTTY verifies that pickFromList returns "" immediately
// when the writer/stdin is not a terminal, without crashing.
func TestPickFromList_NonTTY(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		items   []string
		current string
	}{
		{
			name:    "empty list",
			items:   []string{},
			current: "",
		},
		{
			name:    "single item",
			items:   []string{"claude-sonnet-4-6"},
			current: "claude-sonnet-4-6",
		},
		{
			name:    "multiple items no current",
			items:   []string{"model-a", "model-b", "model-c"},
			current: "",
		},
		{
			name:    "multiple items with current",
			items:   []string{"model-a", "model-b", "model-c"},
			current: "model-b",
		},
		{
			name:    "more items than max visible rows",
			items:   strings.Fields("a b c d e f g h i j k l m n o p"),
			current: "h",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			got := pickFromList(&buf, tt.items, tt.current)
			if got != "" {
				t.Errorf("pickFromList on non-tty = %q, want %q", got, "")
			}
		})
	}
}

// TestPickFromList_NilWriter verifies nil writer does not panic on non-tty path.
func TestPickFromList_NilWriter(t *testing.T) {
	t.Parallel()
	// Should not panic; non-tty path returns "" before writing anything.
	got := pickFromList(nil, []string{"a", "b"}, "a")
	if got != "" {
		t.Errorf("pickFromList with nil writer = %q, want %q", got, "")
	}
}
