package getter

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGet_DispatcherAndFetch(t *testing.T) {
	t.Run("Auth and Content Fetch", func(t *testing.T) {
		content := "fake-tgz-content"
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Verify Basic Auth
			user, pass, ok := r.BasicAuth()
			if !ok || user != "admin" || pass != "password" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(content))
		}))
		defer server.Close()

		// Use a .tgz suffix to trigger tgzGetter
		uri := server.URL + "/chart.tgz"

		r, resolvedURI, err := Get(context.Background(), uri,
			WithCredentials("admin", "password"))

		if err != nil {
			t.Fatalf("Expected successful fetch, got: %v", err)
		}

		if resolvedURI != uri {
			t.Errorf("Expected URI %s, got %s", uri, resolvedURI)
		}

		body, _ := io.ReadAll(r)
		if string(body) != content {
			t.Errorf("Expected content %s, got %s", content, string(body))
		}
	})

	t.Run("MaxResponseSize Limit", func(t *testing.T) {
		// Create a server that sends more than MaxResponseSize
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", fmt.Sprintf("%d", MaxResponseSize+100))
			w.WriteHeader(http.StatusOK)
			// Write a bit more than the limit
			w.Write(make([]byte, MaxResponseSize+100))
		}))
		defer server.Close()

		uri := server.URL + "/huge.tgz"
		r, _, err := Get(context.Background(), uri)
		if err != nil {
			t.Fatalf("Get should not fail, but read should be limited: %v", err)
		}

		body, _ := io.ReadAll(r)
		if len(body) != MaxResponseSize {
			t.Errorf("Expected body to be capped at %d, got %d", MaxResponseSize, len(body))
		}
	})

	t.Run("Redirect Credentials Stripping", func(t *testing.T) {
		var capturedHeader string

		// Target server (different host)
		targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedHeader = r.Header.Get("Authorization")
			w.WriteHeader(http.StatusOK)
		}))
		defer targetServer.Close()

		// Redirect server
		redirectServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, targetServer.URL+"/chart.tgz", http.StatusFound)
		}))
		defer redirectServer.Close()

		// Test: PassCredentialsAll = false (default)
		_, _, _ = Get(context.Background(), redirectServer.URL+"/init.tgz",
			WithCredentials("admin", "pass"))

		if capturedHeader != "" {
			t.Error("Authorization header should have been stripped on cross-domain redirect")
		}

		// Test: PassCredentialsAll = true
		_, _, _ = Get(context.Background(), redirectServer.URL+"/init.tgz",
			WithCredentials("admin", "pass"),
			WithPassCredentialsAll(true),
		)

		if capturedHeader == "" {
			t.Error("Authorization header should have been preserved with PassCredentialsAll=true")
		}
	})

	t.Run("No Handler Found", func(t *testing.T) {
		_, _, err := Get(context.Background(), "ftp://example.com/chart")
		if err == nil || !errors.Is(err, ErrNoHandler) {
			t.Errorf("Expected ErrNoHandler, got: %v", err)
		}
	})
}
