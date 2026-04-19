package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseTasksMDCountsCoursesAndTasks(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.md")
	content := `## Course SPS-6: OpenSpec panel [1 SP]

- [x] SPS-6.1 Data types
- [x] SPS-6.2 Parser
- [ ] SPS-6.3 Renderer

## Course SPS-7: Diff panel [1 SP]

- [x] SPS-7.1 Collection
- [ ] SPS-7.2 Rendering

## Course SPS-8: Compare panel [1.5 SP]

- [ ] SPS-8.1 Trigger
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	courses, err := parseTasksMD(path)
	if err != nil {
		t.Fatalf("parseTasksMD() error = %v", err)
	}
	if len(courses) != 3 {
		t.Fatalf("len(courses) = %d, want 3", len(courses))
	}
	if got, want := courses[0].Done, 2; got != want {
		t.Fatalf("courses[0].Done = %d, want %d", got, want)
	}
	if got, want := courses[0].Total, 3; got != want {
		t.Fatalf("courses[0].Total = %d, want %d", got, want)
	}
	if got, want := courses[1].Tasks[0].ID, "SPS-7.1"; got != want {
		t.Fatalf("courses[1].Tasks[0].ID = %q, want %q", got, want)
	}
}

func TestRenderOpenSpecPanelEmptyTasksShowsNoTasks(t *testing.T) {
	t.Parallel()

	m := NewModel(nil)
	m.openSpecChanges = []openSpecChange{{Name: "milliways-tui-panels", Done: 4, Total: 9, IsActive: true}}
	m.openSpecExpanded = true

	got := m.renderOpenSpecPanel(40, 10)
	if !strings.Contains(got, "(no tasks)") {
		t.Fatalf("renderOpenSpecPanel() = %q, want contains %q", got, "(no tasks)")
	}
}

func TestRenderOpenSpecPanelCourseShowsHundredPercent(t *testing.T) {
	t.Parallel()

	m := NewModel(nil)
	m.openSpecChanges = []openSpecChange{{Name: "milliways-tui-panels", Done: 9, Total: 9, IsActive: true}}
	m.openSpecCourses = []openSpecCourse{{ID: "SPS-6", Name: "OpenSpec panel", Done: 3, Total: 3}}
	m.openSpecExpanded = true

	// Width 80 gives enough room for the full course name + bar + pct in one line.
	got := m.renderOpenSpecPanel(80, 10)
	// A fully-done course shows a solid progress bar (Unicode █).
	if !strings.Contains(got, "█") {
		t.Fatalf("renderOpenSpecPanel() = %q, want contains █ (full bar)", got)
	}
}

func TestRefreshOpenSpecDataMissingCLIIsGraceful(t *testing.T) {
	// Cannot use t.Parallel() with t.Setenv
	m := NewModel(nil)
	t.Setenv("PATH", t.TempDir())

	if err := m.refreshOpenSpecData(); err != nil {
		t.Fatalf("refreshOpenSpecData() error = %v, want nil", err)
	}
	if got, want := m.openSpecStatusMessage, "(openspec not found)"; got != want {
		t.Fatalf("openSpecStatusMessage = %q, want %q", got, want)
	}
	if len(m.openSpecChanges) != 0 {
		t.Fatalf("len(openSpecChanges) = %d, want 0", len(m.openSpecChanges))
	}
}

func TestRefreshOpenSpecDataParsesCLIOutput(t *testing.T) {
	// Cannot use t.Parallel() with t.Setenv / t.Chdir

	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	changeDir := filepath.Join(dir, "openspec", "changes", "milliways-tui-panels")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(bin) error = %v", err)
	}
	if err := os.MkdirAll(changeDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(change) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(changeDir, "tasks.md"), []byte("## Course SPS-6: OpenSpec panel\n\n- [x] SPS-6.1 data\n- [ ] SPS-6.2 parser\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(tasks) error = %v", err)
	}
	script := "#!/bin/sh\nprintf '%s\n' '{\"changes\":[{\"name\":\"milliways-tui-panels\",\"completedTasks\":4,\"totalTasks\":9,\"status\":\"in-progress\"}]}'\n"
	if err := os.WriteFile(filepath.Join(binDir, "openspec"), []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(openspec) error = %v", err)
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(wd)
	})
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir() error = %v", err)
	}
	t.Setenv("PATH", binDir)

	m := NewModel(nil)
	if err := m.refreshOpenSpecData(); err != nil {
		t.Fatalf("refreshOpenSpecData() error = %v", err)
	}
	if len(m.openSpecChanges) != 1 {
		t.Fatalf("len(openSpecChanges) = %d, want 1", len(m.openSpecChanges))
	}
	if got, want := m.openSpecChanges[0].Done, 4; got != want {
		t.Fatalf("openSpecChanges[0].Done = %d, want %d", got, want)
	}
	if !m.openSpecChanges[0].IsActive {
		t.Fatal("openSpecChanges[0].IsActive = false, want true")
	}
	if len(m.openSpecCourses) != 1 {
		t.Fatalf("len(openSpecCourses) = %d, want 1", len(m.openSpecCourses))
	}
	if got, want := m.openSpecCourses[0].Done, 1; got != want {
		t.Fatalf("openSpecCourses[0].Done = %d, want %d", got, want)
	}
}
