package sommelier

import (
	"fmt"
	"sort"
	"strings"
)

// Risk and quality thresholds for signal scoring.
const (
	riskThresholdHigh      = 5
	riskThresholdMedium    = 2
	complexityHigh         = 30
	complexityMedium       = 15
	coverageLow            = 40
	coverageMedium         = 70
	authorRiskThreshold    = 3
	editorContextThreshold = 0.3
)

// Signals holds pantry-derived routing signals for a task.
// Coverage uses -1 as sentinel for "unknown" (zero-value float64 would be 0% which is meaningful).
type Signals struct {
	// GitGraph signals
	FileChurn30d  int
	FileChurn90d  int
	FileStability string // "stable", "active", "volatile", "" if unknown
	FileAuthors   int

	// Editor context signals
	LSPErrors    int
	LSPWarnings  int
	InTestFile   bool
	Dirty        bool
	Language     string
	FilesChanged int

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
	if s.Complexity > complexityHigh {
		score += 3
	} else if s.Complexity > complexityMedium {
		score += 1
	}

	// Coverage risk (only count if known)
	if s.Coverage >= 0 {
		if s.Coverage < coverageLow {
			score += 3
		} else if s.Coverage < coverageMedium {
			score += 1
		}
	}

	// Multi-author risk (many hands = fragile)
	if s.FileAuthors > authorRiskThreshold {
		score += 1
	}

	switch {
	case score >= riskThresholdHigh:
		return "high"
	case score >= riskThresholdMedium:
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
	if s.LSPErrors > 0 {
		parts = append(parts, fmt.Sprintf("lsp_errors=%d", s.LSPErrors))
	}
	if s.LSPWarnings > 0 {
		parts = append(parts, fmt.Sprintf("lsp_warnings=%d", s.LSPWarnings))
	}
	if s.InTestFile {
		parts = append(parts, "in_test_file=true")
	}
	if s.Dirty {
		parts = append(parts, "dirty=true")
	}
	if s.Language != "" {
		parts = append(parts, fmt.Sprintf("language=%s", s.Language))
	}
	if s.FilesChanged > 0 {
		parts = append(parts, fmt.Sprintf("files_changed=%d", s.FilesChanged))
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

	return strings.Join(parts, " ")
}

func editorContextBoost(signals *Signals) string {
	boosted, _ := editorContextBoostWithWeights(signals, nil)
	return boosted
}

func editorContextBoostWithWeights(signals *Signals, weightOn map[string]map[string]float64) (string, float64) {
	if signals == nil {
		return "", 0
	}

	scores := baseEditorContextScores(signals)
	for kitchenName, weights := range weightOn {
		for signalKey, delta := range weights {
			if matchesEditorSignal(signalKey, signals) {
				scores[kitchenName] += delta
			}
		}
	}

	return highestScoringKitchen(scores, editorContextThreshold)
}

func baseEditorContextScores(signals *Signals) map[string]float64 {
	scores := map[string]float64{}
	if signals == nil {
		return scores
	}

	language := normalizeLanguage(signals.Language)
	if signals.InTestFile && isTestAwareLanguage(language) {
		scores["opencode"] += 0.4
		scores["aider"] += 0.35
	}
	if signals.Dirty && signals.FilesChanged > 5 {
		scores["claude"] += 0.4
	}
	if language == "sql" {
		scores["goose"] += 0.5
	}
	if signals.LSPErrors > 0 {
		scores["claude"] += 0.5
	}

	return scores
}

func highestScoringKitchen(scores map[string]float64, threshold float64) (string, float64) {
	if len(scores) == 0 {
		return "", 0
	}

	names := make([]string, 0, len(scores))
	for name := range scores {
		names = append(names, name)
	}
	sort.Strings(names)

	bestKitchen := ""
	bestScore := 0.0
	for _, name := range names {
		score := scores[name]
		if score < threshold {
			continue
		}
		if bestKitchen == "" || score > bestScore {
			bestKitchen = name
			bestScore = score
		}
	}

	return bestKitchen, bestScore
}

func matchesEditorSignal(signalKey string, signals *Signals) bool {
	if signals == nil {
		return false
	}

	switch signalKey {
	case "in_test_file":
		return signals.InTestFile
	case "dirty":
		return signals.Dirty
	case "lsp_errors":
		return signals.LSPErrors > 0
	case "lsp_warnings":
		return signals.LSPWarnings > 0
	}

	const languagePrefix = "language_"
	if strings.HasPrefix(signalKey, languagePrefix) {
		return normalizeLanguage(strings.TrimPrefix(signalKey, languagePrefix)) == normalizeLanguage(signals.Language)
	}

	return false
}

func normalizeLanguage(language string) string {
	return strings.ToLower(strings.TrimSpace(language))
}

func isTestAwareLanguage(language string) bool {
	switch language {
	case "go", "python", "ts", "typescript":
		return true
	default:
		return false
	}
}

func signalScores(signals *Signals) map[string]float64 {
	if signals == nil {
		return nil
	}

	scores := map[string]float64{
		"risk_score":       riskScore(*signals),
		"complexity":       float64(signals.Complexity),
		"file_authors":     float64(signals.FileAuthors),
		"file_churn_30d":   float64(signals.FileChurn30d),
		"file_churn_90d":   float64(signals.FileChurn90d),
		"files_changed":    float64(signals.FilesChanged),
		"lsp_errors":       float64(signals.LSPErrors),
		"lsp_warnings":     float64(signals.LSPWarnings),
		"smells":           float64(signals.Smells),
		"learned_rate":     signals.LearnedRate,
		"editor_ctx_boost": editorContextScore(signals),
	}
	if signals.Coverage >= 0 {
		scores["coverage"] = signals.Coverage
	}
	if signals.InTestFile {
		scores["in_test_file"] = 1
	}
	if signals.Dirty {
		scores["dirty"] = 1
	}

	return scores
}

func riskScore(s Signals) float64 {
	score := 0.0

	switch s.FileStability {
	case "volatile":
		score += 3
	case "active":
		score += 1
	}

	if s.Complexity > complexityHigh {
		score += 3
	} else if s.Complexity > complexityMedium {
		score += 1
	}

	if s.Coverage >= 0 {
		if s.Coverage < coverageLow {
			score += 3
		} else if s.Coverage < coverageMedium {
			score += 1
		}
	}

	if s.FileAuthors > authorRiskThreshold {
		score += 1
	}

	return score
}

func editorContextScore(signals *Signals) float64 {
	_, score := editorContextBoostWithWeights(signals, nil)
	return score
}
