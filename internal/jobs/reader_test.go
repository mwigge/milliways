package jobs

import (
	"database/sql"
	"path/filepath"
	"sync"
	"testing"

	_ "modernc.org/sqlite"
)

func TestNewReaderMissingFile(t *testing.T) {
	missingPath := filepath.Join(t.TempDir(), "missing.db")

	reader, err := newReader(missingPath)
	if err != nil {
		t.Fatalf("NewReader() error = %v", err)
	}
	if reader != nil {
		t.Fatal("NewReader() reader = non-nil, want nil")
	}
}

func TestReaderList(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "jobs.db")
	seedTaskQueueDB(t, path, []jobFixture{
		{id: "job-1", title: "first", status: "pending", createdAt: "2026-04-18T10:00:00Z", updatedAt: "2026-04-18T10:01:00Z", wing: "alpha"},
		{id: "job-2", title: "second", status: "done", createdAt: "2026-04-18T10:02:00Z", updatedAt: "2026-04-18T10:03:00Z", wing: "beta"},
		{id: "job-3", title: "third", status: "failed", createdAt: "2026-04-18T10:04:00Z", updatedAt: "2026-04-18T10:02:00Z", wing: "gamma"},
	})
	reader, err := newReader(path)
	if err != nil {
		t.Fatalf("NewReader() error = %v", err)
	}
	if reader == nil {
		t.Fatal("NewReader() reader = nil, want non-nil")
	}
	t.Cleanup(func() {
		if err := reader.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	})

	tests := []struct {
		name      string
		limit     int
		wantIDs   []string
		wantCount int
	}{
		{name: "ordered by updated at desc", limit: 0, wantIDs: []string{"job-2", "job-3", "job-1"}, wantCount: 3},
		{name: "limited list", limit: 2, wantIDs: []string{"job-2", "job-3"}, wantCount: 2},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := reader.List(tc.limit)
			if err != nil {
				t.Fatalf("List(%d) error = %v", tc.limit, err)
			}
			if len(got) != tc.wantCount {
				t.Fatalf("List(%d) count = %d, want %d", tc.limit, len(got), tc.wantCount)
			}
			for i, wantID := range tc.wantIDs {
				if got[i].ID != wantID {
					t.Fatalf("List(%d)[%d].ID = %q, want %q", tc.limit, i, got[i].ID, wantID)
				}
			}
		})
	}
}

func TestReaderListReturnsEmptySliceWhenNoRows(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "empty.db")
	seedTaskQueueDB(t, path, nil)

	reader, err := newReader(path)
	if err != nil {
		t.Fatalf("NewReader() error = %v", err)
	}
	if reader == nil {
		t.Fatal("NewReader() reader = nil, want non-nil")
	}
	t.Cleanup(func() {
		if err := reader.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	})

	jobs, err := reader.List(0)
	if err != nil {
		t.Fatalf("List(0) error = %v", err)
	}
	if jobs == nil {
		t.Fatal("List(0) = nil slice, want empty slice")
	}
	if len(jobs) != 0 {
		t.Fatalf("List(0) len = %d, want 0", len(jobs))
	}
}

func TestReaderListConcurrent(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "concurrent.db")
	seedTaskQueueDB(t, path, []jobFixture{
		{id: "job-1", title: "first", status: "pending", createdAt: "2026-04-18T10:00:00Z", updatedAt: "2026-04-18T10:01:00Z", wing: "alpha"},
		{id: "job-2", title: "second", status: "done", createdAt: "2026-04-18T10:02:00Z", updatedAt: "2026-04-18T10:03:00Z", wing: "beta"},
	})
	reader, err := newReader(path)
	if err != nil {
		t.Fatalf("NewReader() error = %v", err)
	}
	if reader == nil {
		t.Fatal("NewReader() reader = nil, want non-nil")
	}
	t.Cleanup(func() {
		if err := reader.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	})

	const workers = 8
	var wg sync.WaitGroup
	errCh := make(chan error, workers)

	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			jobs, err := reader.List(0)
			if err != nil {
				errCh <- err
				return
			}
			if len(jobs) != 2 {
				errCh <- sql.ErrNoRows
			}
		}()
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Fatalf("concurrent List() error = %v", err)
		}
	}
}

type jobFixture struct {
	id        string
	title     string
	status    string
	createdAt string
	updatedAt string
	wing      string
}

func seedTaskQueueDB(t *testing.T, path string, jobs []jobFixture) {
	t.Helper()

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}()

	_, err = db.Exec(`
		CREATE TABLE tasks (
			id TEXT,
			title TEXT,
			status TEXT,
			created_at TEXT,
			updated_at TEXT,
			wing TEXT
		);
	`)
	if err != nil {
		t.Fatalf("creating tasks table: %v", err)
	}

	for _, job := range jobs {
		_, err := db.Exec(
			`INSERT INTO tasks (id, title, status, created_at, updated_at, wing) VALUES (?, ?, ?, ?, ?, ?)`,
			job.id,
			job.title,
			job.status,
			job.createdAt,
			job.updatedAt,
			job.wing,
		)
		if err != nil {
			t.Fatalf("inserting job %q: %v", job.id, err)
		}
	}
}
