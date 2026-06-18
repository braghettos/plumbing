package getter

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// --- Mock Registry Helper ---

func newMockHelmRepoServer(t *testing.T, chartName, version string, isRelative bool) *httptest.Server {
	var ts *httptest.Server
	ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/index.yaml") {
			// For Absolute tests, we point back to this same server's IP/Port
			chartURL := fmt.Sprintf("%s/charts/%s-%s.tgz", ts.URL, chartName, version)
			if isRelative {
				chartURL = fmt.Sprintf("%s-%s.tgz", chartName, version)
			}

			// KEYS MUST BE LOWERCASE to match yaml:"..." tags in strict mode
			indexContent := fmt.Sprintf(`
apiVersion: v1
entries:
  %s:
    - name: %s
      version: %s
      urls:
        - %s
`, chartName, chartName, version, chartURL)

			w.Header().Set("Content-Type", "text/yaml")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(indexContent))
			return
		}

		if strings.HasSuffix(r.URL.Path, ".tgz") {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("fake-tgz-content"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	return ts
}

// --- Test Suite ---

func TestHelmRepoGetter_Get(t *testing.T) {
	g := &repoGetter{}

	t.Run("Scheme Validation", func(t *testing.T) {
		_, _, err := g.Get(context.Background(), GetOptions{URI: "oci://registry.io"})
		if err == nil || !errors.Is(err, ErrInvalidRepoRef) {
			t.Errorf("expected ErrInvalidRepoRef for OCI scheme, got: %v", err)
		}
	})

	t.Run("Successful Fetch with Absolute URL", func(t *testing.T) {
		server := newMockHelmRepoServer(t, "mychart", "1.0.0", false)
		defer server.Close()

		opts := GetOptions{
			URI:     server.URL,
			Repo:    "mychart",
			Version: "1.0.0",
		}

		_, resolvedURI, err := g.Get(context.Background(), opts)
		if err != nil {
			t.Fatalf("expected success, got: %v", err)
		}

		// It should be an absolute URL pointing to our local server
		if !strings.HasPrefix(resolvedURI, "http://127.0.0.1") || !strings.HasSuffix(resolvedURI, ".tgz") {
			t.Errorf("expected local absolute URI, got %s", resolvedURI)
		}
	})

	t.Run("Relative URL Resolution", func(t *testing.T) {
		chartName := "localchart"
		version := "2.1.0"
		server := newMockHelmRepoServer(t, chartName, version, true)
		defer server.Close()

		opts := GetOptions{
			URI:     server.URL,
			Repo:    chartName,
			Version: version,
		}

		_, resolvedURI, err := g.Get(context.Background(), opts)
		if err != nil {
			t.Fatalf("failed to resolve relative URL: %v", err)
		}

		expected := fmt.Sprintf("%s/%s-%s.tgz", server.URL, chartName, version)
		if resolvedURI != expected {
			t.Errorf("expected relative resolution %s, got %s", expected, resolvedURI)
		}
	})

	t.Run("No URLs in Index", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// lowercase 'entries'
			w.Write([]byte(`
apiVersion: v1
entries:
  empty:
    - name: empty
      version: 0.1.0
      urls: []
`))
		}))
		defer server.Close()

		opts := GetOptions{URI: server.URL, Repo: "empty", Version: "0.1.0"}
		_, _, err := g.Get(context.Background(), opts)
		if err == nil || !strings.Contains(err.Error(), "no package url found") {
			t.Errorf("expected error for empty URLs, got: %v", err)
		}
	})

	t.Run("Invalid Chart URL in Index", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// lowercase 'entries'
			w.Write([]byte(`
apiVersion: v1
entries:
  bad:
    - name: bad
      version: 1.0.0
      urls:
        - "::not-a-url"
`))
		}))
		defer server.Close()

		opts := GetOptions{URI: server.URL, Repo: "bad", Version: "1.0.0"}
		_, _, err := g.Get(context.Background(), opts)
		if err == nil || !strings.Contains(err.Error(), "invalid chart url") {
			t.Errorf("expected invalid chart url error, got: %v", err)
		}
	})
}

func TestHelmRepoGetter_AuthPassDown(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "admin" || pass != "secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		// lowercase 'entries'
		w.Write([]byte("apiVersion: v1\nentries:\n  auth:\n    - name: auth\n      version: 1.0.0\n      urls: [app.tgz]"))
	}))
	defer server.Close()

	g := &repoGetter{}
	opts := GetOptions{
		URI:      server.URL,
		Repo:     "auth",
		Version:  "1.0.0",
		Username: "admin",
		Password: "secret",
	}

	_, _, err := g.Get(context.Background(), opts)
	if err != nil {
		t.Errorf("auth credentials were not correctly passed to fetch: %v", err)
	}
}

func TestRepoGetter_URLLogic(t *testing.T) {
	g := &repoGetter{}

	testCases := []struct {
		name          string
		indexURLs     []string
		requestedRepo string
		expectError   string
		expectedPath  string // Use path instead of full URI
	}{
		{
			name:          "Join Relative URL",
			indexURLs:     []string{"mychart-1.0.0.tgz"},
			requestedRepo: "mychart",
			expectedPath:  "/mychart-1.0.0.tgz",
		},
		{
			name:          "Handle Empty URL List",
			indexURLs:     []string{},
			requestedRepo: "mychart",
			expectError:   "no package url found",
		},
		{
			name:          "Invalid URL After Join",
			indexURLs:     []string{"::invalid-path"},
			requestedRepo: "mychart",
			expectError:   "invalid chart url",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			indexYAML := fmt.Sprintf(`
apiVersion: v1
entries:
  mychart:
    - name: mychart
      version: 1.0.0
      urls: [%s]
`, strings.Join(func() []string {
				quoted := make([]string, len(tc.indexURLs))
				for i, u := range tc.indexURLs {
					quoted[i] = fmt.Sprintf("%q", u)
				}
				return quoted
			}(), ","))

			// This server provides the index and acts as the base URI
			idxServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, ".tgz") {
					w.WriteHeader(http.StatusOK)
					return
				}
				w.Write([]byte(indexYAML))
			}))
			defer idxServer.Close()

			opts := GetOptions{
				URI:     idxServer.URL,
				Repo:    tc.requestedRepo,
				Version: "1.0.0",
			}

			_, finalURI, err := g.Get(context.Background(), opts)

			if tc.expectError != "" {
				if err == nil || !strings.Contains(err.Error(), tc.expectError) {
					t.Fatalf("expected error containing %q, got: %v", tc.expectError, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Construct the expected URI using the dynamic server URL
			wantURI := idxServer.URL + tc.expectedPath
			if finalURI != wantURI {
				t.Errorf("expected URI %s, got %s", wantURI, finalURI)
			}
		})
	}
}
