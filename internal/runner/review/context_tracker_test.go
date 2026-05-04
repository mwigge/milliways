package review

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestFileContextTracker_AddAndList(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	fileA := filepath.Join(dir, "a.go")
	fileB := filepath.Join(dir, "b.go")
	if err := os.WriteFile(fileA, []byte("package a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fileB, []byte("package b"), 0o644); err != nil {
		t.Fatal(err)
	}

	tracker := NewContextTracker(dir)
	if err := tracker.Add(fileA); err != nil {
		t.Fatalf("Add a.go: %v", err)
	}
	if err := tracker.Add(fileB); err != nil {
		t.Fatalf("Add b.go: %v", err)
	}

	list := tracker.List()
	if len(list) != 2 {
		t.Fatalf("List() len = %d, want 2", len(list))
	}
}

func TestFileContextTracker_Add_FileNotFound(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	tracker := NewContextTracker(dir)

	err := tracker.Add(filepath.Join(dir, "nonexistent.go"))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !isErrFileNotFound(err) {
		t.Errorf("error = %v, want ErrFileNotFound", err)
	}
}

func TestFileContextTracker_Drop_ExistingFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	fileA := filepath.Join(dir, "a.go")
	if err := os.WriteFile(fileA, []byte("package a"), 0o644); err != nil {
		t.Fatal(err)
	}

	tracker := NewContextTracker(dir)
	if err := tracker.Add(fileA); err != nil {
		t.Fatal(err)
	}
	tracker.Drop(fileA)

	if tracker.Size() != 0 {
		t.Errorf("Size() = %d, want 0 after Drop", tracker.Size())
	}
	list := tracker.List()
	if len(list) != 0 {
		t.Errorf("List() = %v, want empty after Drop", list)
	}
}

func TestFileContextTracker_Drop_NotPresent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	tracker := NewContextTracker(dir)

	// Must not panic or return an error — it's a no-op.
	tracker.Drop(filepath.Join(dir, "ghost.go"))
}

func TestFileContextTracker_Contains(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	fileA := filepath.Join(dir, "a.go")
	if err := os.WriteFile(fileA, []byte("package a"), 0o644); err != nil {
		t.Fatal(err)
	}

	tracker := NewContextTracker(dir)
	if err := tracker.Add(fileA); err != nil {
		t.Fatal(err)
	}

	if !tracker.Contains(fileA) {
		t.Error("Contains(fileA) = false, want true")
	}

	tracker.Drop(fileA)
	if tracker.Contains(fileA) {
		t.Error("Contains(fileA) = true after Drop, want false")
	}
}

func TestFileContextTracker_Clear(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for _, name := range []string{"a.go", "b.go", "c.go"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("package x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	tracker := NewContextTracker(dir)
	for _, name := range []string{"a.go", "b.go", "c.go"} {
		if err := tracker.Add(filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	if tracker.Size() != 3 {
		t.Fatalf("Size() = %d before Clear, want 3", tracker.Size())
	}

	tracker.Clear()

	if tracker.Size() != 0 {
		t.Errorf("Size() = %d after Clear, want 0", tracker.Size())
	}
}

func TestFileContextTracker_ConcurrentAddDrop(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	const n = 10
	files := make([]string, n)
	for i := range n {
		p := filepath.Join(dir, "file"+string(rune('0'+i))+".go")
		if err := os.WriteFile(p, []byte("package x"), 0o644); err != nil {
			t.Fatal(err)
		}
		files[i] = p
	}

	tracker := NewContextTracker(dir)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_ = tracker.Add(files[idx])
			tracker.Drop(files[idx])
		}(i)
	}
	wg.Wait()
}

func TestFileContextTracker_List_Sorted(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	// Create files with names that would sort differently if unsorted.
	for _, name := range []string{"z.go", "m.go", "a.go"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("package x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	tracker := NewContextTracker(dir)
	// Add in reverse alpha order.
	for _, name := range []string{"z.go", "m.go", "a.go"} {
		if err := tracker.Add(filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}

	list := tracker.List()
	if len(list) != 3 {
		t.Fatalf("List() len = %d, want 3", len(list))
	}

	wantOrder := []string{
		filepath.Join(dir, "a.go"),
		filepath.Join(dir, "m.go"),
		filepath.Join(dir, "z.go"),
	}
	for i, got := range list {
		if got != wantOrder[i] {
			t.Errorf("List()[%d] = %q, want %q", i, got, wantOrder[i])
		}
	}
}

// isErrFileNotFound uses errors.Is to check for ErrFileNotFound.
func isErrFileNotFound(err error) bool {
	return err != nil && errors.Is(err, ErrFileNotFound)
}
