package review

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// ArchitectPlan is the output of the architect model — a list of proposed changes.
type ArchitectPlan struct {
	Summary string
	Steps   []PlanStep
}

// PlanStep is one actionable change proposed by the architect.
type PlanStep struct {
	File        string // file to change
	Description string // what to do
	Priority    int    // 1 = highest
}

// ArchitectEditor orchestrates the two-model split: a strong model (Architect)
// proposes a plan, and a focused model (Editor) executes each step.
type ArchitectEditor struct {
	Architect GroupClient // strong model — proposes plan
	Editor    GroupClient // focused model — executes edits
}

// NewArchitectEditor returns a new ArchitectEditor wired with the provided
// GroupClient implementations.
func NewArchitectEditor(architect, editor GroupClient) *ArchitectEditor {
	return &ArchitectEditor{
		Architect: architect,
		Editor:    editor,
	}
}

// Execute runs the two-phase architect/editor cycle.
//
// Phase 1 — Architect: builds a context message from tracker.List(), sends to
// ae.Architect.ReviewGroup with a synthetic group, and parses the response as
// an ArchitectPlan.
//
// Phase 2 — Editor: for each step in the plan (sorted by Priority), calls
// ae.Editor.ReviewGroup. Editor errors are non-fatal — processing continues
// for remaining steps.
//
// Returns combined findings from all editor calls and the architect plan.
func (ae *ArchitectEditor) Execute(ctx context.Context, repoPath, task string, tracker ContextTracker) ([]Finding, ArchitectPlan, error) {
	// Phase 1: architect proposes a plan.
	contextMsg := buildContextMessage(tracker.List())

	architectGroup := Group{
		Dir:   "__architect__",
		Files: tracker.List(),
		Lang:  Lang{Name: "task"},
	}
	architectPrior := PriorContext{
		Findings: []Finding{
			{Reason: task},
		},
	}
	// Prepend the file context to the task in Findings[0].
	if contextMsg != "" {
		architectPrior.Findings[0].Reason = task + "\n\n" + contextMsg
	}

	architectFindings, err := ae.Architect.ReviewGroup(ctx, architectGroup, architectPrior)
	if err != nil {
		return nil, ArchitectPlan{}, fmt.Errorf("architect phase: %w", err)
	}

	// Collect the full plan text from all architect findings.
	var planText strings.Builder
	for _, f := range architectFindings {
		planText.WriteString(f.Reason)
		planText.WriteString("\n")
	}
	plan := parseArchitectPlan(planText.String())

	// Phase 2: editor executes each step.
	// Sort by Priority (1 = highest, lower number = first).
	steps := make([]PlanStep, len(plan.Steps))
	copy(steps, plan.Steps)
	sort.Slice(steps, func(i, j int) bool {
		return steps[i].Priority < steps[j].Priority
	})

	var allFindings []Finding
	for _, step := range steps {
		if err := ctx.Err(); err != nil {
			return allFindings, plan, err
		}

		editorGroup := Group{
			Dir:   step.File,
			Files: []string{filepath.Join(repoPath, step.File)},
			Lang:  Lang{Name: "task"},
		}
		editorPrior := PriorContext{
			Findings: []Finding{
				{Reason: step.Description},
			},
		}

		findings, editorErr := ae.Editor.ReviewGroup(ctx, editorGroup, editorPrior)
		if editorErr != nil {
			// Non-fatal: record an error finding and continue with remaining steps.
			allFindings = append(allFindings, Finding{
				Severity: SeverityHigh,
				File:     step.File,
				Reason:   fmt.Sprintf("editor error: %v", editorErr),
			})
			continue
		}
		allFindings = append(allFindings, findings...)
	}

	return allFindings, plan, nil
}

// buildContextMessage reads the first 30 lines of each tracked file and
// returns a formatted context string for the architect prompt.
func buildContextMessage(files []string) string {
	if len(files) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("Active files:\n")
	for _, f := range files {
		sb.WriteString("\n--- ")
		sb.WriteString(f)
		sb.WriteString(" ---\n")
		lines := readFirstLines(f, 30)
		sb.WriteString(lines)
	}
	return sb.String()
}

// readFirstLines returns up to n lines from the file at path, joined by
// newlines. Returns an empty string on any error.
func readFirstLines(path string, n int) string {
	fh, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer fh.Close() //nolint:errcheck

	var lines []string
	scanner := bufio.NewScanner(fh)
	for scanner.Scan() && len(lines) < n {
		lines = append(lines, scanner.Text())
	}
	return strings.Join(lines, "\n")
}

// knownExts is the set of file extensions that are extracted as file paths
// from plan step lines.
var knownExts = []string{".go", ".rs", ".py", ".ts", ".yaml", ".yml", ".json", ".toml", ".md"}

// stepRe matches numbered plan lines like "1. Edit foo.go: description"
var stepRe = regexp.MustCompile(`^\d+\.\s+(.+)$`)

// parseArchitectPlan parses a numbered list from the model response into an
// ArchitectPlan. Lines matching "^\d+\. " are treated as steps. The first
// non-list paragraph is used as the Summary.
func parseArchitectPlan(content string) ArchitectPlan {
	var plan ArchitectPlan
	var summaryLines []string
	inList := false

	lines := strings.Split(content, "\n")
	priority := 1
	for _, line := range lines {
		line = strings.TrimRight(line, "\r")
		m := stepRe.FindStringSubmatch(line)
		if m == nil {
			if !inList && strings.TrimSpace(line) != "" {
				summaryLines = append(summaryLines, strings.TrimSpace(line))
			}
			continue
		}

		inList = true
		stepText := m[1]
		step := PlanStep{
			Description: stepText,
			Priority:    priority,
		}
		priority++

		// Extract file path: look for a word containing a known extension.
		step.File = extractStepFilePath(stepText)
		plan.Steps = append(plan.Steps, step)
	}

	plan.Summary = strings.Join(summaryLines, " ")
	return plan
}

// extractStepFilePath finds the first word in text that looks like a file
// path (contains a known extension).
func extractStepFilePath(text string) string {
	// Split on whitespace and colons to isolate individual tokens.
	words := regexp.MustCompile(`[\s:]+`).Split(text, -1)
	for _, w := range words {
		for _, ext := range knownExts {
			if strings.HasSuffix(w, ext) {
				return w
			}
		}
	}
	return ""
}
