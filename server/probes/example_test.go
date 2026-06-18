package probes_test

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/krateoplatformops/plumbing/server/probes"
)

type okPinger struct{}

func (okPinger) Ping(context.Context) error { return nil }

func ExampleRegister() {
	mux := http.NewServeMux()
	probes.Register(mux, slog.Default(), okPinger{}, time.Second)

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	fmt.Println(rr.Code)
	// Output:
	// 200
}

func ExampleNew() {
	hs := probes.New(slog.Default(), okPinger{}, 0)
	hs.Start()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = hs.Shutdown(ctx)
}
