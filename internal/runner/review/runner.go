package review

import (
	"context"
	"fmt"
	"os"
	"time"
)

// Config holds the runtime configuration for a ReviewRunner run.
type Config struct {
	RepoPath   string
	ModelAlias string
	Resume     bool // if true, resume from the existing scratch file
}

// Runner orchestrates the detect→plan→map→write→reduce review cycle.
type Runner struct {
	detector Detector
	planner  Planner
	router   ModelRouter
	scratch  ScratchWriter
	memory   Memory // nil = no memory operations
	reducer  Reducer
}

// NewWithDeps returns a Runner wired with the provided dependencies.
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
	client, caps, err := r.router.Route(cfg.ModelAlias)
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

		if appendErr := r.scratch.AppendGroup(group, findings); appendErr != nil {
			// Non-fatal: continue reviewing.
			_ = appendErr
		}

		if r.memory != nil {
			// Memory store failures are intentionally non-fatal.
			_ = r.memory.StoreFindings(ctx, cfg.RepoPath, group, findings)
		}

		allFindings = append(allFindings, findings...)
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

	return ReviewResult{
		RepoPath:   cfg.RepoPath,
		Model:      cfg.ModelAlias,
		Groups:     groups,
		Findings:   allFindings,
		Summary:    summary,
		StartedAt:  startedAt,
		FinishedAt: time.Now(),
	}, nil
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
