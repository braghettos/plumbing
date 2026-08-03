package use

import (
	"net/http"
	"strings"

	"log/slog"

	xcontext "github.com/krateo-platformops/plumbing/context"
	"github.com/krateo-platformops/plumbing/jwtutil"
	"github.com/krateo-platformops/plumbing/shortid"
)

func Logger(root *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		fn := func(wri http.ResponseWriter, req *http.Request) {
			traceId := req.Header.Get("X-Krateo-TraceId")
			if len(traceId) == 0 {
				traceId = shortid.MustGenerate()
			}

			var (
				sub  string
				orgs string
			)

			authHeader := req.Header.Get("Authorization")
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) == 2 {
				userInfo, err := jwtutil.ExtractUserInfo(parts[1])
				if err == nil {
					sub = userInfo.Username
					orgs = strings.Join(userInfo.Groups, ",")
				}
			}

			log := root
			if len(sub) > 0 {
				log = root.With("traceId", traceId,
					slog.Group("user",
						slog.String("name", sub),
						slog.String("groups", orgs)),
				)
			}

			ctx := xcontext.BuildContext(req.Context(),
				xcontext.WithTraceId(traceId),
				xcontext.WithLogger(log),
			)

			next.ServeHTTP(wri, req.WithContext(ctx))
		}

		return http.HandlerFunc(fn)
	}
}
