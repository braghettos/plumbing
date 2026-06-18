package getter

import (
	"context"
	"fmt"
	"io"
	"strings"
)

var _ Getter = (*tgzGetter)(nil)

type tgzGetter struct{}

func (g *tgzGetter) Get(ctx context.Context, opts GetOptions) (io.Reader, string, error) {
	if !isTGZ(opts.URI) {
		return nil, "", fmt.Errorf("%w: is not a valid tgz uri '%s'", ErrInvalidRepoRef, opts.URI)
	}

	dat, err := fetch(ctx, opts)
	if err != nil {
		return nil, "", fmt.Errorf("failed to fetch tgz from uri %s: %w", opts.URI, err)
	}

	return dat, opts.URI, nil
}

func isTGZ(url string) bool {
	return strings.HasSuffix(url, ".tgz") || strings.HasSuffix(url, ".tar.gz")
}
