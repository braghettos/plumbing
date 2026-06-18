package use

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestAccessLog(t *testing.T) {
	// Logger semplice per catturare output in test
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil)).
		With(slog.String("service", "test"))

	mux := http.NewServeMux()

	// Endpoint SSE di esempio
	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}

		for i := 0; i < 2; i++ {
			_, _ = w.Write([]byte("data: hello world\n\n"))
			flusher.Flush()
			time.Sleep(100 * time.Millisecond)
		}
	})

	// Endpoint normale
	mux.HandleFunc("/hello", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Custom-Header", "example")
		w.Write([]byte("hello world"))
	})

	// Applichiamo middleware
	handler := Access(log)(mux)

	// Creiamo server di test
	ts := httptest.NewServer(handler)
	defer ts.Close()

	// Test endpoint HTTP normale
	resp, err := http.Get(ts.URL + "/hello")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK, got %d", resp.StatusCode)
	}

	// Test SSE endpoint
	req, _ := http.NewRequest("GET", ts.URL+"/events", nil)
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK for SSE, got %d", resp2.StatusCode)
	}
}
