package cache

import (
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestStop_Idempotent pins the guard added for krateo-platformops/core-provider#99: Stop used to
// call close(c.stopCh) directly, so a second call panicked with "close of closed channel". Stop is
// reached from the helm client's Close(), and callers legitimately invoke that more than once (a
// deferred close on a reassigned client variable, a Close in both an error path and a defer), so
// correct cleanup could crash the process.
func TestStop_Idempotent(t *testing.T) {
	c, err := NewDiskCache(WithDir(t.TempDir()), WithCleanupInterval(10*time.Millisecond))
	if err != nil {
		t.Fatalf("NewDiskCache: %v", err)
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Stop must be idempotent, but a repeated call panicked: %v", r)
		}
	}()

	for i := 0; i < 5; i++ {
		c.Stop()
	}
}

// TestStop_ConcurrentIsSafe covers the other way the bare close blew up: two shutdown paths racing.
// Run under -race to also catch unsynchronised access.
func TestStop_ConcurrentIsSafe(t *testing.T) {
	c, err := NewDiskCache(WithDir(t.TempDir()), WithCleanupInterval(10*time.Millisecond))
	if err != nil {
		t.Fatalf("NewDiskCache: %v", err)
	}

	panics := make(chan any, 16)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					panics <- r
				}
			}()
			c.Stop()
		}()
	}
	wg.Wait()
	close(panics)

	if r, ok := <-panics; ok {
		t.Fatalf("concurrent Stop panicked: %v", r)
	}
}

// TestStop_TerminatesCleanupGoroutine is the point of calling Stop at all: NewDiskCache starts
// startCleanupRoutine unconditionally, and never stopping it is what leaked a goroutine per helm
// client in core-provider#99. This asserts the goroutine is actually gone after Stop, so the guard
// above cannot be "fixed" by making Stop a no-op.
func TestStop_TerminatesCleanupGoroutine(t *testing.T) {
	before := countCleanupGoroutines()

	c, err := NewDiskCache(WithDir(t.TempDir()), WithCleanupInterval(10*time.Millisecond))
	if err != nil {
		t.Fatalf("NewDiskCache: %v", err)
	}

	// The goroutine is started by NewDiskCache; give the scheduler a moment to run it.
	if !eventually(func() bool { return countCleanupGoroutines() > before }, time.Second) {
		t.Skip("cleanup goroutine not observable in this runtime; nothing to assert")
	}

	c.Stop()

	if !eventually(func() bool { return countCleanupGoroutines() <= before }, 2*time.Second) {
		t.Errorf("cleanup goroutine still running after Stop (before=%d, now=%d): "+
			"the DiskCache goroutine leak from core-provider#99 is back",
			before, countCleanupGoroutines())
	}
}

// countCleanupGoroutines counts stacks currently inside startCleanupRoutine.
func countCleanupGoroutines() int {
	buf := make([]byte, 1<<20)
	n := runtime.Stack(buf, true)
	return strings.Count(string(buf[:n]), "startCleanupRoutine")
}

func eventually(cond func() bool, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return cond()
}
