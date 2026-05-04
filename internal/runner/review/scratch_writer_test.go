package review

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- ScratchPathFor ---

func TestScratchPathFor_BareBasename(t *testing.T) {
	t.Parallel()
	got := ScratchPathFor("my-repo")
	if got != "/tmp/review_my-repo.md" {
		t.Errorf("ScratchPathFor(%q) = %q, want %q", "my-repo", got, "/tmp/review_my-repo.md")
	}
}

func TestScratchPathFor_AbsolutePathExtractsBasename(t *testing.T) {
	t.Parallel()
	got := ScratchPathFor("/home/user/dev/my-project")
	if got != "/tmp/review_my-project.md" {
		t.Errorf("ScratchPathFor(%q) = %q, want %q", "/home/user/dev/my-project", got, "/tmp/review_my-project.md")
	}
}

func TestScratchPathFor_SlugifiesSpecialChars(t *testing.T) {
	t.Parallel()
	got := ScratchPathFor("/home/user/dev/My.Cool_Repo!")
	want := "/tmp/review_my-cool-repo-.md"
	if got != want {
		t.Errorf("ScratchPathFor = %q, want %q", got, want)
	}
}

// --- Init ---

func TestFileScratchWriter_Init_WritesPlanHeader(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	sw := newTestScratchWriter(t, dir, "testrepo")

	langs := []Lang{{Name: "Go"}, {Name: "Rust"}}
	groups := []Group{
		{Dir: "pkg/foo", Files: []string{"a.go"}, Lang: Lang{Name: "Go"}, ImpactScore: 0.9},
		{Dir: "pkg/bar", Files: []string{"b.go", "c.go"}, Lang: Lang{Name: "Go"}, ImpactScore: 0.5},
	}

	if err := sw.Init("testrepo", "qwen2.5-coder", langs, groups); err != nil {
		t.Fatalf("Init: unexpected error: %v", err)
	}

	content, err := os.ReadFile(sw.Path())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	s := string(content)

	assertContains(t, s, "# Review: testrepo")
	assertContains(t, s, "Model: qwen2.5-coder")
	assertContains(t, s, "Stack: Go, Rust")
	assertContains(t, s, "## Plan")
	assertContains(t, s, "- [ ] pkg/foo")
	assertContains(t, s, "- [ ] pkg/bar")
}

func TestFileScratchWriter_Init_TwiceSamePath_ReturnsError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	sw := newTestScratchWriter(t, dir, "testrepo")

	langs := []Lang{{Name: "Go"}}
	groups := []Group{{Dir: "pkg/foo", Files: []string{"a.go"}, Lang: Lang{Name: "Go"}}}

	if err := sw.Init("testrepo", "model", langs, groups); err != nil {
		t.Fatalf("first Init: unexpected error: %v", err)
	}
	if err := sw.Init("testrepo", "model", langs, groups); err == nil {
		t.Fatal("second Init on same path: expected error, got nil")
	}
}

// --- AppendGroup ---

func TestFileScratchWriter_AppendGroup_MarksGroupDoneAndAppendsFindings(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	sw := newTestScratchWriter(t, dir, "testrepo")

	groups := []Group{
		{Dir: "pkg/foo", Files: []string{"a.go"}, Lang: Lang{Name: "Go"}, ImpactScore: 0.9},
		{Dir: "pkg/bar", Files: []string{"b.go"}, Lang: Lang{Name: "Go"}, ImpactScore: 0.4},
	}

	if err := sw.Init("testrepo", "model", []Lang{{Name: "Go"}}, groups); err != nil {
		t.Fatalf("Init: %v", err)
	}

	findings := []Finding{
		{Severity: SeverityHigh, File: "a.go", Symbol: "Run", Reason: "goroutine leak"},
		{Severity: SeverityMedium, File: "a.go", Symbol: "Parse", Reason: "unchecked cast"},
	}

	if err := sw.AppendGroup(groups[0], findings); err != nil {
		t.Fatalf("AppendGroup: %v", err)
	}

	content, err := os.ReadFile(sw.Path())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	s := string(content)

	// Plan line updated
	assertContains(t, s, "- [x] pkg/foo")
	// Second group still pending
	assertContains(t, s, "- [ ] pkg/bar")
	// Findings section appended
	assertContains(t, s, "## [1/2] pkg/foo (Go)")
	assertContains(t, s, "**HIGH**")
	assertContains(t, s, "`Run`")
	assertContains(t, s, "goroutine leak")
	assertContains(t, s, "**MEDIUM**")
}

func TestFileScratchWriter_AppendGroup_EmptyFindings_WritesNoIssuesLine(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	sw := newTestScratchWriter(t, dir, "testrepo")

	groups := []Group{
		{Dir: "pkg/clean", Files: []string{"clean.go"}, Lang: Lang{Name: "Go"}},
	}

	if err := sw.Init("testrepo", "model", []Lang{{Name: "Go"}}, groups); err != nil {
		t.Fatalf("Init: %v", err)
	}

	if err := sw.AppendGroup(groups[0], nil); err != nil {
		t.Fatalf("AppendGroup(empty): %v", err)
	}

	content, err := os.ReadFile(sw.Path())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	s := string(content)

	assertContains(t, s, "(no issues found)")
}

// --- NextPending ---

func TestFileScratchWriter_NextPending_BeforeAnyDone_ReturnsFirstGroup(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	sw := newTestScratchWriter(t, dir, "testrepo")

	groups := []Group{
		{Dir: "pkg/alpha", Files: []string{"a.go"}, Lang: Lang{Name: "Go"}},
		{Dir: "pkg/beta", Files: []string{"b.go"}, Lang: Lang{Name: "Go"}},
	}

	if err := sw.Init("testrepo", "model", []Lang{{Name: "Go"}}, groups); err != nil {
		t.Fatalf("Init: %v", err)
	}

	got, ok := sw.NextPending()
	if !ok {
		t.Fatal("NextPending: expected (group, true), got false")
	}
	if got.Dir != "pkg/alpha" {
		t.Errorf("NextPending dir = %q, want %q", got.Dir, "pkg/alpha")
	}
}

func TestFileScratchWriter_NextPending_AfterAllDone_ReturnsFalse(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	sw := newTestScratchWriter(t, dir, "testrepo")

	groups := []Group{
		{Dir: "pkg/alpha", Files: []string{"a.go"}, Lang: Lang{Name: "Go"}},
	}

	if err := sw.Init("testrepo", "model", []Lang{{Name: "Go"}}, groups); err != nil {
		t.Fatalf("Init: %v", err)
	}

	if err := sw.AppendGroup(groups[0], nil); err != nil {
		t.Fatalf("AppendGroup: %v", err)
	}

	_, ok := sw.NextPending()
	if ok {
		t.Fatal("NextPending: expected false after all groups done, got true")
	}
}

// --- LineCount ---

func TestFileScratchWriter_LineCount_MatchesActual(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	sw := newTestScratchWriter(t, dir, "testrepo")

	groups := []Group{
		{Dir: "pkg/foo", Files: []string{"a.go"}, Lang: Lang{Name: "Go"}},
	}

	if err := sw.Init("testrepo", "model", []Lang{{Name: "Go"}}, groups); err != nil {
		t.Fatalf("Init: %v", err)
	}

	content, err := os.ReadFile(sw.Path())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	wantLines := strings.Count(string(content), "\n")

	got, err := sw.LineCount()
	if err != nil {
		t.Fatalf("LineCount: %v", err)
	}
	if got != wantLines {
		t.Errorf("LineCount() = %d, want %d", got, wantLines)
	}
}

// --- Compress ---

// mockGroupClient is a stub GroupClient that returns a fixed summary.
type mockGroupClient struct {
	summary string
	calls   int
}

func (m *mockGroupClient) ReviewGroup(_ context.Context, _ Group, _ PriorContext) ([]Finding, error) {
	m.calls++
	return []Finding{
		{Severity: SeverityHigh, Symbol: "summary", Reason: m.summary},
	}, nil
}

func TestFileScratchWriter_Compress_ReducesLineCount(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	sw := newTestScratchWriter(t, dir, "testrepo")

	// Build enough groups to create >300 lines when appended.
	// 50 groups × 7 findings each = 350+ body lines + plan lines.
	const groupCount = 50
	groups := make([]Group, groupCount)
	for i := range groups {
		groups[i] = Group{
			Dir:   fmt.Sprintf("pkg/pkg%02d", i),
			Files: []string{fmt.Sprintf("f%02d.go", i)},
			Lang:  Lang{Name: "Go"},
		}
	}

	if err := sw.Init("testrepo", "model", []Lang{{Name: "Go"}}, groups); err != nil {
		t.Fatalf("Init: %v", err)
	}

	sevenFindings := []Finding{
		{Severity: SeverityHigh, File: "a.go", Symbol: "FuncA", Reason: "nil pointer dereference in error path causes panic"},
		{Severity: SeverityHigh, File: "b.go", Symbol: "FuncB", Reason: "goroutine leak: done channel never closed on timeout"},
		{Severity: SeverityMedium, File: "c.go", Symbol: "FuncC", Reason: "context not propagated through call chain"},
		{Severity: SeverityMedium, File: "d.go", Symbol: "FuncD", Reason: "sql query built with fmt.Sprintf — use parameterised query"},
		{Severity: SeverityLow, File: "e.go", Symbol: "FuncE", Reason: "exported symbol missing godoc comment"},
		{Severity: SeverityLow, File: "f.go", Symbol: "FuncF", Reason: "redundant type conversion can be removed"},
		{Severity: SeverityLow, File: "g.go", Symbol: "FuncG", Reason: "unnecessary blank identifier assignment"},
	}
	for i, g := range groups {
		if i < 40 { // append findings for first 40 groups to create bulk
			if err := sw.AppendGroup(g, sevenFindings); err != nil {
				t.Fatalf("AppendGroup %d: %v", i, err)
			}
		}
	}

	before, err := sw.LineCount()
	if err != nil {
		t.Fatalf("LineCount before compress: %v", err)
	}
	if before <= 300 {
		t.Skipf("fixture did not reach 300 lines (got %d) — adjust test", before)
	}

	client := &mockGroupClient{summary: "Compressed: multiple issues found across packages."}
	if err := sw.Compress(context.Background(), client); err != nil {
		t.Fatalf("Compress: %v", err)
	}

	after, err := sw.LineCount()
	if err != nil {
		t.Fatalf("LineCount after compress: %v", err)
	}
	if after >= before {
		t.Errorf("LineCount after compress (%d) >= before (%d); expected reduction", after, before)
	}
}

func TestFileScratchWriter_Compress_PlanSectionIntact(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	sw := newTestScratchWriter(t, dir, "testrepo")

	const groupCount = 50
	groups := make([]Group, groupCount)
	for i := range groups {
		groups[i] = Group{
			Dir:   fmt.Sprintf("pkg/pkg%02d", i),
			Files: []string{fmt.Sprintf("f%02d.go", i)},
			Lang:  Lang{Name: "Go"},
		}
	}

	if err := sw.Init("testrepo", "model", []Lang{{Name: "Go"}}, groups); err != nil {
		t.Fatalf("Init: %v", err)
	}

	sevenFindings := []Finding{
		{Severity: SeverityHigh, File: "a.go", Symbol: "Foo", Reason: "nil pointer in error path"},
		{Severity: SeverityHigh, File: "b.go", Symbol: "Bar", Reason: "goroutine leak on timeout"},
		{Severity: SeverityMedium, File: "c.go", Symbol: "Baz", Reason: "context not propagated"},
		{Severity: SeverityMedium, File: "d.go", Symbol: "Qux", Reason: "sql injection via fmt.Sprintf"},
		{Severity: SeverityLow, File: "e.go", Symbol: "Quux", Reason: "missing godoc comment"},
		{Severity: SeverityLow, File: "f.go", Symbol: "Corge", Reason: "redundant type conversion"},
		{Severity: SeverityLow, File: "g.go", Symbol: "Grault", Reason: "unnecessary blank identifier"},
	}
	for i, g := range groups {
		if i < 40 {
			if err := sw.AppendGroup(g, sevenFindings); err != nil {
				t.Fatalf("AppendGroup %d: %v", i, err)
			}
		}
	}

	lc, err := sw.LineCount()
	if err != nil {
		t.Fatalf("LineCount: %v", err)
	}
	if lc <= 300 {
		t.Skipf("fixture did not reach 300 lines (got %d)", lc)
	}

	client := &mockGroupClient{summary: "Short summary paragraph for compress."}
	if err := sw.Compress(context.Background(), client); err != nil {
		t.Fatalf("Compress: %v", err)
	}

	content, err := os.ReadFile(sw.Path())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	s := string(content)

	assertContains(t, s, "## Plan")
	assertContains(t, s, "# Review: testrepo")
	// All 50 plan entries should still be present (some [x], some [ ])
	for _, g := range groups {
		assertContains(t, s, g.Dir)
	}
}

// --- helpers ---

// newTestScratchWriter creates a FileScratchWriter that writes to dir/<repo>.md.
func newTestScratchWriter(t *testing.T, dir, repo string) *FileScratchWriter {
	t.Helper()
	path := filepath.Join(dir, "review_"+repo+".md")
	return &FileScratchWriter{path: path, groups: nil}
}

func assertContains(t *testing.T, s, substr string) {
	t.Helper()
	if !strings.Contains(s, substr) {
		t.Errorf("expected file to contain %q", substr)
	}
}
