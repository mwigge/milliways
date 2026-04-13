package pantry

import (
	"bufio"
	"database/sql"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// GitGraphStore provides access to the mw_gitgraph table.
type GitGraphStore struct {
	db *sql.DB
}

// FileStats holds churn and stability data for a file.
type FileStats struct {
	FilePath   string
	Churn30d   int
	Churn90d   int
	Authors30d int
	LastAuthor string
	Stability  string // "stable", "active", "volatile"
}

// Sync parses git log for a repo and upserts file stats into mw_gitgraph.
func (s *GitGraphStore) Sync(repoPath string) (int, error) {
	stats, err := parseGitLog(repoPath)
	if err != nil {
		return 0, fmt.Errorf("parsing git log: %w", err)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin transaction: %w", err)
	}

	stmt, err := tx.Prepare(`
		INSERT INTO mw_gitgraph (repo, file_path, churn_30d, churn_90d, authors_30d, last_author, last_changed, stability, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(repo, file_path) DO UPDATE SET
			churn_30d = excluded.churn_30d,
			churn_90d = excluded.churn_90d,
			authors_30d = excluded.authors_30d,
			last_author = excluded.last_author,
			last_changed = excluded.last_changed,
			stability = excluded.stability,
			updated_at = excluded.updated_at
	`)
	if err != nil {
		_ = tx.Rollback()
		return 0, fmt.Errorf("prepare upsert: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	now := time.Now().UTC().Format(time.RFC3339)
	count := 0
	for path, fs := range stats {
		stability := classifyStability(fs.Churn90d)
		_, err := stmt.Exec(repoPath, path, fs.Churn30d, fs.Churn90d, len(fs.Authors30d),
			fs.LastAuthor, fs.LastChanged.Format(time.RFC3339), stability, now)
		if err != nil {
			_ = tx.Rollback()
			return 0, fmt.Errorf("upserting %s: %w", path, err)
		}
		count++
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}
	return count, nil
}

// IsHotspot returns file stats for a given path.
func (s *GitGraphStore) IsHotspot(repo, filePath string) (*FileStats, error) {
	var fs FileStats
	err := s.db.QueryRow(`
		SELECT file_path, churn_30d, churn_90d, authors_30d, last_author, stability
		FROM mw_gitgraph
		WHERE repo = ? AND file_path = ?
	`, repo, filePath).Scan(&fs.FilePath, &fs.Churn30d, &fs.Churn90d, &fs.Authors30d, &fs.LastAuthor, &fs.Stability)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("querying gitgraph: %w", err)
	}
	return &fs, nil
}

// classifyStability categorizes file churn.
func classifyStability(churn90d int) string {
	switch {
	case churn90d < 3:
		return "stable"
	case churn90d <= 15:
		return "active"
	default:
		return "volatile"
	}
}

// fileAccum accumulates git log data per file.
type fileAccum struct {
	Churn30d    int
	Churn90d    int
	Authors30d  map[string]bool
	LastAuthor  string
	LastChanged time.Time
}

// parseGitLog runs git log --numstat and aggregates per-file churn.
func parseGitLog(repoPath string) (map[string]*fileAccum, error) {
	now := time.Now()
	since90d := now.AddDate(0, 0, -90).Format("2006-01-02")

	cmd := exec.Command("git", "log", "--numstat", "--format=MILLIWAYS_COMMIT:%aE %aI", "--since="+since90d)
	cmd.Dir = repoPath

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git log: %w", err)
	}

	stats := make(map[string]*fileAccum)
	since30d := now.AddDate(0, 0, -30)

	var currentAuthor string
	var currentDate time.Time

	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		// Author line: "MILLIWAYS_COMMIT:email@example.com 2026-04-01T12:00:00+00:00"
		if strings.HasPrefix(line, "MILLIWAYS_COMMIT:") {
			line = strings.TrimPrefix(line, "MILLIWAYS_COMMIT:")
			parts := strings.SplitN(line, " ", 2)
			if len(parts) == 2 {
				currentAuthor = parts[0]
				parsed, parseErr := time.Parse(time.RFC3339, parts[1])
				if parseErr != nil {
					continue
				}
				currentDate = parsed
			}
			continue
		}

		// Numstat line: "added\tremoved\tfilepath"
		parts := strings.Split(line, "\t")
		if len(parts) != 3 {
			continue
		}

		added, err1 := strconv.Atoi(parts[0])
		removed, err2 := strconv.Atoi(parts[1])
		if err1 != nil || err2 != nil {
			continue // binary file, skip
		}
		filePath := parts[2]
		churn := added + removed

		fs, ok := stats[filePath]
		if !ok {
			fs = &fileAccum{Authors30d: make(map[string]bool)}
			stats[filePath] = fs
		}

		fs.Churn90d += churn
		fs.LastAuthor = currentAuthor
		if fs.LastChanged.IsZero() || currentDate.After(fs.LastChanged) {
			fs.LastChanged = currentDate
		}

		if currentDate.After(since30d) {
			fs.Churn30d += churn
			fs.Authors30d[currentAuthor] = true
		}
	}

	return stats, scanner.Err()
}
