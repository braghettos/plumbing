package getter

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// --- Helper: Layer Selection Logic Test ---

func TestChartLayerSelection(t *testing.T) {
	testCases := []struct {
		name     string
		layers   []ocispec.Descriptor
		expected string // Expected digest or "none"
	}{
		{
			name: "Prefer Helm Media Type",
			layers: []ocispec.Descriptor{
				{MediaType: "application/octet-stream", Digest: "sha256:wrong"},
				{MediaType: ChartLayerMediaType, Digest: "sha256:correct"},
				{MediaType: LegacyLayerMediaType, Digest: "sha256:legacy"},
			},
			expected: "sha256:correct",
		},
		{
			name: "Fallback to Legacy",
			layers: []ocispec.Descriptor{
				{MediaType: "application/octet-stream", Digest: "sha256:wrong"},
				{MediaType: LegacyLayerMediaType, Digest: "sha256:legacy"},
			},
			expected: "sha256:legacy",
		},
		{
			name: "Single Unknown Layer Fallback",
			layers: []ocispec.Descriptor{
				{MediaType: "application/unknown", Digest: "sha256:only"},
			},
			expected: "sha256:only",
		},
		{
			name: "Multiple Unknown Layers (Fail)",
			layers: []ocispec.Descriptor{
				{MediaType: "application/unknown1", Digest: "sha256:1"},
				{MediaType: "application/unknown2", Digest: "sha256:2"},
			},
			expected: "none",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var selected *ocispec.Descriptor
			// Simulate internal logic
			for _, l := range tc.layers {
				if l.MediaType == ChartLayerMediaType || l.MediaType == LegacyLayerMediaType {
					temp := l
					selected = &temp
					break
				}
			}
			if selected == nil && len(tc.layers) == 1 {
				selected = &tc.layers[0]
			}

			if tc.expected == "none" {
				if selected != nil {
					t.Errorf("expected no selection, got %s", selected.Digest)
				}
			} else if selected == nil || string(selected.Digest) != tc.expected {
				t.Errorf("expected %s, got %v", tc.expected, selected)
			}
		})
	}
}

// --- Helper: URI & Scheme Validation ---

func TestOCIGetter_URIParsing(t *testing.T) {
	g := NewOCIGetter()
	testCases := []struct {
		name string
		uri  string
		ok   bool
	}{
		{"Valid OCI", "oci://registry.io/chart", true},
		{"Empty URI", "", false},
		{"HTTP Scheme", "http://registry.io/chart", false},
		{"File Scheme", "file:///path", false},
		{"Missing Scheme", "registry.io/chart", false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := g.Get(context.Background(), GetOptions{URI: tc.uri})
			if tc.ok && err != nil && strings.Contains(err.Error(), "not a valid OCI ref") {
				t.Errorf("unexpected error for %q: %v", tc.uri, err)
			}
			if !tc.ok && (err == nil || !strings.Contains(err.Error(), "not a valid OCI ref")) {
				t.Errorf("expected validation error for %q, got %v", tc.uri, err)
			}
		})
	}
}

// --- Transport & Security ---

func TestTransportBehavior(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	t.Run("Insecure Skips Verify", func(t *testing.T) {
		client := &http.Client{Transport: getTransport(true)}
		resp, err := client.Get(server.URL)
		if err != nil {
			t.Fatalf("failed to connect with insecure transport: %v", err)
		}
		resp.Body.Close()
	})

	t.Run("Secure Rejects Self-Signed", func(t *testing.T) {
		client := &http.Client{Transport: getTransport(false)}
		_, err := client.Get(server.URL)
		if err == nil {
			t.Fatal("expected certificate error, got nil")
		}
	})
}

// --- Concurrency & Context ---

func TestOCIGetter_Safety(t *testing.T) {
	g := NewOCIGetter()

	t.Run("Context Timeout", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
		defer cancel()
		time.Sleep(2 * time.Millisecond)
		_, _, err := g.Get(ctx, GetOptions{URI: "oci://registry.io/chart:1.0.0"})
		if err == nil {
			t.Error("expected timeout error")
		}
	})

	t.Run("Concurrent Requests Race Check", func(t *testing.T) {
		var wg sync.WaitGroup
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = getTransport(i%2 == 0)
				_, _, _ = g.Get(context.Background(), GetOptions{URI: "oci://reg.io/c:1"})
			}()
		}
		wg.Wait()
	})
}

// --- Integration Tests (Mock Registry) ---

func TestOCIGetter_Integration(t *testing.T) {
	chartContent := []byte("standard helm chart content")
	registry := newMockOCIRegistry(chartContent, false)
	server := httptest.NewServer(registry)
	defer server.Close()
	host := strings.TrimPrefix(server.URL, "http://")

	g := NewOCIGetter()

	t.Run("Successful Fetch", func(t *testing.T) {
		opts := GetOptions{URI: fmt.Sprintf("oci://%s/test/chart:1.0.0", host), InsecureSkipVerifyTLS: true}
		reader, _, err := g.Get(context.Background(), opts)
		if err != nil {
			t.Fatalf("fetch failed: %v", err)
		}

		gr, _ := gzip.NewReader(reader)
		data, _ := io.ReadAll(gr)
		if !bytes.Equal(data, chartContent) {
			t.Errorf("content mismatch")
		}
	})

	t.Run("Fetch by Digest", func(t *testing.T) {
		opts := GetOptions{
			URI:                   fmt.Sprintf("oci://%s/test/chart@%s", host, registry.manifDigest.String()),
			InsecureSkipVerifyTLS: true,
		}
		_, _, err := g.Get(context.Background(), opts)
		if err != nil {
			t.Errorf("fetch by digest failed: %v", err)
		}
	})
	t.Run("Successful Authenticated Fetch", func(t *testing.T) {
		// Create a registry that requires auth
		authRegistry := newMockOCIRegistry(chartContent, true)
		authServer := httptest.NewServer(authRegistry)
		defer authServer.Close()
		authHost := strings.TrimPrefix(authServer.URL, "http://")

		opts := GetOptions{
			URI:                   fmt.Sprintf("oci://%s/private/chart:1.0.0", authHost),
			Username:              "testuser", // Matches mockOCIRegistry default
			Password:              "testpass",
			InsecureSkipVerifyTLS: true,
			PassCredentialsAll:    true, // Ensure your logic triggers Login()
		}

		_, _, err := g.Get(context.Background(), opts)
		if err != nil {
			t.Fatalf("authenticated fetch failed: %v", err)
		}
	})

	t.Run("Authentication Failure", func(t *testing.T) {
		authRegistry := newMockOCIRegistry(chartContent, true)
		authServer := httptest.NewServer(authRegistry)
		defer authServer.Close()
		authHost := strings.TrimPrefix(authServer.URL, "http://")

		opts := GetOptions{
			URI:                   fmt.Sprintf("oci://%s/private/chart:1.0.0", authHost),
			Username:              "wrong-user",
			Password:              "wrong-pass",
			InsecureSkipVerifyTLS: true,
		}

		_, _, err := g.Get(context.Background(), opts)
		if err == nil {
			t.Fatal("expected error due to wrong credentials, got nil")
		}
	})

	t.Run("Version Injection", func(t *testing.T) {
		// URI without :1.0.0
		uriWithoutTag := fmt.Sprintf("oci://%s/test/mychart", host)

		opts := GetOptions{
			URI:                   uriWithoutTag,
			Version:               "1.0.0", // This should be appended to the URI
			InsecureSkipVerifyTLS: true,
		}

		// If your code works, it will resolve this to .../mychart:1.0.0
		// and correctly fetch from the mock registry.
		_, resolvedURI, err := g.Get(context.Background(), opts)
		if err != nil {
			t.Fatalf("failed to fetch with injected version: %v", err)
		}

		expected := uriWithoutTag + ":1.0.0"
		if resolvedURI != expected {
			t.Errorf("expected resolved URI %s, got %s", expected, resolvedURI)
		}
	})

	t.Run("Version Not Specified", func(t *testing.T) {
		// Create a dedicated server for this test to ensure clean state
		versionRegistry := newMockOCIRegistry(chartContent, false)
		versionServer := httptest.NewServer(versionRegistry)
		defer versionServer.Close()
		versionHost := strings.TrimPrefix(versionServer.URL, "http://")

		// URI without :1.0.0
		uriWithoutTag := fmt.Sprintf("oci://%s/test/mychart", versionHost)

		opts := GetOptions{
			URI:                   uriWithoutTag,
			InsecureSkipVerifyTLS: true,
		}

		// If your code works, it will resolve this to .../mychart:1.0.0
		// and correctly fetch from the mock registry.
		_, resolvedURI, err := g.Get(context.Background(), opts)
		if err != nil {
			t.Fatalf("failed to fetch with injected version: %v", err)
		}

		expected := uriWithoutTag + ":1.0.0"
		if resolvedURI != expected {
			t.Errorf("expected resolved URI %s, got %s", expected, resolvedURI)
		}
	})

	t.Run("Single Unknown Layer Fallback", func(t *testing.T) {
		// 1. Create content for the unknown layer
		customContent := []byte("fallback content")
		var buf bytes.Buffer
		gw := gzip.NewWriter(&buf)
		gw.Write(customContent)
		gw.Close()

		customDigest := digest.FromBytes(buf.Bytes())

		// 2. Create a registry with a "broken" manifest (unknown media type)
		registry := newMockOCIRegistry(customContent, false)
		registry.manifest.Layers = []ocispec.Descriptor{
			{
				MediaType: "application/vnd.something.else.v1.tar+gzip", // Non-standard
				Digest:    customDigest,
				Size:      int64(buf.Len()),
			},
		}
		// Update the manifest JSON for the mock
		registry.manifestJSON, _ = json.Marshal(registry.manifest)
		registry.manifDigest = digest.FromBytes(registry.manifestJSON)

		server := httptest.NewServer(registry)
		defer server.Close()
		host := strings.TrimPrefix(server.URL, "http://")

		opts := GetOptions{
			URI:                   fmt.Sprintf("oci://%s/fallback/chart:1.0.0", host),
			InsecureSkipVerifyTLS: true,
		}

		// 3. This should trigger the "if len(manifest.Layers) == 1" block
		reader, _, err := g.Get(context.Background(), opts)
		if err != nil {
			t.Fatalf("fallback failed: %v", err)
		}

		gr, _ := gzip.NewReader(reader)
		data, _ := io.ReadAll(gr)
		if !bytes.Equal(data, customContent) {
			t.Errorf("fallback content mismatch")
		}
	})
}

// =============================================================================
// Mock OCI Registry for Integration Tests
// =============================================================================

type mockOCIRegistry struct {
	chartData    []byte
	chartDigest  digest.Digest
	manifest     ocispec.Manifest
	manifestJSON []byte
	manifDigest  digest.Digest
	username     string
	password     string
}

func newMockOCIRegistry(chartContent []byte, requireAuth bool) *mockOCIRegistry {
	// Create gzipped chart data
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	gw.Write(chartContent)
	gw.Close()
	chartData := buf.Bytes()

	chartDigest := digest.FromBytes(chartData)

	manifest := ocispec.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ocispec.MediaTypeImageManifest,
		Config: ocispec.Descriptor{
			MediaType: "application/vnd.cncf.helm.config.v1+json",
			Digest:    digest.FromString("{}"),
			Size:      2,
		},
		Layers: []ocispec.Descriptor{
			{
				MediaType: ChartLayerMediaType,
				Digest:    chartDigest,
				Size:      int64(len(chartData)),
			},
		},
	}

	manifestJSON, _ := json.Marshal(manifest)
	manifDigest := digest.FromBytes(manifestJSON)

	m := &mockOCIRegistry{
		chartData:    chartData,
		chartDigest:  chartDigest,
		manifest:     manifest,
		manifestJSON: manifestJSON,
		manifDigest:  manifDigest,
	}

	if requireAuth {
		m.username = "testuser"
		m.password = "testpass"
	}

	return m
}

func (m *mockOCIRegistry) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Check authentication if required
	if m.username != "" {
		user, pass, ok := r.BasicAuth()
		if !ok || user != m.username || pass != m.password {
			w.Header().Set("WWW-Authenticate", `Basic realm="test"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
	}

	path := r.URL.Path

	// Handle tags list: /v2/<name>/tags/list
	if strings.HasSuffix(path, "/tags/list") {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"name":"test/mychart","tags":["1.0.0","0.9.0"]}`))
		return
	}

	// Handle manifest requests: /v2/<name>/manifests/<reference>
	if strings.Contains(path, "/manifests/") {
		w.Header().Set("Content-Type", ocispec.MediaTypeImageManifest)
		w.Header().Set("Docker-Content-Digest", m.manifDigest.String())

		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(m.manifestJSON)))
			w.WriteHeader(http.StatusOK)
			return
		}

		w.WriteHeader(http.StatusOK)
		w.Write(m.manifestJSON)
		return
	}

	// Handle blob requests: /v2/<name>/blobs/<digest>
	if strings.Contains(path, "/blobs/") {
		parts := strings.Split(path, "/blobs/")
		if len(parts) != 2 {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		requestedDigest := parts[1]

		if requestedDigest == m.chartDigest.String() {
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Docker-Content-Digest", m.chartDigest.String())
			w.WriteHeader(http.StatusOK)
			w.Write(m.chartData)
			return
		}

		// Config blob
		if requestedDigest == m.manifest.Config.Digest.String() {
			w.Header().Set("Content-Type", m.manifest.Config.MediaType)
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("{}"))
			return
		}

		w.WriteHeader(http.StatusNotFound)
		return
	}

	// Handle /v2/ ping
	if path == "/v2/" || path == "/v2" {
		w.WriteHeader(http.StatusOK)
		return
	}

	w.WriteHeader(http.StatusNotFound)
}
