package repo

import (
	"fmt"
	"io"
	"log/slog"

	"go.yaml.in/yaml/v3"
)

// Load loads an index file and does minimal validity checking.
//
// The source parameter is only used for logging.
// This will fail if API Version is not set (ErrNoAPIVersion) or if the decoding fails.
func Load(r io.Reader, source string, log *slog.Logger) (*IndexFile, error) {
	i := &IndexFile{}

	// Create a decoder that reads directly from the stream
	decoder := yaml.NewDecoder(r)

	// Enable Strict mode (equivalent to UnmarshalStrict)
	decoder.KnownFields(true)

	// Decode performs the unmarshal chunk-by-chunk
	if err := decoder.Decode(i); err != nil {
		// Handle EOF (empty file) specifically if needed, otherwise return error
		if err == io.EOF {
			return i, ErrEmptyIndexYaml
		}
		return i, fmt.Errorf("failed to decode index file from %s: %w", source, err)
	}

	// The rest of your logic remains exactly the same...
	for name, cvs := range i.Entries {
		for idx := len(cvs) - 1; idx >= 0; idx-- {
			if cvs[idx] == nil {
				log.Debug("skipping invalid entry", "chart", name, "source", source, "reason", "empty entry")
				continue
			}
			if cvs[idx].APIVersion == "" {
				cvs[idx].APIVersion = APIVersionV1
			}
		}
	}

	i.SortEntries()

	if i.APIVersion == "" {
		return i, ErrNoAPIVersion
	}

	return i, nil
}
