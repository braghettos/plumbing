package getter

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/krateoplatformops/plumbing/helm/getter/repo"
)

var _ Getter = (*repoGetter)(nil)

type repoGetter struct{}

func (g *repoGetter) Get(ctx context.Context, opts GetOptions) (io.Reader, string, error) {
	if !isHTTP(opts.URI) {
		return nil, "", fmt.Errorf("%w: uri '%s'", ErrInvalidRepoRef, opts.URI)
	}

	bufReader, err := fetch(ctx, GetOptions{
		URI:                   fmt.Sprintf("%s/index.yaml", opts.URI),
		InsecureSkipVerifyTLS: opts.InsecureSkipVerifyTLS,
		Username:              opts.Username,
		Password:              opts.Password,
		PassCredentialsAll:    opts.PassCredentialsAll,
	})
	if err != nil {
		return nil, "", fmt.Errorf("failed to fetch index.yaml from repo: %w", err)
	}

	idx, err := repo.Load(bufReader, opts.URI, opts.Logging)
	if err != nil {
		return nil, "", fmt.Errorf("failed to load index.yaml from repo: %w", err)
	}

	res, err := idx.Get(opts.Repo, opts.Version)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get chart %s@%s from index: %w", opts.Repo, opts.Version, err)
	}
	if len(res.URLs) == 0 {
		return nil, "", fmt.Errorf("no package url found in index @ %s/%s", res.Name, res.Version)
	}

	chartUrlStr := res.URLs[0]
	u, err := url.Parse(chartUrlStr)
	if err != nil {
		return nil, "", fmt.Errorf("invalid chart url: %w", err)
	}

	// If the URL is relative, resolve it against the base repository URI
	if !u.IsAbs() {
		// Use URLJoin helper for proper path joining
		joined, err := repo.URLJoin(opts.URI, chartUrlStr)
		if err != nil {
			return nil, "", fmt.Errorf("failed to join chart URL: %w", err)
		}
		u, err = url.Parse(joined)
		if err != nil {
			return nil, "", fmt.Errorf("invalid joined chart URL: %w", err)
		}
	}

	// Final validation: Ensure we have a valid absolute URI with a scheme
	if u.Scheme == "" || u.Host == "" {
		return nil, "", fmt.Errorf("invalid chart url: %s", u.String())
	}

	newopts := GetOptions{
		URI:                   u.String(),
		Version:               res.Version,
		Repo:                  res.Name,
		InsecureSkipVerifyTLS: opts.InsecureSkipVerifyTLS,
		Username:              opts.Username,
		Password:              opts.Password,
		PassCredentialsAll:    opts.PassCredentialsAll,
	}

	dat, err := fetch(ctx, newopts)
	if err != nil {
		return nil, "", fmt.Errorf("failed to fetch chart from url %s: %w", chartUrlStr, err)
	}

	return dat, newopts.URI, nil
}

func isHTTP(uri string) bool {
	return strings.HasPrefix(uri, "http://") || strings.HasPrefix(uri, "https://")
}
