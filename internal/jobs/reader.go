package jobs

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

var defaultDBPath = expandPath("~/.agent_task_queue.db")

var errReaderUnavailable = errors.New("jobs reader is not initialized")

// Job represents a row from the OpenHands task queue tasks table.
type Job struct {
	ID        string
	Title     string
	Status    string
	CreatedAt string
	UpdatedAt string
	Wing      string
}

// Reader provides read-only access to the OpenHands task queue database.
type Reader struct {
	db *sql.DB
}

// NewReader opens the task queue database in read-only mode.
//
// It returns (nil, nil) when the configured database file does not exist.
func NewReader() (*Reader, error) {
	dbPath := os.Getenv("TASK_QUEUE_DB")
	if dbPath == "" {
		dbPath = defaultDBPath
	}

	return newReader(dbPath)
}

func newReader(dbPath string) (*Reader, error) {
	dbPath = expandPath(dbPath)
	if _, err := os.Stat(dbPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("stat task queue db: %w", err)
	}

	dsn := sqliteReadOnlyDSN(dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open task queue db: %w", err)
	}

	db.SetMaxIdleConns(4)
	db.SetMaxOpenConns(4)

	if err := db.Ping(); err != nil {
		if closeErr := db.Close(); closeErr != nil {
			return nil, fmt.Errorf("ping task queue db: %w (close: %v)", err, closeErr)
		}
		return nil, fmt.Errorf("ping task queue db: %w", err)
	}

	return &Reader{db: db}, nil
}

// List returns jobs ordered by updated_at descending.
//
// When n is greater than zero, the result is limited to n rows.
func (r *Reader) List(n int) ([]Job, error) {
	if r == nil || r.db == nil {
		return nil, errReaderUnavailable
	}

	query := `SELECT id, title, status, created_at, updated_at, COALESCE(wing, '') FROM tasks ORDER BY updated_at DESC`
	var args []any
	if n > 0 {
		query += ` LIMIT ?`
		args = append(args, n)
	}

	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list jobs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	jobs := make([]Job, 0)
	for rows.Next() {
		var job Job
		if err := rows.Scan(&job.ID, &job.Title, &job.Status, &job.CreatedAt, &job.UpdatedAt, &job.Wing); err != nil {
			return nil, fmt.Errorf("scan job: %w", err)
		}
		jobs = append(jobs, job)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate jobs: %w", err)
	}

	return jobs, nil
}

// Close closes the underlying database connection.
func (r *Reader) Close() error {
	if r == nil || r.db == nil {
		return nil
	}
	return r.db.Close()
}

func expandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil && home != "" {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

func sqliteReadOnlyDSN(path string) string {
	values := url.Values{}
	values.Set("mode", "ro")
	values.Add("_pragma", "busy_timeout(100)")
	values.Add("_pragma", "query_only(1)")

	return "file:" + filepath.ToSlash(path) + "?" + values.Encode()
}
