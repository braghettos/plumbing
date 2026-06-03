package tracer

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
)

type Resource struct {
	Group     string `json:"group"`
	Version   string `json:"version"`
	Resource  string `json:"resource"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

// Tracer implements http.RoundTripper.  It prints each request and
// response/error to t.OutFile.  WARNING: this may output sensitive information
// including bearer tokens.
type Tracer struct {
	http.RoundTripper
	resources []Resource
	verbose   bool
	context   context.Context
	writer    io.Writer
	logger    *slog.Logger
}

func NewTracer(ctx context.Context, verbose bool) *Tracer {
	return &Tracer{
		verbose: verbose,
		context: ctx,
	}
}

func (t *Tracer) WithRoundTripper(rt http.RoundTripper) *Tracer {
	t.RoundTripper = rt
	return t
}

func (t *Tracer) WithWriter(w io.Writer) *Tracer {
	t.writer = w
	return t
}

// WithLogger sets a custom slog.Logger for structured request logs.
// When unset, the tracer falls back to slog.Default().
func (t *Tracer) WithLogger(l *slog.Logger) *Tracer {
	t.logger = l
	return t
}

func (t *Tracer) GetResources() []Resource {
	return t.resources
}

// RoundTrip implements the http.RoundTripper interface.
// It dumps each request different from GET and its response using the context logger provided.
func (t *Tracer) RoundTrip(req *http.Request) (resp *http.Response, err error) {

	reqBody := ""
	if t.verbose && req.Body != nil { // We need to read the body to log it because t.RoundTripper.RoundTrip may consume it.
		b, err := io.ReadAll(req.Body)
		if err == nil {
			reqBody = string(b)
			// Reconstruct the Body since it was read.
			req.Body = io.NopCloser(strings.NewReader(reqBody))
		}
	}

	// Call the nested RoundTripper.
	resp, err = t.RoundTripper.RoundTrip(req)
	// If an error was returned, dump it to t.OutFile.
	if err != nil {
		return resp, err
	}
	if true {
		split := strings.Split(req.URL.Path, "/")

		if len(split) > 2 {
			if len(split) == 8 && (split[1] == "apis" || split[1] == "api") && split[4] == "namespaces" {
				t.resources = append(t.resources, Resource{
					Group:     split[2],
					Version:   split[3],
					Resource:  split[6],
					Namespace: split[5],
					Name:      split[7],
				})
			} else if len(split) == 7 && (split[1] == "apis" || split[1] == "api") && split[3] == "namespaces" {
				t.resources = append(t.resources, Resource{
					Group:     "",
					Version:   split[2],
					Resource:  split[5],
					Namespace: split[4],
					Name:      split[6],
				})
			} else if len(split) == 6 && (split[1] == "apis" || split[1] == "api") {
				t.resources = append(t.resources, Resource{
					Group:     split[2],
					Version:   split[3],
					Resource:  split[4],
					Namespace: "",
					Name:      split[5],
				})
			} else if len(split) == 5 && (split[1] == "apis" || split[1] == "api") {
				t.resources = append(t.resources, Resource{
					Group:     "",
					Version:   split[2],
					Resource:  split[3],
					Namespace: "",
					Name:      split[4],
				})
			}
		}

		if t.verbose {
			respBody := ""
			if resp.Body != nil {
				b, err := io.ReadAll(resp.Body)
				if err == nil {
					respBody = string(b)
					// Reconstruct the Body since it was read.
					resp.Body = io.NopCloser(strings.NewReader(respBody))
				}
			}

			log := t.logger
			if log == nil {
				log = slog.Default()
			}
			log.With(
				"op", "http-tracer",
				"method", req.Method,
				"url", req.URL.String(),
				"content-type", req.Header.Get("Content-Type"),
				"content-length", req.Header.Get("Content-Length"),
				"accept-encoding", req.Header.Get("Accept"),
				"authorization", "redacted",
				"requestBody", reqBody,
				"responseStatus", resp.Status,
				"responseBody", respBody,
			).DebugContext(t.context, "HTTP Request traced")

			if t.writer != nil {
				fmt.Fprintf(t.writer, "--- HTTP Request ---\n")
				fmt.Fprintf(t.writer, "Method: %s\n", req.Method)
				fmt.Fprintf(t.writer, "URL: %s\n", req.URL.String())
				fmt.Fprintf(t.writer, "Request Body: %s\n", reqBody)
				fmt.Fprintf(t.writer, "Response Status: %s\n", resp.Status)
				fmt.Fprintf(t.writer, "Response Body: %s\n", respBody)
				fmt.Fprintf(t.writer, "--------------------\n\n")
			}
		}
	}

	return resp, err
}
