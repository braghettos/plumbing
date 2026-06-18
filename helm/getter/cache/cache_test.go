package cache

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestConcurrency_RaceConditions attempts to trigger race detector warnings.
// Run with: go test -race
func TestConcurrency_RaceConditions(t *testing.T) {
	dir := t.TempDir()

	c, err := NewDiskCache(
		WithDir(dir),
		WithTTL(100*time.Millisecond),
		WithCleanupInterval(50*time.Millisecond),
	)
	if err != nil {
		t.Fatalf("Failed to create cache: %v", err)
	}

	var wg sync.WaitGroup
	uri := "https://example.com/charts/mychart"
	version := "1.2.3"
	payload := []byte("helm-chart-data")

	// Spawning 50 concurrent writers
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			// varying versions to create multiple files
			v := fmt.Sprintf("%s-%d", version, id%5)
			if err := c.Set(uri, v, bytes.NewReader(payload)); err != nil {
				t.Errorf("Set failed: %v", err)
			}
		}(i)
	}

	// Spawning 50 concurrent readers
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			v := fmt.Sprintf("%s-%d", version, id%5)
			// It's acceptable to miss, but we shouldn't panic or get corrupted data
			c.Get(uri, v)
		}(i)
	}

	wg.Wait()
}

// TestBlockingCleanup demonstrates the "Stop-The-World" problem.
// This test shows that 'Get' is blocked while 'cleanup' is running.
func TestBlockingCleanup(t *testing.T) {
	dir := t.TempDir()
	// Short TTL so items expire quickly
	c, err := NewDiskCache(
		WithDir(dir),
		WithTTL(1*time.Nanosecond),
		WithCleanupInterval(1*time.Hour), // Manual trigger
	)
	if err != nil {
		t.Fatalf("Failed to create cache: %v", err)
	}

	// 1. Populate cache with 10,000 dummy files to slow down ReadDir
	// This simulates a heavy cache load (e.g., a chart repository).
	for i := 0; i < 10000; i++ {
		dummyKey := fmt.Sprintf("dummy-%d", i)
		// Bypass internal Key() to just fill directory quickly
		os.WriteFile(filepath.Join(dir, dummyKey), []byte("x"), 0644)
	}

	// 2. Start a timer
	start := time.Now()

	// 3. Trigger cleanup (which holds the Write Lock) in a goroutine
	go func() {
		c.cleanup()
	}()

	// Give the cleanup a tiny head start to acquire the lock
	time.Sleep(2 * time.Millisecond)

	// 4. Try to Read. This should be instant, but it will hang
	// waiting for cleanup to finish.
	c.Get("valid-uri", "1.0.0")

	duration := time.Since(start)

	// If 'Get' took longer than 100ms (arbitrary threshold), the cleanup blocked it.
	// On a fast SSD this might pass, but on networked storage or heavy load, it fails.
	if duration > 100*time.Millisecond {
		t.Logf("Performance Warning: Get() was blocked for %v during cleanup", duration)
	}
}

// TestGoroutineLeak shows that NewDiskCache leaks a goroutine
// because there is no Shutdown mechanism.
func TestGoroutineLeak(t *testing.T) {
	dir := t.TempDir()

	// Create 1000 cache instances
	for i := 0; i < 1000; i++ {
		_, _ = NewDiskCache(
			WithDir(dir),
			WithTTL(time.Hour),
			WithCleanupInterval(time.Hour),
		)
	}

	// At this point, 1000 tickers are running in the background forever.
	// In a real application, if you create caches dynamically (e.g., per request),
	// this will crash the runtime eventually.
	t.Log("Check runtime.NumGoroutine() - it will be inflated by 1000.")
}

// TestIOErrorHandling checks behavior when disk permissions fail.
func TestIOErrorHandling(t *testing.T) {
	dir := t.TempDir()
	c, _ := NewDiskCache(
		WithDir(dir),
		WithTTL(time.Hour),
	)

	// Setup a valid file
	uri, ver := "http://test", "1.0"
	c.Set(uri, ver, bytes.NewReader([]byte("data")))

	// SABOTAGE: Remove read permissions from the directory
	os.Chmod(dir, 0000)
	defer os.Chmod(dir, 0755) // Restore for cleanup

	// Try to get
	_, found := c.Get(uri, ver)

	// Current implementation swallows the error and returns false (Not Found).
	// This is ambiguous: is it missing, or is the disk broken?
	if found {
		t.Error("Should not find file in unreadable dir")
	}
}
