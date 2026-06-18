package getter

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTGZGetter_Get(t *testing.T) {
	g := &tgzGetter{}

	t.Run("Suffix Validation", func(t *testing.T) {
		testCases := []struct {
			uri     string
			wantErr error
		}{
			{"http://example.com/chart.tgz", nil},    // Should pass validation
			{"http://example.com/chart.tar.gz", nil}, // Should pass validation
			{"http://example.com/chart.zip", ErrInvalidRepoRef},
			{"oci://registry.io/mychart", ErrInvalidRepoRef},
		}

		for _, tc := range testCases {
			// We don't care about the fetch result here, only the validation
			_, _, err := g.Get(context.Background(), GetOptions{URI: tc.uri})

			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Errorf("URI %s: expected %v, got %v", tc.uri, tc.wantErr, err)
				}
			} else {
				// If wantErr is nil, we expect it to PASS isTGZ.
				// It might still fail the fetch() call with a 404,
				// but it should NOT return ErrInvalidRepoRef.
				if errors.Is(err, ErrInvalidRepoRef) {
					t.Errorf("URI %s: validation failed unexpectedly: %v", tc.uri, err)
				}
			}
		}
	})

	t.Run("Successful Fetch", func(t *testing.T) {
		content := "fake-binary-data"
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(content))
		}))
		defer server.Close()

		// Append .tgz to make isTGZ happy
		uri := server.URL + "/mychart.tgz"
		opts := GetOptions{URI: uri}

		reader, resolvedURI, err := g.Get(context.Background(), opts)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if resolvedURI != uri {
			t.Errorf("expected URI %s, got %s", uri, resolvedURI)
		}

		gotBody, _ := io.ReadAll(reader)
		if string(gotBody) != content {
			t.Errorf("expected body %q, got %q", content, string(gotBody))
		}
	})

	t.Run("Fetch Failure", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		opts := GetOptions{URI: server.URL + "/error.tgz"}
		_, _, err := g.Get(context.Background(), opts)
		if err == nil {
			t.Fatal("expected error on 500 status, got nil")
		}
		if !strings.Contains(err.Error(), "failed to fetch tgz") {
			t.Errorf("unexpected error message: %v", err)
		}
	})
}
