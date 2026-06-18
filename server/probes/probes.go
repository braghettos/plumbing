package probes

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"time"
)

const (
	defaultReadyzTimeout = time.Second
)

type Pinger interface {
	Ping(context.Context) error
}

type HealthServer struct {
	srv *http.Server
	log *slog.Logger
}

func LivezHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}
}

func ReadyzHandler(log *slog.Logger, pinger Pinger, timeout time.Duration) http.HandlerFunc {
	if log == nil {
		log = slog.Default()
	}
	if timeout <= 0 {
		timeout = defaultReadyzTimeout
	}

	return func(w http.ResponseWriter, r *http.Request) {
		if pinger == nil {
			log.Error("readiness: pinger is nil")
			http.Error(w, "unreachable", http.StatusServiceUnavailable)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()

		if err := pinger.Ping(ctx); err != nil {
			log.Error("readiness: unreachable", slog.Any("err", err))
			http.Error(w, "unreachable", http.StatusServiceUnavailable)
			return
		}

		w.WriteHeader(http.StatusOK)
	}
}

// Register mounts /livez and /readyz on the provided mux.
func Register(mux *http.ServeMux, log *slog.Logger, pinger Pinger, timeout time.Duration) {
	mux.HandleFunc("/livez", LivezHandler())
	mux.HandleFunc("/readyz", ReadyzHandler(log, pinger, timeout))
}

// New builds an HTTP health server exposing /livez and /readyz.
//
// The readiness probe requires that the Pinger does not return error.
// Liveness always returns 200.
func New(log *slog.Logger, pinger Pinger, port int) *HealthServer {
	if log == nil {
		log = slog.Default()
	}

	mux := http.NewServeMux()
	Register(mux, log, pinger, defaultReadyzTimeout)

	srv := &http.Server{
		Addr:         ":" + strconv.Itoa(port),
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 25 * time.Second,
		IdleTimeout:  15 * time.Second,
	}

	return &HealthServer{srv: srv, log: log}
}

// Start runs the HTTP server in a background goroutine.
func (h *HealthServer) Start() {
	go func() {
		if err := h.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			h.log.Error("health server error", slog.Any("err", err))
		}
	}()
}

// Shutdown gracefully stops the HTTP server.
func (h *HealthServer) Shutdown(ctx context.Context) error {
	return h.srv.Shutdown(ctx)
}
