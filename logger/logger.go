package logger

import (
	"io"
	"log/slog"
	"os"
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
	}

	return slog.NewJSONHandler(writer, opts)
}
