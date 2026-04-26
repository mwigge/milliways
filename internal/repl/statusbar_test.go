package repl

import (
	"os"
	"testing"
)

func TestVisualTruncate_Plain(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		s    string
		cols int
		want string
	}{
		{"exact length", "hello", 5, "hello"},
		{"truncate", "hello world", 5, "hello"},
		{"short content", "hi", 10, "hi"},
		{"empty string", "", 10, ""},
		{"zero cols", "hello", 0, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := visualTruncate(tt.s, tt.cols)
			if got != tt.want {
				t.Errorf("visualTruncate(%q, %d) = %q, want %q", tt.s, tt.cols, got, tt.want)
			}
		})
	}
}

func TestVisualTruncate_WithANSI(t *testing.T) {
	t.Parallel()

	// ANSI codes should not count toward visual width.
	// "\x1b[32m" is green, "\x1b[0m" is reset — neither counts as visual chars.
	colored := "\x1b[32mhello\x1b[0m"

	t.Run("ansi_does_not_count", func(t *testing.T) {
		t.Parallel()
		got := visualTruncate(colored, 10)
		if got != colored {
			t.Errorf("visualTruncate with ANSI, cols=10: got %q, want %q", got, colored)
		}
	})

	t.Run("ansi_truncate_at_visual_width", func(t *testing.T) {
		t.Parallel()
		// "hello world" with green on first word — visual width 11, truncate at 5
		s := "\x1b[32mhello\x1b[0m world"
		got := visualTruncate(s, 5)
		// After truncation at visual width 5, we should have the colored "hello" part
		// The exact bytes depend on implementation but visual width must be <= 5.
		visualWidth := len(stripANSI(got))
		if visualWidth > 5 {
			t.Errorf("visualTruncate visual width = %d, want <= 5; got %q", visualWidth, got)
		}
	})
}

func TestVisualTruncate_Shorter(t *testing.T) {
	t.Parallel()

	s := "hi"
	got := visualTruncate(s, 80)
	if got != s {
		t.Errorf("visualTruncate(%q, 80) = %q, want %q", s, got, s)
	}
}

func TestStatusBar_NewStatusBarForTTY_WithPipe_ReturnsError(t *testing.T) {
	t.Parallel()

	// A pipe is not a TTY — newStatusBarForTTY should return an error.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	defer r.Close()
	defer w.Close()

	_, err = newStatusBarForTTY(w)
	if err == nil {
		t.Error("newStatusBarForTTY with pipe: expected error, got nil")
	}
}

func TestStatusBar_SetContent_InactiveNoWrite(t *testing.T) {
	t.Parallel()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	defer r.Close()

	// Create a StatusBar directly with pipe writer — bypasses TTY check.
	sb := &StatusBar{
		tty:    w,
		rows:   24,
		cols:   80,
		active: false,
	}

	// SetContent on an inactive bar must not panic and must not write to tty.
	sb.SetContent("test content")

	// Close write end and check nothing was written.
	w.Close()
	buf := make([]byte, 128)
	n, _ := r.Read(buf)
	if n != 0 {
		t.Errorf("SetContent on inactive StatusBar wrote %d bytes, want 0", n)
	}
}

// stripANSI removes ANSI escape sequences from s (test helper for measuring visual width).
func stripANSI(s string) string {
	return ansiStripper.ReplaceAllString(s, "")
}
