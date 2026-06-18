package pgutil

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/krateoplatformops/plumbing/wait"
)

func WaitForPostgres(ctx context.Context, log *slog.Logger, dbURL string) (*pgxpool.Pool, error) {
	return WaitForPostgresWithOptions(ctx, dbURL, wait.Options{Logger: log})
}

func WaitForPostgresWithOptions(
	ctx context.Context,
	dbURL string,
	opts wait.Options,
) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		return nil, fmt.Errorf("invalid db config: %w", err)
	}

	return wait.UntilWithOptions(ctx,
		func(ctx context.Context) (*pgxpool.Pool, error) {
			pool, err := pgxpool.NewWithConfig(ctx, cfg)
			if err != nil {
				return nil, err
			}
			if err := pool.Ping(ctx); err != nil {
				pool.Close()
				return nil, err
			}
			return pool, nil
		},
		opts)
}
