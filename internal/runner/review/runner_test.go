package review

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// --- stub implementations for all interfaces ---

type stubDetector struct {
	langs []Lang
	err   error
}

func (s *stubDetector) Detect(_ string) ([]Lang, error) {
	return s.langs, s.err
}

type stubPlanner struct {
	groups []Group
	err    error
}

func (s *stubPlanner) Plan(_ context.Context, _ string, _ []Lang, _ ModelCaps) ([]Group, error) {
	return s.groups, s.err
}

type stubRouter struct {
	client GroupClient
	caps   ModelCaps
	err    error
}

func (s *stubRouter) Route(_ string) (GroupClient, ModelCaps, error) {
	return s.client, s.caps, s.err
}

func (s *stubRouter) RouteWithCG(_ string, _ CodeGraphClient) (GroupClient, ModelCaps, error) {
	return s.client, s.caps, s.err
}

type stubGroupClient struct {
	findings []Finding
	err      error
}

func (s *stubGroupClient) ReviewGroup(_ context.Context, _ Group, _ PriorContext) ([]Finding, error) {
	return s.findings, s.err
}

type stubScratch struct {
	pendingGroups []Group
	pendingIdx    int
	initCalled    bool
	appended      []appendCall
	lineCountVal  int
	path          string
}

type appendCall struct {
	group    Group
	findings []Finding
}

func (s *stubScratch) Init(_ string, _ string, _ []Lang, _ []Group) error {
	s.initCalled = true
	return nil
}

func (s *stubScratch) AppendGroup(group Group, findings []Finding) error {
	s.appended = append(s.appended, appendCall{group: group, findings: findings})
	return nil
}

func (s *stubScratch) NextPending() (Group, bool) {
	if s.pendingIdx >= len(s.pendingGroups) {
		return Group{}, false
	}
	g := s.pendingGroups[s.pendingIdx]
	s.pendingIdx++
	return g, true
}

func (s *stubScratch) LineCount() (int, error) {
	return s.lineCountVal, nil
}

func (s *stubScratch) Compress(_ context.Context, _ GroupClient) error {
	return nil
}

func (s *stubScratch) Path() string {
	return s.path
}

type stubMemory struct {
	prior            PriorContext
	storeFindingsErr error
	storedCalls      int
	logCalled        bool
}

func (s *stubMemory) LoadPrior(_ context.Context, _ string) (PriorContext, error) {
	return s.prior, nil
}

func (s *stubMemory) StoreFindings(_ context.Context, _ string, _ Group, _ []Finding) error {
	s.storedCalls++
	return s.storeFindingsErr
}

func (s *stubMemory) LogSession(_ context.Context, _, _, _ string) error {
	s.logCalled = true
	return nil
}

type stubReducer struct {
	summary string
	err     error
}

func (s *stubReducer) Reduce(_ context.Context, _ GroupClient, _ string, _ PriorContext) (string, error) {
	return s.summary, s.err
}

// makeTestRepo creates a temporary directory to act as a repo root.
func makeTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	return dir
}

// defaultStubs returns a set of stubs for a successful 3-group run.
func defaultStubs(repoPath string) (
	*stubDetector,
	*stubPlanner,
	*stubRouter,
	*stubGroupClient,
	*stubScratch,
	*stubMemory,
	*stubReducer,
) {
	langs := []Lang{{Name: "Go", Ext: []string{".go"}}}
	groups := []Group{
		{Dir: "pkg/a"},
		{Dir: "pkg/b"},
		{Dir: "pkg/c"},
	}
	client := &stubGroupClient{findings: []Finding{{Severity: SeverityHigh, Reason: "issue"}}}
	scratch := &stubScratch{path: filepath.Join(repoPath, ".milliways-review-scratch.md")}
	return &stubDetector{langs: langs},
		&stubPlanner{groups: groups},
		&stubRouter{client: client, caps: ModelCaps{Alias: "devstral-small"}},
		client,
		scratch,
		&stubMemory{},
		&stubReducer{summary: "2 HIGH findings found"}
}

// --- tests ---

func TestRunner_Run_SuccessfulFullRun(t *testing.T) {
	t.Parallel()

	dir := makeTestRepo(t)
	det, plan, router, _, scratch, mem, reducer := defaultStubs(dir)

	r := NewWithDeps(det, plan, router, scratch, mem, reducer)
	cfg := Config{RepoPath: dir, ModelAlias: "devstral-small"}

	result, err := r.Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run: unexpected error: %v", err)
	}
	if result.Summary == "" {
		t.Error("Summary must not be empty")
	}
	if len(result.Findings) == 0 {
		t.Error("Findings must not be empty")
	}
	if result.FinishedAt.IsZero() {
		t.Error("FinishedAt must be set")
	}
	// All 3 groups should be appended
	if len(scratch.appended) != 3 {
		t.Errorf("AppendGroup calls = %d, want 3", len(scratch.appended))
	}
}

func TestRunner_Run_Resume_SkipsCompletedGroup(t *testing.T) {
	t.Parallel()

	dir := makeTestRepo(t)
	det, plan, router, _, _, mem, reducer := defaultStubs(dir)

	// scratch has 2 pending groups (1 already done)
	scratch := &stubScratch{
		path: filepath.Join(dir, ".milliways-review-scratch.md"),
		pendingGroups: []Group{
			{Dir: "pkg/b"},
			{Dir: "pkg/c"},
		},
	}

	r := NewWithDeps(det, plan, router, scratch, mem, reducer)
	cfg := Config{RepoPath: dir, ModelAlias: "devstral-small", Resume: true}

	result, err := r.Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run: unexpected error: %v", err)
	}
	_ = result

	// Only 2 groups should be reviewed (the pending ones)
	if len(scratch.appended) != 2 {
		t.Errorf("AppendGroup calls = %d, want 2 (resume skipped first)", len(scratch.appended))
	}
	// Init must NOT be called in resume mode
	if scratch.initCalled {
		t.Error("Init must not be called in Resume mode")
	}
}

func TestRunner_Run_ContextCancelledMidLoop(t *testing.T) {
	t.Parallel()

	dir := makeTestRepo(t)

	langs := []Lang{{Name: "Go"}}
	groups := []Group{{Dir: "pkg/a"}, {Dir: "pkg/b"}, {Dir: "pkg/c"}}

	ctx, cancel := context.WithCancel(context.Background())
	callCount := 0
	client := &cancellingGroupClient{
		cancel:    cancel,
		cancelAt:  1, // cancel after first group
		callCount: &callCount,
		findings:  []Finding{{Severity: SeverityLow, Reason: "minor"}},
	}

	scratch := &stubScratch{path: filepath.Join(dir, ".milliways-review-scratch.md")}
	r := NewWithDeps(
		&stubDetector{langs: langs},
		&stubPlanner{groups: groups},
		&stubRouter{client: client, caps: ModelCaps{Alias: "devstral-small"}},
		scratch,
		&stubMemory{},
		&stubReducer{summary: "partial"},
	)
	cfg := Config{RepoPath: dir, ModelAlias: "devstral-small"}

	_, err := r.Run(ctx, cfg)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Run: err = %v, want context.Canceled", err)
	}
}

// cancellingGroupClient cancels the context after cancelAt calls.
type cancellingGroupClient struct {
	cancel    context.CancelFunc
	cancelAt  int
	callCount *int
	findings  []Finding
}

func (c *cancellingGroupClient) ReviewGroup(_ context.Context, _ Group, _ PriorContext) ([]Finding, error) {
	*c.callCount++
	if *c.callCount >= c.cancelAt {
		c.cancel()
	}
	return c.findings, nil
}

func TestRunner_Run_RouterError_ReturnsBeforeRepo(t *testing.T) {
	t.Parallel()

	dir := makeTestRepo(t)
	scratch := &stubScratch{path: filepath.Join(dir, ".scratch")}

	r := NewWithDeps(
		&stubDetector{langs: []Lang{{Name: "Go"}}},
		&stubPlanner{groups: []Group{{Dir: "pkg/a"}}},
		&stubRouter{err: ErrModelNotFound},
		scratch,
		&stubMemory{},
		&stubReducer{summary: ""},
	)
	cfg := Config{RepoPath: dir, ModelAlias: "missing-model"}

	_, err := r.Run(context.Background(), cfg)
	if err == nil {
		t.Fatal("Run: expected error when router fails, got nil")
	}
	// Scratch Init must not have been called
	if scratch.initCalled {
		t.Error("Init must not be called when routing fails")
	}
}

func TestRunner_Run_MemoryStoreFailsContinues(t *testing.T) {
	t.Parallel()

	dir := makeTestRepo(t)
	det, plan, router, _, scratch, _, reducer := defaultStubs(dir)

	mem := &stubMemory{storeFindingsErr: errors.New("palace down")}
	r := NewWithDeps(det, plan, router, scratch, mem, reducer)
	cfg := Config{RepoPath: dir, ModelAlias: "devstral-small"}

	// Must complete successfully despite memory errors
	result, err := r.Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run: memory failure must be non-fatal, got: %v", err)
	}
	if result.Summary == "" {
		t.Error("Summary must be set even when memory fails")
	}
}

func TestRunner_Run_GroupWithNoFindings_AppendCalledEmpty(t *testing.T) {
	t.Parallel()

	dir := makeTestRepo(t)

	langs := []Lang{{Name: "Go"}}
	groups := []Group{{Dir: "pkg/a"}}
	client := &stubGroupClient{findings: nil} // no findings
	scratch := &stubScratch{path: filepath.Join(dir, ".scratch")}

	r := NewWithDeps(
		&stubDetector{langs: langs},
		&stubPlanner{groups: groups},
		&stubRouter{client: client, caps: ModelCaps{Alias: "devstral-small"}},
		scratch,
		&stubMemory{},
		&stubReducer{summary: "clean"},
	)
	cfg := Config{RepoPath: dir, ModelAlias: "devstral-small"}

	result, err := r.Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run: unexpected error: %v", err)
	}
	if len(scratch.appended) != 1 {
		t.Fatalf("AppendGroup calls = %d, want 1", len(scratch.appended))
	}
	if len(scratch.appended[0].findings) != 0 {
		t.Errorf("appended findings = %v, want empty", scratch.appended[0].findings)
	}
	if result.Summary != "clean" {
		t.Errorf("Summary = %q, want clean", result.Summary)
	}
}

func TestRunner_Run_RepoDirNotExist_ReturnsError(t *testing.T) {
	t.Parallel()

	r := NewWithDeps(
		&stubDetector{},
		&stubPlanner{},
		&stubRouter{client: &stubGroupClient{}, caps: ModelCaps{}},
		&stubScratch{},
		&stubMemory{},
		&stubReducer{},
	)
	cfg := Config{RepoPath: "/nonexistent/path/that/does/not/exist/abc123"}

	_, err := r.Run(context.Background(), cfg)
	if err == nil {
		t.Fatal("Run: expected error for non-existent repoPath, got nil")
	}
}

func TestRunner_Run_AllGroupsZeroFindings_SummaryStillGenerated(t *testing.T) {
	t.Parallel()

	dir := makeTestRepo(t)

	groups := []Group{{Dir: "pkg/a"}, {Dir: "pkg/b"}}
	client := &stubGroupClient{findings: nil}
	scratch := &stubScratch{path: filepath.Join(dir, ".scratch")}

	r := NewWithDeps(
		&stubDetector{langs: []Lang{{Name: "Go"}}},
		&stubPlanner{groups: groups},
		&stubRouter{client: client, caps: ModelCaps{Alias: "devstral-small"}},
		scratch,
		&stubMemory{},
		&stubReducer{summary: "no issues found"},
	)
	cfg := Config{RepoPath: dir, ModelAlias: "devstral-small"}

	result, err := r.Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run: unexpected error: %v", err)
	}
	if result.Summary != "no issues found" {
		t.Errorf("Summary = %q, want 'no issues found'", result.Summary)
	}
}

func TestRunner_Run_TimestampsSet(t *testing.T) {
	t.Parallel()

	dir := makeTestRepo(t)
	det, plan, router, _, scratch, mem, reducer := defaultStubs(dir)

	before := time.Now()
	r := NewWithDeps(det, plan, router, scratch, mem, reducer)
	result, err := r.Run(context.Background(), Config{RepoPath: dir, ModelAlias: "devstral-small"})
	after := time.Now()

	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.StartedAt.Before(before) || result.StartedAt.After(after) {
		t.Errorf("StartedAt = %v, want between %v and %v", result.StartedAt, before, after)
	}
	if result.FinishedAt.Before(result.StartedAt) {
		t.Errorf("FinishedAt %v before StartedAt %v", result.FinishedAt, result.StartedAt)
	}
}
