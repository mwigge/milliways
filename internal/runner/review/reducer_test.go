package review

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// buildFixtureScratch writes a scratch file with N completed group sections.
// Each section has findingsPerGroup bullet findings.
func buildFixtureScratch(t *testing.T, path string, groupCount, findingsPerGroup int) {
	t.Helper()

	var sb strings.Builder
	sb.WriteString("# Review: fixture-repo\n")
	sb.WriteString("Started: 2026-01-01T00:00:00Z\n")
	sb.WriteString("Model: test-model\n")
	sb.WriteString("Stack: Go\n\n")
	sb.WriteString("## Plan\n")
	for i := 0; i < groupCount; i++ {
		sb.WriteString(fmt.Sprintf("- [x] pkg/pkg%02d  (Go, 1 files, impact: 0.50)\n", i))
	}
	sb.WriteString("\n")

	for i := 0; i < groupCount; i++ {
		sb.WriteString(fmt.Sprintf("## [%d/%d] pkg/pkg%02d (Go)\n", i+1, groupCount, i))
		for j := 0; j < findingsPerGroup; j++ {
			sb.WriteString(fmt.Sprintf("- **HIGH** `Sym%d` in `file%02d.go`: reason for issue %d in group %d\n", j, j, j, i))
		}
		sb.WriteString("\n")
	}

	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatalf("buildFixtureScratch WriteFile: %v", err)
	}
}

// stubSummarise captures the prompt and returns a canned summary.
type stubSummarise struct {
	callCount int
	prompts   []string
	returns   string
	err       error
}

func (s *stubSummarise) call(ctx context.Context, prompt string) (string, error) {
	s.callCount++
	s.prompts = append(s.prompts, prompt)
	return s.returns, s.err
}

func TestSummaryReducer_Reduce_CallsSummariseOnce(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "scratch.md")
	buildFixtureScratch(t, path, 3, 4)

	stub := &stubSummarise{returns: "Executive summary text."}
	r := NewReducer(stub.call)

	summary, err := r.Reduce(context.Background(), nil, path, PriorContext{})
	if err != nil {
		t.Fatalf("Reduce: unexpected error: %v", err)
	}
	if stub.callCount != 1 {
		t.Errorf("summarise called %d times, want 1", stub.callCount)
	}
	if summary != "Executive summary text." {
		t.Errorf("summary = %q, want %q", summary, "Executive summary text.")
	}
}

func TestSummaryReducer_Reduce_FindingsTextInPrompt(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "scratch.md")
	buildFixtureScratch(t, path, 3, 4)

	stub := &stubSummarise{returns: "ok"}
	r := NewReducer(stub.call)

	if _, err := r.Reduce(context.Background(), nil, path, PriorContext{}); err != nil {
		t.Fatalf("Reduce: %v", err)
	}

	if len(stub.prompts) == 0 {
		t.Fatal("summarise was never called")
	}
	// All group section headings should appear in the prompt
	if !strings.Contains(stub.prompts[0], "pkg/pkg00") {
		t.Errorf("prompt missing pkg00 findings; prompt snippet: %q", stub.prompts[0][:min(200, len(stub.prompts[0]))])
	}
}

func TestSummaryReducer_Reduce_LargeFile_ReadsBothHalves(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "scratch.md")
	// 20 groups × 15 findings = 300+ lines
	buildFixtureScratch(t, path, 20, 15)

	stub := &stubSummarise{returns: "large summary"}
	r := NewReducer(stub.call)

	if _, err := r.Reduce(context.Background(), nil, path, PriorContext{}); err != nil {
		t.Fatalf("Reduce: %v", err)
	}

	// For large files, both halves must still be fed to summarise as one call
	if stub.callCount != 1 {
		t.Errorf("summarise called %d times, want 1 (both halves concatenated)", stub.callCount)
	}
	if len(stub.prompts[0]) == 0 {
		t.Error("prompt is empty")
	}
}

func TestSummaryReducer_Reduce_PriorContextInjected(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "scratch.md")
	buildFixtureScratch(t, path, 2, 2)

	stub := &stubSummarise{returns: "summary with prior"}
	r := NewReducer(stub.call)

	prior := PriorContext{
		Findings: []Finding{
			{Severity: SeverityHigh, File: "old.go", Symbol: "OldFunc", Reason: "recurring issue from last review"},
		},
	}

	if _, err := r.Reduce(context.Background(), nil, path, prior); err != nil {
		t.Fatalf("Reduce: %v", err)
	}

	if !strings.Contains(stub.prompts[0], "recurring issue from last review") {
		t.Errorf("prompt does not contain prior context reason; prompt = %q", stub.prompts[0][:min(300, len(stub.prompts[0]))])
	}
}

func TestSummaryReducer_Reduce_SummariseError_Propagates(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "scratch.md")
	buildFixtureScratch(t, path, 2, 2)

	sentinel := errors.New("model unreachable")
	stub := &stubSummarise{err: sentinel}
	r := NewReducer(stub.call)

	_, err := r.Reduce(context.Background(), nil, path, PriorContext{})
	if err == nil {
		t.Fatal("Reduce: expected error, got nil")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("Reduce error = %v, want wrapping %v", err, sentinel)
	}
}

func TestSummaryReducer_Reduce_MissingFile_ReturnsError(t *testing.T) {
	t.Parallel()

	r := NewReducer(func(_ context.Context, _ string) (string, error) {
		return "ok", nil
	})

	_, err := r.Reduce(context.Background(), nil, "/nonexistent/scratch.md", PriorContext{})
	if err == nil {
		t.Fatal("Reduce: expected error for missing file, got nil")
	}
}

func TestSummaryReducer_Reduce_AppendsSummarySection(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "scratch.md")
	buildFixtureScratch(t, path, 3, 2)

	stub := &stubSummarise{returns: "Final executive summary content here."}
	r := NewReducer(stub.call)

	if _, err := r.Reduce(context.Background(), nil, path, PriorContext{}); err != nil {
		t.Fatalf("Reduce: %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile after Reduce: %v", err)
	}
	s := string(content)

	if !strings.Contains(s, "# Executive Summary") {
		t.Error("scratch file missing '# Executive Summary' section after Reduce")
	}
	if !strings.Contains(s, "Final executive summary content here.") {
		t.Error("scratch file missing summary text after Reduce")
	}
}
