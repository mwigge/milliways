package review

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// ErrFileNotFound is returned by ContextTracker.Add when the given path does
// not exist on the filesystem.
var ErrFileNotFound = errors.New("file not found")

// ContextTracker manages the set of files the model has explicit access to
// within a session. Files outside the tracked set are not sent to the model.
type ContextTracker interface {
	// Add marks a file as active. Path is absolute or relative to RepoPath.
	// Returns ErrFileNotFound if the path does not exist.
	Add(path string) error
	// Drop removes a file from the active set. No-op if not present.
	Drop(path string)
	// List returns all currently active file paths (absolute).
	List() []string
	// Clear removes all files from the active set.
	Clear()
	// Contains returns true if path is in the active set.
	Contains(path string) bool
	// Size returns the number of active files.
	Size() int
}

// FileContextTracker is an in-memory, thread-safe implementation of
// ContextTracker. Files are stored by their absolute path.
type FileContextTracker struct {
	repoPath string
	files    map[string]struct{} // keyed by absolute path
	mu       sync.RWMutex
}

// NewContextTracker returns a ContextTracker scoped to repoPath.
// Relative paths passed to Add/Drop/Contains are resolved relative to
// repoPath.
func NewContextTracker(repoPath string) ContextTracker {
	return &FileContextTracker{
		repoPath: repoPath,
		files:    make(map[string]struct{}),
	}
}

// Add marks path as active after verifying it exists.
// Returns ErrFileNotFound (wrapped) when the path does not exist on disk.
func (t *FileContextTracker) Add(path string) error {
	abs, err := t.resolve(path)
	if err != nil {
		return fmt.Errorf("context tracker add %s: %w", path, err)
	}

	if _, statErr := os.Stat(abs); statErr != nil {
		if os.IsNotExist(statErr) {
			return fmt.Errorf("context tracker add %s: %w", path, ErrFileNotFound)
		}
		return fmt.Errorf("context tracker add %s: %w", path, statErr)
	}

	t.mu.Lock()
	t.files[abs] = struct{}{}
	t.mu.Unlock()
	return nil
}

// Drop removes path from the active set. It is a no-op if path is not present.
func (t *FileContextTracker) Drop(path string) {
	abs, err := t.resolve(path)
	if err != nil {
		return // cannot resolve — nothing to drop
	}
	t.mu.Lock()
	delete(t.files, abs)
	t.mu.Unlock()
}

// List returns a sorted slice of all active absolute file paths.
func (t *FileContextTracker) List() []string {
	t.mu.RLock()
	out := make([]string, 0, len(t.files))
	for p := range t.files {
		out = append(out, p)
	}
	t.mu.RUnlock()
	sort.Strings(out)
	return out
}

// Clear removes all files from the active set.
func (t *FileContextTracker) Clear() {
	t.mu.Lock()
	t.files = make(map[string]struct{})
	t.mu.Unlock()
}

// Contains reports whether path is in the active set.
func (t *FileContextTracker) Contains(path string) bool {
	abs, err := t.resolve(path)
	if err != nil {
		return false
	}
	t.mu.RLock()
	_, ok := t.files[abs]
	t.mu.RUnlock()
	return ok
}

// Size returns the number of files in the active set.
func (t *FileContextTracker) Size() int {
	t.mu.RLock()
	n := len(t.files)
	t.mu.RUnlock()
	return n
}

// resolve returns the absolute path for p, using repoPath as the base when p
// is relative.
func (t *FileContextTracker) resolve(p string) (string, error) {
	if filepath.IsAbs(p) {
		return filepath.Clean(p), nil
	}
	abs, err := filepath.Abs(filepath.Join(t.repoPath, p))
	if err != nil {
		return "", fmt.Errorf("resolve path %s: %w", p, err)
	}
	return abs, nil
}
