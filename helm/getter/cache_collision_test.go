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
	"sync/atomic"
	"testing"

	"github.com/krateo-platformops/plumbing/helm/getter/cache"
	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// TestGet_CacheKey_DifferentRepoIndependent is a hermetic regression test for
// the cache key bug: two charts that share URI+Version but differ in Repo must
// get independent cache entries. We pre-populate both via the same encoding
// Get() uses internally, then verify each Get() returns its own bytes.
func TestGet_CacheKey_DifferentRepoIndependent(t *testing.T) {
	dir := t.TempDir()
	c, err := cache.NewDiskCache(cache.WithDir(dir))
	if err != nil {
		t.Fatalf("create cache: %v", err)
	}
	defer c.Stop()

	const (
		sharedURI = "https://registry.example.com"
		version   = "1.0.0"
	)
	chartA := []byte("CHART-A-bytes")
	chartB := []byte("CHART-B-bytes")

	if err := c.Set(cacheKey(sharedURI, "chart-a"), version, bytes.NewReader(chartA)); err != nil {
		t.Fatalf("cache.Set chart-a: %v", err)
	}
	if err := c.Set(cacheKey(sharedURI, "chart-b"), version, bytes.NewReader(chartB)); err != nil {
		t.Fatalf("cache.Set chart-b: %v", err)
	}

	gotA, err := readGet(t, sharedURI, "chart-a", version, c)
	if err != nil {
		t.Fatalf("Get chart-a: %v", err)
	}
	if !bytes.Equal(gotA, chartA) {
		t.Errorf("chart-a content mismatch: got %q want %q", gotA, chartA)
	}

	gotB, err := readGet(t, sharedURI, "chart-b", version, c)
	if err != nil {
		t.Fatalf("Get chart-b: %v", err)
	}
	if !bytes.Equal(gotB, chartB) {
		t.Errorf("chart-b content mismatch: got %q want %q", gotB, chartB)
	}
	if bytes.Equal(gotB, chartA) {
		t.Fatalf("REGRESSION: chart-b request returned chart-a bytes — cache key collision is back")
	}
}

// readGet runs Get with the cache and returns the raw bytes from the reader,
// without any gzip handling (used by the hermetic test where pre-populated
// content is raw).
func readGet(t *testing.T, uri, repo, version string, c *cache.DiskCache) ([]byte, error) {
	t.Helper()
	r, _, err := Get(context.Background(), uri,
		WithRepo(repo),
		WithVersion(version),
		WithCache(c),
	)
	if err != nil {
		return nil, err
	}
	defer closeIfCloser(r)
	return io.ReadAll(r)
}

// TestGet_OCI_CacheCollision_ReturnsWrongChart exercises the full OCI fetch
// path with the cache enabled. Two distinct charts share the same registry
// URI and version but differ in Repo. The cache pollutes the second request
// with the first chart's content.
func TestGet_OCI_CacheCollision_ReturnsWrongChart(t *testing.T) {
	chartA := []byte("CHART-A-content-zzz")
	chartB := []byte("CHART-B-content-yyy")

	reg := newMultiChartOCIRegistry(map[string][]byte{
		"team/chart-a": chartA,
		"team/chart-b": chartB,
	})
	server := httptest.NewServer(reg)
	defer server.Close()
	host := strings.TrimPrefix(server.URL, "http://")

	dir := t.TempDir()
	c, err := cache.NewDiskCache(cache.WithDir(dir))
	if err != nil {
		t.Fatalf("create cache: %v", err)
	}
	defer c.Stop()

	sharedURI := "oci://" + host
	version := "1.0.0"

	// First request: chart A. Populates the cache at key (sharedURI, version).
	gotA, err := fetchAndDecompress(t, sharedURI, "team/chart-a", version, c)
	if err != nil {
		t.Fatalf("fetch chart A: %v", err)
	}
	if !bytes.Equal(gotA, chartA) {
		t.Fatalf("chart A mismatch on first fetch: got %q want %q", gotA, chartA)
	}

	// Second request: chart B. Should fetch chart B, but the cache lookup
	// uses (sharedURI, version) only and returns chart A's bytes.
	gotB, err := fetchAndDecompress(t, sharedURI, "team/chart-b", version, c)
	if err != nil {
		t.Fatalf("fetch chart B: %v", err)
	}
	if bytes.Equal(gotB, chartA) {
		t.Fatalf("BUG: requested chart B but got chart A bytes — cache key (URI, Version) ignores Repo")
	}
	if !bytes.Equal(gotB, chartB) {
		t.Fatalf("chart B mismatch: got %q want %q", gotB, chartB)
	}
}

// TestGet_OCI_ConcurrentMixedCharts_NoCrossContamination spawns N goroutines
// requesting two different charts at the same registry URI. With the cache
// key bug the first writer wins and most subsequent goroutines receive the
// wrong chart — reproducing the production symptom of an error referring to
// an unrelated chart.
func TestGet_OCI_ConcurrentMixedCharts_NoCrossContamination(t *testing.T) {
	chartA := []byte("CHART-A-content-aaa")
	chartB := []byte("CHART-B-content-bbb")

	reg := newMultiChartOCIRegistry(map[string][]byte{
		"team/chart-a": chartA,
		"team/chart-b": chartB,
	})
	server := httptest.NewServer(reg)
	defer server.Close()
	host := strings.TrimPrefix(server.URL, "http://")

	dir := t.TempDir()
	c, err := cache.NewDiskCache(cache.WithDir(dir))
	if err != nil {
		t.Fatalf("create cache: %v", err)
	}
	defer c.Stop()

	sharedURI := "oci://" + host
	version := "1.0.0"

	const N = 40
	var wg sync.WaitGroup
	var contamination atomic.Int32
	var fetchErrors atomic.Int32

	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			var repo string
			var expected []byte
			if idx%2 == 0 {
				repo = "team/chart-a"
				expected = chartA
			} else {
				repo = "team/chart-b"
				expected = chartB
			}

			got, err := fetchAndDecompress(t, sharedURI, repo, version, c)
			if err != nil {
				fetchErrors.Add(1)
				t.Logf("goroutine %d (%s): fetch error: %v", idx, repo, err)
				return
			}
			if !bytes.Equal(got, expected) {
				contamination.Add(1)
				t.Logf("goroutine %d requested %s but received wrong bytes (len=%d): %q",
					idx, repo, len(got), got)
			}
		}(i)
	}
	wg.Wait()

	if fetchErrors.Load() > 0 {
		t.Errorf("%d/%d goroutines failed to fetch", fetchErrors.Load(), N)
	}
	if contamination.Load() > 0 {
		t.Fatalf("BUG: %d/%d goroutines received the wrong chart due to cache key collision",
			contamination.Load(), N)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func closeIfCloser(r io.Reader) {
	if c, ok := r.(io.Closer); ok {
		_ = c.Close()
	}
}

// fetchAndDecompress runs Get and returns the un-gzipped chart bytes if the
// payload is gzipped, otherwise returns the raw bytes (handy for the hermetic
// unit test that pre-populates the cache with non-gzipped data).
func fetchAndDecompress(t *testing.T, uri, repo, version string, c *cache.DiskCache) ([]byte, error) {
	t.Helper()
	reader, _, err := Get(context.Background(), uri,
		WithRepo(repo),
		WithVersion(version),
		WithCache(c),
	)
	if err != nil {
		return nil, err
	}
	defer closeIfCloser(reader)

	all, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	gz, err := gzip.NewReader(bytes.NewReader(all))
	if err != nil {
		return all, nil
	}
	out, err := io.ReadAll(gz)
	if err != nil {
		return nil, fmt.Errorf("gunzip: %w", err)
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Multi-chart OCI mock registry
// ---------------------------------------------------------------------------

type chartArtifact struct {
	chartData    []byte // gzipped chart content as served by the registry
	chartDigest  digest.Digest
	manifestJSON []byte
	manifDigest  digest.Digest
}

type multiChartOCIRegistry struct {
	charts map[string]chartArtifact // name (e.g. "team/chart-a") -> artifact
}

func newMultiChartOCIRegistry(charts map[string][]byte) *multiChartOCIRegistry {
	out := &multiChartOCIRegistry{charts: make(map[string]chartArtifact)}

	for name, content := range charts {
		var buf bytes.Buffer
		gw := gzip.NewWriter(&buf)
		gw.Write(content)
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
		out.charts[name] = chartArtifact{
			chartData:    chartData,
			chartDigest:  chartDigest,
			manifestJSON: manifestJSON,
			manifDigest:  digest.FromBytes(manifestJSON),
		}
	}
	return out
}

func (m *multiChartOCIRegistry) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	if path == "/v2/" || path == "/v2" {
		w.WriteHeader(http.StatusOK)
		return
	}

	if !strings.HasPrefix(path, "/v2/") {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	rest := strings.TrimPrefix(path, "/v2/")

	if strings.HasSuffix(rest, "/tags/list") {
		name := strings.TrimSuffix(rest, "/tags/list")
		if _, ok := m.charts[name]; !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"name":%q,"tags":["1.0.0"]}`, name)
		return
	}

	if strings.Contains(rest, "/manifests/") {
		parts := strings.SplitN(rest, "/manifests/", 2)
		name := parts[0]
		art, ok := m.charts[name]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", ocispec.MediaTypeImageManifest)
		w.Header().Set("Docker-Content-Digest", art.manifDigest.String())
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(art.manifestJSON)))
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write(art.manifestJSON)
		return
	}

	if strings.Contains(rest, "/blobs/") {
		parts := strings.SplitN(rest, "/blobs/", 2)
		name := parts[0]
		wantDigest := parts[1]
		art, ok := m.charts[name]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if wantDigest == art.chartDigest.String() {
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Docker-Content-Digest", art.chartDigest.String())
			w.WriteHeader(http.StatusOK)
			w.Write(art.chartData)
			return
		}
		if wantDigest == digest.FromString("{}").String() {
			w.Header().Set("Content-Type", "application/vnd.cncf.helm.config.v1+json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("{}"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusNotFound)
}
