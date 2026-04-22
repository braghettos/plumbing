package logger

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"
)

func TestNewHandlerWritesStructuredOutput(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	log := slog.New(NewHandler(false, &buf))
	log.Info("hello", slog.String("traceId", "trace-123"))

	var payload map[string]any
	if err := json.Unmarshal(buf.Bytes(), &payload); err != nil {
		t.Fatalf("expected valid JSON log line, got %q: %v", buf.String(), err)
	}
	if got, want := payload["msg"], "hello"; got != want {
		t.Fatalf("unexpected msg: got %v want %v", got, want)
	}
	if got, want := payload["traceId"], "trace-123"; got != want {
		t.Fatalf("unexpected traceId: got %v want %v", got, want)
	}
}
