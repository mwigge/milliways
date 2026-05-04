package review

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// summariseFn is the function type used by SummaryReducer to summarise text.
// It accepts a context and the full prompt string and returns the summary.
type summariseFn func(ctx context.Context, prompt string) (string, error)

// SummaryReducer implements Reducer by reading the completed scratch file and
// calling a summarise function to produce an executive summary.
type SummaryReducer struct {
	summarise summariseFn
}

// NewReducer returns a Reducer backed by fn for text summarisation.
func NewReducer(fn summariseFn) Reducer {
	return &SummaryReducer{summarise: fn}
}

// Reduce reads the scratch file at scratchPath, builds a prompt from all
// completed section contents and any prior context, calls the summarise
// function once, then appends an "# Executive Summary" section to the file
// and returns the summary text.
func (r *SummaryReducer) Reduce(ctx context.Context, _ GroupClient, scratchPath string, prior PriorContext) (string, error) {
	raw, err := os.ReadFile(scratchPath)
	if err != nil {
		return "", fmt.Errorf("read scratch file %s: %w", scratchPath, err)
	}
	content := string(raw)

	prompt := buildReducePrompt(content, prior)

	summary, err := r.summarise(ctx, prompt)
	if err != nil {
		return "", fmt.Errorf("summarise: %w", err)
	}

	// Append the executive summary section.
	updated := content + "\n# Executive Summary\n" + summary + "\n"
	if writeErr := os.WriteFile(scratchPath, []byte(updated), 0o644); writeErr != nil {
		// Writing back is best-effort; the summary is still returned.
		_ = writeErr
	}

	return summary, nil
}

// buildReducePrompt assembles the summarisation prompt from the scratch
// content and any prior context.
func buildReducePrompt(content string, prior PriorContext) string {
	var sb strings.Builder
	sb.WriteString("Synthesise an executive summary of the following code review findings.\n\n")

	if len(prior.Findings) > 0 {
		sb.WriteString("Prior known issues for context:\n")
		for _, f := range prior.Findings {
			sb.WriteString(fmt.Sprintf("- [%s] %s: %s\n", f.Severity, f.Symbol, f.Reason))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("Review findings:\n")
	sb.WriteString(content)
	return sb.String()
}
