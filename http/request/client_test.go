package request

import (
	"testing"

	"github.com/krateo-platformops/plumbing/endpoints"
)

func TestHTTPClientForEndpoint(t *testing.T) {
	tests := []struct {
		name      string
		ep        endpoints.Endpoint
		expectErr bool
	}{
		{
			name:      "valid endpoint without auth",
			ep:        endpoints.Endpoint{},
			expectErr: false,
		},
		{
			name:      "valid endpoint with bearer token",
			ep:        endpoints.Endpoint{Token: "test-token"},
			expectErr: false,
		},
		{
			name:      "valid endpoint with basic auth",
			ep:        endpoints.Endpoint{Username: "user", Password: "pass"},
			expectErr: false,
		},
		{
			name:      "invalid endpoint with both auth methods",
			ep:        endpoints.Endpoint{Username: "user", Password: "pass", Token: "token"},
			expectErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client, err := HTTPClientForEndpoint(&tc.ep, nil)
			if (err != nil) != tc.expectErr {
				t.Errorf("unexpected error status: got %v, expectErr %v", err, tc.expectErr)
			}
			if !tc.expectErr && client == nil {
				t.Errorf("expected client, got nil")
			}
		})
	}
}
