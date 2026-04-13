package sommelier

import (
	"strings"
	"testing"
)

func TestSignals_RiskLevel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		signals Signals
		want    string
	}{
		{"all green", Signals{FileStability: "stable", Complexity: 5, Coverage: 90}, "low"},
		{"volatile only", Signals{FileStability: "volatile", Complexity: 5, Coverage: 90}, "medium"},
		{"complex only", Signals{FileStability: "stable", Complexity: 35, Coverage: 90}, "medium"},
		{"low coverage only", Signals{FileStability: "stable", Complexity: 5, Coverage: 30}, "medium"},
		{"volatile + complex", Signals{FileStability: "volatile", Complexity: 35, Coverage: 90}, "high"},
		{"volatile + low coverage", Signals{FileStability: "volatile", Complexity: 5, Coverage: 30}, "high"},
		{"all red", Signals{FileStability: "volatile", Complexity: 35, Coverage: 20, FileAuthors: 5}, "high"},
		{"unknown coverage ignored", Signals{FileStability: "stable", Complexity: 5, Coverage: -1}, "low"},
		{"empty signals", Signals{Coverage: -1}, "low"},
		{"many authors adds risk", Signals{FileStability: "active", Complexity: 16, Coverage: -1, FileAuthors: 5}, "medium"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.signals.RiskLevel(); got != tt.want {
				t.Errorf("RiskLevel() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSignals_Summary(t *testing.T) {
	t.Parallel()

	t.Run("full signals", func(t *testing.T) {
		t.Parallel()
		s := Signals{
			FileStability:  "volatile",
			FileChurn90d:   45,
			Complexity:     34,
			Coverage:       62,
			LearnedKitchen: "claude",
			LearnedRate:    95,
		}
		summary := s.Summary()
		if !strings.Contains(summary, "stability=volatile") {
			t.Errorf("expected stability in summary: %q", summary)
		}
		if !strings.Contains(summary, "churn90d=45") {
			t.Errorf("expected churn in summary: %q", summary)
		}
		if !strings.Contains(summary, "complexity=34") {
			t.Errorf("expected complexity in summary: %q", summary)
		}
		if !strings.Contains(summary, "coverage=62%") {
			t.Errorf("expected coverage in summary: %q", summary)
		}
		if !strings.Contains(summary, "learned=claude@95%") {
			t.Errorf("expected learned in summary: %q", summary)
		}
	})

	t.Run("empty signals", func(t *testing.T) {
		t.Parallel()
		s := Signals{Coverage: -1}
		if s.Summary() != "no signals" {
			t.Errorf("expected 'no signals', got %q", s.Summary())
		}
	})

	t.Run("unknown coverage excluded", func(t *testing.T) {
		t.Parallel()
		s := Signals{Coverage: -1, FileStability: "stable"}
		summary := s.Summary()
		if strings.Contains(summary, "coverage") {
			t.Errorf("negative coverage should be excluded: %q", summary)
		}
	})
}
