package pantry

import (
	"database/sql"
	"fmt"
)

// QualityStore provides access to the mw_quality table.
type QualityStore struct {
	db *sql.DB
}

// QualityMetrics holds complexity and coverage data for a file/function.
type QualityMetrics struct {
	FilePath             string
	FunctionName         string
	CyclomaticComplexity int
	CognitiveComplexity  int
	CoveragePct          float64 // -1 if unknown
	SmellCount           int
}

// Upsert inserts or updates quality metrics for a file/function.
func (s *QualityStore) Upsert(repo string, m QualityMetrics) error {
	_, err := s.db.Exec(`
		INSERT INTO mw_quality (repo, file_path, function_name, cyclomatic_complexity, cognitive_complexity, coverage_pct, smell_count, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, datetime('now'))
		ON CONFLICT(repo, file_path, function_name) DO UPDATE SET
			cyclomatic_complexity = excluded.cyclomatic_complexity,
			cognitive_complexity = excluded.cognitive_complexity,
			coverage_pct = excluded.coverage_pct,
			smell_count = excluded.smell_count,
			updated_at = datetime('now')
	`, repo, m.FilePath, m.FunctionName, m.CyclomaticComplexity, m.CognitiveComplexity, m.CoveragePct, m.SmellCount)
	if err != nil {
		return fmt.Errorf("upserting quality metrics: %w", err)
	}
	return nil
}

// FileRisk returns aggregate quality metrics for a file (max complexity across functions).
func (s *QualityStore) FileRisk(repo, filePath string) (*QualityMetrics, error) {
	var m QualityMetrics
	err := s.db.QueryRow(`
		SELECT file_path,
		       COALESCE(MAX(cyclomatic_complexity), 0) as cyclomatic_complexity,
		       COALESCE(MAX(cognitive_complexity), 0) as cognitive_complexity,
		       COALESCE(MIN(CASE WHEN coverage_pct >= 0 THEN coverage_pct ELSE NULL END), -1) as coverage_pct,
		       COALESCE(SUM(smell_count), 0) as smell_count
		FROM mw_quality
		WHERE repo = ? AND file_path = ?
		GROUP BY file_path
	`, repo, filePath).Scan(&m.FilePath, &m.CyclomaticComplexity, &m.CognitiveComplexity, &m.CoveragePct, &m.SmellCount)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("querying file risk: %w", err)
	}
	return &m, nil
}

// ImportCoverage reads a Go coverage profile and upserts coverage percentages per file.
func (s *QualityStore) ImportCoverage(repo string, coverageByFile map[string]float64) (int, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin transaction: %w", err)
	}

	stmt, err := tx.Prepare(`
		INSERT INTO mw_quality (repo, file_path, function_name, coverage_pct, updated_at)
		VALUES (?, ?, '', ?, datetime('now'))
		ON CONFLICT(repo, file_path, function_name) DO UPDATE SET
			coverage_pct = excluded.coverage_pct,
			updated_at = datetime('now')
	`)
	if err != nil {
		_ = tx.Rollback()
		return 0, fmt.Errorf("prepare statement: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	count := 0
	for file, pct := range coverageByFile {
		if _, err := stmt.Exec(repo, file, pct); err != nil {
			_ = tx.Rollback()
			return 0, fmt.Errorf("upserting coverage for %s: %w", file, err)
		}
		count++
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}
	return count, nil
}
