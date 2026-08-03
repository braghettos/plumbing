package kubeconfig

import (
	"log/slog"
	"net/http"
	"net/http/httputil"

	xcontext "github.com/krateo-platformops/plumbing/context"
	"k8s.io/client-go/transport"
)

func newDebuggingRoundTripper(log *slog.Logger, traceId string, verbose bool) transport.WrapperFunc {
	return func(rt http.RoundTripper) http.RoundTripper {
		return &debuggingRoundTripper{
			delegatedRoundTripper: rt,
			traceId:               traceId,
			log:                   log,
			verbose:               verbose,
		}
	}
}

type debuggingRoundTripper struct {
	delegatedRoundTripper http.RoundTripper
	traceId               string
	log                   *slog.Logger
	verbose               bool
}

func (rt *debuggingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	traceId := req.Header.Get(xcontext.LabelKrateoTraceId)
	if len(traceId) == 0 {
		traceId = rt.traceId // xcontext.TraceId(req.Context(), true)
	}
	req.Header.Set(xcontext.LabelKrateoTraceId, traceId)

	b, err := httputil.DumpRequestOut(req, rt.verbose)
	if err != nil {
		return nil, err
	}
	rt.log.Debug("request details", "wire", string(b))

	resp, err := rt.delegatedRoundTripper.RoundTrip(req)
	if err != nil {
		return resp, err
	}

	withBody := req.URL.Query().Get("watch") != "true"
	withBody = withBody && rt.verbose

	b, err = httputil.DumpResponse(resp, withBody)
	if err != nil {
		return nil, err
	}
	rt.log.Debug("response details", "wire", string(b))

	return resp, err
}
