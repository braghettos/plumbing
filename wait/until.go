package wait

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

const (
	defaultBackoff = time.Second
	defaultMaxBack = 30 * time.Second
)

type Options struct {
	Logger         *slog.Logger
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
}

func Until[T any](
	ctx context.Context,
	log *slog.Logger,
	check func(context.Context) (T, error),
) (T, error) {
	return UntilWithOptions(ctx, check, Options{Logger: log})
}

func UntilWithOptions[T any](
	ctx context.Context,
	check func(context.Context) (T, error),
	opts Options,
) (T, error) {
	var zero T
	if check == nil {
		return zero, fmt.Errorf("nil check func")
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.InitialBackoff <= 0 {
		opts.InitialBackoff = defaultBackoff
	}
	if opts.MaxBackoff <= 0 {
		opts.MaxBackoff = defaultMaxBack
	}
	if opts.MaxBackoff < opts.InitialBackoff {
		opts.MaxBackoff = opts.InitialBackoff
	}

	backoff := opts.InitialBackoff
	for {
		value, err := check(ctx)
		if err == nil {
			return value, nil
		}

		opts.Logger.Debug("condition not ready yet. retrying",
			slog.Any("err", err),
			slog.String("wait", backoff.String()),
			func() slog.Attr {
				if deadline, ok := ctx.Deadline(); ok {
					return slog.String("time_remaining", time.Until(deadline).String())
				}
				return slog.Any("time_remaining", "unknown")
			}(),
		)

		select {
		case <-time.After(backoff):
			backoff *= 2
			if backoff > opts.MaxBackoff {
				backoff = opts.MaxBackoff
			}
		case <-ctx.Done():
			return zero, ctx.Err()
		}
	}
}
