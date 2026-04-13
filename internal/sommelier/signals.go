package sommelier

import "fmt"

// Signals holds pantry-derived routing signals for a task.
// Coverage uses -1 as sentinel for "unknown" (zero-value float64 would be 0% which is meaningful).
type Signals struct {
	// GitGraph signals
	FileChurn30d  int
	FileChurn90d  int
	FileStability string // "stable", "active", "volatile", "" if unknown
	FileAuthors   int

	// QualityGraph signals
	Complexity int
	Coverage   float64 // 0-100 percentage, -1 means unknown
	Smells     int

	// Learned routing signal
	LearnedKitchen string  // best kitchen from history, "" if insufficient data
	LearnedRate    float64 // success rate percentage
}

// NewSignals creates signals with Coverage defaulting to -1 (unknown).
func NewSignals() *Signals {
	return &Signals{Coverage: -1}
}

// RiskLevel computes an aggregate risk level from signals.
// HIGH = volatile + complex + low coverage → route to careful kitchen (claude)
// MEDIUM = one amber signal → keyword routing may be overridden
// LOW = all green → keyword routing is fine
func (s Signals) RiskLevel() string {
	score := 0

	// Churn risk
	switch s.FileStability {
	case "volatile":
		score += 3
	case "active":
		score += 1
	}

	// Complexity risk
	if s.Complexity > 30 {
		score += 3
	} else if s.Complexity > 15 {
		score += 1
	}

	// Coverage risk (only count if known)
	if s.Coverage >= 0 {
		if s.Coverage < 40 {
			score += 3
		} else if s.Coverage < 70 {
			score += 1
		}
	}

	// Multi-author risk (many hands = fragile)
	if s.FileAuthors > 3 {
		score += 1
	}

	switch {
	case score >= 5:
		return "high"
	case score >= 2:
		return "medium"
	default:
		return "low"
	}
}

// Summary returns a human-readable explanation of the signals.
func (s Signals) Summary() string {
	parts := []string{}

	if s.FileStability != "" {
		parts = append(parts, "stability="+s.FileStability)
	}
	if s.FileChurn90d > 0 {
		parts = append(parts, fmt.Sprintf("churn90d=%d", s.FileChurn90d))
	}
	if s.Complexity > 0 {
		parts = append(parts, fmt.Sprintf("complexity=%d", s.Complexity))
	}
	if s.Coverage >= 0 {
		parts = append(parts, fmt.Sprintf("coverage=%.0f%%", s.Coverage))
	}
	if s.LearnedKitchen != "" {
		parts = append(parts, fmt.Sprintf("learned=%s@%.0f%%", s.LearnedKitchen, s.LearnedRate))
	}

	if len(parts) == 0 {
		return "no signals"
	}

	result := ""
	for i, p := range parts {
		if i > 0 {
			result += " "
		}
		result += p
	}
	return result
}
