package context

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/krateo-platformops/plumbing/endpoints"
	"github.com/krateo-platformops/plumbing/env"
	"github.com/krateo-platformops/plumbing/jwtutil"
	"github.com/krateo-platformops/plumbing/shortid"
)

const (
	Trace uint = 1 << iota // 1 << 0 = 1
	Debug                  // 1 << 1 = 2
)

const (
	LabelKrateoTraceId = "X-Krateo-TraceId"
)

func Logger(ctx context.Context) *slog.Logger {
	log, ok := ctx.Value(contextKeyLogger).(*slog.Logger)
	if !ok {
		log = slog.New(slog.NewJSONHandler(os.Stderr,
			&slog.HandlerOptions{Level: slog.LevelDebug})).
			With("traceId", TraceId(ctx, false))
	}

	return log
}

func TraceId(ctx context.Context, generate bool) string {
	traceId, ok := ctx.Value(contextKeyTraceId).(string)
	if ok {
		return traceId
	}

	if generate {
		traceId = shortid.MustGenerate()
	}

	return traceId
}

func UserConfig(ctx context.Context) (endpoints.Endpoint, error) {
	ep, ok := ctx.Value(contextKeyUserConfig).(endpoints.Endpoint)
	if !ok {
		return endpoints.Endpoint{}, fmt.Errorf("user *Endpoint not found in context")
	}
	if !env.TestMode() {
		ep.ServerURL = "https://kubernetes.default.svc"
	}
	return ep, nil
}

func UserInfo(ctx context.Context) (jwtutil.UserInfo, error) {
	ui, ok := ctx.Value(contextKeyUserInfo).(jwtutil.UserInfo)
	if !ok {
		return jwtutil.UserInfo{}, fmt.Errorf("user info not found in context")
	}

	return ui, nil
}

func AccessToken(ctx context.Context) (string, bool) {
	ui, ok := ctx.Value(contextKeyAccessToken).(string)
	return ui, ok
}

func WithTraceId(traceId string) WithContextFunc {
	return func(ctx context.Context) context.Context {
		return context.WithValue(ctx, contextKeyTraceId, traceId)
	}
}

func WithLogger(root *slog.Logger) WithContextFunc {
	return func(ctx context.Context) context.Context {
		if root == nil {
			logLevel := slog.LevelInfo
			if env.True("DEBUG") {
				logLevel = slog.LevelDebug
			}
			root = slog.New(slog.NewJSONHandler(os.Stderr,
				&slog.HandlerOptions{Level: logLevel}))
		}

		return context.WithValue(ctx, contextKeyLogger,
			root.With("traceId", TraceId(ctx, false)))
	}
}

func WithUserConfig(ep endpoints.Endpoint) WithContextFunc {
	return func(ctx context.Context) context.Context {
		return context.WithValue(ctx, contextKeyUserConfig, ep)
	}
}

func WithUserInfo(ui jwtutil.UserInfo) WithContextFunc {
	return func(ctx context.Context) context.Context {
		return context.WithValue(ctx, contextKeyUserInfo, ui)
	}
}

func WithAccessToken(tok string) WithContextFunc {
	return func(ctx context.Context) context.Context {
		return context.WithValue(ctx, contextKeyAccessToken, tok)
	}
}

func BuildContext(ctx context.Context, opts ...WithContextFunc) context.Context {
	for _, fn := range opts {
		ctx = fn(ctx)
	}

	return ctx
}

type WithContextFunc func(context.Context) context.Context

type contextKey string

func (c contextKey) String() string {
	return "snowplow." + string(c)
}

var (
	contextKeyTraceId     = contextKey("traceId")
	contextKeyLogger      = contextKey("logger")
	contextKeyUserConfig  = contextKey("userConfig")
	contextKeyUserInfo    = contextKey("userInfo")
	contextKeyJQ          = contextKey("jq")
	contextKeyAuthnNS     = contextKey("authnNS")
	contextKeyAccessToken = contextKey("accessToken")
)
