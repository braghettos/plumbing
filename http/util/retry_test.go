package util_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/krateo-platformops/plumbing/http/util"
)

func TestRetryOn429(t *testing.T) {
	attempts := 0

	// Fake server: respond with 429 twice, then 200
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts <= 2 {
			w.Header().Set("Retry-After", "1") // 1 second
			http.Error(w, "too many requests", http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	cli := &util.RetryClient{
		Client:      http.DefaultClient,
		MaxRetries:  5,                      // or configurable
		BaseBackoff: 500 * time.Millisecond, // base backoff
		MaxBackoff:  10 * time.Second,       // max backoff
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL, nil)
	resp, err := cli.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}

	if attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}
}

func TestNoRetryOnPost(t *testing.T) {
	attempts := 0

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer ts.Close()

	cli := &util.RetryClient{
		Client:      http.DefaultClient,
		MaxRetries:  5,                      // or configurable
		BaseBackoff: 500 * time.Millisecond, // base backoff
		MaxBackoff:  10 * time.Second,       // max backoff
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, ts.URL, nil)
	resp, err := cli.Do(req)

	if err == nil {
		resp.Body.Close()
	}

	if attempts != 1 {
		t.Fatalf("expected 1 attempt for POST, got %d", attempts)
	}
}
