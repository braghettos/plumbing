package request

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/krateo-platformops/plumbing/endpoints"
	"github.com/krateo-platformops/plumbing/ptr"
)

func TestDo(t *testing.T) {
	tests := []struct {
		name           string
		serverHandler  http.HandlerFunc
		opts           RequestOptions
		expectedStatus int
	}{
		{
			name: "Successful GET request with JSON response",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"message": "success"}`))
			},
			opts: RequestOptions{
				RequestInfo: RequestInfo{
					Path: "/test",
					Verb: ptr.To(http.MethodGet),
				},
				Endpoint: &endpoints.Endpoint{ServerURL: "http://example.com"},
			},
			expectedStatus: http.StatusNoContent,
		},
		{
			name: "Non-JSON response should return 406",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/plain")
				w.WriteHeader(http.StatusOK)
			},
			opts: RequestOptions{
				RequestInfo: RequestInfo{
					Path: "/invalid",
					Verb: ptr.To(http.MethodGet),
				},
				Endpoint: &endpoints.Endpoint{ServerURL: "http://example.com"},
			},
			expectedStatus: http.StatusNotAcceptable,
		},
		{
			name: "Server returns 500 error",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			opts: RequestOptions{
				RequestInfo: RequestInfo{
					Path: "/error",
					Verb: ptr.To(http.MethodGet),
				},
				Endpoint: &endpoints.Endpoint{ServerURL: "http://example.com"},
			},
			expectedStatus: http.StatusInternalServerError,
		},
		{
			name: "Request with payload",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				if string(body) != `{"key":"value"}` {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
			},
			opts: RequestOptions{
				RequestInfo: RequestInfo{
					Path:    "/post",
					Verb:    ptr.To(http.MethodPost),
					Payload: ptr.To(`{"key":"value"}`),
				},
				Endpoint: &endpoints.Endpoint{ServerURL: "http://example.com"},
			},
			expectedStatus: http.StatusNoContent,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.serverHandler)
			defer server.Close()

			tt.opts.Endpoint.ServerURL = server.URL

			status := Do(context.Background(), tt.opts)
			if status.Code != tt.expectedStatus {
				t.Errorf("expected status: %d, got: %d", tt.expectedStatus, status.Code)
			}
		})
	}
}
