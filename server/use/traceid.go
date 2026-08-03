package use

import (
	"net/http"

	xcontext "github.com/krateo-platformops/plumbing/context"
	"github.com/krateo-platformops/plumbing/shortid"
)

func TraceId() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		fn := func(wri http.ResponseWriter, req *http.Request) {

			traceId := req.Header.Get(xcontext.LabelKrateoTraceId)
			if len(traceId) == 0 {
				traceId = shortid.MustGenerate()
			}
			req.Header.Set(xcontext.LabelKrateoTraceId, traceId)

			ctx := xcontext.BuildContext(req.Context(),
				xcontext.WithTraceId(traceId),
			)

			next.ServeHTTP(wri, req.WithContext(ctx))
		}

		return http.HandlerFunc(fn)
	}
}
