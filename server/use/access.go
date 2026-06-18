package use

import (
	"log/slog"
	"net/http"
	"time"
)

func Access(l *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			ip := r.RemoteAddr
			if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
				ip = forwarded
			}

			next.ServeHTTP(w, r)

			l.Info("http request",
				slog.String("ip", ip),
				slog.String("method", r.Method),
				slog.String("url", r.URL.String()),
				slog.String("user_agent", r.UserAgent()),
				slog.String("latency", time.Since(start).String()),
			)
		})
	}
}
