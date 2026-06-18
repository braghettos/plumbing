package crdgen_test

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/krateoplatformops/plumbing/crdgen"
)

func TestConcurrentGenerationSameKindAndGroup(t *testing.T) {
	const numWorkers = 5
	schemaPath := filepath.Join("testdata", "basic.schema.json")
	input, _ := os.ReadFile(schemaPath)
	const defaultStatus = `{"type": "object", "additionalProperties": true}`

	var wg sync.WaitGroup
	var mu sync.Mutex
	success := 0

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, err := crdgen.Generate(crdgen.Options{
				Group:        "test.krateo.io",
				Version:      "v1.0.0",
				Kind:         "Test",
				SpecSchema:   input,
				StatusSchema: []byte(defaultStatus),
			})
			mu.Lock()
			if err == nil {
				success++
			}
			mu.Unlock()
		}(i)
	}

	wg.Wait()
	if success < numWorkers {
		t.Fatalf("Expected %d successes, got %d", numWorkers, success)
	}
}

func TestConcurrentGenerationDifferentVersions(t *testing.T) {
	const numWorkers = 5
	schemaPath := filepath.Join("testdata", "basic.schema.json")
	input, _ := os.ReadFile(schemaPath)
	const defaultStatus = `{"type": "object", "additionalProperties": true}`

	var wg sync.WaitGroup
	var mu sync.Mutex
	success := 0
	failures := make([]error, numWorkers)

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			// Use DIFFERENT versions per worker to expose race condition
			version := "v1.0." + string(rune('0'+idx%10))
			_, err := crdgen.Generate(crdgen.Options{
				Group:        "test.krateo.io",
				Version:      version,
				Kind:         "Test",
				SpecSchema:   input,
				StatusSchema: []byte(defaultStatus),
			})
			mu.Lock()
			if err == nil {
				success++
			} else {
				failures[idx] = err
			}
			mu.Unlock()
		}(i)
	}

	wg.Wait()
	if success < numWorkers {
		t.Logf("Failures: %v", failures)
		t.Fatalf("Expected %d successes, got %d - this indicates a race condition", numWorkers, success)
	}
}
