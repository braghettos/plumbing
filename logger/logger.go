package logger

import (
	"io"
	"log/slog"
	"os"
	"time"
)

func New(serviceName string, debugOn bool) *slog.Logger {
	h := NewHandler(debugOn, os.Stderr)
	log := slog.New(h).With(slog.String("service", serviceName))
	if debugOn {
		log.Debug("environment variables", slog.Any("env", os.Environ()))
	}
	return log
}

func NewHandler(debugOn bool, writer io.Writer) slog.Handler {
	if writer == nil {
		writer = os.Stderr
	}

	logLevel := slog.LevelInfo
	if debugOn {
		logLevel = slog.LevelDebug
	}

	opts := &slog.HandlerOptions{
		Level:     logLevel,
		AddSource: false,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				ts := a.Value.Time().UTC().Format(time.RFC3339Nano)
				return slog.String("timestamp", ts)
			}
			return a
		},
	}

	return slog.NewJSONHandler(writer, opts)
}
