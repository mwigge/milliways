package review

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"
)

// scratchCompressThreshold is the line count above which Compress is called.
const scratchCompressThreshold = 300

// loopGuardMax is the maximum consecutive ReviewGroup calls permitted without
// an AppendGroup completing. Exceeding this limit forces an empty AppendGroup.
const loopGuardMax = 8

// Config holds the runtime configuration for a ReviewRunner run.
type Config struct {
	RepoPath   string
	Endpoint   string // default "http://localhost:8765/v1"
	ModelAlias string // override; empty = query /v1/models
	OutPath    string // write final report here; empty = return in result only
	Resume     bool   // continue from existing scratch file
	NoMemory   bool   // skip all MemPalace calls
	SocketPath string // daemon socket for MemPalace; default ~/.local/state/milliways/sock
	GitCommit  bool   // auto-commit after each group that produces file edits
	LintAfterEdit bool // run lint/tests after edits and feed failures back
}

// Runner orchestrates the detect→plan→map→write→reduce review cycle.
type Runner struct {
	detector Detector
	planner  Planner
	router   ModelRouter
	cg       CodeGraphClient  // optional; nil disables CodeGraph context injection
	scratch  ScratchWriter
	memory   Memory // nil = no memory operations
	reducer  Reducer
	git      GitIntegration   // nil = no git operations
	linter   Linter           // nil = no lint after edits
	context  ContextTracker   // tracks active file set for architect mode
}

// New wires real dependencies from cfg and returns a Runner ready to run.
func New(cfg Config) (*Runner, error) {
	endpoint := cfg.Endpoint
	if endpoint == "" {
		endpoint = "http://localhost:8765/v1"
	}

	router := NewModelRouter(endpoint)
	cg := NewCodeGraphClient(cfg.SocketPath)

	// Route now to get caps for the planner; re-route during Run.
	client, caps, err := router.RouteWithCG(cfg.ModelAlias, cg)
	if err != nil {
		return nil, fmt.Errorf("new runner route %s: %w", cfg.ModelAlias, err)
	}
	_ = caps // used implicitly through the router during Run

	summarise := func(ctx context.Context, prompt string) (string, error) {
		findings, err := client.ReviewGroup(ctx, Group{Dir: "summary", Lang: Lang{Name: "text"}}, PriorContext{
			Findings: []Finding{{Reason: prompt}},
		})
		if err != nil {
			return "", err
		}
		if len(findings) > 0 {
			return findings[0].Reason, nil
		}
		return "", nil
	}

	var mem Memory
	if cfg.NoMemory {
		mem = NoopMemory{}
	} else {
		mem = NewMemPalaceMemory(cfg.SocketPath)
	}

	var git GitIntegration
	if cfg.GitCommit {
		git = NewGitIntegration(cfg.RepoPath)
	}
	var linter Linter
	if cfg.LintAfterEdit {
		linter = NewLinter()
	}

	return &Runner{
		detector: NewDetector(),
		planner:  NewPlanner(cg),
		router:   router,
		cg:       cg,
		scratch:  NewScratchWriter(cfg.RepoPath),
		memory:   mem,
		reducer:  NewReducer(summarise),
		git:      git,
		linter:   linter,
		context:  NewContextTracker(cfg.RepoPath),
	}, nil
}

// NewWithDeps returns a Runner wired with the provided dependencies.
// Used in tests to inject stubs. Optional fields (git, linter, context)
// default to nil which disables those features.
func NewWithDeps(
	detector Detector,
	planner Planner,
	router ModelRouter,
	scratch ScratchWriter,
	memory Memory,
	reducer Reducer,
) *Runner {
	return &Runner{
		detector: detector,
		planner:  planner,
		router:   router,
		scratch:  scratch,
		memory:   memory,
		reducer:  reducer,
		context:  NewContextTracker("."),
	}
}

// Run executes the full review cycle for the repository at cfg.RepoPath.
// It returns a ReviewResult containing all findings and an executive summary.
func (r *Runner) Run(ctx context.Context, cfg Config) (ReviewResult, error) {
	startedAt := time.Now()

	// Validate repo path.
	if _, err := os.Stat(cfg.RepoPath); err != nil {
		return ReviewResult{}, fmt.Errorf("repo path %s: %w", cfg.RepoPath, err)
	}

	// Route the model first — fail fast if alias is not available.
	client, caps, err := r.router.RouteWithCG(cfg.ModelAlias, r.cg)
	if err != nil {
		return ReviewResult{}, fmt.Errorf("route model %s: %w", cfg.ModelAlias, err)
	}

	// Load prior context from memory (best-effort).
	var prior PriorContext
	if r.memory != nil {
		prior, _ = r.memory.LoadPrior(ctx, cfg.RepoPath) //nolint:errcheck // memory is non-fatal
	}

	var groups []Group

	if cfg.Resume {
		// Resume: iterate over pending groups from the existing scratch file.
		groups = r.pendingGroups()
	} else {
		// Fresh run: detect → plan → init scratch.
		langs, err := r.detector.Detect(cfg.RepoPath)
		if err != nil {
			return ReviewResult{}, fmt.Errorf("detect languages: %w", err)
		}

		groups, err = r.planner.Plan(ctx, cfg.RepoPath, langs, caps)
		if err != nil {
			return ReviewResult{}, fmt.Errorf("plan groups: %w", err)
		}

		if err := r.scratch.Init(cfg.RepoPath, cfg.ModelAlias, langs, groups); err != nil {
			return ReviewResult{}, fmt.Errorf("init scratch: %w", err)
		}
	}

	// Map phase: review each group in order.
	var allFindings []Finding
	consecutiveCalls := 0 // loop guard: consecutive ReviewGroup without AppendGroup

	for _, group := range groups {
		if err := ctx.Err(); err != nil {
			return ReviewResult{}, err
		}

		findings, err := client.ReviewGroup(ctx, group, prior)
		if err != nil {
			if ctx.Err() != nil {
				return ReviewResult{}, ctx.Err()
			}
			// Non-fatal: record empty findings for this group.
			findings = nil
		}

		consecutiveCalls++

		// Loop guard: if too many consecutive calls without AppendGroup completing,
		// force an empty AppendGroup to unblock the scratch file.
		if consecutiveCalls > loopGuardMax {
			slog.Warn("review loop guard triggered: forcing empty append",
				"consecutive_calls", consecutiveCalls,
				"group", group.Dir,
			)
			if appendErr := r.scratch.AppendGroup(Group{}, nil); appendErr != nil {
				_ = appendErr // non-fatal
			}
			consecutiveCalls = 0
		}

		if appendErr := r.scratch.AppendGroup(group, findings); appendErr != nil {
			_ = appendErr // non-fatal: continue reviewing
		} else {
			consecutiveCalls = 0
		}

		if r.memory != nil {
			_ = r.memory.StoreFindings(ctx, cfg.RepoPath, group, findings)
		}

		// Run linter after any edits this group produced (non-fatal).
		if r.linter != nil {
			lintFindings, lintErr := r.linter.Run(ctx, cfg.RepoPath)
			if lintErr != nil {
				slog.Warn("linter error", "group", group.Dir, "error", lintErr)
			} else {
				findings = append(findings, lintFindings...)
			}
		}

		// Auto-commit if git integration is enabled and tree is dirty.
		if r.git != nil && r.git.IsRepo() {
			if dirty, _ := r.git.Dirty(); dirty {
				msg := fmt.Sprintf("review: %s findings in %s", group.Lang.Name, group.Dir)
				if commitErr := r.git.CommitAll(msg); commitErr != nil {
					slog.Warn("git commit failed", "group", group.Dir, "error", commitErr)
				}
			}
		}

		allFindings = append(allFindings, findings...)

		// Scratch compress guard: keep file size manageable.
		if lc, lcErr := r.scratch.LineCount(); lcErr == nil && lc > scratchCompressThreshold {
			if compressErr := r.scratch.Compress(ctx, client); compressErr != nil {
				slog.Warn("scratch compress failed", "error", compressErr)
			}
		}
	}

	// Reduce phase: produce executive summary from the completed scratch file.
	summary, err := r.reducer.Reduce(ctx, client, r.scratch.Path(), prior)
	if err != nil {
		return ReviewResult{}, fmt.Errorf("reduce: %w", err)
	}

	// Log the session to memory (best-effort).
	if r.memory != nil {
		_ = r.memory.LogSession(ctx, cfg.RepoPath, cfg.ModelAlias, summary)
	}

	result := ReviewResult{
		RepoPath:   cfg.RepoPath,
		Model:      cfg.ModelAlias,
		Groups:     groups,
		Findings:   allFindings,
		Summary:    summary,
		StartedAt:  startedAt,
		FinishedAt: time.Now(),
	}

	// Write report to OutPath when configured.
	if cfg.OutPath != "" {
		if writeErr := os.WriteFile(cfg.OutPath, []byte(summary), 0o644); writeErr != nil {
			slog.Warn("write report to OutPath failed", "error", writeErr, "path", cfg.OutPath)
		}
	}

	return result, nil
}

// pendingGroups drains NextPending to build the slice of groups for resume.
func (r *Runner) pendingGroups() []Group {
	var groups []Group
	for {
		g, ok := r.scratch.NextPending()
		if !ok {
			break
		}
		groups = append(groups, g)
	}
	return groups
}
