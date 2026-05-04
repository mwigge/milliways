package review

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// indexChecker is an optional extension of CodeGraphClient. When the client
// implements this interface, the planner calls IsIndexed before attempting
// impact scoring, logging and falling back to directory order when false.
type indexChecker interface {
	IsIndexed(ctx context.Context) bool
}

// ImpactPlanner is a Planner that groups files by immediate parent directory
// and optionally scores each group via CodeGraph impact analysis.
type ImpactPlanner struct {
	CG CodeGraphClient // nil = fallback to directory order with score 0
}

// NewPlanner returns a Planner. When cg is nil, all groups receive
// ImpactScore 0 and are returned in directory order.
func NewPlanner(cg CodeGraphClient) Planner {
	return ImpactPlanner{CG: cg}
}

// Plan collects files for each Lang using the Lang's FindPattern, groups them
// by immediate parent directory, optionally scores groups via CodeGraph, and
// enforces caps.MaxGroupLines by splitting oversized groups.
//
// It returns ErrNoPlan when no reviewable groups are produced.
func (p ImpactPlanner) Plan(ctx context.Context, repoPath string, langs []Lang, caps ModelCaps) ([]Group, error) {
	var groups []Group

	for _, lang := range langs {
		files, err := collectFiles(ctx, lang.FindPattern)
		if err != nil {
			// Shell command failure is not fatal — skip this lang.
			continue
		}

		byDir := groupByDir(files)
		for dir, dirFiles := range byDir {
			groups = append(groups, Group{
				Dir:         dir,
				Files:       dirFiles,
				Lang:        lang,
				ImpactScore: 0.0,
			})
		}
	}

	if len(groups) == 0 {
		return nil, ErrNoPlan
	}

	// Sort groups by directory name for deterministic ordering before scoring.
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].Dir < groups[j].Dir
	})

	// Score groups via CodeGraph when available.
	if p.CG != nil {
		// If the client supports index-readiness checking, verify the index
		// is populated before attempting impact scoring. An empty index means
		// directory order is the better default.
		if ic, ok := p.CG.(indexChecker); ok && !ic.IsIndexed(ctx) {
			slog.Info("codegraph not indexed, using directory order")
		} else {
			groups = p.scoreGroups(ctx, repoPath, groups)
		}
	}

	// Split groups that exceed caps.MaxGroupLines.
	if caps.MaxGroupLines > 0 {
		groups = splitOversizedGroups(groups, caps.MaxGroupLines)
	}

	if len(groups) == 0 {
		return nil, ErrNoPlan
	}
	return groups, nil
}

// collectFiles executes findPattern as a shell command and returns the
// non-empty lines from stdout as a slice of file paths.
func collectFiles(ctx context.Context, findPattern string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", findPattern)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("exec find pattern: %w", err)
	}

	var files []string
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line != "" {
			files = append(files, line)
		}
	}
	return files, nil
}

// groupByDir maps each file to its immediate parent directory and collects all
// files that share a parent into the same group.
func groupByDir(files []string) map[string][]string {
	m := make(map[string][]string)
	for _, f := range files {
		dir := filepath.Dir(f)
		m[dir] = append(m[dir], f)
	}
	return m
}

// scoreGroups calls CodeGraph for each group's directory name and assigns
// ImpactScore. If CG.Files returns an error the groups are left with score 0
// (fallback to directory order).
func (p ImpactPlanner) scoreGroups(ctx context.Context, repoPath string, groups []Group) []Group {
	// Call Files to verify CodeGraph connectivity; fall back on error.
	if _, err := p.CG.Files(ctx, repoPath); err != nil {
		// CodeGraph unavailable — keep directory order, scores remain 0.
		return groups
	}

	for i, g := range groups {
		// Use the last path component as the symbol for impact lookup.
		symbol := filepath.Base(g.Dir)
		score, err := p.CG.Impact(ctx, symbol, 2)
		if err != nil {
			score = 0.0
		}
		groups[i].ImpactScore = score
	}

	// Sort descending by impact score.
	sort.SliceStable(groups, func(i, j int) bool {
		return groups[i].ImpactScore > groups[j].ImpactScore
	})
	return groups
}

// splitOversizedGroups splits any group whose total line count exceeds maxLines
// into sub-groups, each within the cap. Groups with zero files are dropped.
func splitOversizedGroups(groups []Group, maxLines int) []Group {
	result := make([]Group, 0, len(groups))
	for _, g := range groups {
		if len(g.Files) == 0 {
			continue
		}
		sub := splitGroup(g, maxLines)
		result = append(result, sub...)
	}
	return result
}

// splitGroup splits a single group into sub-groups so that each sub-group's
// total line count does not exceed maxLines.
func splitGroup(g Group, maxLines int) []Group {
	var batches []Group
	var current Group
	current.Dir = g.Dir
	current.Lang = g.Lang
	current.ImpactScore = g.ImpactScore
	currentLines := 0

	for _, f := range g.Files {
		n := countFileLines(f)
		if len(current.Files) > 0 && currentLines+n > maxLines {
			batches = append(batches, current)
			current = Group{Dir: g.Dir, Lang: g.Lang, ImpactScore: g.ImpactScore}
			currentLines = 0
		}
		current.Files = append(current.Files, f)
		currentLines += n
	}
	if len(current.Files) > 0 {
		batches = append(batches, current)
	}
	return batches
}

// countFileLines counts the lines in a file by reading it. Returns 0 on error.
func countFileLines(path string) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close() //nolint:errcheck // best-effort close on read-only file

	n := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		n++
	}
	return n
}
