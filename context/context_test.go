package context

import (
	"context"
	"testing"

	"github.com/krateo-platformops/plumbing/endpoints"
)

func TestTraceId(t *testing.T) {
	tests := []struct {
		name     string
		ctx      context.Context
		generate bool
		expectID bool
	}{
		{
			name:     "existing traceId",
			ctx:      context.WithValue(context.Background(), contextKeyTraceId, "test-id"),
			generate: false,
			expectID: true,
		},
		{
			name:     "generate new traceId",
			ctx:      context.Background(),
			generate: true,
			expectID: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			traceId := TraceId(tc.ctx, tc.generate)
			if (traceId != "") != tc.expectID {
				t.Errorf("unexpected traceId: got %v, expectID %v", traceId, tc.expectID)
			}
		})
	}
}

func TestUserConfig(t *testing.T) {
	tests := []struct {
		name      string
		ctx       context.Context
		expectErr bool
	}{
		{
			name:      "valid user config",
			ctx:       context.WithValue(context.Background(), contextKeyUserConfig, endpoints.Endpoint{}),
			expectErr: false,
		},
		{
			name:      "missing user config",
			ctx:       context.Background(),
			expectErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := UserConfig(tc.ctx)
			if (err != nil) != tc.expectErr {
				t.Errorf("unexpected error status: got %v, expectErr %v", err, tc.expectErr)
			}
		})
	}
}

func TestWithTraceId(t *testing.T) {
	ctx := context.Background()
	traceId := "custom-trace-id"
	newCtx := WithTraceId(traceId)(ctx)
	got := TraceId(newCtx, false)
	if got != traceId {
		t.Errorf("expected %v, got %v", traceId, got)
	}
}
