package memory

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestWorkingMemorySetGetDelete(t *testing.T) {
	t.Parallel()

	memory := NewWorkingMemory()
	t.Cleanup(memory.Close)

	memory.Set("active_branch", "feat/test", 0)
	value, ok := memory.Get("active_branch")
	if !ok {
		t.Fatal("expected active_branch to exist")
	}
	if value != "feat/test" {
		t.Fatalf("Get() = %q, want feat/test", value)
	}

	memory.Delete("active_branch")
	if _, ok := memory.Get("active_branch"); ok {
		t.Fatal("expected key to be deleted")
	}
}

func TestWorkingMemoryExpiresEntries(t *testing.T) {
	t.Parallel()

	memory := newWorkingMemoryWithInterval(5 * time.Millisecond)
	t.Cleanup(memory.Close)

	memory.Set("short", "lived", 20*time.Millisecond)
	time.Sleep(30 * time.Millisecond)
	if _, ok := memory.Get("short"); ok {
		t.Fatal("expected expired key to be missing")
	}
	time.Sleep(20 * time.Millisecond)
	if got := memory.Keys(); len(got) != 0 {
		t.Fatalf("Keys() = %v, want empty", got)
	}
}

func TestWorkingMemoryConcurrentAccess(t *testing.T) {
	t.Parallel()

	memory := NewWorkingMemory()
	t.Cleanup(memory.Close)

	const workers = 16
	const writesPerWorker = 25
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		worker := worker
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := 0; index < writesPerWorker; index++ {
				key := fmt.Sprintf("k-%d-%d", worker, index)
				memory.Set(key, "value", 0)
				if _, ok := memory.Get(key); !ok {
					t.Errorf("missing key %s", key)
				}
			}
		}()
	}
	wg.Wait()

	if got := len(memory.Keys()); got != workers*writesPerWorker {
		t.Fatalf("len(Keys()) = %d, want %d", got, workers*writesPerWorker)
	}
}
