package ledger

import "fmt"

// DualWriter writes to both ndjson and SQLite simultaneously.
type DualWriter struct {
	ndjson *Writer
	store  *Store
}

// NewDualWriter creates a writer that persists to both backends.
func NewDualWriter(ndjsonPath, dbPath string) (*DualWriter, error) {
	store, err := OpenStore(dbPath)
	if err != nil {
		return nil, fmt.Errorf("opening ledger store: %w", err)
	}
	return &DualWriter{
		ndjson: NewWriter(ndjsonPath),
		store:  store,
	}, nil
}

// Write persists an entry to both ndjson and SQLite.
// ndjson errors are non-fatal (logged); SQLite errors are non-fatal (logged).
// Returns the first error encountered, if any.
func (dw *DualWriter) Write(e Entry) error {
	var firstErr error

	if err := dw.ndjson.Write(e); err != nil {
		firstErr = fmt.Errorf("ndjson: %w", err)
	}

	if err := dw.store.Insert(e); err != nil {
		if firstErr == nil {
			firstErr = fmt.Errorf("sqlite: %w", err)
		}
	}

	return firstErr
}

// Store returns the underlying SQLite store for queries.
func (dw *DualWriter) Store() *Store {
	return dw.store
}

// Close closes the SQLite connection.
func (dw *DualWriter) Close() error {
	return dw.store.Close()
}
