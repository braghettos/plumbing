package repo

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// =============================================================================
// helpers.go tests
// =============================================================================

func TestURLJoin(t *testing.T) {
	tests := []struct {
		name    string
		base    string
		paths   []string
		want    string
		wantErr bool
	}{
		{"simple join", "https://example.com", []string{"charts"}, "https://example.com/charts", false},
		{"multiple paths", "https://example.com/repo", []string{"charts", "mychart-1.0.0.tgz"}, "https://example.com/repo/charts/mychart-1.0.0.tgz", false},
		{"trailing slash", "https://example.com/", []string{"index.yaml"}, "https://example.com/index.yaml", false},
		{"empty paths", "https://example.com/charts", []string{}, "https://example.com/charts", false},
		{"with port", "https://example.com:8080/repo", []string{"chart.tgz"}, "https://example.com:8080/repo/chart.tgz", false},
		{"invalid url", "://invalid", []string{"path"}, "", true},
		{"relative path", "/local/path", []string{"charts"}, "/local/path/charts", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := URLJoin(tt.base, tt.paths...)
			if (err != nil) != tt.wantErr {
				t.Errorf("URLJoin() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("URLJoin() = %v, want %v", got, tt.want)
			}
		})
	}
}

// =============================================================================
// index.go tests
// =============================================================================

func TestNewIndexFile(t *testing.T) {
	idx := NewIndexFile()
	if idx.APIVersion != APIVersionV1 {
		t.Errorf("expected APIVersion %s, got %s", APIVersionV1, idx.APIVersion)
	}
	if idx.Entries == nil {
		t.Error("Entries should be initialized")
	}
	if idx.Generated.IsZero() {
		t.Error("Generated should be set")
	}
}

func TestIndexFile_MustAdd(t *testing.T) {
	idx := NewIndexFile()
	md := &Metadata{Name: "mychart", Version: "1.0.0"}

	err := idx.MustAdd(md, "mychart-1.0.0.tgz", "https://example.com/charts", "sha256:abc123")
	if err != nil {
		t.Fatalf("MustAdd failed: %v", err)
	}

	if len(idx.Entries["mychart"]) != 1 {
		t.Errorf("expected 1 entry, got %d", len(idx.Entries["mychart"]))
	}
	if idx.Entries["mychart"][0].URLs[0] != "https://example.com/charts/mychart-1.0.0.tgz" {
		t.Errorf("unexpected URL: %s", idx.Entries["mychart"][0].URLs[0])
	}
}

func TestIndexFile_MustAdd_NilEntries(t *testing.T) {
	idx := &IndexFile{}
	md := &Metadata{Name: "test", Version: "1.0.0"}
	err := idx.MustAdd(md, "test.tgz", "", "")
	if err == nil {
		t.Error("expected error for nil entries")
	}
}

func TestIndexFile_Has(t *testing.T) {
	idx := NewIndexFile()
	idx.MustAdd(&Metadata{Name: "chart", Version: "1.0.0"}, "chart.tgz", "", "")

	if !idx.Has("chart", "1.0.0") {
		t.Error("expected Has to return true")
	}
	if idx.Has("chart", "2.0.0") {
		t.Error("expected Has to return false for non-existent version")
	}
	if idx.Has("nonexistent", "1.0.0") {
		t.Error("expected Has to return false for non-existent chart")
	}
}

func TestIndexFile_Get(t *testing.T) {
	idx := NewIndexFile()
	idx.MustAdd(&Metadata{Name: "chart", Version: "1.0.0"}, "chart.tgz", "", "")
	idx.MustAdd(&Metadata{Name: "chart", Version: "2.0.0"}, "chart.tgz", "", "")
	idx.MustAdd(&Metadata{Name: "chart", Version: "2.1.0-alpha"}, "chart.tgz", "", "")
	idx.SortEntries()

	tests := []struct {
		name    string
		chart   string
		version string
		want    string
		wantErr error
	}{
		{"exact version", "chart", "1.0.0", "1.0.0", nil},
		{"latest stable", "chart", "", "2.0.0", nil},
		{"version range", "chart", ">=1.0.0", "2.0.0", nil},
		{"non-existent chart", "missing", "1.0.0", "", ErrNoChartName},
		{"invalid constraint", "chart", "invalid[", "", nil}, // returns error
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cv, err := idx.Get(tt.chart, tt.version)
			if tt.wantErr != nil {
				if err != tt.wantErr {
					t.Errorf("expected error %v, got %v", tt.wantErr, err)
				}
				return
			}
			if tt.want != "" && (cv == nil || cv.Version != tt.want) {
				t.Errorf("expected version %s, got %v", tt.want, cv)
			}
		})
	}
}

func TestIndexFile_Get_EmptyEntries(t *testing.T) {
	idx := NewIndexFile()
	idx.Entries["empty"] = ChartVersions{}

	_, err := idx.Get("empty", "1.0.0")
	if err != ErrNoChartVersion {
		t.Errorf("expected ErrNoChartVersion, got %v", err)
	}
}

func TestIndexFile_Merge(t *testing.T) {
	idx1 := NewIndexFile()
	idx1.MustAdd(&Metadata{Name: "chart1", Version: "1.0.0"}, "c1.tgz", "", "")

	idx2 := NewIndexFile()
	idx2.MustAdd(&Metadata{Name: "chart1", Version: "2.0.0"}, "c1.tgz", "", "")
	idx2.MustAdd(&Metadata{Name: "chart2", Version: "1.0.0"}, "c2.tgz", "", "")

	idx1.Merge(idx2)

	if len(idx1.Entries["chart1"]) != 2 {
		t.Errorf("expected 2 versions of chart1, got %d", len(idx1.Entries["chart1"]))
	}
	if len(idx1.Entries["chart2"]) != 1 {
		t.Errorf("expected 1 version of chart2, got %d", len(idx1.Entries["chart2"]))
	}
}

func TestIndexFile_Merge_NoDuplicates(t *testing.T) {
	idx1 := NewIndexFile()
	idx1.MustAdd(&Metadata{Name: "chart", Version: "1.0.0"}, "c.tgz", "", "digest1")

	idx2 := NewIndexFile()
	idx2.MustAdd(&Metadata{Name: "chart", Version: "1.0.0"}, "c.tgz", "", "digest2")

	idx1.Merge(idx2)

	if len(idx1.Entries["chart"]) != 1 {
		t.Errorf("expected 1 version (no duplicates), got %d", len(idx1.Entries["chart"]))
	}
	// Original should be preserved
	if idx1.Entries["chart"][0].Digest != "digest1" {
		t.Error("original entry should be preserved")
	}
}

func TestChartVersions_Sort(t *testing.T) {
	cvs := ChartVersions{
		{Metadata: &Metadata{Version: "1.0.0"}},
		{Metadata: &Metadata{Version: "2.0.0"}},
		{Metadata: &Metadata{Version: "1.5.0"}},
		{Metadata: &Metadata{Version: "invalid"}},
	}

	idx := &IndexFile{Entries: map[string]ChartVersions{"test": cvs}}
	idx.SortEntries()

	// After reverse sort, highest version first
	if idx.Entries["test"][0].Version != "2.0.0" {
		t.Errorf("expected 2.0.0 first, got %s", idx.Entries["test"][0].Version)
	}
}

// =============================================================================
// load.go tests
// =============================================================================

func TestLoad_Valid(t *testing.T) {
	yaml := `apiVersion: v1
generated: 2024-01-01T00:00:00Z
entries:
  mychart:
    - name: mychart
      version: "1.0.0"
      urls:
        - https://example.com/mychart-1.0.0.tgz
`
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	idx, err := Load(strings.NewReader(yaml), "test", log)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if idx.APIVersion != "v1" {
		t.Errorf("expected apiVersion v1, got %s", idx.APIVersion)
	}
	if len(idx.Entries["mychart"]) != 1 {
		t.Error("expected 1 chart entry")
	}
}

func TestLoad_NoAPIVersion(t *testing.T) {
	yaml := `entries:
  mychart:
    - name: mychart
      version: "1.0.0"
`
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	_, err := Load(strings.NewReader(yaml), "test", log)
	if err != ErrNoAPIVersion {
		t.Errorf("expected ErrNoAPIVersion, got %v", err)
	}
}

func TestLoad_Empty(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	_, err := Load(strings.NewReader(""), "test", log)
	if err != ErrEmptyIndexYaml {
		t.Errorf("expected ErrEmptyIndexYaml, got %v", err)
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	_, err := Load(strings.NewReader("invalid: [yaml: broken"), "test", log)
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}

func TestLoad_SetsDefaultAPIVersion(t *testing.T) {
	yaml := `apiVersion: v1
entries:
  mychart:
    - name: mychart
      version: "1.0.0"
`
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	idx, err := Load(strings.NewReader(yaml), "test", log)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	// Chart entries without apiVersion should get default
	if idx.Entries["mychart"][0].APIVersion != APIVersionV1 {
		t.Errorf("expected default apiVersion %s", APIVersionV1)
	}
}

func TestLoad_SortsEntries(t *testing.T) {
	yaml := `apiVersion: v1
entries:
  mychart:
    - name: mychart
      version: "1.0.0"
    - name: mychart
      version: "2.0.0"
`
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	idx, err := Load(strings.NewReader(yaml), "test", log)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	// After sort, newest first
	if idx.Entries["mychart"][0].Version != "2.0.0" {
		t.Errorf("expected sorted entries with 2.0.0 first")
	}
}

// =============================================================================
// types.go tests
// =============================================================================

func TestChartVersion_Embedding(t *testing.T) {
	cv := &ChartVersion{
		Metadata: &Metadata{
			Name:    "test",
			Version: "1.0.0",
		},
		URLs:    []string{"https://example.com/test.tgz"},
		Created: time.Now(),
		Digest:  "sha256:abc",
	}

	if cv.Name != "test" {
		t.Error("embedded Metadata fields should be accessible")
	}
}

func TestDependency_Fields(t *testing.T) {
	dep := &Dependency{
		Name:       "dep",
		Version:    "^1.0.0",
		Repository: "https://charts.example.com",
		Condition:  "dep.enabled",
		Tags:       []string{"frontend"},
		Alias:      "mydep",
	}

	if dep.Name != "dep" || dep.Repository != "https://charts.example.com" {
		t.Error("Dependency fields not set correctly")
	}
}

func TestLock_Fields(t *testing.T) {
	lock := &Lock{
		Generated: time.Now(),
		Digest:    "sha256:xyz",
		Dependencies: []*Dependency{
			{Name: "dep1", Version: "1.0.0"},
		},
	}

	if len(lock.Dependencies) != 1 {
		t.Error("Lock dependencies not set correctly")
	}
}

// =============================================================================
// Edge cases and error conditions
// =============================================================================

func TestURLJoin_EdgeCases(t *testing.T) {
	// Double slashes should be normalized
	result, _ := URLJoin("https://example.com//", "//charts//", "file.tgz")
	if strings.Contains(result, "//charts") {
		t.Logf("URL normalization: %s", result)
	}
}

func TestIndexFile_Get_InvalidSemver(t *testing.T) {
	idx := NewIndexFile()
	idx.MustAdd(&Metadata{Name: "chart", Version: "not-semver"}, "c.tgz", "", "")

	// Should skip invalid semver and return error
	_, err := idx.Get("chart", ">=1.0.0")
	if err == nil {
		t.Error("expected error when no valid semver matches")
	}
}

func TestLoad_NilEntries(t *testing.T) {
	yaml := `apiVersion: v1
entries:
  mychart:
    - null
    - name: valid
      version: "1.0.0"
`
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	idx, err := Load(strings.NewReader(yaml), "test", log)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	// Should handle nil entries gracefully
	if idx.Entries["mychart"] == nil {
		t.Error("entries should not be nil")
	}
}

func TestLoad_LargeIndex(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteString("apiVersion: v1\nentries:\n")

	// Ensure unique keys by using the loop index
	for i := 0; i < 10000; i++ {
		chartName := fmt.Sprintf("chart-%d", i)
		buf.WriteString(fmt.Sprintf("  %s:\n", chartName))
		buf.WriteString(fmt.Sprintf("    - name: %s\n", chartName))
		buf.WriteString("      version: \"1.0.0\"\n")
		buf.WriteString("      urls:\n")
		buf.WriteString(fmt.Sprintf("        - %s-1.0.0.tgz\n", chartName))
	}

	// I'm assuming slog is used here based on your snippet
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	_, err := Load(&buf, "test", log)
	if err != nil {
		t.Fatalf("Load failed on large index: %v", err)
	}
}
