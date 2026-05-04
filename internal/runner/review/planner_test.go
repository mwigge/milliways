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

// --- stub CodeGraphClient ---

type stubCG struct {
	files      []CodeGraphFile
	impact     map[string]float64 // symbol (dir basename) → score
	filesErr   error
	isIndexed  bool
	impactCalls int
}

func (s *stubCG) Files(_ context.Context, _ string) ([]CodeGraphFile, error) {
	if s.filesErr != nil {
		return nil, s.filesErr
	}
	return s.files, nil
}

func (s *stubCG) Impact(_ context.Context, symbol string, _ int) (float64, error) {
	s.impactCalls++
	if score, ok := s.impact[symbol]; ok {
		return score, nil
	}
	return 0.0, nil
}

// IsIndexed satisfies the indexChecker interface so the planner can check
// index readiness on this stub.
func (s *stubCG) IsIndexed(_ context.Context) bool {
	return s.isIndexed
}

// --- tests ---

func TestImpactPlanner_Plan_ThreeDirsNoCG(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	// 3 subdirectories, each with one .go file
	dirs := []string{"alpha", "beta", "gamma"}
	for _, d := range dirs {
		sub := filepath.Join(dir, d)
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatalf("MkdirAll %s: %v", d, err)
		}
		writeGoFile(t, sub, "file.go", 10)
	}

	lang := goLangWithPattern(dir)
	caps := ModelCaps{MaxGroupLines: 500}
	p := NewPlanner(nil)

	groups, err := p.Plan(context.Background(), dir, []Lang{lang}, caps)
	if err != nil {
		t.Fatalf("Plan unexpected error: %v", err)
	}
	if len(groups) != 3 {
		t.Errorf("Plan() = %d groups, want 3", len(groups))
	}
	// All impact scores should be 0.0 when CG is nil
	for _, g := range groups {
		if g.ImpactScore != 0.0 {
			t.Errorf("group %q ImpactScore = %f, want 0.0", g.Dir, g.ImpactScore)
		}
	}
}

func TestImpactPlanner_Plan_CGSortsHighImpactFirst(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dirs := []string{"alpha", "beta", "gamma"}
	scores := map[string]float64{
		"alpha": 0.9,
		"beta":  0.1,
		"gamma": 0.5,
	}
	for _, d := range dirs {
		sub := filepath.Join(dir, d)
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatalf("MkdirAll %s: %v", d, err)
		}
		writeGoFile(t, sub, "file.go", 10)
	}

	lang := goLangWithPattern(dir)
	caps := ModelCaps{MaxGroupLines: 500}
	cg := &stubCG{impact: scores, isIndexed: true}
	p := NewPlanner(cg)

	groups, err := p.Plan(context.Background(), dir, []Lang{lang}, caps)
	if err != nil {
		t.Fatalf("Plan unexpected error: %v", err)
	}
	if len(groups) != 3 {
		t.Fatalf("Plan() = %d groups, want 3", len(groups))
	}
	// First group should have impact 0.9, second 0.5, third 0.1
	wantOrder := []float64{0.9, 0.5, 0.1}
	for i, want := range wantOrder {
		if groups[i].ImpactScore != want {
			t.Errorf("groups[%d].ImpactScore = %f, want %f", i, groups[i].ImpactScore, want)
		}
	}
}

func TestImpactPlanner_Plan_LargeGroupSplit(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	sub := filepath.Join(dir, "bigpkg")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// 3 files × 60 lines = 180 lines total
	for i := range 3 {
		writeGoFile(t, sub, fmt.Sprintf("file%d.go", i), 60)
	}

	lang := goLangWithPattern(dir)
	// Cap at 100 lines — 180 lines should be split into at least 2 groups
	caps := ModelCaps{MaxGroupLines: 100}
	p := NewPlanner(nil)

	groups, err := p.Plan(context.Background(), dir, []Lang{lang}, caps)
	if err != nil {
		t.Fatalf("Plan unexpected error: %v", err)
	}
	if len(groups) < 2 {
		t.Errorf("Plan() = %d groups, want ≥2 (split expected)", len(groups))
	}
	for _, g := range groups {
		total := groupLineCount(t, g)
		if total > caps.MaxGroupLines {
			t.Errorf("group %q has %d lines, exceeds cap %d", g.Dir, total, caps.MaxGroupLines)
		}
	}
}

func TestImpactPlanner_Plan_EmptyRepo(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	// go.mod present but no .go files in subdirs
	lang := goLangWithPattern(dir)
	caps := ModelCaps{MaxGroupLines: 500}
	p := NewPlanner(nil)

	_, err := p.Plan(context.Background(), dir, []Lang{lang}, caps)
	if !errors.Is(err, ErrNoPlan) {
		t.Errorf("Plan(empty) = %v, want ErrNoPlan", err)
	}
}

func TestImpactPlanner_Plan_CGFilesErrorFallsBackToDirectoryOrder(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dirs := []string{"alpha", "beta"}
	for _, d := range dirs {
		sub := filepath.Join(dir, d)
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatalf("MkdirAll %s: %v", d, err)
		}
		writeGoFile(t, sub, "file.go", 10)
	}

	lang := goLangWithPattern(dir)
	caps := ModelCaps{MaxGroupLines: 500}
	cg := &stubCG{filesErr: errors.New("codegraph unavailable")}
	p := NewPlanner(cg)

	groups, err := p.Plan(context.Background(), dir, []Lang{lang}, caps)
	if err != nil {
		t.Fatalf("Plan with CG error should not fail, got: %v", err)
	}
	if len(groups) != 2 {
		t.Errorf("Plan() = %d groups, want 2", len(groups))
	}
	// All scores should be 0 after fallback
	for _, g := range groups {
		if g.ImpactScore != 0.0 {
			t.Errorf("group %q ImpactScore = %f, want 0.0 after CG fallback", g.Dir, g.ImpactScore)
		}
	}
}

func TestImpactPlanner_Plan_CGNotIndexed_FallsBackToDirectoryOrder(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dirs := []string{"alpha", "beta", "gamma"}
	for _, d := range dirs {
		sub := filepath.Join(dir, d)
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatalf("MkdirAll %s: %v", d, err)
		}
		writeGoFile(t, sub, "file.go", 10)
	}

	lang := goLangWithPattern(dir)
	caps := ModelCaps{MaxGroupLines: 500}
	// isIndexed=false means the planner must not call Impact.
	cg := &stubCG{
		isIndexed: false,
		impact: map[string]float64{
			"alpha": 0.9,
			"beta":  0.1,
			"gamma": 0.5,
		},
	}
	p := NewPlanner(cg)

	groups, err := p.Plan(context.Background(), dir, []Lang{lang}, caps)
	if err != nil {
		t.Fatalf("Plan() unexpected error: %v", err)
	}
	if len(groups) != 3 {
		t.Fatalf("Plan() = %d groups, want 3", len(groups))
	}

	// All scores must be 0 — no impact scoring when CG not indexed.
	for _, g := range groups {
		if g.ImpactScore != 0.0 {
			t.Errorf("group %q ImpactScore = %f, want 0.0 (CG not indexed)", g.Dir, g.ImpactScore)
		}
	}

	// No Impact calls must have been made.
	if cg.impactCalls != 0 {
		t.Errorf("Impact() called %d times, want 0 when CG not indexed", cg.impactCalls)
	}

	// Groups should be in directory (lexicographic) order.
	for i := 1; i < len(groups); i++ {
		if groups[i-1].Dir > groups[i].Dir {
			t.Errorf("groups not in dir order at [%d]=%q > [%d]=%q",
				i-1, groups[i-1].Dir, i, groups[i].Dir)
		}
	}
}

// --- helpers ---

// goLangWithPattern builds a Lang whose FindPattern lists .go files under dir,
// excluding the vendor directory.
func goLangWithPattern(dir string) Lang {
	return Lang{
		Name:        "Go",
		Ext:         []string{".go"},
		FindPattern: fmt.Sprintf(`find %s -type f -name "*.go" -not -path "*/vendor/*"`, dir),
		Excludes:    []string{"vendor/"},
	}
}

// writeGoFile writes a .go file with n lines of valid Go to dir/name.
func writeGoFile(t *testing.T, dir, name string, lines int) {
	t.Helper()
	var sb strings.Builder
	sb.WriteString("package pkg\n\n")
	for i := range lines - 2 {
		sb.WriteString(fmt.Sprintf("// line %d\n", i+1))
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(sb.String()), 0o644); err != nil {
		t.Fatalf("writeGoFile %s/%s: %v", dir, name, err)
	}
}

// groupLineCount returns the total line count of all files in a group.
func groupLineCount(t *testing.T, g Group) int {
	t.Helper()
	total := 0
	for _, f := range g.Files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("groupLineCount ReadFile %s: %v", f, err)
		}
		total += strings.Count(string(data), "\n")
	}
	return total
}
