package getter

import (
	"log/slog"
	"time"

	"github.com/krateoplatformops/plumbing/helm/getter/cache"
)

type GetOptions struct {
	URI                   string
	Version               string
	Repo                  string
	InsecureSkipVerifyTLS bool
	Username              string
	Password              string
	PassCredentialsAll    bool
	Timeout               time.Duration
	Logging               *slog.Logger
	Cache                 *cache.DiskCache
}

type Option func(*GetOptions) error

func WithLogger(l *slog.Logger) Option {
	return func(o *GetOptions) error {
		o.Logging = l
		return nil
	}
}

func WithVersion(v string) Option {
	return func(o *GetOptions) error {
		o.Version = v
		return nil
	}
}

func WithRepo(r string) Option {
	return func(o *GetOptions) error {
		o.Repo = r
		return nil
	}
}

func WithCredentials(username, password string) Option {
	return func(o *GetOptions) error {
		o.Username = username
		o.Password = password
		return nil
	}
}

func WithPassCredentialsAll(pass bool) Option {
	return func(o *GetOptions) error {
		o.PassCredentialsAll = pass
		return nil
	}
}

func WithInsecureSkipVerifyTLS(skip bool) Option {
	return func(o *GetOptions) error {
		o.InsecureSkipVerifyTLS = skip
		return nil
	}
}

func WithTimeout(t time.Duration) Option {
	return func(o *GetOptions) error {
		o.Timeout = t
		return nil
	}
}

func WithCache(c *cache.DiskCache) Option {
	return func(o *GetOptions) error {
		o.Cache = c
		return nil
	}
}
