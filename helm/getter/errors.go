package getter

import (
	"errors"
)

var (
	// ErrNoHandler is returned when no suitable getter is found for the URL scheme.
	ErrNoHandler = errors.New("no handler found for url")

	// ErrFetchFailed is a generic error for download failures.
	ErrFetchFailed = errors.New("fetch failed")

	// ErrInvalidRepoRef is returned when a repo URL is malformed.
	ErrInvalidRepoRef = errors.New("invalid repo reference")
)
