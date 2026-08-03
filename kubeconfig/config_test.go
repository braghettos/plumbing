package kubeconfig

import (
	"context"
	"testing"

	"github.com/krateo-platformops/plumbing/endpoints"
)

func TestNewClientConfig(t *testing.T) {
	tests := []struct {
		name        string
		endpoint    endpoints.Endpoint
		expectError bool
	}{
		{
			name: "Valid endpoint",
			endpoint: endpoints.Endpoint{
				ServerURL: "https://example.com",
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := NewClientConfig(context.Background(), tt.endpoint)
			if (err != nil) != tt.expectError {
				t.Errorf("expected error: %v, got: %v", tt.expectError, err)
			}

			if err == nil && cfg == nil {
				t.Errorf("expected valid *rest.Config, got nil")
			}
		})
	}
}
